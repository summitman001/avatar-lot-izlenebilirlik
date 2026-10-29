package domain

import (
	"encoding/json"
	"time"
)

// Package domain (invoice_import.go): fatura/irsaliye OCR akisi (bkz. docs/invoice-ocr.md).
//
// KESIN KURAL: OCR SADECE oneri uretir, hicbir alan "dogru" diye sunulmaz. Onay (confirm) olmadan
// hicbir lot/stok hareketi/alis fisi olusturulmaz -- confirm govdesi (asagida) OCR ciktisi DEGIL,
// KULLANICININ DUZELTTIGI veridir ve mevcut purchase-receipts olusturma yolunu (createPurchaseLotInTx)
// AYNEN kullanir, ikinci bir yol acilmaz.

// InvoiceImportListItem GET /invoice-imports liste ogesi (OCR govdesini icermez, performans icin
// GET /{id} ile ayni desen -- bkz. diger liste uc noktalari).
type InvoiceImportListItem struct {
	ID                   string    `json:"id"`
	FileName             string    `json:"file_name"`
	UploadedAt           time.Time `json:"uploaded_at"`
	Status               string    `json:"status"` // extracted | confirmed | discarded
	OverallConfidence    float64   `json:"overall_confidence"`
	PurchaseReceiptID    *string   `json:"purchase_receipt_id"`
	PurchaseReceiptDocNo *string   `json:"purchase_receipt_doc_no"`
}

// MatchCandidate fuzzy eslestirme sonucu bir aday (tedarikci veya urun). OTOMATIK SECIM YAPILMAZ:
// kullanici adaylardan birini secer ya da yeni kayit olusturur (bkz. docs/invoice-ocr.md).
type MatchCandidate struct {
	ID    string  `json:"id"`
	Code  string  `json:"code"`
	Name  string  `json:"name"`
	Score float64 `json:"score"` // 0..1, 1 = birebir (normalize edilmis) eslesme
}

// InvoiceImportDetail GET /invoice-imports/{id}: yuklenen belgenin OCR onerisi (ham, ML yanitinin
// aynisi) + taze hesaplanan tedarikci/urun eslestirme adaylari (bkz. repo/invoice_import.go
// matchSupplierCandidates/matchItemCandidates). line_item_candidates, ocr.lines ile AYNI sirada
// paralel bir dizidir.
type InvoiceImportDetail struct {
	ID                 string             `json:"id"`
	FileName           string             `json:"file_name"`
	ContentType        string             `json:"content_type"`
	UploadedAt         time.Time          `json:"uploaded_at"`
	Status             string             `json:"status"`
	OCR                json.RawMessage    `json:"ocr"`
	OverallConfidence  float64            `json:"overall_confidence"`
	SupplierCandidates []MatchCandidate   `json:"supplier_candidates"`
	LineItemCandidates [][]MatchCandidate `json:"line_item_candidates"`
	PurchaseReceiptID  *string            `json:"purchase_receipt_id"`
	ConfirmedAt        *time.Time         `json:"confirmed_at"`
	DiscardedAt        *time.Time         `json:"discarded_at"`
	CreatedAt          time.Time          `json:"created_at"`
}
