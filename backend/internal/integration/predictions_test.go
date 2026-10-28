// GET /predictions ve GET /predictions/summary: prediction tablosunun TUM satirlarini (production +
// cutting, gecmis dahil) duz listeleyen ve target_type bazinda MAE/MAPE ozetleyen uc noktalar
// (bkz. docs/ml-report.md "ornek tahminler ve dogruluk degerlendirmesi").
package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	apihttp "github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/http"
	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/mlclient"
	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/repo"
)

// newEnvPredictionML: /predict/production SABIT 84.87 adet, /predict/cutting SABIT but=18.00/fire=3.00
// kg doner -- boylece hem production hem cutting tahmin hatasi ELLE (testin kendi bagimsiz hesabiyla)
// dogrulanabilir.
func newEnvPredictionML(t *testing.T) *env {
	t.Helper()
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/predict/production":
			_ = json.NewEncoder(w).Encode(mlclient.ProductionPrediction{
				ModelName: "mock_prod", ModelVersion: "test", Baseline: true, SupplierUsed: "ALL",
				PredictedYieldRatio: "0.8487", PredictedOutputKg: "84.87", PredictedDeliveredQty: "84.87",
			})
		case "/predict/cutting":
			_ = json.NewEncoder(w).Encode(mlclient.CuttingPrediction{
				ModelName: "mock_cut", ModelVersion: "test", Baseline: true, BreedUsed: "lamb",
				CarcassKg: "25.000", SellableKg: "18.00", ExpectedFireKg: "3.00", ExpectedFireRatio: "0.1200",
				Parts: []mlclient.CuttingPredictionPart{{Part: "but", Kg: "18.00", Ratio: "0.8800"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(mock.Close)

	reset(t)
	store := repo.New(testDB).SetMLClient(mlclient.New(mock.URL))
	h := apihttp.NewRouter(apihttp.Deps{DB: testDB, Store: store, CORSOrigins: []string{"*"}})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &env{t: t, srv: srv}
}

// predictionCuttingFixture: tek ana parca (but, %88) + fire (%12) iceren basit bir recete/lot
// (cuttingFixture'daki "CUT-" onekli kodlardan farkli olarak "KUZU-" oneki kullanir, boylece
// partMLKey ML'in dondugu "but"/"fire" anahtarlariyla birebir eslesir). Kendi tedarikci/depo/urununu
// olusturur (prodFixture ile AYNI testte kullanildiginda kod carpismasini onlemek icin -- bkz.
// TestPredictionsListAndSummary).
func (e *env) predictionCuttingFixture() (recipeID, lotID, butItemID, fireItemID string) {
	e.t.Helper()
	item := e.must(e.do("POST", "/api/v1/items", map[string]any{
		"code": "KUZU-BUTUN-PRED", "name": "Butun Kuzu (tahmin testi)", "item_type": "raw", "base_uom": "kg"}), 201).str("id")
	supplier := e.must(e.do("POST", "/api/v1/suppliers", map[string]any{
		"code": "SUP-HAY-PRED", "name": "Yayla Hayvancilik (tahmin testi)", "supplier_type": "livestock"}), 201).str("id")
	warehouse := e.must(e.do("POST", "/api/v1/warehouses", map[string]any{
		"code": "WH-PRED", "name": "Tahmin Testi Deposu", "warehouse_type": "internal"}), 201).str("id")
	but := e.must(e.do("POST", "/api/v1/items", map[string]any{
		"code": "KUZU-BUT", "name": "Kuzu But", "item_type": "cut_part", "base_uom": "kg"}), 201).str("id")
	fire := e.must(e.do("POST", "/api/v1/items", map[string]any{
		"code": "KUZU-FIRE", "name": "Kuzu Fire", "item_type": "waste", "base_uom": "kg"}), 201).str("id")
	recipe := e.must(e.do("POST", "/api/v1/cutting-recipes", map[string]any{
		"code": "REC-PRED-TEST", "name": "Tahmin testi recetesi", "animal_type": "lamb",
		"lines": []map[string]any{
			{"part_item_id": but, "expected_ratio": "0.8800", "reference_sale_price": "16.0000", "is_by_product": false, "is_waste": false},
			{"part_item_id": fire, "expected_ratio": "0.1200", "reference_sale_price": "0.0000", "is_by_product": false, "is_waste": true},
		},
	}), 201)
	lot := e.must(e.do("POST", "/api/v1/lots", map[string]any{
		"lot_no": "KZ-PRED-001", "item_id": item, "supplier_id": supplier, "warehouse_id": warehouse,
		"received_at": "2026-03-02", "qty": "25.000", "unit_price": "11.5000",
	}), 201)
	return recipe.str("id"), lot.str("id"), but, fire
}

func TestPredictionsListAndSummary(t *testing.T) {
	e := newEnvPredictionML(t)

	// ---- Production: draft (tahminsiz EKP) + materials_sent (predicted_ekp_unit dolu) + receive (actual yazilir).
	pf := e.prodFixture()
	etLot := e.must(e.do("POST", "/api/v1/lots", map[string]any{
		"lot_no": "ET-2026-910", "item_id": pf.meatItem, "supplier_id": pf.supplier, "warehouse_id": pf.mainWh,
		"received_at": "2026-03-02", "qty": "100.000", "unit_price": "9.7000"}), 201).str("id")
	order := e.must(e.do("POST", "/api/v1/production-orders", map[string]any{
		"product_item_id": pf.product, "producer_supplier_id": pf.producer, "external_warehouse_id": pf.extWh,
		"planned_qty": "85", "service_unit_price": "5.5000",
	}), 201)
	orderID := order.str("id")
	e.must(e.do("POST", "/api/v1/production-orders/"+orderID+"/send-materials", map[string]any{
		"lines": []map[string]any{{"lot_id": etLot, "qty": "100.000"}},
	}), 200)
	e.must(e.do("POST", "/api/v1/production-orders/"+orderID+"/receive", map[string]any{
		"received_qty": "85", "service_invoice_no": "INV-910", "service_invoiced_qty": "85",
	}), 200)

	// ---- Cutting: tek siparis, close sonrasi actual/error yazilir.
	recipeID, lotID, _, _ := e.predictionCuttingFixture()
	cutOrder := e.must(e.do("POST", "/api/v1/cutting-orders", map[string]any{
		"recipe_id": recipeID, "input_lot_id": lotID, "input_weight_kg": "25", "allocation_method": "value",
		"cut_at": "2026-03-05T10:00:00Z",
	}), 201)
	cutOrderID := cutOrder.str("id")
	but, fire := cutOrder.JSON["outputs"].([]any)[0].(map[string]any)["part_item_id"].(string),
		cutOrder.JSON["outputs"].([]any)[1].(map[string]any)["part_item_id"].(string)
	e.must(e.do("PUT", "/api/v1/cutting-orders/"+cutOrderID+"/outputs", map[string]any{
		"outputs": []any{
			map[string]any{"part_item_id": but, "actual_weight_kg": "22.000"},
			map[string]any{"part_item_id": fire, "actual_weight_kg": "3.000"},
		},
	}), 200)
	e.must(e.do("POST", "/api/v1/cutting-orders/"+cutOrderID+"/close", nil), 200)

	// ---------------------------------------------------------------------
	// GET /predictions: en az 2 production (draft+materials_sent) + 1 cutting satiri olmali.
	// ---------------------------------------------------------------------
	list := e.must(e.do("GET", "/api/v1/predictions", nil), 200)
	items, _ := list.JSON["items"].([]any)
	var prodWithActual, cutRow map[string]any
	prodCount, cutCount := 0, 0
	for _, raw := range items {
		it := raw.(map[string]any)
		switch it["target_type"] {
		case "production":
			prodCount++
			if it["ref_id"] == orderID && it["has_actual"] == true {
				prodWithActual = it
			}
		case "cutting":
			cutCount++
			if it["ref_id"] == cutOrderID {
				cutRow = it
			}
		}
	}
	if prodCount != 2 {
		t.Fatalf("2 production tahmin satiri (draft+materials_sent) beklenirdi, %d geldi: %s", prodCount, list.Raw)
	}
	if cutCount != 1 {
		t.Fatalf("1 cutting tahmin satiri beklenirdi, %d geldi: %s", cutCount, list.Raw)
	}
	if prodWithActual == nil {
		t.Fatalf("actual'li production satiri bulunamadi: %s", list.Raw)
	}
	if prodWithActual["ref_no"] != order.str("order_no") || prodWithActual["stage"] != "materials_sent" {
		t.Fatalf("production satiri ref_no/stage beklenmedik: %v", prodWithActual)
	}
	if prodWithActual["predicted_value"] != "84.87" || prodWithActual["actual_value"] != "85.000" {
		t.Fatalf("predicted/actual_value: %v / %v", prodWithActual["predicted_value"], prodWithActual["actual_value"])
	}
	// elle: |85.000-84.87|=0.130, pct=round2(0.13*100/84.87)=0.15
	if prodWithActual["error_abs"] != "0.130" || prodWithActual["error_pct"] != "0.15" {
		t.Fatalf("production error_abs/pct: %v / %v", prodWithActual["error_abs"], prodWithActual["error_pct"])
	}

	if cutRow == nil {
		t.Fatalf("cutting satiri bulunamadi: %s", list.Raw)
	}
	if cutRow["ref_no"] != cutOrder.str("order_no") || cutRow["has_actual"] != true {
		t.Fatalf("cutting satiri ref_no/has_actual beklenmedik: %v", cutRow)
	}
	// elle: toplam predicted=18.00+3.00=21.000, actual=22.000+3.000=25.000
	if cutRow["predicted_value"] != "21.000" || cutRow["actual_value"] != "25.000" {
		t.Fatalf("cutting predicted/actual_value: %v / %v", cutRow["predicted_value"], cutRow["actual_value"])
	}
	// elle: but diff=4.000 kg (pct 22.22), fire diff=0.000 kg (pct 0.00) -> ortalama 2.000 kg / 11.11 %
	if cutRow["error_abs"] != "2.000" || cutRow["error_pct"] != "11.11" {
		t.Fatalf("cutting error_abs/pct: %v / %v", cutRow["error_abs"], cutRow["error_pct"])
	}
	parts, _ := cutRow["parts"].([]any)
	if len(parts) != 2 {
		t.Fatalf("2 parca kirilimi (but+fire) beklenirdi: %v", parts)
	}
	byKey := map[string]map[string]any{}
	for _, p := range parts {
		pm := p.(map[string]any)
		byKey[pm["part_key"].(string)] = pm
	}
	if byKey["but"]["predicted_kg"] != "18.00" || byKey["but"]["actual_kg"] != "22.000" || byKey["but"]["diff_kg"] != "4.000" || byKey["but"]["diff_pct"] != "22.22" {
		t.Fatalf("but parca kirilimi: %v", byKey["but"])
	}
	if byKey["fire"]["predicted_kg"] != "3.00" || byKey["fire"]["actual_kg"] != "3.000" || byKey["fire"]["diff_kg"] != "0.000" || byKey["fire"]["diff_pct"] != "0.00" {
		t.Fatalf("fire parca kirilimi: %v", byKey["fire"])
	}

	// ---------------------------------------------------------------------
	// Filtreler: target_type, has_actual.
	// ---------------------------------------------------------------------
	onlyCutting := e.must(e.do("GET", "/api/v1/predictions?target_type=cutting", nil), 200)
	if items, _ := onlyCutting.JSON["items"].([]any); len(items) != 1 {
		t.Fatalf("target_type=cutting: 1 kayit beklenirdi, %d", len(items))
	}
	withActual := e.must(e.do("GET", "/api/v1/predictions?target_type=production&has_actual=true", nil), 200)
	if items, _ := withActual.JSON["items"].([]any); len(items) != 1 {
		t.Fatalf("target_type=production&has_actual=true: 1 kayit beklenirdi, %d", len(items))
	}
	withoutActual := e.must(e.do("GET", "/api/v1/predictions?target_type=production&has_actual=false", nil), 200)
	if items, _ := withoutActual.JSON["items"].([]any); len(items) != 1 {
		t.Fatalf("target_type=production&has_actual=false: 1 kayit (draft) beklenirdi, %d", len(items))
	}
	if bad := e.do("GET", "/api/v1/predictions?target_type=nope", nil); bad.Status != 400 {
		t.Errorf("gecersiz target_type 400 olmali: %d", bad.Status)
	}

	// ---------------------------------------------------------------------
	// GET /predictions/summary: sadece has_actual kayitlar uzerinden MAE/MAPE.
	// ---------------------------------------------------------------------
	summary := e.must(e.do("GET", "/api/v1/predictions/summary", nil), 200)
	prod, _ := summary.JSON["production"].(map[string]any)
	cut, _ := summary.JSON["cutting"].(map[string]any)
	if prod["count"] != float64(2) || prod["count_with_actual"] != float64(1) {
		t.Fatalf("production count/count_with_actual: %v", prod)
	}
	if prod["mae"] != "0.1300" || prod["mape"] != "0.15" {
		t.Fatalf("production mae/mape: %v / %v", prod["mae"], prod["mape"])
	}
	if cut["count"] != float64(1) || cut["count_with_actual"] != float64(1) {
		t.Fatalf("cutting count/count_with_actual: %v", cut)
	}
	if cut["mae"] != "2.0000" || cut["mape"] != "11.11" {
		t.Fatalf("cutting mae/mape: %v / %v", cut["mae"], cut["mape"])
	}
}
