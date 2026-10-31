package repo

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
)

// Package repo (lot_ops.go): Gorev 3'un iki eksik maddesi (bkz. docs/traceability.md):
//   - "yeniden isleme ve fire hareketleri lot bazinda kaydedilir": RecordWaste (waste_out) ve
//     CreateReprocessOrder (reprocess_consume + reprocess_in + lot_link 'reprocess', migration 000012)
//   - "ikinci kademe dagitim yalnizca kaydedilmisse kesin olarak gosterilir": downstream_distribution
//     kaydi; izleme ve geri cagirma yalnizca bu kayitlari kesin olarak gosterir (uydurma rakam yok).

// ownWarehouseType depoyu dogrular ve tipini doner. Dis uretici deposundaki stok uretim emrine aittir:
// yalnizca sevk/teslim/iade ile hareket eder. Transfer/fire/yeniden isleme ona dokunursa teslimdeki
// production_consume dis depoyu eksiye dusurur (ayni malzeme hem tuketilmis hem bizde gorunur).
func ownWarehouseType(ctx context.Context, tx *sql.Tx, field, id string) (string, error) {
	var typ string
	err := tx.QueryRowContext(ctx, `SELECT warehouse_type FROM warehouse WHERE id = ?`, id).Scan(&typ)
	if errors.Is(err, sql.ErrNoRows) {
		return "", domain.Invalid("%s bulunamadi", field)
	} else if err != nil {
		return "", err
	}
	if typ == "external_producer" {
		return "", domain.Invalid("%s: dis uretici deposundaki stok yalnizca uretim emriyle (sevk/teslim/iade) hareket eder", field)
	}
	return typ, nil
}

// ---------------------------------------------------------------------------
// fire
// ---------------------------------------------------------------------------

// RecordWaste lotun bir depodaki miktarini fire olarak dusurur: tek bir waste_out (-) hareketi,
// lotun birim maliyetiyle (ref_type='lot_waste', ref_id=lot, note=sebep). Lot FOR UPDATE kilitlenir,
// depo bakiyesi yetmezse 409 (negatif stok yok). Geri cagrilmis/bloke lotun imhasi da bu yoldur.
// Ayni istemci id'siyle tekrar gelen istek mevcut hareketi doner.
func (s *Store) RecordWaste(ctx context.Context, lotID string, req domain.WasteRequest) (*domain.StockMovement, error) {
	if req.ID != nil {
		existing, err := getMovement(ctx, s.db, *req.ID)
		if err == nil {
			if existing.MovementType != "waste_out" || existing.LotID != lotID {
				return nil, domain.Conflict("hareket %s zaten baska bir islem icin kullanilmis", *req.ID)
			}
			return existing, nil
		} else if !errors.Is(err, domain.ErrNotFound) {
			return nil, err
		}
	}
	var out *domain.StockMovement
	err := s.InTx(ctx, func(tx *sql.Tx) error {
		lot, err := lockedLotForSale(ctx, tx, lotID)
		if err != nil {
			return err
		}
		if _, err := ownWarehouseType(ctx, tx, "warehouse_id", req.WarehouseID); err != nil {
			return err
		}
		var onHand domain.Qty
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(SUM(qty), 0) FROM stock_movement WHERE lot_id = ? AND warehouse_id = ?`,
			lotID, req.WarehouseID).Scan(&onHand); err != nil {
			return err
		}
		if onHand.LessThan(req.Qty.Decimal) {
			return domain.Conflict("depoda stok yetersiz (lot %s, eldeki %s, fire %s, INSUFFICIENT_STOCK)",
				lot.LotNo, onHand.StringFixed(domain.QtyScale), req.Qty.StringFixed(domain.QtyScale))
		}
		m := domain.StockMovement{
			LotID: lotID, WarehouseID: req.WarehouseID, MovementType: "waste_out",
			Qty: domain.NewQty(req.Qty.Decimal.Neg()), UnitCost: lot.UnitCost,
			RefType: "lot_waste", RefID: lotID, Note: &req.Reason,
		}
		if req.ID != nil {
			m.ID = *req.ID
		}
		if out, err = insertMovement(ctx, tx, m); err != nil {
			return err
		}
		return appendLotBalance(ctx, tx, lotID)
	})
	return out, err
}

// ---------------------------------------------------------------------------
// yeniden isleme
// ---------------------------------------------------------------------------

const reprocessCols = `r.id, r.order_no, r.warehouse_id, w.code, r.output_lot_id, ol.lot_no, oi.name, ol.initial_qty,
       ol.uom, ol.unit_cost, r.total_cost, r.note, r.created_at`

const reprocessFrom = ` FROM reprocess_order r
JOIN warehouse w ON w.id = r.warehouse_id
LEFT JOIN lot ol ON ol.id = r.output_lot_id
LEFT JOIN item oi ON oi.id = ol.item_id`

func scanReprocess(row interface{ Scan(...any) error }) (*domain.ReprocessOrder, error) {
	var o domain.ReprocessOrder
	if err := row.Scan(&o.ID, &o.OrderNo, &o.WarehouseID, &o.WarehouseCode, &o.OutputLotID, &o.OutputLotNo,
		&o.OutputItem, &o.OutputQty, &o.OutputUOM, &o.OutputUnit, &o.TotalCost, &o.Note, &o.CreatedAt); err != nil {
		return nil, mapErr(err)
	}
	return &o, nil
}

func loadReprocessInputs(ctx context.Context, q DBTX, orderID string) ([]domain.ReprocessInputLine, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT ll.parent_lot_id, l.lot_no, i.name, ll.qty_from_parent, ll.uom, l.unit_cost
		 FROM lot_link ll JOIN lot l ON l.id = ll.parent_lot_id JOIN item i ON i.id = l.item_id
		 WHERE ll.process_type = 'reprocess' AND ll.process_id = ? ORDER BY l.lot_no`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ReprocessInputLine{}
	for rows.Next() {
		var in domain.ReprocessInputLine
		if err := rows.Scan(&in.LotID, &in.LotNo, &in.ItemName, &in.Qty, &in.UOM, &in.UnitCost); err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

func getReprocessOrder(ctx context.Context, q DBTX, id string) (*domain.ReprocessOrder, error) {
	o, err := scanReprocess(q.QueryRowContext(ctx, `SELECT `+reprocessCols+reprocessFrom+` WHERE r.id = ?`, id))
	if err != nil {
		return nil, err
	}
	if o.Inputs, err = loadReprocessInputs(ctx, q, id); err != nil {
		return nil, err
	}
	return o, nil
}

func (s *Store) GetReprocessOrder(ctx context.Context, id string) (*domain.ReprocessOrder, error) {
	return getReprocessOrder(ctx, s.db, id)
}

func (s *Store) ListReprocessOrders(ctx context.Context, p Page) ([]domain.ReprocessOrder, error) {
	limit, offset := p.normalized()
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+reprocessCols+reprocessFrom+` ORDER BY r.created_at DESC, r.order_no DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	var out []domain.ReprocessOrder
	for rows.Next() {
		o, err := scanReprocess(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, *o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out { // girdiler ayri sorgu: liste rows'u kapandiktan SONRA
		if out[i].Inputs, err = loadReprocessInputs(ctx, s.db, out[i].ID); err != nil {
			return nil, err
		}
	}
	if out == nil {
		out = []domain.ReprocessOrder{}
	}
	return out, nil
}

// CreateReprocessOrder girdi lotlarini depodan tuketip (reprocess_consume) yeni bir cikti lotu uretir
// (reprocess_in), tek transaction'da. Maliyet aktarilir: her girdi satiri round2(miktar x birim maliyet),
// toplam = satirlar toplami, cikti birim maliyeti = round4(toplam / cikti miktari) (dis uretimdeki EKP
// kuraliyla ayni, bkz. docs/assumptions.md). Her girdi icin lot_link (process_type='reprocess') yazilir.
// Girdi lotu aktif olmali (bloke/geri cagrilmis lot yeniden islenemez, 422) ve depoda yeterli olmali (409).
func (s *Store) CreateReprocessOrder(ctx context.Context, req domain.CreateReprocessOrderRequest) (*domain.ReprocessOrder, error) {
	if req.ID != nil {
		existing, err := s.GetReprocessOrder(ctx, *req.ID)
		if err == nil {
			return existing, nil
		} else if !errors.Is(err, domain.ErrNotFound) {
			return nil, err
		}
	}
	id, err := resolveIDPtr(req.ID)
	if err != nil {
		return nil, err
	}
	err = s.InTx(ctx, func(tx *sql.Tx) error {
		if _, err := ownWarehouseType(ctx, tx, "warehouse_id", req.WarehouseID); err != nil {
			return err
		}
		item, err := getItem(ctx, tx, req.Output.ItemID)
		if errors.Is(err, domain.ErrNotFound) {
			return domain.Invalid("output.item_id bulunamadi")
		} else if err != nil {
			return err
		}
		if !item.IsActive {
			return domain.Invalid("urun pasif: %s", item.Code)
		}
		orderNo, err := nextSequenceNo(ctx, tx, "YIS")
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO reprocess_order (id, order_no, warehouse_id, note) VALUES (?, ?, ?, ?)`,
			id, orderNo, req.WarehouseID, req.Note); err != nil {
			return mapErr(err)
		}

		total := decimal.Zero
		inputLots := make([]*domain.Lot, len(req.Inputs))
		for i, in := range req.Inputs {
			lot, err := lockedLotForSale(ctx, tx, in.LotID)
			if errors.Is(err, domain.ErrNotFound) {
				return domain.Invalid("inputs[%d].lot_id bulunamadi", i)
			} else if err != nil {
				return err
			}
			if lot.Status != "active" {
				return domain.Invalid("inputs[%d]: lot %s aktif degil (durum: %s), yeniden islenemez", i, lot.LotNo, lot.Status)
			}
			var onHand domain.Qty
			if err := tx.QueryRowContext(ctx,
				`SELECT COALESCE(SUM(qty), 0) FROM stock_movement WHERE lot_id = ? AND warehouse_id = ?`,
				in.LotID, req.WarehouseID).Scan(&onHand); err != nil {
				return err
			}
			if onHand.LessThan(in.Qty.Decimal) {
				return domain.Conflict("inputs[%d]: depoda stok yetersiz (lot %s, eldeki %s, istenen %s, INSUFFICIENT_STOCK)",
					i, lot.LotNo, onHand.StringFixed(domain.QtyScale), in.Qty.StringFixed(domain.QtyScale))
			}
			if _, err := insertMovement(ctx, tx, domain.StockMovement{
				LotID: in.LotID, WarehouseID: req.WarehouseID, MovementType: "reprocess_consume",
				Qty: domain.NewQty(in.Qty.Decimal.Neg()), UnitCost: lot.UnitCost, RefType: "reprocess_order", RefID: id,
			}); err != nil {
				return err
			}
			total = total.Add(domain.LineTotal(in.Qty.Decimal, lot.UnitCost.Decimal))
			inputLots[i] = lot
		}

		outputLotID := domain.NewID()
		lotNo := ""
		if req.Output.LotNo != nil {
			lotNo = *req.Output.LotNo
		} else if lotNo, err = nextSequenceNo(ctx, tx, "YIL"); err != nil {
			return err
		}
		unitCost := domain.NewAmount(domain.Round4(total.Div(req.Output.Qty.Decimal)))
		// Yeniden isleme raf omrunu uzatamaz: cikti lotunun SKT'si en erken SKT'li girdiyi gecemez.
		expiry := domain.ExpiryFrom(req.Output.ExpiryDate, domain.Today(), item.ShelfLifeDays)
		for _, l := range inputLots {
			if l.ExpiryDate == nil || (expiry != nil && !l.ExpiryDate.Before(expiry.Time)) {
				continue
			}
			if req.Output.ExpiryDate != nil {
				return domain.Invalid("output.expiry_date, girdi lotu %s'in SKT'sinden (%s) sonra olamaz",
					l.LotNo, l.ExpiryDate.Format(time.DateOnly))
			}
			expiry = l.ExpiryDate
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO lot (id, lot_no, item_id, supplier_id, origin_type, origin_id, received_or_produced_at,
			                  expiry_date, initial_qty, uom, unit_cost, status)
			 VALUES (?, ?, ?, NULL, 'reprocess', ?, ?, ?, ?, ?, ?, 'active')`,
			outputLotID, lotNo, item.ID, id, domain.Today(), expiry,
			req.Output.Qty, item.BaseUOM, unitCost); err != nil {
			return mapErr(err)
		}
		if _, err := insertMovement(ctx, tx, domain.StockMovement{
			LotID: outputLotID, WarehouseID: req.WarehouseID, MovementType: "reprocess_in",
			Qty: req.Output.Qty, UnitCost: unitCost, RefType: "reprocess_order", RefID: id,
		}); err != nil {
			return err
		}
		for i, in := range req.Inputs {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO lot_link (id, parent_lot_id, child_lot_id, qty_from_parent, uom, process_type, process_id)
				 VALUES (?, ?, ?, ?, ?, 'reprocess', ?)`,
				domain.NewID(), in.LotID, outputLotID, in.Qty, inputLots[i].UOM, id); err != nil {
				return mapErr(err)
			}
			if err := appendLotBalance(ctx, tx, in.LotID); err != nil {
				return err
			}
		}
		if err := appendLotBalance(ctx, tx, outputLotID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE reprocess_order SET output_lot_id = ?, total_cost = ? WHERE id = ?`,
			outputLotID, domain.NewAmount(total), id)
		return mapErr(err)
	})
	if err != nil {
		return nil, err
	}
	return s.GetReprocessOrder(ctx, id)
}

// ---------------------------------------------------------------------------
// ikinci kademe dagitim
// ---------------------------------------------------------------------------

const downstreamCols = `d.id, d.customer_id, c.name, d.lot_id, l.lot_no, d.recipient_name, d.recipient_phone, d.qty,
       d.distributed_at, d.note, d.created_at`

const downstreamFrom = ` FROM downstream_distribution d
JOIN customer c ON c.id = d.customer_id
JOIN lot l ON l.id = d.lot_id`

func scanDownstream(row interface{ Scan(...any) error }) (*domain.DownstreamDistribution, error) {
	var d domain.DownstreamDistribution
	if err := row.Scan(&d.ID, &d.CustomerID, &d.CustomerName, &d.LotID, &d.LotNo, &d.RecipientName, &d.RecipientPhone,
		&d.Qty, &d.DistributedAt, &d.Note, &d.CreatedAt); err != nil {
		return nil, mapErr(err)
	}
	return &d, nil
}

// loadDownstream bir musterinin verilen lotlardan yaptigi kayitli ikinci kademe dagitimlari doner.
func loadDownstream(ctx context.Context, q DBTX, customerID string, lotIDs []string) ([]domain.DownstreamDistribution, error) {
	out := []domain.DownstreamDistribution{}
	if len(lotIDs) == 0 {
		return out, nil
	}
	args := []any{customerID}
	for _, id := range lotIDs {
		args = append(args, id)
	}
	rows, err := q.QueryContext(ctx,
		`SELECT `+downstreamCols+downstreamFrom+` WHERE d.customer_id = ? AND d.lot_id IN (?`+strings.Repeat(",?", len(lotIDs)-1)+`)
		 ORDER BY d.distributed_at, d.recipient_name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		d, err := scanDownstream(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// downstreamStatus kayitli dagitimlarin durumunu belirler: kayit yoksa bilinmiyor (sabit not); kayit
// varsa gosterilir ve kesindir; kayitlar musteriye giden net miktari karsilamiyor ya da asiyorsa not.
func downstreamStatus(entries []domain.DownstreamDistribution, netShipped decimal.Decimal) (bool, *string) {
	if len(entries) == 0 {
		note := domain.DownstreamNoteText
		return false, &note
	}
	recorded := decimal.Zero
	for _, e := range entries {
		recorded = recorded.Add(e.Qty.Decimal)
	}
	var note string
	switch {
	case recorded.LessThan(netShipped):
		note = domain.DownstreamPartialNoteText
	case recorded.GreaterThan(netShipped): // dagitim kaydindan sonra iade gelmis
		note = domain.DownstreamExceedsNoteText
	default:
		return true, nil
	}
	return true, &note
}

func (s *Store) ListDownstream(ctx context.Context, customerID, lotID string) ([]domain.DownstreamDistribution, error) {
	var b query
	if customerID != "" {
		b.add(`d.customer_id = ?`, customerID)
	}
	if lotID != "" {
		b.add(`d.lot_id = ?`, lotID)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+downstreamCols+downstreamFrom+b.sql()+` ORDER BY d.distributed_at DESC, c.name`, b.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.DownstreamDistribution{}
	for rows.Next() {
		d, err := scanDownstream(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// CreateDownstream bir musterinin aldigi lottan kendi alicisina dagittigi miktari kaydeder. Toplam
// kayitli dagitim, musteriye o lottan sevk edilen net miktari (sevk - iade) asamaz (422): olmayan
// bir dagitim kaydedilemez. Musteri satiri FOR UPDATE kilitlenir (es zamanli kayitlar siraya girer).
// Ayni istemci id'siyle tekrar gelen istek mevcut kaydi doner.
func (s *Store) CreateDownstream(ctx context.Context, req domain.CreateDownstreamRequest) (*domain.DownstreamDistribution, error) {
	getOne := func(q DBTX, id string) (*domain.DownstreamDistribution, error) {
		return scanDownstream(q.QueryRowContext(ctx, `SELECT `+downstreamCols+downstreamFrom+` WHERE d.id = ?`, id))
	}
	if req.ID != nil {
		existing, err := getOne(s.db, *req.ID)
		if err == nil {
			return existing, nil
		} else if !errors.Is(err, domain.ErrNotFound) {
			return nil, err
		}
	}
	id, err := resolveIDPtr(req.ID)
	if err != nil {
		return nil, err
	}
	err = s.InTx(ctx, func(tx *sql.Tx) error {
		var tmp string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM customer WHERE id = ? FOR UPDATE`, req.CustomerID).Scan(&tmp); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return domain.Invalid("customer_id bulunamadi")
			}
			return err
		}
		if ok, err := exists(ctx, tx, "lot", req.LotID); err != nil {
			return err
		} else if !ok {
			return domain.Invalid("lot_id bulunamadi")
		}
		// Sevk/iade, geri cagirmayla AYNI kaynaktan (satis satirlari + siparissiz sevkiyatlar): sevkiyatsiz
		// satisin alicisi da dagitim kaydedebilir, iki ekran ayni rakami gorur.
		aggs, err := aggregateCustomersForLot(ctx, tx, req.LotID)
		if err != nil {
			return err
		}
		var shipped, returned, recorded domain.Qty
		for _, a := range aggs {
			if strings.EqualFold(a.customerID, req.CustomerID) {
				shipped, returned = domain.NewQty(a.qtyShipped), domain.NewQty(a.qtyReturned)
			}
		}
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(SUM(qty), 0) FROM downstream_distribution WHERE customer_id = ? AND lot_id = ?`,
			req.CustomerID, req.LotID).Scan(&recorded); err != nil {
			return err
		}
		available := shipped.Decimal.Sub(returned.Decimal).Sub(recorded.Decimal)
		if req.Qty.GreaterThan(available) {
			return domain.Invalid("dagitim miktari musteriye sevk edilen net miktari asiyor (sevk %s, iade %s, kayitli dagitim %s, istenen %s)",
				shipped.StringFixed(domain.QtyScale), returned.StringFixed(domain.QtyScale),
				recorded.StringFixed(domain.QtyScale), req.Qty.StringFixed(domain.QtyScale))
		}
		_, err = tx.ExecContext(ctx,
			`INSERT INTO downstream_distribution (id, customer_id, lot_id, recipient_name, recipient_phone, qty, distributed_at, note)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			id, req.CustomerID, req.LotID, req.RecipientName, req.RecipientPhone, req.Qty, req.DistributedAt, req.Note)
		return mapErr(err)
	})
	if err != nil {
		return nil, err
	}
	return getOne(s.db, id)
}
