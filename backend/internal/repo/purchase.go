package repo

import (
	"context"
	"database/sql"

	"github.com/shopspring/decimal"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
)

// Package repo (purchase.go): GET/POST /purchase-receipts. Yazma yolu lots.go'daki
// createPurchaseLotInTx'i (CreatePurchaseLot ile AYNI kod) her satir icin bir kez cagirir -- tek
// transaction'da, belge ortak alanlari (tedarikci/fatura/tarih/belge no) bir kez, satirlar N kez.

const purchaseReceiptListSelect = `
SELECT r.id, r.doc_no, r.supplier_id, s.name, r.invoice_no, r.received_at, r.created_at,
       COUNT(prl.id), COALESCE(SUM(prl.line_total), 0), COALESCE(MAX(prl.has_mismatch), FALSE)
FROM purchase_receipt r
JOIN supplier s ON s.id = r.supplier_id
LEFT JOIN purchase_receipt_line prl ON prl.receipt_id = r.id`

const purchaseReceiptGroupBy = ` GROUP BY r.id, r.doc_no, r.supplier_id, s.name, r.invoice_no, r.received_at, r.created_at`

func scanPurchaseReceiptListItem(row interface{ Scan(...any) error }) (*domain.PurchaseReceiptListItem, error) {
	var it domain.PurchaseReceiptListItem
	var invoiceNo sql.NullString
	var total domain.Amount
	if err := row.Scan(&it.ID, &it.DocNo, &it.SupplierID, &it.SupplierName, &invoiceNo, &it.ReceivedAt,
		&it.CreatedAt, &it.LineCount, &total, &it.HasMismatch); err != nil {
		return nil, mapErr(err)
	}
	if invoiceNo.Valid {
		it.InvoiceNo = &invoiceNo.String
	}
	it.TotalAmount = total
	return &it, nil
}

type PurchaseReceiptFilter struct {
	SupplierID  string
	ItemID      string
	From, To    string // received_at tarih araligi (YYYY-MM-DD)
	HasMismatch *bool
	Page        Page
}

func (s *Store) ListPurchaseReceipts(ctx context.Context, f PurchaseReceiptFilter) ([]domain.PurchaseReceiptListItem, error) {
	var b query
	if f.SupplierID != "" {
		b.add(`r.supplier_id = ?`, f.SupplierID)
	}
	if f.ItemID != "" {
		b.add(`EXISTS (SELECT 1 FROM purchase_receipt_line x WHERE x.receipt_id = r.id AND x.item_id = ?)`, f.ItemID)
	}
	if f.From != "" {
		b.add(`r.received_at >= ?`, f.From)
	}
	if f.To != "" {
		b.add(`r.received_at <= ?`, f.To)
	}
	having := ""
	args := b.args
	if f.HasMismatch != nil {
		having = ` HAVING COALESCE(MAX(prl.has_mismatch), FALSE) = ?`
		args = append(args, *f.HasMismatch)
	}
	limit, offset := f.Page.normalized()
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx,
		purchaseReceiptListSelect+b.sql()+purchaseReceiptGroupBy+having+
			` ORDER BY r.received_at DESC, r.doc_no DESC LIMIT ? OFFSET ?`,
		args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.PurchaseReceiptListItem{}
	for rows.Next() {
		it, err := scanPurchaseReceiptListItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

const purchaseReceiptLineSelect = `
SELECT prl.id, prl.item_id, i.code, i.name, prl.lot_id, l.lot_no, prl.qty, prl.unit_price,
       prl.line_total, prl.declared_line_total, prl.has_mismatch
FROM purchase_receipt_line prl
JOIN item i ON i.id = prl.item_id
JOIN lot l ON l.id = prl.lot_id
WHERE prl.receipt_id = ?
ORDER BY prl.created_at`

func loadPurchaseReceiptLines(ctx context.Context, q DBTX, receiptID string) ([]domain.PurchaseReceiptLineView, error) {
	rows, err := q.QueryContext(ctx, purchaseReceiptLineSelect, receiptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.PurchaseReceiptLineView{}
	for rows.Next() {
		var l domain.PurchaseReceiptLineView
		var declared *domain.Amount
		if err := rows.Scan(&l.ID, &l.ItemID, &l.ItemCode, &l.ItemName, &l.LotID, &l.LotNo, &l.Qty,
			&l.UnitPrice, &l.LineTotal, &declared, &l.HasMismatch); err != nil {
			return nil, err
		}
		l.DeclaredLineTotal = declared
		out = append(out, l)
	}
	return out, rows.Err()
}

// getPurchaseReceiptDetail: belge basligi + satirlar + satirlarin olusturdugu lotlar (tam Lot
// gorunumu, bakiye dahil) + ilgili stok hareketleri (purchase_in).
func getPurchaseReceiptDetail(ctx context.Context, q DBTX, id string) (*domain.PurchaseReceiptDetail, error) {
	var d domain.PurchaseReceiptDetail
	var invoiceNo sql.NullString
	err := q.QueryRowContext(ctx,
		`SELECT r.id, r.doc_no, r.supplier_id, s.name, r.invoice_no, r.received_at, r.created_at
		 FROM purchase_receipt r JOIN supplier s ON s.id = r.supplier_id WHERE r.id = ?`, id).
		Scan(&d.ID, &d.DocNo, &d.SupplierID, &d.SupplierName, &invoiceNo, &d.ReceivedAt, &d.CreatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	if invoiceNo.Valid {
		d.InvoiceNo = &invoiceNo.String
	}
	lines, err := loadPurchaseReceiptLines(ctx, q, id)
	if err != nil {
		return nil, err
	}
	d.Lines = lines
	total := decimal.Zero
	for _, l := range lines {
		total = total.Add(l.LineTotal.Decimal)
		if l.HasMismatch {
			d.HasMismatch = true
		}
	}
	d.TotalAmount = domain.NewAmount(total)

	d.Lots = make([]domain.Lot, 0, len(lines))
	for _, l := range lines {
		lot, err := getLot(ctx, q, l.LotID, true)
		if err != nil {
			return nil, err
		}
		d.Lots = append(d.Lots, *lot)
	}

	moveRows, err := q.QueryContext(ctx, `SELECT `+movementCols+movementFrom+
		` WHERE sm.ref_type = 'purchase_receipt' AND sm.ref_id = ? ORDER BY sm.created_at`, id)
	if err != nil {
		return nil, err
	}
	defer moveRows.Close()
	d.Movements = []domain.StockMovement{}
	for moveRows.Next() {
		m, err := scanMovement(moveRows)
		if err != nil {
			return nil, err
		}
		d.Movements = append(d.Movements, *m)
	}
	if err := moveRows.Err(); err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *Store) GetPurchaseReceipt(ctx context.Context, id string) (*domain.PurchaseReceiptDetail, error) {
	return getPurchaseReceiptDetail(ctx, s.db, id)
}

// createPurchaseReceiptInTx: belge (yoksa) + N satir (her biri bir lot + purchase_in hareketi)
// VERILEN tx icinde yazar -- bir satir basarisiz olursa hicbiri kalmaz. doc_no verilmemisse
// "AF-<yil>-NNN" atomik uretilir (Alis Fisi; tum satirlar AYNI belgeye yazilir).
//
// CreatePurchaseReceipt (asagida) VE fatura OCR onay akisi (repo/invoice_import.go
// ConfirmInvoiceImport) bu AYNI fonksiyonu paylasir -- ikinci bir alis fisi olusturma yolu
// YOKTUR (bkz. docs/invoice-ocr.md).
func createPurchaseReceiptInTx(ctx context.Context, tx *sql.Tx, req domain.CreatePurchaseReceiptRequest) (receiptID string, lotIDs []string, err error) {
	if ok, err := exists(ctx, tx, "supplier", req.SupplierID); err != nil {
		return "", nil, err
	} else if !ok {
		return "", nil, domain.Invalid("supplier_id bulunamadi")
	}
	docNo := req.DocNo
	if docNo == nil {
		generated, err := nextSequenceNo(ctx, tx, "AF")
		if err != nil {
			return "", nil, err
		}
		docNo = &generated
	}
	lotIDs = make([]string, 0, len(req.Lines))
	for _, line := range req.Lines {
		lotID, rID, err := createPurchaseLotInTx(ctx, tx, domain.PurchaseLotRequest{
			ID:                line.ID,
			LotNo:             line.LotNo,
			ItemID:            line.ItemID,
			SupplierID:        req.SupplierID,
			WarehouseID:       line.WarehouseID,
			ReceivedAt:        req.ReceivedAt,
			ExpiryDate:        line.ExpiryDate,
			Qty:               line.Qty,
			UnitPrice:         line.UnitPrice,
			DeclaredLineTotal: line.DeclaredLineTotal,
			DocNo:             docNo,
			InvoiceNo:         req.InvoiceNo,
		})
		if err != nil {
			return "", nil, err
		}
		receiptID, lotIDs = rID, append(lotIDs, lotID)
	}
	return receiptID, lotIDs, nil
}

// CreatePurchaseReceipt createPurchaseReceiptInTx'i TEK transaction'da calistirir, sonra en iyi
// gayretle anomali tespiti yapar (commit sonrasi, bkz. repo/anomaly.go detectPurchaseCostAnomaly).
func (s *Store) CreatePurchaseReceipt(ctx context.Context, req domain.CreatePurchaseReceiptRequest) (*domain.PurchaseReceiptDetail, error) {
	var receiptID string
	var lotIDs []string
	err := s.InTx(ctx, func(tx *sql.Tx) error {
		var err error
		receiptID, lotIDs, err = createPurchaseReceiptInTx(ctx, tx, req)
		return err
	})
	if err != nil {
		return nil, err
	}
	for i, line := range req.Lines {
		s.detectPurchaseCostAnomaly(ctx, receiptID, line.ItemID, lotIDs[i], line.UnitPrice.Decimal)
	}
	return s.GetPurchaseReceipt(ctx, receiptID)
}
