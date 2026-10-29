// Fatura/irsaliye OCR akisi (bkz. docs/invoice-ocr.md): upload -> extracted, confirm -> alis
// fisi/lot/stok hareketi, discard, ve EN ONEMLISI onay olmadan hicbir kayit olusmadigi.
package integration

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/mlclient"
	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/repo"

	apihttp "github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/http"
)

// mockInvoiceOCRResponse ml/app/main.py InvoiceExtractionResponse ile ayni sekil (bkz.
// docs/invoice-ocr.md); testin kendi OCR onerisini SABIT doner, gercek Tesseract'a ihtiyac yok.
const mockInvoiceOCRResponse = `{
  "supplier_name": {"value": "Yayla Hayvancılık", "confidence": 0.95, "source_text": "Yayla Hayvancılık", "warnings": []},
  "document_no": {"value": "HY-F-0001", "confidence": 0.9, "source_text": "Fatura No: HY-F-0001", "warnings": []},
  "document_date": {"value": "2026-03-02", "confidence": 0.9, "source_text": "02.03.2026", "warnings": []},
  "currency": {"value": "EUR", "confidence": 0.8, "source_text": "€", "warnings": []},
  "lines": [
    {
      "product_name": {"value": "Bütün Kuzu", "confidence": 0.92, "source_text": "Bütün Kuzu", "warnings": []},
      "quantity": {"value": "25.000", "confidence": 0.9, "source_text": "25,000 kg", "warnings": []},
      "unit": {"value": "kg", "confidence": 0.9, "source_text": "kg", "warnings": []},
      "unit_price": {"value": "11.50", "confidence": 0.9, "source_text": "11,50", "warnings": []},
      "line_total": {"value": "351.00", "confidence": 0.9, "source_text": "351,00", "warnings": ["TUTAR_UYUSMAZLIGI: 25.000 x 11.50 = 287.50, okunan satir tutari 351.00"]},
      "lot_no": {"value": "KZ-2026-900", "confidence": 0.8, "source_text": "LOT: KZ-2026-900", "warnings": []},
      "expiry_date": {"value": null, "confidence": 0.0, "source_text": "", "warnings": []},
      "warnings": ["TUTAR_UYUSMAZLIGI: 25.000 x 11.50 = 287.50, okunan satir tutari 351.00"]
    }
  ],
  "overall_confidence": 0.87,
  "raw_text": "Yayla Hayvancılık\nFatura No: HY-F-0001 Tarih: 02.03.2026\nBütün Kuzu 25,000 kg 11,50 351,00 €",
  "warnings": []
}`

// newEnvMockOCR ML servisini taklit eder: /extract/invoice her zaman sabit mockInvoiceOCRResponse doner.
func newEnvMockOCR(t *testing.T) *env {
	t.Helper()
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/extract/invoice" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mockInvoiceOCRResponse))
	}))
	t.Cleanup(mock.Close)

	reset(t)
	store := repo.New(testDB).SetMLClient(mlclient.New(mock.URL)).SetUploadsDir(t.TempDir())
	h := apihttp.NewRouter(apihttp.Deps{DB: testDB, Store: store, CORSOrigins: []string{"*"}})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &env{t: t, srv: srv}
}

// uploadInvoice multipart/form-data POST /invoice-imports gonderir (e.do JSON gonderir, dosya
// yuklemesi icin kullanilamaz).
func (e *env) uploadInvoice(filename, content string) resp {
	e.t.Helper()
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		e.t.Fatal(err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		e.t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		e.t.Fatal(err)
	}
	req, err := http.NewRequest("POST", e.srv.URL+"/api/v1/invoice-imports", body)
	if err != nil {
		e.t.Fatal(err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := resp{Status: res.StatusCode, Raw: string(raw)}
	_ = json.Unmarshal(raw, &out.JSON)
	return out
}

func TestInvoiceImportUploadCreatesExtractedRecordNoReceiptYet(t *testing.T) {
	e := newEnvMockOCR(t)
	up := e.must(e.uploadInvoice("fatura.png", "sahte-goruntu-baytlari"), 201)

	if up.str("status") != "extracted" {
		t.Fatalf("status=extracted beklenirdi: %s", up.Raw)
	}
	if up.JSON["purchase_receipt_id"] != nil {
		t.Fatalf("confirm oncesi purchase_receipt_id NULL olmali: %s", up.Raw)
	}
	ocr, _ := up.JSON["ocr"].(map[string]any)
	if ocr == nil || ocr["supplier_name"].(map[string]any)["value"] != "Yayla Hayvancılık" {
		t.Fatalf("OCR onerisi yaniti eksik: %s", up.Raw)
	}
	if n := count(t, `SELECT COUNT(*) FROM invoice_import WHERE id = ?`, up.str("id")); n != 1 {
		t.Fatalf("invoice_import kaydi olusmali")
	}

	// ONAY OLMADAN hicbir lot/hareket/fis olusmaz.
	if n := count(t, `SELECT COUNT(*) FROM purchase_receipt`); n != 0 {
		t.Fatalf("onay oncesi purchase_receipt olusmamali: %d", n)
	}
	if n := count(t, `SELECT COUNT(*) FROM lot`); n != 0 {
		t.Fatalf("onay oncesi lot olusmamali: %d", n)
	}
	if n := count(t, `SELECT COUNT(*) FROM stock_movement`); n != 0 {
		t.Fatalf("onay oncesi stok hareketi olusmamali: %d", n)
	}

	list := e.must(e.do("GET", "/api/v1/invoice-imports", nil), 200)
	items, _ := list.JSON["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("liste: 1 kayit beklenirdi: %s", list.Raw)
	}
}

func TestInvoiceImportConfirmCreatesReceiptLotAndMovement(t *testing.T) {
	e := newEnvMockOCR(t)
	f := e.fixture()
	up := e.must(e.uploadInvoice("fatura.png", "sahte-goruntu"), 201)
	id := up.str("id")

	confirmBody := map[string]any{
		"supplier_id": f.supplier,
		"received_at": "2026-03-02",
		"lines": []map[string]any{
			{
				"lot_no": "KZ-2026-900", "item_id": f.item, "warehouse_id": f.warehouse,
				"qty": "25.000", "unit_price": "11.5000", "declared_line_total": "351.0000",
			},
		},
	}
	confirmed := e.must(e.do("POST", "/api/v1/invoice-imports/"+id+"/confirm", confirmBody), 200)
	if confirmed.str("status") != "confirmed" {
		t.Fatalf("status=confirmed beklenirdi: %s", confirmed.Raw)
	}
	receiptID, _ := confirmed.JSON["purchase_receipt_id"].(string)
	if receiptID == "" {
		t.Fatalf("purchase_receipt_id dolu olmali: %s", confirmed.Raw)
	}

	// Mevcut purchase-receipts yolu AYNEN kullanildi: fis + lot + purchase_in hareketi olustu,
	// tutar uyusmazligi (25x11.50=287.50 != 351.00) da OCR confirm akisinda da yakalandi.
	receipt := e.must(e.do("GET", "/api/v1/purchase-receipts/"+receiptID, nil), 200)
	if receipt.JSON["has_mismatch"] != true {
		t.Fatalf("has_mismatch=true beklenirdi (287.50 != 351.00): %s", receipt.Raw)
	}
	if n := count(t, `SELECT COUNT(*) FROM lot WHERE lot_no = 'KZ-2026-900'`); n != 1 {
		t.Fatalf("lot olusmali")
	}
	if n := count(t, `SELECT COUNT(*) FROM stock_movement WHERE movement_type = 'purchase_in'`); n != 1 {
		t.Fatalf("purchase_in hareketi olusmali")
	}
	if n := count(t, `SELECT COUNT(*) FROM invoice_import WHERE id = ? AND status = 'confirmed' AND confirmed_at IS NOT NULL`, id); n != 1 {
		t.Fatalf("invoice_import confirmed olarak isaretlenmeli")
	}

	// Zaten confirmed ise tekrar confirm 409.
	again := e.do("POST", "/api/v1/invoice-imports/"+id+"/confirm", confirmBody)
	if again.Status != 409 {
		t.Fatalf("409 beklenirdi: %d %s", again.Status, again.Raw)
	}
	// Ikinci confirm denemesi (409) YENI bir lot/fis OLUSTURMADI.
	if n := count(t, `SELECT COUNT(*) FROM purchase_receipt`); n != 1 {
		t.Fatalf("reddedilen confirm yeni fis olusturmamali: %d", n)
	}
}

func TestInvoiceImportDiscardThenConfirmFails(t *testing.T) {
	e := newEnvMockOCR(t)
	f := e.fixture()
	up := e.must(e.uploadInvoice("fatura.png", "x"), 201)
	id := up.str("id")

	discarded := e.must(e.do("POST", "/api/v1/invoice-imports/"+id+"/discard", nil), 200)
	if discarded.str("status") != "discarded" {
		t.Fatalf("status=discarded beklenirdi: %s", discarded.Raw)
	}

	confirmBody := map[string]any{
		"supplier_id": f.supplier, "received_at": "2026-03-02",
		"lines": []map[string]any{{"lot_no": "X", "item_id": f.item, "warehouse_id": f.warehouse, "qty": "1.000", "unit_price": "1.0000"}},
	}
	after := e.do("POST", "/api/v1/invoice-imports/"+id+"/confirm", confirmBody)
	if after.Status != 409 {
		t.Fatalf("discard sonrasi confirm 409 beklenirdi: %d %s", after.Status, after.Raw)
	}
	if n := count(t, `SELECT COUNT(*) FROM purchase_receipt`); n != 0 {
		t.Fatalf("discard edilmis kayittan fis olusmamali: %d", n)
	}
}

func TestInvoiceImportUploadFailsWhenMLDown(t *testing.T) {
	reset(t)
	store := repo.New(testDB).SetMLClient(mlclient.New("http://127.0.0.1:1")).SetUploadsDir(t.TempDir()) // hicbir zaman dinlemeyen port
	h := apihttp.NewRouter(apihttp.Deps{DB: testDB, Store: store, CORSOrigins: []string{"*"}})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	e := &env{t: t, srv: srv}

	down := e.uploadInvoice("fatura.png", "x")
	if down.Status != 503 {
		t.Fatalf("503 beklenirdi: %d %s", down.Status, down.Raw)
	}
	if down.str("error", "code") != "unavailable" {
		t.Fatalf("error.code=unavailable beklenirdi: %s", down.Raw)
	}
	if n := count(t, `SELECT COUNT(*) FROM invoice_import`); n != 0 {
		t.Fatalf("ML kapaliyken kayit olusmamali: %d", n)
	}
}

func TestInvoiceImportFuzzyMatchCandidates(t *testing.T) {
	e := newEnvMockOCR(t)
	// mockInvoiceOCRResponse supplier_name="Yayla Hayvancılık" ve bir satir product_name="Bütün Kuzu" doner.
	e.must(e.do("POST", "/api/v1/suppliers", map[string]any{
		"code": "SUP-YAYLA", "name": "Yayla Hayvancılık Gıda Ltd. Şti.", "supplier_type": "livestock"}), 201)
	e.must(e.do("POST", "/api/v1/items", map[string]any{
		"code": "KUZU-BUTUN", "name": "Bütün Kuzu", "item_type": "raw", "base_uom": "kg"}), 201)

	up := e.must(e.uploadInvoice("fatura.png", "x"), 201)
	got := e.must(e.do("GET", "/api/v1/invoice-imports/"+up.str("id"), nil), 200)

	supCands, _ := got.JSON["supplier_candidates"].([]any)
	if len(supCands) == 0 {
		t.Fatalf("tedarikci adayi bulunmaliydi (benzer isim var): %s", got.Raw)
	}
	if supCands[0].(map[string]any)["name"] != "Yayla Hayvancılık Gıda Ltd. Şti." {
		t.Fatalf("en iyi aday beklenen degildi: %s", got.Raw)
	}

	lineCands, _ := got.JSON["line_item_candidates"].([]any)
	if len(lineCands) != 1 {
		t.Fatalf("1 satirlik aday dizisi beklenirdi: %s", got.Raw)
	}
	firstLineCands, _ := lineCands[0].([]any)
	if len(firstLineCands) == 0 || firstLineCands[0].(map[string]any)["name"] != "Bütün Kuzu" {
		t.Fatalf("urun adayi bulunmaliydi: %s", got.Raw)
	}
}
