// Gorev 2 (parcalama) entegrasyon testleri. Golden karsilastirmasi gercek HTTP uc noktalariyla
// (recete + alis lotu + POST /cutting-orders + PUT .../outputs) yapilir, beklenen degerler
// docs/golden/cutting_cases.json'dan OKUNUR; donen allocation.quantity/allocation.value aynen
// domain.CuttingResult'tir (ayni alanlar), bu yuzden golden'in "expected" govdesiyle BIREBIR
// yapisal karsilastirilir.
package integration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/shopspring/decimal"
)

// ---------------------------------------------------------------------------
// golden
// ---------------------------------------------------------------------------

type cuttingGoldenPart struct {
	Name           string `json:"name"`
	Kg             string `json:"kg"`
	ReferencePrice string `json:"reference_price"`
	IsWaste        bool   `json:"is_waste"`
	IsByProduct    bool   `json:"is_by_product"`
}

type cuttingGoldenInput struct {
	InputWeightKg  string              `json:"input_weight_kg"`
	UnitCostPerKg  *string             `json:"unit_cost_per_kg"`
	DeclaredTotal  *string             `json:"declared_total_cost"`
	CostBasis      *string             `json:"cost_basis"`
	ResolutionNote *string             `json:"resolution_note"`
	Method         string              `json:"method"`
	Parts          []cuttingGoldenPart `json:"parts"`
}

type cuttingGoldenCase struct {
	CaseID      string             `json:"case_id"`
	Description string             `json:"description"`
	Input       cuttingGoldenInput `json:"input"`
	Expected    json.RawMessage    `json:"expected"`
}

func loadCuttingGolden(t *testing.T) []cuttingGoldenCase {
	t.Helper()
	dir := os.Getenv("GOLDEN_DIR")
	if dir == "" {
		dir = filepath.Join("..", "..", "..", "docs", "golden")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "cutting_cases.json"))
	if err != nil {
		t.Fatalf("golden dosya okunamadi: %v", err)
	}
	var cases []cuttingGoldenCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("cutting_cases.json bos")
	}
	return cases
}

// equalRatios N parca icin toplami TAM 1.0000 olan recete oranlari uretir (parcalama maliyet
// hesabinda expected_ratio kullanilmaz -- yalniz domain.CuttingRecipe.Validate() icin gecerli olmasi
// yeter -- kalan fark son satira eklenir, boylece N ne olursa olsun toplam birebir 1.0000'dir).
func equalRatios(n int) []string {
	out := make([]string, n)
	each := decimal.NewFromInt(1).DivRound(decimal.NewFromInt(int64(n)), 4)
	sum := decimal.Zero
	for i := 0; i < n-1; i++ {
		out[i] = each.StringFixed(4)
		sum = sum.Add(each)
	}
	out[n-1] = decimal.NewFromInt(1).Sub(sum).StringFixed(4)
	return out
}

func assertJSONStructEqual(t *testing.T, label string, gotRaw, wantRaw json.RawMessage) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(gotRaw, &g); err != nil {
		t.Fatalf("%s: gelen JSON cozumlenemedi: %v (%s)", label, err, string(gotRaw))
	}
	if err := json.Unmarshal(wantRaw, &w); err != nil {
		t.Fatalf("%s: beklenen JSON cozumlenemedi: %v", label, err)
	}
	if !reflect.DeepEqual(g, w) {
		gp, _ := json.MarshalIndent(g, "", "  ")
		wp, _ := json.MarshalIndent(w, "", "  ")
		t.Errorf("%s: golden ile uyusmuyor\n--- beklenen\n%s\n--- API sonucu\n%s", label, wp, gp)
	}
}

// TestCuttingGoldenScenarios docs/golden/cutting_cases.json'daki TUM senaryolari gercek HTTP
// uc noktalariyla (recete -> alis lotu -> POST /cutting-orders -> PUT .../outputs) kosturur ve
// secilen yontemin (quantity/value) onizlemesini golden'in "expected" govdesiyle karsilastirir.
func TestCuttingGoldenScenarios(t *testing.T) {
	e := newEnv(t)
	supplierID := e.must(e.do("POST", "/api/v1/suppliers", map[string]any{
		"code": "SUP-CUT-GOLDEN", "name": "Golden Tedarikci", "supplier_type": "livestock"}), 201).str("id")
	warehouseID := e.must(e.do("POST", "/api/v1/warehouses", map[string]any{
		"code": "WH-CUT-GOLDEN", "name": "Golden Depo", "warehouse_type": "internal"}), 201).str("id")
	carcassItemID := e.must(e.do("POST", "/api/v1/items", map[string]any{
		"code": "CARCASS-GOLDEN", "name": "Golden Karkas", "item_type": "raw", "base_uom": "kg"}), 201).str("id")

	for ci, c := range loadCuttingGolden(t) {
		c := c
		t.Run(c.CaseID, func(t *testing.T) {
			n := len(c.Input.Parts)
			ratios := equalRatios(n)

			lines := make([]map[string]any, n)
			partItemIDs := make([]string, n)
			for i, p := range c.Input.Parts {
				code := fmt.Sprintf("CUTPART-%d-%d", ci, i)
				item := e.must(e.do("POST", "/api/v1/items", map[string]any{
					"code": code, "name": p.Name, "item_type": "cut_part", "base_uom": "kg"}), 201)
				partItemIDs[i] = item.str("id")
				lines[i] = map[string]any{
					"part_item_id": partItemIDs[i], "expected_ratio": ratios[i],
					"reference_sale_price": p.ReferencePrice, "is_waste": p.IsWaste, "is_by_product": p.IsByProduct,
				}
			}
			recipe := e.must(e.do("POST", "/api/v1/cutting-recipes", map[string]any{
				"code": fmt.Sprintf("REC-GOLDEN-%d", ci), "name": c.CaseID, "animal_type": "golden", "lines": lines,
			}), 201)

			lotBody := map[string]any{
				"lot_no": fmt.Sprintf("CARCASS-%d", ci), "item_id": carcassItemID, "supplier_id": supplierID,
				"warehouse_id": warehouseID, "received_at": "2026-03-02", "qty": c.Input.InputWeightKg,
				"unit_price": *c.Input.UnitCostPerKg,
			}
			if c.Input.DeclaredTotal != nil {
				lotBody["declared_line_total"] = *c.Input.DeclaredTotal
			}
			lot := e.must(e.do("POST", "/api/v1/lots", lotBody), 201)

			orderBody := map[string]any{
				"recipe_id": recipe.str("id"), "input_lot_id": lot.str("id"), "input_weight_kg": c.Input.InputWeightKg,
				"allocation_method": c.Input.Method, "cut_at": "2026-03-05T10:00:00Z",
			}
			if c.Input.CostBasis != nil {
				orderBody["cost_basis"] = *c.Input.CostBasis
			}
			if c.Input.ResolutionNote != nil {
				orderBody["resolution_note"] = *c.Input.ResolutionNote
			}
			order := e.must(e.do("POST", "/api/v1/cutting-orders", orderBody), 201)

			outputs := make([]map[string]any, n)
			for i, p := range c.Input.Parts {
				outputs[i] = map[string]any{"part_item_id": partItemIDs[i], "actual_weight_kg": p.Kg}
			}
			updated := e.must(e.do("PUT", "/api/v1/cutting-orders/"+order.str("id")+"/outputs", map[string]any{"outputs": outputs}), 200)

			allocation, _ := updated.JSON["allocation"].(map[string]any)
			got, ok := allocation[c.Input.Method]
			if !ok {
				t.Fatalf("allocation.%s eksik: %s", c.Input.Method, updated.Raw)
			}
			gotRaw, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			assertJSONStructEqual(t, c.CaseID, gotRaw, c.Expected)

			// Taze hesap: GET ayni sonucu dondurmeli (DB'de saklanmayan alanlar her istekte yeniden hesaplanir).
			again := e.must(e.do("GET", "/api/v1/cutting-orders/"+order.str("id"), nil), 200)
			allocation2, _ := again.JSON["allocation"].(map[string]any)
			got2Raw, _ := json.Marshal(allocation2[c.Input.Method])
			assertJSONStructEqual(t, c.CaseID+"/GET", got2Raw, c.Expected)
		})
	}
}

// ---------------------------------------------------------------------------
// Yasam dongusu + izlenebilirlik entegrasyonu
// ---------------------------------------------------------------------------

// cuttingFixture: KUZU benzeri 7 parcali bir recete + 25 kg'lik (mismatch'li) bir alis lotu kurar.
type cuttingFixture struct {
	fixture
	recipeID    string
	partItemIDs map[string]string // "But" -> item id
	lotID       string
}

func (e *env) cuttingFixture() cuttingFixture {
	e.t.Helper()
	f := e.fixture() // item KUZU-BUTUN, supplier SUP-HAY, warehouse WH-MAIN

	parts := []struct {
		name, price string
		isWaste     bool
	}{
		{"But", "16.00", false}, {"Kol", "15.00", false}, {"Sirt", "18.00", false},
		{"Kaburga", "12.00", false}, {"Gerdan", "10.00", false}, {"Kiyma", "11.00", false},
		{"Fire", "0.00", true},
	}
	ratios := []string{"0.3000", "0.1800", "0.1000", "0.1200", "0.0800", "0.1000", "0.1200"}
	lines := make([]map[string]any, len(parts))
	ids := map[string]string{}
	for i, p := range parts {
		item := e.must(e.do("POST", "/api/v1/items", map[string]any{
			"code": "CUT-" + p.name, "name": "Kuzu " + p.name, "item_type": "cut_part", "base_uom": "kg"}), 201)
		ids[p.name] = item.str("id")
		lines[i] = map[string]any{
			"part_item_id": item.str("id"), "expected_ratio": ratios[i],
			"reference_sale_price": p.price, "is_waste": p.isWaste, "is_by_product": false,
		}
	}
	recipe := e.must(e.do("POST", "/api/v1/cutting-recipes", map[string]any{
		"code": "REC-TEST-KUZU", "name": "Test kuzu receteesi", "animal_type": "lamb", "lines": lines,
	}), 201)

	lot := e.must(e.do("POST", "/api/v1/lots", f.lotBody("KZ-TEST-001", "25.000", "11.5000", sp("351.0000"))), 201)

	return cuttingFixture{fixture: f, recipeID: recipe.str("id"), partItemIDs: ids, lotID: lot.str("id")}
}

func (cf cuttingFixture) outputsBody(but, kol, sirt, kaburga, gerdan, kiyma, fire string) map[string]any {
	mk := func(name, kg string) map[string]any {
		return map[string]any{"part_item_id": cf.partItemIDs[name], "actual_weight_kg": kg}
	}
	return map[string]any{"outputs": []any{
		mk("But", but), mk("Kol", kol), mk("Sirt", sirt), mk("Kaburga", kaburga), mk("Gerdan", gerdan), mk("Kiyma", kiyma), mk("Fire", fire),
	}}
}

func TestCuttingOrderFullLifecycleAndTraceability(t *testing.T) {
	e := newEnv(t)
	cf := e.cuttingFixture()
	cust := e.must(e.do("POST", "/api/v1/customers", map[string]any{"code": "MKT-CUT", "name": "Market Cut", "customer_type": "market"}), 201)

	order := e.must(e.do("POST", "/api/v1/cutting-orders", map[string]any{
		"recipe_id": cf.recipeID, "input_lot_id": cf.lotID, "input_weight_kg": "25", "allocation_method": "value",
		"cut_at": "2026-03-05T10:00:00Z", "cost_basis": "computed",
		"resolution_note": "Birim fiyat ve agirlik tutarli; hatali olan beyan edilen toplam.",
	}), 201)
	if order.str("status") != "draft" || order.str("total_input_cost") != "287.5000" {
		t.Fatalf("beklenmeyen siparis: %s", order.Raw)
	}

	updated := e.must(e.do("PUT", "/api/v1/cutting-orders/"+order.str("id")+"/outputs",
		cf.outputsBody("7.5", "4.5", "2.5", "3.0", "2.0", "2.5", "3.0")), 200)
	if updated.JSON["closable"] != true || updated.str("balance_diff_kg") != "0.000" {
		t.Fatalf("dengeli girdi sonrasi closable=true beklenirdi: %s", updated.Raw)
	}

	closed := e.must(e.do("POST", "/api/v1/cutting-orders/"+order.str("id")+"/close", nil), 200)
	if closed.str("status") != "closed" {
		t.Fatalf("kapanis basarisiz: %s", closed.Raw)
	}
	outputs, _ := closed.JSON["outputs"].([]any)
	if len(outputs) != 7 {
		t.Fatalf("7 parca (6+fire) beklenirdi: %d", len(outputs))
	}

	// Giris lotu bakiyesi 0; her cikti lotu kendi agirliginda.
	var inputOnHand string
	if err := testDB.QueryRow(`SELECT COALESCE(SUM(qty), 0) FROM stock_movement WHERE lot_id = ?`, cf.lotID).Scan(&inputOnHand); err != nil || inputOnHand != "0.000" {
		t.Fatalf("giris lotu bakiyesi 0 olmali: %q %v", inputOnHand, err)
	}
	var butLotID, kolLotID string
	totalOutputValue := decimal.Zero
	for _, raw := range outputs {
		o := raw.(map[string]any)
		code := o["part_item_code"].(string)
		if o["output_lot_id"] == nil {
			t.Fatalf("parca %s icin cikti lotu olusmali: %v", code, o)
		}
		lotID := o["output_lot_id"].(string)
		if code == "CUT-But" {
			butLotID = lotID
		}
		if code == "CUT-Kol" {
			kolLotID = lotID
		}
		got := e.must(e.do("GET", "/api/v1/lots/"+lotID, nil), 200)
		if got.str("qty_on_hand") != o["actual_weight_kg"] {
			t.Fatalf("parca %s bakiyesi fiili agirliga esit olmali: %s != %v", code, got.str("qty_on_hand"), o["actual_weight_kg"])
		}
		alloc := decimal.RequireFromString(o["allocated_cost"].(string))
		totalOutputValue = totalOutputValue.Add(alloc)
	}
	if totalOutputValue.StringFixed(2) != "287.50" {
		t.Fatalf("toplam cikti degeri giris maliyetine (287.50) esit olmali: %s", totalOutputValue.StringFixed(2))
	}
	if butLotID == "" || kolLotID == "" {
		t.Fatal("But/Kol cikti lotlari bulunamadi")
	}

	// lot_link: 7 child (6 parca + fire), miktarlar fiili agirliklara esit.
	if n := count(t, `SELECT COUNT(*) FROM lot_link WHERE parent_lot_id = ? AND process_type = 'cutting' AND process_id = ?`,
		cf.lotID, order.str("id")); n != 7 {
		t.Fatalf("7 lot_link beklenirdi, %d geldi", n)
	}

	// Parcalama -> satis zinciri: But lotunu MKT-CUT'a sat.
	sale := e.must(e.do("POST", "/api/v1/sales-orders", map[string]any{
		"customer_id": cust.str("id"), "warehouse_id": cf.warehouse, "sold_at": "2026-03-06T09:00:00Z",
		"lines": []any{map[string]any{"item_id": cf.partItemIDs["But"], "lot_id": butLotID, "qty": "5.000", "unit_price": "16.0000"}},
	}), 201)

	// trace-forward (giris lotu): parca lotlari VE But'tan yapilan satis gorunmeli.
	fwd := e.must(e.do("GET", "/api/v1/lots/"+cf.lotID+"/trace-forward", nil), 200)
	nodes, _ := fwd.JSON["nodes"].([]any)
	if len(nodes) != 8 { // kok + 7 parca
		t.Fatalf("8 dugum (kok+7 parca) beklenirdi: %d", len(nodes))
	}
	var sawSaleOnBut bool
	for _, raw := range nodes {
		n := raw.(map[string]any)
		if n["lot_id"] == butLotID {
			sales, _ := n["sales"].([]any)
			for _, sraw := range sales {
				s := sraw.(map[string]any)
				if s["order_no"] == sale.str("order_no") {
					sawSaleOnBut = true
				}
			}
		}
	}
	if !sawSaleOnBut {
		t.Fatalf("But lotundaki satis trace-forward'da gorunmeli: %s", fwd.Raw)
	}

	// trace-backward (Kol parca lotu): giris lotuna ve tedarikcisine ulasmali.
	bwd := e.must(e.do("GET", "/api/v1/lots/"+kolLotID+"/trace-backward", nil), 200)
	bnodes, _ := bwd.JSON["nodes"].([]any)
	var sawInputLot bool
	for _, raw := range bnodes {
		n := raw.(map[string]any)
		if n["lot_id"] == cf.lotID {
			sawInputLot = true
			if n["supplier_name"] != "Yayla Hayvancılık" {
				t.Errorf("tedarikci adi beklenmedik: %v", n["supplier_name"])
			}
		}
	}
	if !sawInputLot {
		t.Fatalf("Kol lotundan geri izlemede giris lotu bulunmali: %s", bwd.Raw)
	}

	// Geri cagirma: giris lotu icin acilinca TUM parca lotlari (7) + giris lotu bloke olmali.
	recall := e.must(e.do("POST", "/api/v1/recalls", map[string]any{"root_lot_id": cf.lotID, "reason": "kalite sikayeti"}), 201)
	if recall.JSON["affected_lot_count"].(float64) != 8 {
		t.Fatalf("8 etkilenen lot (kok+7 parca) beklenirdi: %v", recall.JSON["affected_lot_count"])
	}
	for _, lotID := range []string{cf.lotID, butLotID, kolLotID} {
		got := e.must(e.do("GET", "/api/v1/lots/"+lotID, nil), 200)
		if got.str("status") != "recalled" {
			t.Errorf("lot %s recalled olmali: %s", lotID, got.str("status"))
		}
	}
}

// ---------------------------------------------------------------------------
// Hata senaryolari
// ---------------------------------------------------------------------------

func TestCuttingOrderBlockedLotCannotBeCut(t *testing.T) {
	e := newEnv(t)
	cf := e.cuttingFixture()
	e.must(e.do("POST", "/api/v1/lots/"+cf.lotID+"/block", map[string]any{"reason": "test"}), 200)

	r := e.do("POST", "/api/v1/cutting-orders", map[string]any{
		"recipe_id": cf.recipeID, "input_lot_id": cf.lotID, "input_weight_kg": "25", "allocation_method": "quantity",
		"cut_at": "2026-03-05T10:00:00Z", "cost_basis": "computed", "resolution_note": "not",
	})
	if r.Status != 422 {
		t.Fatalf("bloke lot icin 422 beklenirdi: %d %s", r.Status, r.Raw)
	}
}

func TestCuttingOrderMismatchRequiresResolutionToClose(t *testing.T) {
	e := newEnv(t)
	cf := e.cuttingFixture()
	// cost_basis/resolution_note VERILMEDEN siparis olustur: MISMATCH_RESOLUTION_MISSING, closable=false.
	order := e.must(e.do("POST", "/api/v1/cutting-orders", map[string]any{
		"recipe_id": cf.recipeID, "input_lot_id": cf.lotID, "input_weight_kg": "25", "allocation_method": "quantity",
		"cut_at": "2026-03-05T10:00:00Z",
	}), 201)
	if order.JSON["closable"] != false {
		t.Fatalf("cozumlenmemis uyusmazlikta closable=false beklenirdi: %s", order.Raw)
	}
	e.must(e.do("PUT", "/api/v1/cutting-orders/"+order.str("id")+"/outputs",
		cf.outputsBody("7.5", "4.5", "2.5", "3.0", "2.0", "2.5", "3.0")), 200)

	r := e.do("POST", "/api/v1/cutting-orders/"+order.str("id")+"/close", nil)
	if r.Status != 422 {
		t.Fatalf("cozumlenmemis uyusmazlikla kapanis 422 beklenirdi: %d %s", r.Status, r.Raw)
	}
}

func TestCuttingOrderCloseRequiresWeightBalance(t *testing.T) {
	e := newEnv(t)
	cf := e.cuttingFixture()
	order := e.must(e.do("POST", "/api/v1/cutting-orders", map[string]any{
		"recipe_id": cf.recipeID, "input_lot_id": cf.lotID, "input_weight_kg": "25", "allocation_method": "quantity",
		"cut_at": "2026-03-05T10:00:00Z", "cost_basis": "computed", "resolution_note": "not",
	}), 201)
	// Fire kaydedilmemis: 22 kg parca + 0 kg fire = 3 kg fark (dokumandaki senaryo).
	updated := e.must(e.do("PUT", "/api/v1/cutting-orders/"+order.str("id")+"/outputs",
		cf.outputsBody("7.5", "4.5", "2.5", "3.0", "2.0", "2.5", "0")), 200)
	if updated.JSON["closable"] != false {
		t.Fatalf("3 kg dengesizlikte closable=false beklenirdi: %s", updated.Raw)
	}
	warnings, _ := updated.JSON["warnings"].([]any)
	if !containsStr(warnings, "WEIGHT_IMBALANCE") {
		t.Fatalf("WEIGHT_IMBALANCE uyarisi beklenirdi: %v", warnings)
	}

	r := e.do("POST", "/api/v1/cutting-orders/"+order.str("id")+"/close", nil)
	if r.Status != 422 {
		t.Fatalf("dengesiz parcalamada kapanis 422 beklenirdi: %d %s", r.Status, r.Raw)
	}
}

func TestCuttingOrderClosedOrderIsImmutable(t *testing.T) {
	e := newEnv(t)
	cf := e.cuttingFixture()
	order := e.must(e.do("POST", "/api/v1/cutting-orders", map[string]any{
		"recipe_id": cf.recipeID, "input_lot_id": cf.lotID, "input_weight_kg": "25", "allocation_method": "quantity",
		"cut_at": "2026-03-05T10:00:00Z", "cost_basis": "computed", "resolution_note": "not",
	}), 201)
	e.must(e.do("PUT", "/api/v1/cutting-orders/"+order.str("id")+"/outputs",
		cf.outputsBody("7.5", "4.5", "2.5", "3.0", "2.0", "2.5", "3.0")), 200)
	e.must(e.do("POST", "/api/v1/cutting-orders/"+order.str("id")+"/close", nil), 200)

	if r := e.do("PUT", "/api/v1/cutting-orders/"+order.str("id")+"/outputs", cf.outputsBody("7.5", "4.5", "2.5", "3.0", "2.0", "2.5", "3.0")); r.Status != 409 {
		t.Errorf("kapali siparişte outputs guncelleme 409 beklenirdi: %d %s", r.Status, r.Raw)
	}
	if r := e.do("POST", "/api/v1/cutting-orders/"+order.str("id")+"/close", nil); r.Status != 409 {
		t.Errorf("kapali siparişi tekrar kapatmak 409 beklenirdi: %d %s", r.Status, r.Raw)
	}
}

func TestCuttingRecipeRatioValidation(t *testing.T) {
	e := newEnv(t)
	item := e.must(e.do("POST", "/api/v1/items", map[string]any{"code": "CUT-X", "name": "X", "item_type": "cut_part", "base_uom": "kg"}), 201)
	r := e.do("POST", "/api/v1/cutting-recipes", map[string]any{
		"code": "REC-BAD", "name": "Bad", "animal_type": "lamb",
		"lines": []any{map[string]any{"part_item_id": item.str("id"), "expected_ratio": "0.5000", "reference_sale_price": "10.00", "is_waste": false, "is_by_product": false}},
	})
	if r.Status != 422 {
		t.Fatalf("oran toplami 1.0000 degilse 422 beklenirdi: %d %s", r.Status, r.Raw)
	}
}

func containsStr(list []any, want string) bool {
	for _, v := range list {
		if s, ok := v.(string); ok && s == want {
			return true
		}
	}
	return false
}
