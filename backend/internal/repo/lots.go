package repo

import (
	"context"
	"database/sql"
	"errors"

	"github.com/shopspring/decimal"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
)

const lotSelect = `
SELECT l.id, l.lot_no, l.item_id, i.code, i.name, l.supplier_id, s.name,
       l.origin_type, l.origin_id, l.received_or_produced_at, l.expiry_date,
       l.initial_qty, l.uom, l.unit_cost, l.status, l.blocked_reason, l.created_at,
       r.id, r.doc_no, r.invoice_no, prl.unit_price, prl.line_total, prl.declared_line_total, prl.has_mismatch,
       (SELECT COALESCE(SUM(sm.qty), 0) FROM stock_movement sm WHERE sm.lot_id = l.id)
FROM lot l
JOIN item i ON i.id = l.item_id
LEFT JOIN supplier s ON s.id = l.supplier_id
LEFT JOIN purchase_receipt_line prl ON prl.lot_id = l.id
LEFT JOIN purchase_receipt r ON r.id = prl.receipt_id`

func scanLot(row interface{ Scan(...any) error }, withQty bool) (*domain.Lot, error) {
	var (
		l         domain.Lot
		receiptID sql.NullString
		docNo     sql.NullString
		invoiceNo sql.NullString
		unitPrice *domain.Amount
		lineTotal *domain.Amount
		declared  *domain.Amount
		mismatch  sql.NullBool
		onHand    domain.Qty
	)
	err := row.Scan(&l.ID, &l.LotNo, &l.ItemID, &l.ItemCode, &l.ItemName, &l.SupplierID, &l.SupplierName,
		&l.OriginType, &l.OriginID, &l.ReceivedOrProducedAt, &l.ExpiryDate,
		&l.InitialQty, &l.UOM, &l.UnitCost, &l.Status, &l.BlockedReason, &l.CreatedAt,
		&receiptID, &docNo, &invoiceNo, &unitPrice, &lineTotal, &declared, &mismatch, &onHand)
	if err != nil {
		return nil, mapErr(err)
	}
	if receiptID.Valid {
		p := &domain.LotPurchase{
			ReceiptID:         receiptID.String,
			DocNo:             docNo.String,
			UnitPrice:         *unitPrice,
			LineTotal:         *lineTotal,
			DeclaredLineTotal: declared,
			HasMismatch:       mismatch.Bool,
		}
		if invoiceNo.Valid {
			p.InvoiceNo = &invoiceNo.String
		}
		l.Purchase = p
	}
	if withQty {
		l.QtyOnHand = &onHand
	}
	return &l, nil
}

// getLot: withDetail=true ise toplam bakiye ve depo kirilimi de dolar. change_log payload'i detaysizdir
// (bakiye hareketle degisir, lot varligiyla senkronlanmaz).
func getLot(ctx context.Context, q DBTX, id string, withDetail bool) (*domain.Lot, error) {
	l, err := scanLot(q.QueryRowContext(ctx, lotSelect+` WHERE l.id = ?`, id), withDetail)
	if err != nil || !withDetail {
		return l, err
	}
	rows, err := q.QueryContext(ctx,
		`SELECT b.warehouse_id, w.code, b.qty_on_hand
		 FROM v_lot_balance b JOIN warehouse w ON w.id = b.warehouse_id
		 WHERE b.lot_id = ? ORDER BY w.code`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	l.Balances = []domain.LotBalance{}
	for rows.Next() {
		var b domain.LotBalance
		if err := rows.Scan(&b.WarehouseID, &b.WarehouseCode, &b.QtyOnHand); err != nil {
			return nil, err
		}
		l.Balances = append(l.Balances, b)
	}
	return l, rows.Err()
}

func (s *Store) GetLot(ctx context.Context, id string) (*domain.Lot, error) {
	return getLot(ctx, s.db, id, true)
}

// LotByNo lot numarasindan lot arar (seed idempotency icin); yoksa ErrNotFound.
func (s *Store) LotByNo(ctx context.Context, lotNo string) (*domain.Lot, error) {
	return scanLot(s.db.QueryRowContext(ctx, lotSelect+` WHERE l.lot_no = ?`, lotNo), true)
}

type LotFilter struct {
	ItemID      string
	Status      string
	Q           string
	HasMismatch *bool
	Page        Page
}

func (s *Store) ListLots(ctx context.Context, f LotFilter) ([]domain.Lot, error) {
	var b query
	if f.ItemID != "" {
		b.add(`l.item_id = ?`, f.ItemID)
	}
	if f.Status != "" {
		b.add(`l.status = ?`, f.Status)
	}
	if f.Q != "" {
		b.add(`(l.lot_no LIKE ? OR i.code LIKE ? OR i.name LIKE ?)`, like(f.Q), like(f.Q), like(f.Q))
	}
	if f.HasMismatch != nil {
		b.add(`COALESCE(prl.has_mismatch, FALSE) = ?`, *f.HasMismatch)
	}
	limit, offset := f.Page.normalized()
	rows, err := s.db.QueryContext(ctx,
		lotSelect+b.sql()+` ORDER BY l.received_or_produced_at DESC, l.lot_no LIMIT ? OFFSET ?`,
		append(b.args, limit, offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Lot{}
	for rows.Next() {
		l, err := scanLot(rows, true)
		if err != nil {
			return nil, err
		}
		out = append(out, *l)
	}
	return out, rows.Err()
}

func exists(ctx context.Context, q DBTX, table, id string) (bool, error) {
	var one int
	err := q.QueryRowContext(ctx, `SELECT 1 FROM `+table+` WHERE id = ?`, id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// CreatePurchaseLot bir alis lotunu TEK transaction'da olusturur:
// purchase_receipt (varsa yeniden kullanilir) + lot + purchase_receipt_line + purchase_in stok hareketi + change_log.
//
// line_total = round2(qty x unit_price) (hesaplanan). declared_line_total verilmis ve farkliysa
// has_mismatch=true (ornek KZ-2026-001: 25 x 11.50 = 287.50, belgede 351.00).
func (s *Store) CreatePurchaseLot(ctx context.Context, in domain.PurchaseLotRequest) (*domain.Lot, error) {
	lotID, err := resolveIDPtr(in.ID)
	if err != nil {
		return nil, err
	}
	var out *domain.Lot
	err = s.InTx(ctx, func(tx *sql.Tx) error {
		item, err := getItem(ctx, tx, in.ItemID)
		if errors.Is(err, domain.ErrNotFound) {
			return domain.Invalid("item_id bulunamadi")
		} else if err != nil {
			return err
		}
		if !item.IsActive {
			return domain.Invalid("urun pasif: %s", item.Code)
		}
		for table, id := range map[string]string{"supplier": in.SupplierID, "warehouse": in.WarehouseID} {
			ok, err := exists(ctx, tx, table, id)
			if err != nil {
				return err
			}
			if !ok {
				return domain.Invalid("%s_id bulunamadi", table)
			}
		}

		// Belge: ayni belge no + ayni tedarikci => mevcut belgeye satir eklenir.
		docNo := "PR-" + in.LotNo
		if in.DocNo != nil {
			docNo = *in.DocNo
		}
		var receiptID, receiptSupplier string
		err = tx.QueryRowContext(ctx, `SELECT id, supplier_id FROM purchase_receipt WHERE doc_no = ? FOR UPDATE`, docNo).
			Scan(&receiptID, &receiptSupplier)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			receiptID = domain.NewID()
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO purchase_receipt (id, doc_no, supplier_id, invoice_no, received_at) VALUES (?, ?, ?, ?, ?)`,
				receiptID, docNo, in.SupplierID, in.InvoiceNo, in.ReceivedAt); err != nil {
				return mapErr(err)
			}
		case err != nil:
			return err
		case receiptSupplier != in.SupplierID:
			return domain.Conflict("belge %s baska bir tedarikciye ait", docNo)
		}

		lineTotal := domain.LineTotal(in.Qty.Decimal, in.UnitPrice.Decimal)
		var declared *decimal.Decimal
		var declaredAmt *domain.Amount
		if in.DeclaredLineTotal != nil {
			declared = &in.DeclaredLineTotal.Decimal
			declaredAmt = in.DeclaredLineTotal
		}
		mismatch := domain.HasMismatch(lineTotal, declared)

		if _, err := tx.ExecContext(ctx,
			`INSERT INTO lot (id, lot_no, item_id, supplier_id, origin_type, origin_id, received_or_produced_at,
			                  expiry_date, initial_qty, uom, unit_cost, status)
			 VALUES (?, ?, ?, ?, 'purchase', ?, ?, ?, ?, ?, ?, 'active')`,
			lotID, in.LotNo, in.ItemID, in.SupplierID, receiptID, in.ReceivedAt, in.ExpiryDate,
			in.Qty, item.BaseUOM, in.UnitPrice); err != nil {
			return mapErr(err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO purchase_receipt_line (id, receipt_id, item_id, lot_id, qty, unit_price, line_total,
			                                    declared_line_total, has_mismatch)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			domain.NewID(), receiptID, in.ItemID, lotID, in.Qty, in.UnitPrice,
			domain.NewAmount(lineTotal), declaredAmt, mismatch); err != nil {
			return mapErr(err)
		}
		if _, err := insertMovement(ctx, tx, domain.StockMovement{
			LotID:        lotID,
			WarehouseID:  in.WarehouseID,
			MovementType: "purchase_in",
			Qty:          in.Qty,
			UnitCost:     in.UnitPrice,
			RefType:      "purchase_receipt",
			RefID:        receiptID,
		}); err != nil {
			return err
		}
		// hareketten SONRA ve bakiyeli: cihaz yeni lotun stogunu pull'dan ogrenir (docs/sync.md)
		if out, err = getLot(ctx, tx, lotID, true); err != nil {
			return err
		}
		return appendChange(ctx, tx, "lot", lotID, "upsert", out)
	})
	if err != nil {
		return nil, err
	}
	return s.GetLot(ctx, out.ID)
}
