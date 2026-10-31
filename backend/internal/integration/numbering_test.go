package integration

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Numara elle verilince (seed, "Emir No" alani) sayac ilerlemez; bos birakilan alan kullanilmis
// numarayi uretip 409 vermemeli, siradaki bos numaraya gecmeli.
func TestGeneratedNumberSkipsManuallyUsedOnes(t *testing.T) {
	e := newEnv(t)
	cf := e.cuttingFixture()
	year := time.Now().UTC().Year()
	body := func(orderNo string) map[string]any {
		b := map[string]any{
			"recipe_id": cf.recipeID, "input_lot_id": cf.lotID, "input_weight_kg": "1", "allocation_method": "value",
			"cut_at": "2026-03-05T10:00:00Z", "cost_basis": "computed", "resolution_note": "not",
		}
		if orderNo != "" {
			b["order_no"] = orderNo
		}
		return b
	}

	e.must(e.do("POST", "/api/v1/cutting-orders", body(fmt.Sprintf("CUT-%d-001", year))), 201)
	e.must(e.do("POST", "/api/v1/cutting-orders", body(fmt.Sprintf("CUT-%d-002", year))), 201)

	generated := e.must(e.do("POST", "/api/v1/cutting-orders", body("")), 201)
	if want := fmt.Sprintf("CUT-%d-003", year); generated.str("order_no") != want {
		t.Fatalf("order_no=%s beklenirdi: %s", want, generated.Raw)
	}
}

// Yeniden isleme formu lotlari secilen depoya gore listeler: baska depodaki veya tukenen lot gelmez,
// gelen lotta depo kirilimi vardir.
func TestLotListFiltersByWarehouseStock(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	lot := e.must(e.do("POST", "/api/v1/lots", f.lotBody("KZ-DEPO-1", "25.000", "11.5000", nil)), 201)
	other := e.must(e.do("POST", "/api/v1/warehouses", map[string]any{
		"code": "WH-OTHER", "name": "Diger Depo", "warehouse_type": "internal"}), 201)

	here := e.must(e.do("GET", "/api/v1/lots?warehouse_id="+f.warehouse, nil), 200)
	items, _ := here.JSON["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("bu depoda 1 lot beklenirdi: %s", here.Raw)
	}
	first, _ := items[0].(map[string]any)
	balances, _ := first["balances"].([]any)
	if first["id"] != lot.str("id") || len(balances) != 1 {
		t.Fatalf("lot ve depo kirilimi beklenirdi: %s", here.Raw)
	}

	empty := e.must(e.do("GET", "/api/v1/lots?warehouse_id="+other.str("id"), nil), 200)
	if rows, _ := empty.JSON["items"].([]any); len(rows) != 0 {
		t.Fatalf("diger depoda lot olmamali: %s", empty.Raw)
	}
}

// Elle verilen numara gercekten kullaniliyorsa 409 doner; mesajda tablo/indeks adi yer almaz.
func TestDuplicateNumberMessageHidesDatabaseDetails(t *testing.T) {
	e := newEnv(t)
	cf := e.cuttingFixture()
	body := map[string]any{
		"order_no": "CUT-TEKRAR", "recipe_id": cf.recipeID, "input_lot_id": cf.lotID, "input_weight_kg": "1",
		"allocation_method": "value", "cut_at": "2026-03-05T10:00:00Z", "cost_basis": "computed", "resolution_note": "not",
	}
	e.must(e.do("POST", "/api/v1/cutting-orders", body), 201)

	r := e.do("POST", "/api/v1/cutting-orders", body)
	if r.Status != 409 {
		t.Fatalf("409 beklenirdi: %d %s", r.Status, r.Raw)
	}
	if !strings.Contains(r.Raw, "CUT-TEKRAR") || strings.Contains(r.Raw, "Duplicate") || strings.Contains(r.Raw, "uq_") {
		t.Fatalf("mesaj numarayi gostermeli, veritabani ayrintisini gostermemeli: %s", r.Raw)
	}
}
