package domain

import (
	"strings"
	"time"
)

// Package domain (purchase.go): GET/POST /purchase-receipts API gorunumleri ve istek govdeleri.
// Bir "alis fisi" (purchase_receipt) birden fazla satir (purchase_receipt_line) icerir; her satir
// TEK bir lot + purchase_in hareketi dogurur (bkz. repo/lots.go createPurchaseLotInTx, repo/purchase.go).

// PurchaseReceiptLineRequest POST /purchase-receipts govdesindeki bir satir. CreatePurchaseLot ile
// AYNI alanlar (receipt'in ortak alanlari -- tedarikci/fatura/tarih/belge no -- ustte bir kez verilir).
type PurchaseReceiptLineRequest struct {
	ID                *string `json:"id"` // opsiyonel istemci UUIDv7'si (lot id)
	LotNo             string  `json:"lot_no"`
	ItemID            string  `json:"item_id"`
	WarehouseID       string  `json:"warehouse_id"`
	Qty               Qty     `json:"qty"`
	UnitPrice         Amount  `json:"unit_price"`
	DeclaredLineTotal *Amount `json:"declared_line_total"`
	ExpiryDate        *Date   `json:"expiry_date"`
}

func (l *PurchaseReceiptLineRequest) validate(idx int) error {
	l.LotNo = strings.TrimSpace(l.LotNo)
	if err := required("lot_no", l.LotNo, 40); err != nil {
		return lineErr(idx, err)
	}
	for field, id := range map[string]string{"item_id": l.ItemID, "warehouse_id": l.WarehouseID} {
		if _, err := ParseID(id); err != nil {
			return lineErr(idx, Invalid("%s: %s", field, err))
		}
	}
	if l.ID != nil {
		if _, err := ParseID(*l.ID); err != nil {
			return lineErr(idx, Invalid("id: %s", err))
		}
	}
	if !l.Qty.IsPositive() {
		return lineErr(idx, Invalid("qty sifirdan buyuk olmali"))
	}
	if l.UnitPrice.IsNegative() {
		return lineErr(idx, Invalid("unit_price negatif olamaz"))
	}
	if l.DeclaredLineTotal != nil && l.DeclaredLineTotal.IsNegative() {
		return lineErr(idx, Invalid("declared_line_total negatif olamaz"))
	}
	return nil
}

func lineErr(idx int, err error) error {
	if ve, ok := err.(*ValidationError); ok {
		return Invalid("lines[%d].%s", idx, ve.Msg)
	}
	return err
}

// CreatePurchaseReceiptRequest POST /purchase-receipts govdesi: tedarikci + fatura/tarih bilgisi +
// bir ya da daha fazla satir. doc_no bos ise "AF-<yil>-NNN" atomik uretilir (bkz. repo/purchase.go).
type CreatePurchaseReceiptRequest struct {
	ID         *string                      `json:"id"` // opsiyonel istemci UUIDv7'si (receipt id)
	DocNo      *string                      `json:"doc_no"`
	SupplierID string                       `json:"supplier_id"`
	InvoiceNo  *string                      `json:"invoice_no"`
	ReceivedAt Date                         `json:"received_at"`
	Lines      []PurchaseReceiptLineRequest `json:"lines"`
}

func (r *CreatePurchaseReceiptRequest) Validate() error {
	if _, err := ParseID(r.SupplierID); err != nil {
		return Invalid("supplier_id: %s", err)
	}
	if r.ID != nil {
		if _, err := ParseID(*r.ID); err != nil {
			return Invalid("id: %s", err)
		}
	}
	if r.ReceivedAt.IsZero() {
		return Invalid("received_at zorunlu")
	}
	if len(r.Lines) == 0 {
		return Invalid("en az bir satir (lines) gerekli")
	}
	trimPtr(&r.DocNo)
	trimPtr(&r.InvoiceNo)
	for i := range r.Lines {
		if err := r.Lines[i].validate(i); err != nil {
			return err
		}
	}
	return nil
}

// PurchaseReceiptListItem GET /purchase-receipts liste ogesidir (satirlar icermez, performans icin
// GET /purchase-receipts/{id} ile ayni desen -- bkz. production/cutting order listeleri).
type PurchaseReceiptListItem struct {
	ID           string    `json:"id"`
	DocNo        string    `json:"doc_no"`
	SupplierID   string    `json:"supplier_id"`
	SupplierName string    `json:"supplier_name"`
	InvoiceNo    *string   `json:"invoice_no"`
	ReceivedAt   Date      `json:"received_at"`
	LineCount    int       `json:"line_count"`
	TotalAmount  Amount    `json:"total_amount"` // hesaplanan (line_total) toplami
	HasMismatch  bool      `json:"has_mismatch"` // en az bir satirda uyusmazlik varsa true
	CreatedAt    time.Time `json:"created_at"`
}

// PurchaseReceiptLineView GET /purchase-receipts/{id} detayindaki bir satir: hesaplanan ve beyan
// edilen tutar yan yana (jüri KZ-2026-001'in 287,50/351,00 uyusmazligini burada gorur).
type PurchaseReceiptLineView struct {
	ID                string  `json:"id"`
	ItemID            string  `json:"item_id"`
	ItemCode          string  `json:"item_code"`
	ItemName          string  `json:"item_name"`
	LotID             string  `json:"lot_id"`
	LotNo             string  `json:"lot_no"`
	Qty               Qty     `json:"qty"`
	UnitPrice         Amount  `json:"unit_price"`
	LineTotal         Amount  `json:"line_total"` // hesaplanan: round2(qty x unit_price)
	DeclaredLineTotal *Amount `json:"declared_line_total"`
	HasMismatch       bool    `json:"has_mismatch"`
}

// PurchaseReceiptDetail GET /purchase-receipts/{id}: satirlar + olusan lotlar + ilgili stok hareketleri.
type PurchaseReceiptDetail struct {
	ID           string                    `json:"id"`
	DocNo        string                    `json:"doc_no"`
	SupplierID   string                    `json:"supplier_id"`
	SupplierName string                    `json:"supplier_name"`
	InvoiceNo    *string                   `json:"invoice_no"`
	ReceivedAt   Date                      `json:"received_at"`
	TotalAmount  Amount                    `json:"total_amount"`
	HasMismatch  bool                      `json:"has_mismatch"`
	CreatedAt    time.Time                 `json:"created_at"`
	Lines        []PurchaseReceiptLineView `json:"lines"`
	Lots         []Lot                     `json:"lots"`
	Movements    []StockMovement           `json:"movements"`
}
