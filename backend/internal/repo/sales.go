package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
)

// Satış/sevkiyat/iade + bloke lot mekanizması.
//
// CreateSalesOrder TEK servis fonksiyonudur: hem POST /sales-orders (REST, opts.Online=true) hem
// /sync/push (sales_order/create, opts.Online=false) tarafından çağrılır -- gerçek iş mantığı
// createSalesOrderInTx'tedir, ikisi de aynı kodu farklı bir transaction bağlamında çalıştırır
// (REST kendi transaction'ını açar; sync zaten açık olan transaction'ı -- processed_request
// rezervasyonuyla birlikte -- kullanır, bkz. repo/sync.go). Online modda bloke lot ve yetersiz stok
// sert hata üretir (422/409); offline modda satış YİNE KAYDEDİLİR, ihlal kodları (violations) döner
// ve sync katmanı bunu "flagged" sonucuna çevirir (bkz. docs/sync.md).

// SaleOptions satışın hangi kanaldan geldiğini belirler.
type SaleOptions struct {
	// Online: true ise REST'ten doğrudan gelen istek (bloke lot/yetersiz stok sert hata üretir).
	// false ise offline/sync (satış kaydedilir, ihlal kodu döner).
	Online bool
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

// ---------------------------------------------------------------------------
// Okuma
// ---------------------------------------------------------------------------

const salesOrderCols = `so.id, so.order_no, so.customer_id, c.code, c.name, so.device_id,
       so.warehouse_id, w.code, so.sold_at, so.status, so.total_amount,
       EXISTS (SELECT 1 FROM sales_line sl WHERE sl.sales_order_id = so.id AND sl.recall_violation = TRUE),
       so.created_at`

const salesOrderFrom = ` FROM sales_order so
JOIN customer c ON c.id = so.customer_id
JOIN warehouse w ON w.id = so.warehouse_id`

func scanSalesOrder(row interface{ Scan(...any) error }) (*domain.SalesOrder, error) {
	var o domain.SalesOrder
	err := row.Scan(&o.ID, &o.OrderNo, &o.CustomerID, &o.CustomerCode, &o.CustomerName, &o.DeviceID,
		&o.WarehouseID, &o.WarehouseCode, &o.SoldAt, &o.Status, &o.TotalAmount, &o.HasViolation, &o.CreatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return &o, nil
}

func getSalesOrderRow(ctx context.Context, q DBTX, id string) (*domain.SalesOrder, error) {
	return scanSalesOrder(q.QueryRowContext(ctx, `SELECT `+salesOrderCols+salesOrderFrom+` WHERE so.id = ?`, id))
}

func loadSalesLines(ctx context.Context, q DBTX, orderID string) ([]domain.SalesLine, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT sl.id, sl.sales_order_id, sl.item_id, it.code, it.name, sl.lot_id, l.lot_no,
		        sl.qty, sl.unit_price, sl.line_total, sl.stock_movement_id, sl.recall_violation, sl.created_at
		 FROM sales_line sl
		 JOIN item it ON it.id = sl.item_id
		 JOIN lot l ON l.id = sl.lot_id
		 WHERE sl.sales_order_id = ? ORDER BY sl.created_at`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.SalesLine{}
	for rows.Next() {
		var l domain.SalesLine
		if err := rows.Scan(&l.ID, &l.SalesOrderID, &l.ItemID, &l.ItemCode, &l.ItemName, &l.LotID, &l.LotNo,
			&l.Qty, &l.UnitPrice, &l.LineTotal, &l.StockMovementID, &l.RecallViolation, &l.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func loadShipmentLines(ctx context.Context, q DBTX, shipmentID string) ([]domain.ShipmentLine, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT shl.id, shl.lot_id, l.lot_no, l.item_id, shl.qty
		 FROM shipment_line shl JOIN lot l ON l.id = shl.lot_id
		 WHERE shl.shipment_id = ? ORDER BY shl.created_at`, shipmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ShipmentLine{}
	for rows.Next() {
		var l domain.ShipmentLine
		if err := rows.Scan(&l.ID, &l.LotID, &l.LotNo, &l.ItemID, &l.Qty); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// loadShipmentsForOrder: ONCE sevkiyat satirlari tam olarak okunup rows kapatilir, SONRA her sevkiyat
// icin ayri bir sorguyla satirlar yuklenir. Bir *sql.Tx TEK bir baglantiya bagli oldugu icin, bu rows
// acikken ayni tx uzerinde baska bir sorgu calistirmak ("invalid connection" hatasi) YASAKTIR --
// MySQL surucusu es zamanli birden fazla acik sonuc kumesini bir baglanti uzerinde desteklemez.
func loadShipmentsForOrder(ctx context.Context, q DBTX, orderID string) ([]domain.Shipment, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT sh.id, sh.shipment_no, sh.customer_id, c.name, sh.sales_order_id, sh.shipped_at, sh.vehicle, sh.created_at
		 FROM shipment sh JOIN customer c ON c.id = sh.customer_id
		 WHERE sh.sales_order_id = ? ORDER BY sh.created_at`, orderID)
	if err != nil {
		return nil, err
	}
	out := []domain.Shipment{}
	for rows.Next() {
		var sh domain.Shipment
		if err := rows.Scan(&sh.ID, &sh.ShipmentNo, &sh.CustomerID, &sh.CustomerName, &sh.SalesOrderID,
			&sh.ShippedAt, &sh.Vehicle, &sh.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, sh)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	for i := range out {
		lines, err := loadShipmentLines(ctx, q, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Lines = lines
	}
	return out, nil
}

func getSalesOrderDetail(ctx context.Context, q DBTX, id string) (*domain.SalesOrder, error) {
	o, err := getSalesOrderRow(ctx, q, id)
	if err != nil {
		return nil, err
	}
	if o.Lines, err = loadSalesLines(ctx, q, id); err != nil {
		return nil, err
	}
	if o.Shipments, err = loadShipmentsForOrder(ctx, q, id); err != nil {
		return nil, err
	}
	return o, nil
}

func (s *Store) GetSalesOrder(ctx context.Context, id string) (*domain.SalesOrder, error) {
	return getSalesOrderDetail(ctx, s.db, id)
}

type SalesOrderFilter struct {
	CustomerID string
	LotID      string
	From, To   string // "YYYY-MM-DD", sold_at uzerinde
	Page       Page
}

func (s *Store) ListSalesOrders(ctx context.Context, f SalesOrderFilter) ([]domain.SalesOrder, error) {
	var b query
	if f.CustomerID != "" {
		b.add(`so.customer_id = ?`, f.CustomerID)
	}
	if f.LotID != "" {
		b.add(`EXISTS (SELECT 1 FROM sales_line sl WHERE sl.sales_order_id = so.id AND sl.lot_id = ?)`, f.LotID)
	}
	if f.From != "" {
		b.add(`so.sold_at >= ?`, f.From)
	}
	if f.To != "" {
		b.add(`so.sold_at < DATE_ADD(?, INTERVAL 1 DAY)`, f.To)
	}
	limit, offset := f.Page.normalized()
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+salesOrderCols+salesOrderFrom+b.sql()+` ORDER BY so.sold_at DESC, so.order_no DESC LIMIT ? OFFSET ?`,
		append(b.args, limit, offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.SalesOrder{}
	for rows.Next() {
		o, err := scanSalesOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *o)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Yazma: satış
// ---------------------------------------------------------------------------

// lockedLotForSale lot satırını FOR UPDATE kilitler (es zamanlı satışlar sıraya girer) ve o anki
// (kilit sonrası, güncel) durumunu/birim maliyetini döner.
func lockedLotForSale(ctx context.Context, tx *sql.Tx, lotID string) (*domain.Lot, error) {
	var tmp string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM lot WHERE id = ? FOR UPDATE`, lotID).Scan(&tmp); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, mapErr(err)
	}
	return getLot(ctx, tx, lotID, false)
}

// CreateSalesOrder: REST girişi. Kendi transaction'ını açar ve createSalesOrderInTx'i çalıştırır.
// Online idempotency: aynı istemci id'siyle tekrar gelen istek mevcut siparişi döner (yeniden işlenmez).
func (s *Store) CreateSalesOrder(ctx context.Context, req domain.CreateSalesOrderRequest, opts SaleOptions) (*domain.SalesOrder, []string, error) {
	if opts.Online && req.ID != nil {
		if id, perr := domain.ParseID(*req.ID); perr == nil {
			existing, err := getSalesOrderDetail(ctx, s.db, id)
			if err == nil {
				return existing, nil, nil
			} else if !errors.Is(err, domain.ErrNotFound) {
				return nil, nil, err
			}
		}
	}
	var order *domain.SalesOrder
	var violations []string
	err := s.InTx(ctx, func(tx *sql.Tx) error {
		o, v, err := s.createSalesOrderInTx(ctx, tx, req, opts)
		order, violations = o, v
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return order, violations, nil
}

// createSalesOrderInTx asıl iş mantığıdır: her satır için lot satırı FOR UPDATE kilitlenir, sale_out
// hareketi + sales_line yazılır; create_shipment (varsayılan true) ise aynı satırlardan bir sevkiyat
// da oluşturulur. Dönen []string offline modda oluşan ihlal kodlarıdır (online modda her zaman boştur:
// ihlal sert hataya döner, fonksiyon hatayla geri döner). Verilen tx'i açmaz/kapatmaz (çağıran yönetir).
func (s *Store) createSalesOrderInTx(ctx context.Context, tx *sql.Tx, req domain.CreateSalesOrderRequest, opts SaleOptions) (*domain.SalesOrder, []string, error) {
	id, err := resolveIDPtr(req.ID)
	if err != nil {
		return nil, nil, err
	}

	var clientCreatedAt *time.Time
	if !opts.Online {
		clientCreatedAt = &req.SoldAt
	}

	var violations []string

	if ok, err := exists(ctx, tx, "customer", req.CustomerID); err != nil {
		return nil, nil, err
	} else if !ok {
		return nil, nil, domain.Invalid("customer_id bulunamadi")
	}
	if ok, err := exists(ctx, tx, "warehouse", req.WarehouseID); err != nil {
		return nil, nil, err
	} else if !ok {
		return nil, nil, domain.Invalid("warehouse_id bulunamadi")
	}
	if req.DeviceID != nil {
		if ok, err := exists(ctx, tx, "device", *req.DeviceID); err != nil {
			return nil, nil, err
		} else if !ok {
			return nil, nil, domain.Invalid("device_id bulunamadi")
		}
	}

	orderNo := ""
	if req.OrderNo != nil {
		orderNo = *req.OrderNo
	} else if orderNo, err = nextSequenceNo(ctx, tx, "SAT"); err != nil {
		return nil, nil, err
	}

	// sales_order satiri ONCE yazilir (total_amount=0 yer tutucu ile): sales_line.sales_order_id FK'si
	// bu satira isaret eder, bu yuzden satirlardan once var olmasi gerekir. Dongu sonunda gercek
	// total_amount ile UPDATE edilir.
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO sales_order (id, order_no, customer_id, device_id, warehouse_id, sold_at, status, total_amount)
		 VALUES (?, ?, ?, ?, ?, ?, 'confirmed', 0)`,
		id, orderNo, req.CustomerID, req.DeviceID, req.WarehouseID, req.SoldAt); err != nil {
		return nil, nil, mapErr(err)
	}

	totalAmount := domain.NewAmount(decimal.Zero)
	touchedLots := map[string]bool{}

	type preparedLine struct {
		lineID, lotID string
		qty           domain.Qty
	}
	prepared := make([]preparedLine, 0, len(req.Lines))

	for i, line := range req.Lines {
		if _, err := getItem(ctx, tx, line.ItemID); errors.Is(err, domain.ErrNotFound) {
			return nil, nil, domain.Invalid("lines[%d].item_id bulunamadi", i)
		} else if err != nil {
			return nil, nil, err
		}

		lot, err := lockedLotForSale(ctx, tx, line.LotID)
		if errors.Is(err, domain.ErrNotFound) {
			return nil, nil, domain.Invalid("lines[%d].lot_id bulunamadi", i)
		} else if err != nil {
			return nil, nil, err
		}
		if lot.ItemID != line.ItemID {
			return nil, nil, domain.Invalid("lines[%d]: lot %s bu urune ait degil", i, lot.LotNo)
		}

		recallViolation := false
		if lot.Status != "active" {
			if opts.Online {
				return nil, nil, domain.Invalid("lines[%d]: lot %s aktif degil (durum: %s, LOT_BLOCKED)", i, lot.LotNo, lot.Status)
			}
			violations = appendUnique(violations, domain.ViolationLotBlocked)
			recallViolation = true
		}

		var onHand domain.Qty
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(SUM(qty), 0) FROM stock_movement WHERE lot_id = ? AND warehouse_id = ?`,
			line.LotID, req.WarehouseID).Scan(&onHand); err != nil {
			return nil, nil, err
		}
		insufficient := onHand.LessThan(line.Qty.Decimal)
		if insufficient {
			if opts.Online {
				return nil, nil, domain.Conflict("lines[%d]: stok yetersiz (lot %s, eldeki %s, istenen %s, INSUFFICIENT_STOCK)",
					i, lot.LotNo, onHand.StringFixed(domain.QtyScale), line.Qty.StringFixed(domain.QtyScale))
			}
			violations = appendUnique(violations, domain.ViolationNegativeStock)
		}

		lineTotal := domain.NewAmount(domain.LineTotal(line.Qty.Decimal, line.UnitPrice.Decimal))
		totalAmount = domain.NewAmount(totalAmount.Add(lineTotal.Decimal))

		movement, err := insertMovement(ctx, tx, domain.StockMovement{
			LotID: line.LotID, WarehouseID: req.WarehouseID, MovementType: "sale_out",
			Qty: domain.NewQty(line.Qty.Decimal.Neg()), UnitCost: lot.UnitCost,
			RefType: "sales_order", RefID: id, DeviceID: req.DeviceID, ClientCreatedAt: clientCreatedAt,
		})
		if err != nil {
			return nil, nil, err
		}

		lineID, err := resolveIDPtr(line.ID)
		if err != nil {
			return nil, nil, err
		}
		prepared = append(prepared, preparedLine{lineID: lineID, lotID: line.LotID, qty: line.Qty})

		if _, err := tx.ExecContext(ctx,
			`INSERT INTO sales_line (id, sales_order_id, item_id, lot_id, qty, unit_price, line_total, stock_movement_id, recall_violation)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			lineID, id, line.ItemID, line.LotID, line.Qty, line.UnitPrice, lineTotal, movement.ID, recallViolation); err != nil {
			return nil, nil, mapErr(err)
		}

		if insufficient {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO anomaly_alert (id, alert_type, severity, ref_type, ref_id, message)
				 VALUES (?, 'NEGATIVE_STOCK_OFFLINE', 'critical', 'sales_line', ?, ?)`,
				domain.NewID(), lineID,
				fmt.Sprintf("Offline satis stogu negatife dusurdu: lot %s, depo %s, eldeki %s, satilan %s",
					lot.LotNo, req.WarehouseID, onHand.StringFixed(domain.QtyScale), line.Qty.StringFixed(domain.QtyScale))); err != nil {
				return nil, nil, mapErr(err)
			}
		}
		touchedLots[line.LotID] = true
	}

	if _, err := tx.ExecContext(ctx, `UPDATE sales_order SET total_amount = ? WHERE id = ?`, totalAmount, id); err != nil {
		return nil, nil, mapErr(err)
	}

	createShipment := req.CreateShipment == nil || *req.CreateShipment
	if createShipment {
		shipmentID := domain.NewID()
		shipmentNo, err := nextSequenceNo(ctx, tx, "SHP")
		if err != nil {
			return nil, nil, err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO shipment (id, shipment_no, customer_id, sales_order_id, shipped_at) VALUES (?, ?, ?, ?, ?)`,
			shipmentID, shipmentNo, req.CustomerID, id, req.SoldAt); err != nil {
			return nil, nil, mapErr(err)
		}
		for _, p := range prepared {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO shipment_line (id, shipment_id, lot_id, qty) VALUES (?, ?, ?, ?)`,
				domain.NewID(), shipmentID, p.lotID, p.qty); err != nil {
				return nil, nil, mapErr(err)
			}
		}
	}

	for lotID := range touchedLots {
		lotView, err := getLot(ctx, tx, lotID, true)
		if err != nil {
			return nil, nil, err
		}
		if err := appendChange(ctx, tx, "lot", lotID, "upsert", lotView); err != nil {
			return nil, nil, err
		}
	}

	out, err := getSalesOrderDetail(ctx, tx, id)
	if err != nil {
		return nil, nil, err
	}
	return out, violations, nil
}

// ---------------------------------------------------------------------------
// Yazma: müşteri iadesi
// ---------------------------------------------------------------------------

func getCustomerReturnDetail(ctx context.Context, q DBTX, id string) (*domain.CustomerReturn, error) {
	var r domain.CustomerReturn
	err := q.QueryRowContext(ctx,
		`SELECT cr.id, cr.return_no, cr.customer_id, c.name, cr.sales_line_id, cr.lot_id, l.lot_no,
		        cr.qty, cr.reason, cr.returned_at, cr.created_at
		 FROM customer_return cr
		 JOIN customer c ON c.id = cr.customer_id
		 JOIN lot l ON l.id = cr.lot_id
		 WHERE cr.id = ?`, id).
		Scan(&r.ID, &r.ReturnNo, &r.CustomerID, &r.CustomerName, &r.SalesLineID, &r.LotID, &r.LotNo,
			&r.Qty, &r.Reason, &r.ReturnedAt, &r.CreatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return &r, nil
}

// CreateCustomerReturn: REST girişi. Kendi transaction'ını açar ve createCustomerReturnInTx'i çalıştırır.
func (s *Store) CreateCustomerReturn(ctx context.Context, req domain.CreateCustomerReturnRequest) (*domain.CustomerReturn, error) {
	var out *domain.CustomerReturn
	err := s.InTx(ctx, func(tx *sql.Tx) error {
		o, err := s.createCustomerReturnInTx(ctx, tx, req)
		out = o
		return err
	})
	return out, err
}

// createCustomerReturnInTx asıl iş mantığıdır. Satılan miktarın üstünde iade 422 (sales_line_id
// verilmişse o satırın qty'sine, verilmemişse lotun toplam satışına göre). Dönüş deposu: sales_line_id
// verilmişse o siparişin deposu; yalnız lot_id verilmişse lotun en son sale_out hareketinin deposu
// (bkz. docs/assumptions.md). Verilen tx'i açmaz/kapatmaz (çağıran yönetir).
func (s *Store) createCustomerReturnInTx(ctx context.Context, tx *sql.Tx, req domain.CreateCustomerReturnRequest) (*domain.CustomerReturn, error) {
	id, err := resolveIDPtr(req.ID)
	if err != nil {
		return nil, err
	}
	returnedAt := time.Now().UTC()
	if req.ReturnedAt != nil {
		returnedAt = *req.ReturnedAt
	}

	if ok, err := exists(ctx, tx, "customer", req.CustomerID); err != nil {
		return nil, err
	} else if !ok {
		return nil, domain.Invalid("customer_id bulunamadi")
	}

	var lotID, warehouseID string
	var soldQty, alreadyReturned domain.Qty

	if req.SalesLineID != nil {
		var lineLotID string
		if err := tx.QueryRowContext(ctx,
			`SELECT sl.lot_id, sl.qty, so.warehouse_id
			 FROM sales_line sl JOIN sales_order so ON so.id = sl.sales_order_id
			 WHERE sl.id = ? FOR UPDATE`, *req.SalesLineID).
			Scan(&lineLotID, &soldQty, &warehouseID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, domain.Invalid("sales_line_id bulunamadi")
			}
			return nil, mapErr(err)
		}
		lotID = lineLotID
		if req.LotID != nil && *req.LotID != lotID {
			return nil, domain.Invalid("lot_id, sales_line_id ile eslesmiyor")
		}
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(SUM(qty), 0) FROM customer_return WHERE sales_line_id = ?`, *req.SalesLineID).
			Scan(&alreadyReturned); err != nil {
			return nil, err
		}
	} else {
		lotID = *req.LotID
		if _, err := getLot(ctx, tx, lotID, false); err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return nil, domain.Invalid("lot_id bulunamadi")
			}
			return nil, err
		}
		if err := tx.QueryRowContext(ctx,
			`SELECT warehouse_id FROM stock_movement WHERE lot_id = ? AND movement_type = 'sale_out'
			 ORDER BY created_at DESC LIMIT 1`, lotID).Scan(&warehouseID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, domain.Invalid("bu lot hic satilmamis, iade edilemez")
			}
			return nil, mapErr(err)
		}
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(SUM(-qty), 0) FROM stock_movement WHERE lot_id = ? AND movement_type = 'sale_out'`, lotID).
			Scan(&soldQty); err != nil {
			return nil, err
		}
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(SUM(qty), 0) FROM customer_return WHERE lot_id = ?`, lotID).
			Scan(&alreadyReturned); err != nil {
			return nil, err
		}
	}

	if alreadyReturned.Add(req.Qty.Decimal).GreaterThan(soldQty.Decimal) {
		return nil, domain.Invalid("iade miktari satilan miktardan fazla (satilan %s, iade edilmis %s, istenen %s)",
			soldQty.StringFixed(domain.QtyScale), alreadyReturned.StringFixed(domain.QtyScale), req.Qty.StringFixed(domain.QtyScale))
	}

	lot, err := lockedLotForSale(ctx, tx, lotID)
	if err != nil {
		return nil, err
	}

	returnNo := ""
	if req.ReturnNo != nil {
		returnNo = *req.ReturnNo
	} else if returnNo, err = nextSequenceNo(ctx, tx, "IAD"); err != nil {
		return nil, err
	}

	if _, err := insertMovement(ctx, tx, domain.StockMovement{
		LotID: lotID, WarehouseID: warehouseID, MovementType: "return_in",
		Qty: req.Qty, UnitCost: lot.UnitCost, RefType: "customer_return", RefID: id,
	}); err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO customer_return (id, return_no, customer_id, sales_line_id, lot_id, qty, reason, returned_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, returnNo, req.CustomerID, req.SalesLineID, lotID, req.Qty, req.Reason, returnedAt); err != nil {
		return nil, mapErr(err)
	}

	lotView, err := getLot(ctx, tx, lotID, true)
	if err != nil {
		return nil, err
	}
	if err := appendChange(ctx, tx, "lot", lotID, "upsert", lotView); err != nil {
		return nil, err
	}
	return getCustomerReturnDetail(ctx, tx, id)
}

// ---------------------------------------------------------------------------
// Bloke lot mekanizması
// ---------------------------------------------------------------------------

// SetLotBlocked lot.status'u (blocked|active) ve blocked_reason'ı günceller, change_log'a yazar.
// Geri çağırma servisinin kendisi (Görev 3) bu fonksiyonu yeniden kullanacaktır.
func (s *Store) SetLotBlocked(ctx context.Context, lotID string, blocked bool, reason *string) (*domain.Lot, error) {
	var out *domain.Lot
	err := s.InTx(ctx, func(tx *sql.Tx) error {
		var tmp string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM lot WHERE id = ? FOR UPDATE`, lotID).Scan(&tmp); err != nil {
			return mapErr(err)
		}
		status := "active"
		var blockedReason *string
		if blocked {
			status = "blocked"
			blockedReason = reason
		}
		if _, err := tx.ExecContext(ctx, `UPDATE lot SET status = ?, blocked_reason = ? WHERE id = ?`,
			status, blockedReason, lotID); err != nil {
			return mapErr(err)
		}
		var err error
		out, err = getLot(ctx, tx, lotID, true)
		if err != nil {
			return err
		}
		return appendChange(ctx, tx, "lot", lotID, "upsert", out)
	})
	return out, err
}
