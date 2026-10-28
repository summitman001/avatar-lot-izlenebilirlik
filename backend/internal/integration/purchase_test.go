// GET/POST /purchase-receipts: coklu satirli alis fisi (bkz. docs/api-contract.md, repo/purchase.go).
// Yazma yolu lots.go'daki createPurchaseLotInTx'i (POST /lots ile AYNI kod) her satir icin bir kez
// cagirir; bu dosya ozellikle COKLU SATIR + ATOMIKLIK + liste/detay gorunumlerini test eder (tek
// satirlik POST /lots davranisi zaten integration_test.go'da kapsanmistir).
package integration

import (
	"testing"
)

// purchaseFixture: bir urun + iki tedarikci + bir depo (coklu tedarikci filtresi testi icin).
type purchaseFixture struct {
	item, supplier1, supplier2, warehouse string
}

func (e *env) purchaseFixture() purchaseFixture {
	e.t.Helper()
	item := e.must(e.do("POST", "/api/v1/items", map[string]any{
		"code": "KUZU-BUTUN", "name": "Bütün Kuzu", "item_type": "raw", "base_uom": "kg"}), 201).str("id")
	sup1 := e.must(e.do("POST", "/api/v1/suppliers", map[string]any{
		"code": "SUP-HAY-1", "name": "Yayla Hayvancılık", "supplier_type": "livestock"}), 201).str("id")
	sup2 := e.must(e.do("POST", "/api/v1/suppliers", map[string]any{
		"code": "SUP-HAY-2", "name": "Ova Hayvancılık", "supplier_type": "livestock"}), 201).str("id")
	wh := e.must(e.do("POST", "/api/v1/warehouses", map[string]any{
		"code": "WH-MAIN", "name": "Ana Depo", "warehouse_type": "internal"}), 201).str("id")
	return purchaseFixture{item, sup1, sup2, wh}
}

// Coklu satirli alis fisi: 2 satir, biri uyusmazlikli (KZ-2026-001: 25x11.50=287.50 != beyan 351.00).
// Her satir kendi lotunu + purchase_in hareketini olusturur; TEK belgeye (ayni doc_no) yazilirlar.
func TestCreatePurchaseReceiptMultiLine(t *testing.T) {
	e := newEnv(t)
	f := e.purchaseFixture()

	receipt := e.must(e.do("POST", "/api/v1/purchase-receipts", map[string]any{
		"supplier_id": f.supplier1, "invoice_no": "HY-F-0001", "received_at": "2026-03-02",
		"lines": []map[string]any{
			{"lot_no": "KZ-2026-001", "item_id": f.item, "warehouse_id": f.warehouse, "qty": "25.000", "unit_price": "11.5000", "declared_line_total": "351.0000"},
			{"lot_no": "KZ-2026-002", "item_id": f.item, "warehouse_id": f.warehouse, "qty": "18.500", "unit_price": "11.7500"},
		},
	}), 201)

	if receipt.str("supplier_id") != f.supplier1 || receipt.str("invoice_no") != "HY-F-0001" {
		t.Fatalf("beklenmeyen alis fisi: %s", receipt.Raw)
	}
	docNo := receipt.str("doc_no")
	if docNo == "" {
		t.Fatalf("doc_no uretilmemis: %s", receipt.Raw)
	}
	if receipt.JSON["has_mismatch"] != true {
		t.Fatalf("has_mismatch=true beklenirdi (satir 1 uyusmazlikli): %s", receipt.Raw)
	}
	// total_amount = round2(25*11.50) + round2(18.5*11.75) = 287.50 + 217.38 = 504.88
	if receipt.str("total_amount") != "504.8800" {
		t.Fatalf("total_amount: %v", receipt.JSON["total_amount"])
	}
	lines, _ := receipt.JSON["lines"].([]any)
	if len(lines) != 2 {
		t.Fatalf("2 satir beklenirdi: %s", receipt.Raw)
	}
	l1 := lines[0].(map[string]any)
	if l1["lot_no"] != "KZ-2026-001" || l1["line_total"] != "287.5000" || l1["declared_line_total"] != "351.0000" || l1["has_mismatch"] != true {
		t.Fatalf("satir 1: %v", l1)
	}
	l2 := lines[1].(map[string]any)
	if l2["lot_no"] != "KZ-2026-002" || l2["line_total"] != "217.3800" || l2["declared_line_total"] != nil || l2["has_mismatch"] != false {
		t.Fatalf("satir 2: %v", l2)
	}

	lots, _ := receipt.JSON["lots"].([]any)
	if len(lots) != 2 {
		t.Fatalf("2 olusan lot beklenirdi: %s", receipt.Raw)
	}
	for _, raw := range lots {
		lot := raw.(map[string]any)
		if lot["status"] != "active" || lot["qty_on_hand"] == nil {
			t.Fatalf("olusan lot: %v", lot)
		}
	}

	movements, _ := receipt.JSON["movements"].([]any)
	if len(movements) != 2 {
		t.Fatalf("2 purchase_in hareketi beklenirdi: %s", receipt.Raw)
	}
	for _, raw := range movements {
		m := raw.(map[string]any)
		if m["movement_type"] != "purchase_in" || m["ref_type"] != "purchase_receipt" {
			t.Fatalf("hareket: %v", m)
		}
	}

	if n := count(t, `SELECT COUNT(*) FROM purchase_receipt WHERE doc_no = ?`, docNo); n != 1 {
		t.Fatalf("TEK belge beklenirdi (ayni doc_no, 2 satir), %d belge var", n)
	}
	if n := count(t, `SELECT COUNT(*) FROM stock_movement WHERE ref_type = 'purchase_receipt' AND ref_id = ?`, receipt.str("id")); n != 2 {
		t.Fatalf("belgeye bagli 2 hareket beklenirdi, %d", n)
	}

	// GET /purchase-receipts/{id}: ayni govdeyi doner.
	got := e.must(e.do("GET", "/api/v1/purchase-receipts/"+receipt.str("id"), nil), 200)
	if got.str("doc_no") != docNo || len(got.JSON["lines"].([]any)) != 2 {
		t.Fatalf("GET detay: %s", got.Raw)
	}
}

// doc_no verilmemisse "AF-<yil>-NNN" atomik uretilir; verilmisse aynen kullanilir (ayni doc_no +
// AYNI tedarikci => mevcut belgeye satir eklenir -- POST /lots ile birebir ayni kural).
func TestCreatePurchaseReceiptDocNoGeneration(t *testing.T) {
	e := newEnv(t)
	f := e.purchaseFixture()

	auto := e.must(e.do("POST", "/api/v1/purchase-receipts", map[string]any{
		"supplier_id": f.supplier1, "received_at": "2026-03-02",
		"lines": []map[string]any{{"lot_no": "ET-2026-500", "item_id": f.item, "warehouse_id": f.warehouse, "qty": "10.000", "unit_price": "9.0000"}},
	}), 201)
	if auto.str("doc_no") == "" {
		t.Fatalf("doc_no uretilmemis: %s", auto.Raw)
	}

	explicit := e.must(e.do("POST", "/api/v1/purchase-receipts", map[string]any{
		"doc_no": "AF-OZEL-001", "supplier_id": f.supplier1, "received_at": "2026-03-03",
		"lines": []map[string]any{{"lot_no": "ET-2026-501", "item_id": f.item, "warehouse_id": f.warehouse, "qty": "5.000", "unit_price": "9.0000"}},
	}), 201)
	if explicit.str("doc_no") != "AF-OZEL-001" {
		t.Fatalf("verilen doc_no korunmali: %s", explicit.Raw)
	}
	// ayni doc_no + ayni tedarikci: ikinci cagri AYNI belgeye satir ekler.
	appended := e.must(e.do("POST", "/api/v1/purchase-receipts", map[string]any{
		"doc_no": "AF-OZEL-001", "supplier_id": f.supplier1, "received_at": "2026-03-03",
		"lines": []map[string]any{{"lot_no": "ET-2026-502", "item_id": f.item, "warehouse_id": f.warehouse, "qty": "3.000", "unit_price": "9.0000"}},
	}), 201)
	if appended.str("id") != explicit.str("id") {
		t.Fatalf("ayni doc_no+tedarikci AYNI belgeye eklenmeli: %s", appended.Raw)
	}
	if n := count(t, `SELECT COUNT(*) FROM purchase_receipt_line WHERE receipt_id = ?`, explicit.str("id")); n != 2 {
		t.Fatalf("belgede 2 satir olmali, %d", n)
	}
	// ayni doc_no, BASKA tedarikci: 409.
	if r := e.do("POST", "/api/v1/purchase-receipts", map[string]any{
		"doc_no": "AF-OZEL-001", "supplier_id": f.supplier2, "received_at": "2026-03-03",
		"lines": []map[string]any{{"lot_no": "ET-2026-503", "item_id": f.item, "warehouse_id": f.warehouse, "qty": "1.000", "unit_price": "9.0000"}},
	}); r.Status != 409 {
		t.Errorf("baska tedarikciye ait belge 409 olmali: %d %s", r.Status, r.Raw)
	}
}

// Satirlardan biri basarisiz olursa (ornek: ayni lot_no iki kez) TUM istek geri alinir -- hicbir
// satirin lotu/hareketi/belgesi kalmaz (bkz. createPurchaseLotInTx, TEK transaction).
func TestCreatePurchaseReceiptRollsBackOnLineFailure(t *testing.T) {
	e := newEnv(t)
	f := e.purchaseFixture()

	bad := e.do("POST", "/api/v1/purchase-receipts", map[string]any{
		"supplier_id": f.supplier1, "received_at": "2026-03-02",
		"lines": []map[string]any{
			{"lot_no": "ET-2026-700", "item_id": f.item, "warehouse_id": f.warehouse, "qty": "10.000", "unit_price": "9.0000"},
			{"lot_no": "ET-2026-700", "item_id": f.item, "warehouse_id": f.warehouse, "qty": "5.000", "unit_price": "9.0000"}, // ayni lot_no -> UNIQUE ihlali
		},
	})
	if bad.Status != 409 {
		t.Fatalf("409 beklenirdi: %d %s", bad.Status, bad.Raw)
	}
	if n := count(t, `SELECT COUNT(*) FROM lot WHERE lot_no = 'ET-2026-700'`); n != 0 {
		t.Errorf("basarisiz istekten HICBIR lot kalmamali, %d kaldi", n)
	}
	if n := count(t, `SELECT COUNT(*) FROM purchase_receipt`); n != 0 {
		t.Errorf("basarisiz istekten belge kalmamali, %d kaldi", n)
	}
	if n := count(t, `SELECT COUNT(*) FROM stock_movement`); n != 0 {
		t.Errorf("basarisiz istekten hareket kalmamali, %d kaldi", n)
	}
	// lines bos olamaz, supplier_id gecersiz UUID olamaz.
	if r := e.do("POST", "/api/v1/purchase-receipts", map[string]any{"supplier_id": f.supplier1, "received_at": "2026-03-02", "lines": []any{}}); r.Status != 422 {
		t.Errorf("bos lines 422 olmali: %d %s", r.Status, r.Raw)
	}
}

// GET /purchase-receipts: supplier_id/item_id/from-to/has_mismatch filtreleri.
func TestListPurchaseReceiptsFilters(t *testing.T) {
	e := newEnv(t)
	f := e.purchaseFixture()

	e.must(e.do("POST", "/api/v1/purchase-receipts", map[string]any{
		"supplier_id": f.supplier1, "received_at": "2026-03-01",
		"lines": []map[string]any{{"lot_no": "KZ-2026-010", "item_id": f.item, "warehouse_id": f.warehouse, "qty": "25.000", "unit_price": "11.5000", "declared_line_total": "351.0000"}},
	}), 201)
	e.must(e.do("POST", "/api/v1/purchase-receipts", map[string]any{
		"supplier_id": f.supplier2, "received_at": "2026-03-15",
		"lines": []map[string]any{{"lot_no": "KZ-2026-011", "item_id": f.item, "warehouse_id": f.warehouse, "qty": "10.000", "unit_price": "11.5000"}},
	}), 201)

	for path, want := range map[string]int{
		"/api/v1/purchase-receipts":                            2,
		"/api/v1/purchase-receipts?supplier_id=" + f.supplier1: 1,
		"/api/v1/purchase-receipts?supplier_id=" + f.supplier2: 1,
		"/api/v1/purchase-receipts?has_mismatch=true":          1,
		"/api/v1/purchase-receipts?has_mismatch=false":         1,
		"/api/v1/purchase-receipts?from=2026-03-10":            1,
		"/api/v1/purchase-receipts?to=2026-03-10":              1,
		"/api/v1/purchase-receipts?item_id=" + f.item:          2,
	} {
		r := e.must(e.do("GET", path, nil), 200)
		if items, _ := r.JSON["items"].([]any); len(items) != want {
			t.Errorf("%s: %d kayit bekleniyordu, %d geldi: %s", path, want, len(items), r.Raw)
		}
	}
}
