// Hibrit EKP tahmini testi: ML servisinin sabit (mock) predicted_delivered_qty donmesini saglayip
// predicted_ekp_unit'in formule gore ELLE hesaplanan degerle birebir ayni oldugunu dogrular
// (bkz. docs/assumptions.md, "Hibrit EKP tahmini").
package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
	apihttp "github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/http"
	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/mlclient"
	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/repo"
)

// newEnvMockML ML servisini TAKLIT eden gercek bir HTTP sunucuya baglar: /predict/production her
// zaman sabit predictedQty doner, boylece hibrit EKP formulu deterministik test edilebilir.
func newEnvMockML(t *testing.T, predictedQty string) *env {
	t.Helper()
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/predict/production" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mlclient.ProductionPrediction{
			ModelName: "mock_model", ModelVersion: "test", Baseline: true, SupplierUsed: "ALL",
			PredictedYieldRatio: "0.8487", PredictedOutputKg: predictedQty, PredictedDeliveredQty: predictedQty,
		})
	}))
	t.Cleanup(mock.Close)

	reset(t)
	store := repo.New(testDB).SetMLClient(mlclient.New(mock.URL))
	h := apihttp.NewRouter(apihttp.Deps{DB: testDB, Store: store, CORSOrigins: []string{"*"}})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &env{t: t, srv: srv}
}

// TestProductionHybridEkpPrediction: predicted_qty=84.87 (URT-2026-045/UR-001 ile ayni hammaddeler,
// material_cost=1076.00, service_unit_price=5.50) sabitlenince:
//  1. draft asamasinda predicted_ekp_unit NULL'dur (malzeme maliyeti henuz bilinmez),
//  2. send-materials sonrasi (materials_sent asamasi) predicted_ekp_unit, formule gore ELLE
//     hesaplanan degere ESITTIR: (material_cost + round2(qty*fiyat)) / qty, 4 ondalik,
//  3. receive sonrasi ekp_error_abs/pct DOGRU ISARETLE (actual-predicted) yazilir.
func TestProductionHybridEkpPrediction(t *testing.T) {
	const predictedQtyStr = "84.87"
	e := newEnvMockML(t, predictedQtyStr)
	f := e.prodFixture()

	etLot := e.must(e.do("POST", "/api/v1/lots", map[string]any{
		"lot_no": "ET-2026-900", "item_id": f.meatItem, "supplier_id": f.supplier, "warehouse_id": f.mainWh,
		"received_at": "2026-03-02", "qty": "100.000", "unit_price": "9.7000"}), 201).str("id")
	bhrLot := e.must(e.do("POST", "/api/v1/lots", map[string]any{
		"lot_no": "BHR-2026-900", "item_id": f.spiceItem, "supplier_id": f.supplier, "warehouse_id": f.mainWh,
		"received_at": "2026-03-02", "qty": "2.000", "unit_price": "24.0000"}), 201).str("id")
	bgrLot := e.must(e.do("POST", "/api/v1/lots", map[string]any{
		"lot_no": "BGR-2026-900", "item_id": f.casingItem, "supplier_id": f.supplier, "warehouse_id": f.mainWh,
		"received_at": "2026-03-02", "qty": "5.000", "unit_price": "11.6000"}), 201).str("id")

	order := e.must(e.do("POST", "/api/v1/production-orders", map[string]any{
		"product_item_id": f.product, "producer_supplier_id": f.producer, "external_warehouse_id": f.extWh,
		"planned_qty": "85", "service_unit_price": "5.5000",
	}), 201)
	orderID := order.str("id")

	// 1) draft asamasinda tahmin alinmis olmali (ML mock acik); predicted_ekp_unit bu asamada NULL.
	draft := e.must(e.do("GET", "/api/v1/production-orders/"+orderID, nil), 200)
	draftPred, _ := draft.JSON["prediction"].(map[string]any)
	if draftPred == nil {
		t.Fatalf("draft asamasinda tahmin alinmis olmaliydi: %s", draft.Raw)
	}
	if draftPred["stage"] != "draft" {
		t.Fatalf("stage=draft beklenirdi: %v", draftPred["stage"])
	}
	if draftPred["predicted_ekp_unit"] != nil {
		t.Fatalf("draft asamasinda predicted_ekp_unit NULL olmali (malzeme maliyeti henuz bilinmez): %v", draftPred["predicted_ekp_unit"])
	}

	// 2) send-materials sonrasi: materials_sent asamasi tahmini, formule gore elle hesaplanan degerle ayni.
	sent := e.must(e.do("POST", "/api/v1/production-orders/"+orderID+"/send-materials", map[string]any{
		"lines": []map[string]any{
			{"lot_id": etLot, "qty": "100.000"}, {"lot_id": bhrLot, "qty": "2.000"}, {"lot_id": bgrLot, "qty": "5.000"},
		},
	}), 200)
	pred, _ := sent.JSON["prediction"].(map[string]any)
	if pred == nil {
		t.Fatalf("send-materials sonrasi tahmin (materials_sent asamasi) olmaliydi: %s", sent.Raw)
	}
	if pred["stage"] != "materials_sent" {
		t.Fatalf("en guncel tahmin stage=materials_sent olmali (onceki uzerine yazilmaz, yeni satir en guncelidir): %v", pred["stage"])
	}
	if pred["predicted_delivered_qty"] != predictedQtyStr {
		t.Fatalf("predicted_delivered_qty = %s beklenirdi, %v geldi", predictedQtyStr, pred["predicted_delivered_qty"])
	}

	// Elle hesapla (testin kendi bagimsiz hesabi -- uygulama kodunu cagirmaz):
	//   material_cost = round2(100*9.70) + round2(2*24.00) + round2(5*11.60) = 970.00+48.00+58.00 = 1076.00
	materialCost := decimal.RequireFromString("1076.00")
	predictedQty := decimal.RequireFromString(predictedQtyStr)
	serviceUnitPrice := decimal.RequireFromString("5.5000")
	wantServiceCost := domain.LineTotal(predictedQty, serviceUnitPrice) // round2(84.87*5.50) = 466.79
	wantEkp := materialCost.Add(wantServiceCost).DivRound(predictedQty, domain.AmountScale)

	gotEkp, ok := pred["predicted_ekp_unit"].(string)
	if !ok {
		t.Fatalf("predicted_ekp_unit string olmali: %v", pred["predicted_ekp_unit"])
	}
	if gotEkp != wantEkp.StringFixed(domain.AmountScale) {
		t.Fatalf("predicted_ekp_unit: elle hesaplanan %s, API %s", wantEkp.StringFixed(domain.AmountScale), gotEkp)
	}

	// 3) receive sonrasi: ekp_error_abs/pct dogru isaretle.
	received := e.must(e.do("POST", "/api/v1/production-orders/"+orderID+"/receive", map[string]any{
		"received_qty": "85", "service_invoice_no": "INV-900", "service_invoiced_qty": "85",
	}), 200)
	actualEkp := decimal.RequireFromString(received.str("ekp_unit")) // golden UR-001 ile ayni girdiler: 18.1588

	finalPred, _ := received.JSON["prediction"].(map[string]any)
	if finalPred == nil {
		t.Fatalf("receive sonrasi prediction olmali: %s", received.Raw)
	}
	actualEkpView, ok := finalPred["actual_ekp_unit"].(string)
	if !ok || actualEkpView != received.str("ekp_unit") {
		t.Fatalf("actual_ekp_unit = %s beklenirdi, %v geldi", received.str("ekp_unit"), finalPred["actual_ekp_unit"])
	}

	wantErrAbs := actualEkp.Sub(wantEkp) // isaretli: actual - predicted
	gotErrAbs, ok := finalPred["ekp_error_abs"].(string)
	if !ok {
		t.Fatalf("ekp_error_abs string olmali: %v", finalPred["ekp_error_abs"])
	}
	if gotErrAbs != wantErrAbs.StringFixed(domain.AmountScale) {
		t.Fatalf("ekp_error_abs: elle hesaplanan %s (isaretli), API %s", wantErrAbs.StringFixed(domain.AmountScale), gotErrAbs)
	}
	// Isaret kontrolu: actual EKP, predicted EKP'den farkliysa ekp_error_abs'in isareti
	// (actual-predicted farkinin isaretiyle) birebir eslesmeli.
	if wantSign, gotSign := wantErrAbs.Sign(), decimal.RequireFromString(gotErrAbs).Sign(); wantSign != gotSign {
		t.Fatalf("ekp_error_abs isareti yanlis: beklenen isaret %d, gelen %d (deger %s)", wantSign, gotSign, gotErrAbs)
	}

	wantErrPct := domain.Round2(wantErrAbs.Abs().Mul(decimal.NewFromInt(100)).Div(wantEkp))
	gotErrPct, ok := finalPred["ekp_error_pct"].(string)
	if !ok || gotErrPct != wantErrPct.StringFixed(2) {
		t.Fatalf("ekp_error_pct: elle hesaplanan %s, API %v", wantErrPct.StringFixed(2), finalPred["ekp_error_pct"])
	}
}

// TestProductionDraftOnlyPredictionHasNoEkp: materials_sent'e hic gecmeyen (hammadde sevk edilmeyen)
// bir siparis icin tek tahmin draft asamasidir; predicted_ekp_unit her zaman NULL kalir.
func TestProductionDraftOnlyPredictionHasNoEkp(t *testing.T) {
	e := newEnvMockML(t, "84.87")
	f := e.prodFixture()
	order := e.must(e.do("POST", "/api/v1/production-orders", map[string]any{
		"product_item_id": f.product, "producer_supplier_id": f.producer, "external_warehouse_id": f.extWh,
		"planned_qty": "85", "service_unit_price": "5.5000",
	}), 201)
	pred, _ := order.JSON["prediction"].(map[string]any)
	if pred == nil || pred["stage"] != "draft" || pred["predicted_ekp_unit"] != nil {
		t.Fatalf("draft-only siparis: stage=draft ve predicted_ekp_unit=null beklenirdi: %v", pred)
	}
}
