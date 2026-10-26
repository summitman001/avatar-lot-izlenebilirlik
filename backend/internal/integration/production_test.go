// Gorev 1 uctan uca entegrasyon testleri: dis hizmetle uretim ve gercek maliyet (EKP).
// Golden karsilastirmasi gercek HTTP uc noktalariyla yapilir (create -> send-materials -> receive),
// beklenen degerler docs/golden/production_cases.json'dan OKUNUR (elle yazilmaz).
package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
	apihttp "github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/http"
	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/mlclient"
	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/repo"
)

// newEnvML newEnv ile ayni ama Store'a GERCEK (ama erisilemez) bir ML istemcisi baglar; "ML servisi
// kapaliyken emir olusturma basarili" senaryosunu bir ag hatasiyla (sahte degil) dogrulamak icin.
func newEnvML(t *testing.T) *env {
	t.Helper()
	reset(t)
	store := repo.New(testDB).SetMLClient(mlclient.New("http://127.0.0.1:1")) // hicbir zaman dinlemeyen port
	h := apihttp.NewRouter(apihttp.Deps{DB: testDB, Store: store, CORSOrigins: []string{"*"}})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &env{t: t, srv: srv}
}

// ---------------------------------------------------------------------------
// Golden karsilastirma: UR-001, UR-002, UR-003, UR-006
// ---------------------------------------------------------------------------

type productionGoldenCase struct {
	CaseID      string                     `json:"case_id"`
	Description string                     `json:"description"`
	Input       productionGoldenInput      `json:"input"`
	Expected    map[string]json.RawMessage `json:"expected"`
}

type productionGoldenLot struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	Quantity string `json:"quantity"`
	UnitCost string `json:"unit_cost"`
}

type productionGoldenInput struct {
	UnitWeightKg     *string               `json:"unit_weight_kg"`
	Lots             []productionGoldenLot `json:"lots"`
	DeliveredQty     string                `json:"delivered_qty"`
	ServiceUnitPrice string                `json:"service_unit_price"`
	InvoicedQty      *string               `json:"invoiced_qty"`
	SalePrice        *string               `json:"sale_price"`
}

func loadProductionGolden(t *testing.T) []productionGoldenCase {
	t.Helper()
	dir := os.Getenv("GOLDEN_DIR")
	if dir == "" {
		dir = filepath.Join("..", "..", "..", "docs", "golden")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "production_cases.json"))
	if err != nil {
		t.Fatalf("golden dosya okunamadi: %v", err)
	}
	var cases []productionGoldenCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("production_cases.json bos")
	}
	return cases
}

// rawStr golden'in bir alanini *string olarak doner (null ise nil).
func rawStr(t *testing.T, m map[string]json.RawMessage, key string) *string {
	t.Helper()
	raw, ok := m[key]
	if !ok || string(raw) == "null" {
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("%s cozumlenemedi: %v", key, err)
	}
	return &s
}

func rawStrList(t *testing.T, m map[string]json.RawMessage, key string) []string {
	t.Helper()
	raw, ok := m[key]
	if !ok {
		return nil
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s cozumlenemedi: %v", key, err)
	}
	return out
}

// eqPtrStr beklenen (golden) ve gercek (*string, JSON'dan) degerleri karsilastirir; ikisi de nil ise gecer.
func eqPtrStr(t *testing.T, label string, want *string, got any) {
	t.Helper()
	var gotStr *string
	switch v := got.(type) {
	case nil:
	case string:
		gotStr = &v
	}
	switch {
	case want == nil && gotStr == nil:
	case want == nil || gotStr == nil:
		t.Errorf("%s: beklenen %v, gelen %v", label, ptrOrNull(want), ptrOrNull(gotStr))
	case *want != *gotStr:
		t.Errorf("%s: beklenen %q, gelen %q", label, *want, *gotStr)
	}
}

func ptrOrNull(p *string) string {
	if p == nil {
		return "null"
	}
	return *p
}

// eqDecimalPtrStr delivered_qty/invoiced_qty/sale_price gibi alanlar icin kullanilir: golden dosyasi
// bu alanlari GIRDININ kendi olcegiyle yazar ("85"), API ise HER ZAMAN sabit olcekle doner ("85.000",
// bkz. docs/api-contract.md). Deger ayni, bicim farkli; sayisal esitlik karsilastirilir.
func eqDecimalPtrStr(t *testing.T, label string, want *string, got any) {
	t.Helper()
	var gotStr *string
	if s, ok := got.(string); ok {
		gotStr = &s
	}
	switch {
	case want == nil && gotStr == nil:
	case want == nil || gotStr == nil:
		t.Errorf("%s: beklenen %v, gelen %v", label, ptrOrNull(want), ptrOrNull(gotStr))
	default:
		wd, err := decimal.NewFromString(*want)
		if err != nil {
			t.Fatalf("%s golden degeri cozumlenemedi %q: %v", label, *want, err)
		}
		gd, err := decimal.NewFromString(*gotStr)
		if err != nil {
			t.Fatalf("%s API degeri cozumlenemedi %q: %v", label, *gotStr, err)
		}
		if !wd.Equal(gd) {
			t.Errorf("%s: beklenen %q, gelen %q (sayisal olarak da farkli)", label, *want, *gotStr)
		}
	}
}

// TestProductionGoldenScenarios golden UR-001/002/003/006 senaryolarini gercek API uzerinden
// (POST /production-orders -> send-materials -> receive) kosturur ve cost-report sonucunu
// docs/golden/production_cases.json'daki expected degerlerle karsilastirir.
func TestProductionGoldenScenarios(t *testing.T) {
	all := loadProductionGolden(t)
	wanted := map[string]bool{"UR-001": true, "UR-002": true, "UR-003": true, "UR-006": true}
	ran := 0
	for _, c := range all {
		if !wanted[c.CaseID] {
			continue
		}
		ran++
		t.Run(c.CaseID, func(t *testing.T) {
			e := newEnv(t)

			// Hammadde kategorileri icin tek seferlik urunler (meat/spice/casing).
			catItem := map[string]string{}
			for _, cat := range []string{"meat", "spice", "casing"} {
				r := e.must(e.do("POST", "/api/v1/items", map[string]any{
					"code": "RAW-" + cat, "name": "raw-" + cat, "item_type": "raw", "base_uom": "kg", "material_category": cat,
				}), 201)
				catItem[cat] = r.str("id")
			}
			supplier := e.must(e.do("POST", "/api/v1/suppliers", map[string]any{
				"code": "SUP-MAT", "name": "Hammadde Tedarikci", "supplier_type": "material"}), 201).str("id")
			mainWh := e.must(e.do("POST", "/api/v1/warehouses", map[string]any{
				"code": "WH-MAIN", "name": "Ana Depo", "warehouse_type": "internal"}), 201).str("id")
			extWh := e.must(e.do("POST", "/api/v1/warehouses", map[string]any{
				"code": "WH-EXT", "name": "Dis Uretici Deposu", "warehouse_type": "external_producer"}), 201).str("id")
			producer := e.must(e.do("POST", "/api/v1/suppliers", map[string]any{
				"code": "SUP-PRD", "name": "Dis Uretici", "supplier_type": "external_producer"}), 201).str("id")

			productBody := map[string]any{"code": "PROD-" + c.CaseID, "name": "Mamul " + c.CaseID, "item_type": "finished", "base_uom": "adet"}
			if c.Input.UnitWeightKg != nil {
				productBody["unit_weight_kg"] = *c.Input.UnitWeightKg
			}
			product := e.must(e.do("POST", "/api/v1/items", productBody), 201).str("id")

			var lines []map[string]any
			for i, l := range c.Input.Lots {
				itemID, ok := catItem[l.Category]
				if !ok {
					t.Fatalf("bilinmeyen kategori: %s", l.Category)
				}
				lotNo := goldenLotNo(c.CaseID, i)
				lot := e.must(e.do("POST", "/api/v1/lots", map[string]any{
					"lot_no": lotNo, "item_id": itemID, "supplier_id": supplier, "warehouse_id": mainWh,
					"received_at": "2026-03-02", "qty": l.Quantity, "unit_price": l.UnitCost,
				}), 201)
				lines = append(lines, map[string]any{"lot_id": lot.str("id"), "qty": l.Quantity})
			}

			orderBody := map[string]any{
				"product_item_id": product, "producer_supplier_id": producer, "external_warehouse_id": extWh,
				"planned_qty":        c.Input.DeliveredQty, // planned=delivered: QTY_VARIANCE golden'da yok, karsilastirmayi bozmasin
				"service_unit_price": c.Input.ServiceUnitPrice,
			}
			if c.Input.SalePrice != nil {
				orderBody["sale_price"] = *c.Input.SalePrice
			}
			order := e.must(e.do("POST", "/api/v1/production-orders", orderBody), 201)
			orderID := order.str("id")

			e.must(e.do("POST", "/api/v1/production-orders/"+orderID+"/send-materials", map[string]any{"lines": lines}), 200)

			receiveBody := map[string]any{"received_qty": c.Input.DeliveredQty}
			if c.Input.InvoicedQty != nil {
				receiveBody["service_invoiced_qty"] = *c.Input.InvoicedQty
			}
			e.must(e.do("POST", "/api/v1/production-orders/"+orderID+"/receive", receiveBody), 200)

			report := e.must(e.do("GET", "/api/v1/production-orders/"+orderID+"/cost-report", nil), 200)
			g := report.JSON

			eqPtrStr(t, "material_cost", rawStr(t, c.Expected, "material_cost"), g["material_cost"])
			eqPtrStr(t, "service_cost", rawStr(t, c.Expected, "service_cost"), g["service_cost"])
			eqPtrStr(t, "total_cost", rawStr(t, c.Expected, "total_cost"), g["total_cost"])
			eqDecimalPtrStr(t, "delivered_qty", rawStr(t, c.Expected, "delivered_qty"), g["received_qty"])
			eqDecimalPtrStr(t, "invoiced_qty", rawStr(t, c.Expected, "invoiced_qty"), g["invoiced_qty"])
			eqPtrStr(t, "ekp_unit", rawStr(t, c.Expected, "ekp_unit"), g["ekp_unit"])
			eqPtrStr(t, "ekp_display", rawStr(t, c.Expected, "ekp_display"), g["ekp_display"])
			eqPtrStr(t, "unit_weight_kg", rawStr(t, c.Expected, "unit_weight_kg"), g["unit_weight_kg"])
			eqPtrStr(t, "output_kg", rawStr(t, c.Expected, "output_kg"), g["output_kg"])
			eqPtrStr(t, "ekp_per_kg", rawStr(t, c.Expected, "ekp_per_kg"), g["ekp_per_kg"])
			eqPtrStr(t, "input_kg_total", rawStr(t, c.Expected, "input_kg_total"), g["input_kg_total"])
			eqPtrStr(t, "yield_input_pct", rawStr(t, c.Expected, "yield_input_pct"), g["yield_input_pct"])
			eqPtrStr(t, "yield_meat_ratio", rawStr(t, c.Expected, "yield_meat_ratio"), g["yield_meat_ratio"])
			eqDecimalPtrStr(t, "sale_price", rawStr(t, c.Expected, "sale_price"), g["sale_price"])
			eqPtrStr(t, "revenue", rawStr(t, c.Expected, "revenue"), g["revenue"])
			eqPtrStr(t, "gross_profit_unit", rawStr(t, c.Expected, "gross_profit_unit"), g["gross_profit_unit"])
			eqPtrStr(t, "gross_profit_total", rawStr(t, c.Expected, "gross_profit_total"), g["gross_profit_total"])
			eqPtrStr(t, "margin_pct", rawStr(t, c.Expected, "margin_pct"), g["margin_pct"])

			wantWarnings := rawStrList(t, c.Expected, "warnings")
			var gotWarnings []string
			for _, w := range g["warnings"].([]any) {
				gotWarnings = append(gotWarnings, w.(string))
			}
			if len(wantWarnings) != len(gotWarnings) {
				t.Fatalf("warnings: beklenen %v, gelen %v", wantWarnings, gotWarnings)
			}
			for i := range wantWarnings {
				if wantWarnings[i] != gotWarnings[i] {
					t.Errorf("warnings[%d]: beklenen %q, gelen %q", i, wantWarnings[i], gotWarnings[i])
				}
			}
		})
	}
	if ran != 4 {
		t.Fatalf("4 golden senaryo (UR-001/002/003/006) beklenirdi, %d calisti", ran)
	}
}

func goldenLotNo(caseID string, i int) string {
	return "LOT-" + caseID + "-" + string(rune('A'+i))
}

// ---------------------------------------------------------------------------
// Durum makinesi, defter, lot_link, hata senaryolari
// ---------------------------------------------------------------------------

// prodFixture URT-2026-045 benzeri bir senaryo icin hazir urun/tedarikci/depo kurar.
type prodFixture struct {
	meatItem, spiceItem, casingItem string
	product, producer, supplier     string
	mainWh, extWh                   string
}

func (e *env) prodFixture() prodFixture {
	e.t.Helper()
	meat := e.must(e.do("POST", "/api/v1/items", map[string]any{
		"code": "ET-DANA", "name": "Dana Eti", "item_type": "raw", "base_uom": "kg", "material_category": "meat"}), 201).str("id")
	spice := e.must(e.do("POST", "/api/v1/items", map[string]any{
		"code": "BAHARAT-SUCUK", "name": "Sucuk Baharatı", "item_type": "raw", "base_uom": "kg", "material_category": "spice"}), 201).str("id")
	casing := e.must(e.do("POST", "/api/v1/items", map[string]any{
		"code": "BAGIRSAK", "name": "Bağırsak", "item_type": "raw", "base_uom": "kg", "material_category": "casing"}), 201).str("id")
	product := e.must(e.do("POST", "/api/v1/items", map[string]any{
		"code": "SUCUK-NARIN-1KG", "name": "Narin Sucuk 1 kg", "item_type": "finished", "base_uom": "adet", "unit_weight_kg": "1.000"}), 201).str("id")
	producer := e.must(e.do("POST", "/api/v1/suppliers", map[string]any{
		"code": "SUP-URT", "name": "Narin Dış Üretim Atölyesi", "supplier_type": "external_producer"}), 201).str("id")
	mainSupplier := e.must(e.do("POST", "/api/v1/suppliers", map[string]any{
		"code": "SUP-ET", "name": "Et Tedarikci", "supplier_type": "material"}), 201).str("id")
	mainWh := e.must(e.do("POST", "/api/v1/warehouses", map[string]any{
		"code": "WH-MAIN", "name": "Ana Depo", "warehouse_type": "internal"}), 201).str("id")
	extWh := e.must(e.do("POST", "/api/v1/warehouses", map[string]any{
		"code": "WH-EXT-01", "name": "Dis Uretici Deposu", "warehouse_type": "external_producer"}), 201).str("id")
	return prodFixture{
		meatItem: meat, spiceItem: spice, casingItem: casing,
		product: product, producer: producer, supplier: mainSupplier,
		mainWh: mainWh, extWh: extWh,
	}
}

func TestProductionOrderFullLifecycle(t *testing.T) {
	e := newEnv(t)
	f := e.prodFixture()

	etLot := e.must(e.do("POST", "/api/v1/lots", map[string]any{
		"lot_no": "ET-2026-001", "item_id": f.meatItem, "supplier_id": f.supplier, "warehouse_id": f.mainWh,
		"received_at": "2026-03-02", "qty": "100.000", "unit_price": "9.7000"}), 201).str("id")
	bhrLot := e.must(e.do("POST", "/api/v1/lots", map[string]any{
		"lot_no": "BHR-2026-001", "item_id": f.spiceItem, "supplier_id": f.supplier, "warehouse_id": f.mainWh,
		"received_at": "2026-03-02", "qty": "2.000", "unit_price": "24.0000"}), 201).str("id")
	bgrLot := e.must(e.do("POST", "/api/v1/lots", map[string]any{
		"lot_no": "BGR-2026-001", "item_id": f.casingItem, "supplier_id": f.supplier, "warehouse_id": f.mainWh,
		"received_at": "2026-03-02", "qty": "5.000", "unit_price": "11.6000"}), 201).str("id")

	order := e.must(e.do("POST", "/api/v1/production-orders", map[string]any{
		"order_no": "URT-2026-045", "product_item_id": f.product, "producer_supplier_id": f.producer,
		"external_warehouse_id": f.extWh, "planned_qty": "85", "service_unit_price": "5.5000", "sale_price": "24.9000",
	}), 201)
	if order.str("status") != "draft" {
		t.Fatalf("draft beklenirdi: %s", order.Raw)
	}
	orderID := order.str("id")

	// --- durum gecisleri ---
	if r := e.do("POST", "/api/v1/production-orders/"+orderID+"/receive", map[string]any{"received_qty": "85"}); r.Status != 409 {
		t.Fatalf("draft -> receive 409 beklenirdi: %d %s", r.Status, r.Raw)
	}
	if r := e.do("POST", "/api/v1/production-orders/"+orderID+"/close", nil); r.Status != 409 {
		t.Fatalf("draft -> close 409 beklenirdi: %d %s", r.Status, r.Raw)
	}

	sent := e.must(e.do("POST", "/api/v1/production-orders/"+orderID+"/send-materials", map[string]any{
		"lines": []map[string]any{
			{"lot_id": etLot, "qty": "100.000"}, {"lot_id": bhrLot, "qty": "2.000"}, {"lot_id": bgrLot, "qty": "5.000"},
		},
	}), 200)
	if sent.str("status") != "materials_sent" {
		t.Fatalf("materials_sent beklenirdi: %s", sent.Raw)
	}

	// defter: sevk sonrasi WH-MAIN'de ET-2026-001 bakiyesi 0, WH-EXT-01'de 100
	mainBal := lotWarehouseBalance(t, etLot, f.mainWh)
	extBal := lotWarehouseBalance(t, etLot, f.extWh)
	if mainBal != "0.000" || extBal != "100.000" {
		t.Fatalf("sevk sonrasi bakiye: WH-MAIN=%s WH-EXT=%s", mainBal, extBal)
	}

	received := e.must(e.do("POST", "/api/v1/production-orders/"+orderID+"/receive", map[string]any{
		"received_qty": "85", "output_lot_no": "SUC-2026-045",
		"service_invoice_no": "URT-2026-045-INV", "service_invoiced_qty": "85",
	}), 200)
	if received.str("status") != "received" || received.str("ekp_unit") != "18.1588" {
		t.Fatalf("received/EKP 18.1588 beklenirdi: %s", received.Raw)
	}
	outputLotID := received.str("output_lot_id")

	// kabul sonrasi: WH-EXT-01'de 0, SUC-2026-045 WH-MAIN'de 85
	if bal := lotWarehouseBalance(t, etLot, f.extWh); bal != "0.000" {
		t.Fatalf("kabul sonrasi WH-EXT bakiyesi 0 olmali: %s", bal)
	}
	if bal := lotWarehouseBalance(t, outputLotID, f.mainWh); bal != "85.000" {
		t.Fatalf("cikti lotu WH-MAIN'de 85 olmali: %s", bal)
	}
	// cikti lotu cihazlara sync pull ile gitmeli (Android satis bu lotu bilmeli)
	if n := count(t, `SELECT COUNT(*) FROM change_log WHERE entity = 'lot' AND entity_id = ?`, outputLotID); n != 1 {
		t.Fatalf("cikti lotu change_log'a 1 kez yazilmali: %d", n)
	}

	// Hammadde degeri mamule aktarilir: hammadde girdisinin tam degeri (material_cost=1076.00)
	// ciktinin toplam maliyetine (total_cost=1543.50) dahildir; aradaki fark (467.50) hizmet
	// bedelidir -- gercek nakit maliyet, envanterden gelmez. Bu yuzden "toplam stok degeri
	// degismez" degil, tam olarak hizmet bedeli kadar ARTAR (bkz. docs/assumptions.md).
	if received.str("material_cost") != "1076.0000" || received.str("total_cost") != "1543.5000" {
		t.Fatalf("material_cost/total_cost: %s", received.Raw)
	}

	// lot_link: 3 parent, miktarlari 100/2/5
	rows := queryLotLinkQtys(t, outputLotID)
	want := map[string]string{etLot: "100.000", bhrLot: "2.000", bgrLot: "5.000"}
	if len(rows) != 3 {
		t.Fatalf("3 lot_link beklenirdi, %d geldi: %v", len(rows), rows)
	}
	for parent, qty := range rows {
		if want[parent] != qty {
			t.Errorf("lot_link %s: beklenen %s, gelen %s", parent, want[parent], qty)
		}
	}

	// aynı emre iki kez receive -> 409
	if r := e.do("POST", "/api/v1/production-orders/"+orderID+"/receive", map[string]any{"received_qty": "85"}); r.Status != 409 {
		t.Fatalf("ikinci receive 409 beklenirdi: %d %s", r.Status, r.Raw)
	}

	closed := e.must(e.do("POST", "/api/v1/production-orders/"+orderID+"/close", nil), 200)
	if closed.str("status") != "closed" {
		t.Fatalf("closed beklenirdi: %s", closed.Raw)
	}
	// closed emre islem -> 409
	for _, path := range []string{"/send-materials", "/receive", "/close"} {
		body := any(nil)
		if path == "/send-materials" {
			body = map[string]any{"lines": []map[string]any{{"lot_id": etLot, "qty": "1.000"}}}
		} else if path == "/receive" {
			body = map[string]any{"received_qty": "1"}
		}
		if r := e.do("POST", "/api/v1/production-orders/"+orderID+path, body); r.Status != 409 {
			t.Errorf("closed -> %s 409 beklenirdi: %d %s", path, r.Status, r.Raw)
		}
	}

	// movements ucnoktasi: bu siparise ait tum hareketler
	moves := e.must(e.do("GET", "/api/v1/production-orders/"+orderID+"/movements", nil), 200)
	items, _ := moves.JSON["items"].([]any)
	if len(items) != 10 { // 3x(send_to_producer+transfer_in) + 3x production_consume + 1x production_in
		t.Fatalf("10 hareket beklenirdi, %d geldi: %s", len(items), moves.Raw)
	}
	// siparise bagli hareketin tek basina geri alinmasi 422 (transferin yarisi geri alinirsa stok yoktan olusur)
	for _, it := range items {
		mv := it.(map[string]any)
		if r := e.do("POST", "/api/v1/stock-movements/"+mv["id"].(string)+"/reverse", nil); r.Status != 422 {
			t.Errorf("%s geri alma 422 beklenirdi: %d %s", mv["movement_type"], r.Status, r.Raw)
		}
	}
	if n := count(t, `SELECT COUNT(*) FROM stock_movement WHERE movement_type = 'reversal'`); n != 0 {
		t.Fatalf("reddedilen geri alma kayit birakmamali: %d", n)
	}

	// cost-report kapandiktan sonra da erisilebilir
	report := e.must(e.do("GET", "/api/v1/production-orders/"+orderID+"/cost-report", nil), 200)
	if report.str("ekp_unit") != "18.1588" || report.str("margin_pct") != "27.07" {
		t.Fatalf("cost-report: %s", report.Raw)
	}
}

func TestProductionBlockedLotCannotBeShipped(t *testing.T) {
	e := newEnv(t)
	f := e.prodFixture()
	lot := e.must(e.do("POST", "/api/v1/lots", map[string]any{
		"lot_no": "ET-2026-002", "item_id": f.meatItem, "supplier_id": f.supplier, "warehouse_id": f.mainWh,
		"received_at": "2026-03-02", "qty": "10.000", "unit_price": "9.7000"}), 201).str("id")
	setLotStatus(t, lot, "blocked")

	order := e.must(e.do("POST", "/api/v1/production-orders", map[string]any{
		"product_item_id": f.product, "producer_supplier_id": f.producer, "external_warehouse_id": f.extWh,
		"planned_qty": "1", "service_unit_price": "1.0000",
	}), 201).str("id")
	r := e.do("POST", "/api/v1/production-orders/"+order+"/send-materials",
		map[string]any{"lines": []map[string]any{{"lot_id": lot, "qty": "5.000"}}})
	if r.Status != 422 {
		t.Fatalf("bloke lot icin 422 beklenirdi: %d %s", r.Status, r.Raw)
	}
}

// Kategorisiz hammadde (ör. seed'deki KUZU-BUTUN) sevkte reddedilmeli; aksi halde receive 422 verir ve
// siparis materials_sent'te, stok dis depoda kilitli kalir.
func TestProductionUncategorizedLotCannotBeShipped(t *testing.T) {
	e := newEnv(t)
	f := e.prodFixture()
	kuzu := e.must(e.do("POST", "/api/v1/items", map[string]any{
		"code": "KUZU-BUTUN", "name": "Bütün Kuzu", "item_type": "raw", "base_uom": "kg"}), 201).str("id")
	lot := e.must(e.do("POST", "/api/v1/lots", map[string]any{
		"lot_no": "KZ-2026-001", "item_id": kuzu, "supplier_id": f.supplier, "warehouse_id": f.mainWh,
		"received_at": "2026-03-02", "qty": "25.000", "unit_price": "11.5000"}), 201).str("id")
	order := e.must(e.do("POST", "/api/v1/production-orders", map[string]any{
		"product_item_id": f.product, "producer_supplier_id": f.producer, "external_warehouse_id": f.extWh,
		"planned_qty": "1", "service_unit_price": "1.0000",
	}), 201).str("id")
	r := e.do("POST", "/api/v1/production-orders/"+order+"/send-materials",
		map[string]any{"lines": []map[string]any{{"lot_id": lot, "qty": "5.000"}}})
	if r.Status != 422 || !strings.Contains(r.str("error", "message"), "kategorisi") {
		t.Fatalf("kategorisiz lot icin 422 beklenirdi: %d %s", r.Status, r.Raw)
	}
	if n := count(t, `SELECT COUNT(*) FROM stock_movement WHERE ref_type = 'production_order' AND ref_id = ?`, order); n != 0 {
		t.Fatalf("reddedilen sevk hareket birakmamali: %d", n)
	}
	if st := e.must(e.do("GET", "/api/v1/production-orders/"+order, nil), 200).str("status"); st != "draft" {
		t.Fatalf("siparis draft kalmali: %s", st)
	}
}

func TestProductionInsufficientBalanceRejected(t *testing.T) {
	e := newEnv(t)
	f := e.prodFixture()
	lot := e.must(e.do("POST", "/api/v1/lots", map[string]any{
		"lot_no": "ET-2026-003", "item_id": f.meatItem, "supplier_id": f.supplier, "warehouse_id": f.mainWh,
		"received_at": "2026-03-02", "qty": "10.000", "unit_price": "9.7000"}), 201).str("id")
	order := e.must(e.do("POST", "/api/v1/production-orders", map[string]any{
		"product_item_id": f.product, "producer_supplier_id": f.producer, "external_warehouse_id": f.extWh,
		"planned_qty": "1", "service_unit_price": "1.0000",
	}), 201).str("id")
	r := e.do("POST", "/api/v1/production-orders/"+order+"/send-materials",
		map[string]any{"lines": []map[string]any{{"lot_id": lot, "qty": "15.000"}}})
	if r.Status != 409 {
		t.Fatalf("yetersiz bakiye icin 409 beklenirdi: %d %s", r.Status, r.Raw)
	}
}

// TestProductionPartialReturn: sevk edilenin bir kismi dis ureticiden iade edilir; maliyet SADECE
// tuketilen (iade edilmeyen) miktar uzerinden hesaplanir ve bakiyeler buna gore gider.
func TestProductionPartialReturn(t *testing.T) {
	e := newEnv(t)
	f := e.prodFixture()
	meatLot := e.must(e.do("POST", "/api/v1/lots", map[string]any{
		"lot_no": "ET-2026-010", "item_id": f.meatItem, "supplier_id": f.supplier, "warehouse_id": f.mainWh,
		"received_at": "2026-03-02", "qty": "50.000", "unit_price": "10.0000"}), 201).str("id")
	spiceLot := e.must(e.do("POST", "/api/v1/lots", map[string]any{
		"lot_no": "BHR-2026-010", "item_id": f.spiceItem, "supplier_id": f.supplier, "warehouse_id": f.mainWh,
		"received_at": "2026-03-02", "qty": "1.000", "unit_price": "20.0000"}), 201).str("id")

	order := e.must(e.do("POST", "/api/v1/production-orders", map[string]any{
		"product_item_id": f.product, "producer_supplier_id": f.producer, "external_warehouse_id": f.extWh,
		"planned_qty": "40", "service_unit_price": "2.0000",
	}), 201).str("id")
	e.must(e.do("POST", "/api/v1/production-orders/"+order+"/send-materials", map[string]any{
		"lines": []map[string]any{{"lot_id": meatLot, "qty": "50.000"}, {"lot_id": spiceLot, "qty": "1.000"}},
	}), 200)

	// 10 kg et iade edilir; tuketilen = 40 kg et + 1 kg baharat
	received := e.must(e.do("POST", "/api/v1/production-orders/"+order+"/receive", map[string]any{
		"received_qty": "40",
		"returns":      []map[string]any{{"lot_id": meatLot, "qty": "10.000"}},
	}), 200)
	// material_cost = round2(40*10.00) + round2(1*20.00) = 400.00 + 20.00 = 420.00
	if received.str("material_cost") != "420.0000" {
		t.Fatalf("material_cost = 420.0000 beklenirdi: %s", received.Raw)
	}
	// iade edilen 10 kg ana depoya geri doner, dis depoda kalan 0 olur
	if bal := lotWarehouseBalance(t, meatLot, f.mainWh); bal != "10.000" {
		t.Fatalf("iadeden sonra WH-MAIN'de 10 kg et beklenirdi: %s", bal)
	}
	if bal := lotWarehouseBalance(t, meatLot, f.extWh); bal != "0.000" {
		t.Fatalf("iadeden sonra WH-EXT'te 0 beklenirdi: %s", bal)
	}

	// iade edilenden daha fazla iade istenirse 422
	order2 := e.must(e.do("POST", "/api/v1/production-orders", map[string]any{
		"product_item_id": f.product, "producer_supplier_id": f.producer, "external_warehouse_id": f.extWh,
		"planned_qty": "1", "service_unit_price": "1.0000",
	}), 201).str("id")
	lot2 := e.must(e.do("POST", "/api/v1/lots", map[string]any{
		"lot_no": "ET-2026-011", "item_id": f.meatItem, "supplier_id": f.supplier, "warehouse_id": f.mainWh,
		"received_at": "2026-03-02", "qty": "5.000", "unit_price": "10.0000"}), 201).str("id")
	e.must(e.do("POST", "/api/v1/production-orders/"+order2+"/send-materials",
		map[string]any{"lines": []map[string]any{{"lot_id": lot2, "qty": "5.000"}}}), 200)
	r := e.do("POST", "/api/v1/production-orders/"+order2+"/receive", map[string]any{
		"received_qty": "1", "returns": []map[string]any{{"lot_id": lot2, "qty": "6.000"}},
	})
	if r.Status != 422 {
		t.Fatalf("sevk edilenden fazla iade icin 422 beklenirdi: %d %s", r.Status, r.Raw)
	}
}

func TestProductionMLUnreachableStillCreatesOrder(t *testing.T) {
	e := newEnvML(t)
	f := e.prodFixture()
	order := e.must(e.do("POST", "/api/v1/production-orders", map[string]any{
		"product_item_id": f.product, "producer_supplier_id": f.producer, "external_warehouse_id": f.extWh,
		"planned_qty": "85", "service_unit_price": "5.5000",
	}), 201)
	if order.str("status") != "draft" {
		t.Fatalf("ML erisilemez olsa da siparis olusturulmali: %s", order.Raw)
	}
	// prediction hic alinamadi: detay yanitinda "prediction" alani olmamali/null olmali
	got := e.must(e.do("GET", "/api/v1/production-orders/"+order.str("id"), nil), 200)
	if _, ok := got.JSON["prediction"]; ok && got.JSON["prediction"] != nil {
		t.Fatalf("ML kapaliyken prediction olmamali: %s", got.Raw)
	}
}

// ---------------------------------------------------------------------------
// DB yardimcilari
// ---------------------------------------------------------------------------

func lotWarehouseBalance(t *testing.T, lotID, warehouseID string) string {
	t.Helper()
	var bal domain.Qty
	err := testDB.QueryRow(`SELECT COALESCE(SUM(qty), 0) FROM stock_movement WHERE lot_id = ? AND warehouse_id = ?`, lotID, warehouseID).Scan(&bal)
	if err != nil {
		t.Fatal(err)
	}
	return bal.StringFixed(domain.QtyScale)
}

func queryLotLinkQtys(t *testing.T, childLotID string) map[string]string {
	t.Helper()
	rows, err := testDB.Query(`SELECT parent_lot_id, qty_from_parent FROM lot_link WHERE child_lot_id = ?`, childLotID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var parent string
		var qty domain.Qty
		if err := rows.Scan(&parent, &qty); err != nil {
			t.Fatal(err)
		}
		out[parent] = qty.StringFixed(domain.QtyScale)
	}
	return out
}

func setLotStatus(t *testing.T, lotID, status string) {
	t.Helper()
	if _, err := testDB.Exec(`UPDATE lot SET status = ? WHERE id = ?`, status, lotID); err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------------------
// Atomik numara uretimi (number_sequence)
// ---------------------------------------------------------------------------

// postOrderNoRaw dogrudan HTTP ile POST /production-orders cagirir; t.Fatal KULLANMAZ (goroutine
// icinden guvenli degildir) -- hata, cagirana donulur.
func postOrderNoRaw(srv *httptest.Server, body any) (string, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/production-orders", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}
	if res.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("status %d: %s", res.StatusCode, string(b))
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return "", err
	}
	orderNo, _ := out["order_no"].(string)
	if orderNo == "" {
		return "", fmt.Errorf("order_no bos: %s", string(b))
	}
	return orderNo, nil
}

// TestProductionOrderNumberingIsAtomicUnderConcurrency: 10 siparis AYNI ANDA (order_no verilmeden)
// olusturulur; number_sequence tablosu + INSERT...ON DUPLICATE KEY UPDATE sayesinde hepsi benzersiz
// URT-<yil>-NNN numarasi almali (eski MAX+1 yaklasiminda bu yaris durumunda numara cakisabilirdi).
func TestProductionOrderNumberingIsAtomicUnderConcurrency(t *testing.T) {
	e := newEnv(t)
	f := e.prodFixture()

	const n = 10
	type result struct {
		orderNo string
		err     error
	}
	results := make(chan result, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			orderNo, err := postOrderNoRaw(e.srv, map[string]any{
				"product_item_id": f.product, "producer_supplier_id": f.producer, "external_warehouse_id": f.extWh,
				"planned_qty": "1", "service_unit_price": "1.0000",
			})
			results <- result{orderNo: orderNo, err: err}
		}()
	}
	wg.Wait()
	close(results)

	seen := map[string]bool{}
	for res := range results {
		if res.err != nil {
			t.Fatal(res.err)
		}
		if !strings.HasPrefix(res.orderNo, "URT-") {
			t.Errorf("beklenmeyen order_no bicimi: %s", res.orderNo)
		}
		if seen[res.orderNo] {
			t.Errorf("tekrarlanan order_no: %s", res.orderNo)
		}
		seen[res.orderNo] = true
	}
	if len(seen) != n {
		t.Fatalf("%d benzersiz numara beklenirdi, %d geldi: %v", n, len(seen), seen)
	}

	// Veritabaninda da dogrula: tam olarak n farkli order_no var.
	if got := count(t, `SELECT COUNT(DISTINCT order_no) FROM production_order WHERE order_no LIKE 'URT-%'`); got != n {
		t.Fatalf("DB'de %d benzersiz order_no beklenirdi, %d bulundu", n, got)
	}
}
