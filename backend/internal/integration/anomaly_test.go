// Anomali tespiti entegrasyonu (bkz. docs/anomaly-integration.md): ML tetikleme noktalari (VERIM_DUSUK,
// EKP_SAPMA), ML'den bagimsiz kural tabanli kontrol (KUTLE_DENGESI), mukerrer onleme, okuma/ack
// uc noktalari ve "anomali hicbir islemi bozmaz/bloke etmez" garantisi.
package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
	apihttp "github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/http"
	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/mlclient"
	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/repo"
)

// newEnvAnomalyML: /predict/production SABIT bir tahmin doner (hibrit EKP akisini bozmasin diye);
// /detect/anomaly cagrildiginda HER ZAMAN tek bir VERIM_DUSUK bulgusu doner (ML'in kendi esik
// mantigini test etmek bu testin kapsaminda degil -- o ml/tests/test_anomaly.py'dedir; burada
// Go'nun ML'i dogru noktada cagirip sonucu anomaly_alert'e yazdigini test ediyoruz).
func newEnvAnomalyML(t *testing.T) *env {
	t.Helper()
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/predict/production":
			_ = json.NewEncoder(w).Encode(mlclient.ProductionPrediction{
				ModelName: "mock_model", ModelVersion: "test", Baseline: true, SupplierUsed: "ALL",
				PredictedYieldRatio: "0.85", PredictedOutputKg: "85.000", PredictedDeliveredQty: "85.00",
			})
		case "/detect/anomaly":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			refID, _ := body["reference_id"].(string)
			_ = json.NewEncoder(w).Encode(mlclient.DetectAnomalyOutput{
				ReferenceID: refID,
				Findings: []mlclient.AnomalyFinding{{
					Code: "VERIM_DUSUK", Severity: "critical",
					Message:  "Gercek verim model tahmininden 4.00 sigma asagida.",
					Evidence: map[string]string{"z_score": "4.00"},
				}},
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

// receiveLowYield: dusuk verimli bir uretim siparisi olusturup teslim alir (100 kg etten 70 adet --
// normalde 85 adet beklenir), siparis ID'sini doner. ML ayakta veya kapali olabilir (oluşturma/sevk/
// teslim ML'den BAGIMSIZ basarili olmali, bkz. docs/assumptions.md "en iyi gayret").
func (e *env) receiveLowYield() (orderID string, receiveResp resp) {
	e.t.Helper()
	f := e.prodFixture()
	etLot := e.must(e.do("POST", "/api/v1/lots", map[string]any{
		"lot_no": "ET-2026-048", "item_id": f.meatItem, "supplier_id": f.supplier, "warehouse_id": f.mainWh,
		"received_at": "2026-03-02", "qty": "100.000", "unit_price": "9.7000"}), 201).str("id")
	order := e.must(e.do("POST", "/api/v1/production-orders", map[string]any{
		"product_item_id": f.product, "producer_supplier_id": f.producer, "external_warehouse_id": f.extWh,
		"planned_qty": "70", "service_unit_price": "5.5000",
	}), 201)
	orderID = order.str("id")
	e.must(e.do("POST", "/api/v1/production-orders/"+orderID+"/send-materials", map[string]any{
		"lines": []map[string]any{{"lot_id": etLot, "qty": "100.000"}},
	}), 200)
	receiveResp = e.must(e.do("POST", "/api/v1/production-orders/"+orderID+"/receive", map[string]any{
		"received_qty": "70", "service_invoice_no": "URT-2026-048-INV", "service_invoiced_qty": "70",
	}), 200)
	return orderID, receiveResp
}

// 1) ML ayakta: dusuk verimli teslim sonrasi VERIM_DUSUK uyarisi olusuyor, mesajda sayisal kanit var,
// ve islem (receive) BASARILI kaliyor (anomali hicbir seyi bloke etmiyor).
func TestProductionAnomalyDetectedWhenMLUp(t *testing.T) {
	e := newEnvAnomalyML(t)
	orderID, received := e.receiveLowYield()
	if received.str("status") != "received" {
		t.Fatalf("anomali olsa da teslim basarili olmali: %s", received.Raw)
	}
	if n := count(t, `SELECT COUNT(*) FROM anomaly_alert WHERE ref_type = 'production_order' AND ref_id = ? AND alert_type = 'VERIM_DUSUK'`, orderID); n != 1 {
		t.Fatalf("1 VERIM_DUSUK uyarisi beklenirdi, %d", n)
	}
	var message string
	if err := testDB.QueryRow(`SELECT message FROM anomaly_alert WHERE ref_id = ? AND alert_type = 'VERIM_DUSUK'`, orderID).Scan(&message); err != nil {
		t.Fatal(err)
	}
	if !strings.ContainsAny(message, "0123456789") {
		t.Fatalf("mesajda sayisal kanit olmali: %q", message)
	}

	// Detaya gomulu: GET /production-orders/{id} anomaly_alerts icerir.
	got := e.must(e.do("GET", "/api/v1/production-orders/"+orderID, nil), 200)
	alerts, _ := got.JSON["anomaly_alerts"].([]any)
	if len(alerts) != 1 || alerts[0].(map[string]any)["alert_type"] != "VERIM_DUSUK" {
		t.Fatalf("production-orders detayinda anomaly_alerts beklenirdi: %s", got.Raw)
	}

	// Anomali lotu/siparisi bloke etmez: siparis hala kapatilabilir.
	closed := e.must(e.do("POST", "/api/v1/production-orders/"+orderID+"/close", nil), 200)
	if closed.str("status") != "closed" {
		t.Fatalf("anomali olsa da kapatma basarili olmali: %s", closed.Raw)
	}
}

// 2) ML KAPALI: ayni dusuk-verim senaryosu, islem YINE basarili, ama hic anomaly_alert olusmaz
// (ML'den bagimsiz kural yoksa kontrol tamamen atlanir -- bkz. docs/anomaly-integration.md).
func TestProductionAnomalySkippedWhenMLDown(t *testing.T) {
	e := newEnvML(t) // ML_URL hicbir zaman dinlemeyen bir porta isaret eder (production_test.go)
	orderID, received := e.receiveLowYield()
	if received.str("status") != "received" {
		t.Fatalf("ML kapaliyken de teslim basarili olmali: %s", received.Raw)
	}
	if n := count(t, `SELECT COUNT(*) FROM anomaly_alert WHERE ref_type = 'production_order' AND ref_id = ?`, orderID); n != 0 {
		t.Fatalf("ML kapaliyken anomaly_alert olusmamali, %d olustu", n)
	}
}

// 3) Kural tabanli KUTLE_DENGESI: ML'siz (newEnv, ML client yok) bile ciktilar girisi asinca critical
// uyari olusur; AYNI emre ikinci kez (hala dengesiz) tetiklenince mukerrer kayit olusmaz (guncellenir).
// Uyari acikken dahi -- dengeyi duzeltip -- parcalama normal sekilde kapanabilir (anomali bloke etmez).
func TestCuttingMassBalanceRuleDedupAndNoBlock(t *testing.T) {
	e := newEnv(t) // ML yok: bu kontrolun ML'den bagimsiz oldugunu da kanitlar
	cf := e.cuttingFixture()
	order := e.must(e.do("POST", "/api/v1/cutting-orders", map[string]any{
		"recipe_id": cf.recipeID, "input_lot_id": cf.lotID, "input_weight_kg": "25", "allocation_method": "value",
		"cut_at": "2026-03-05T10:00:00Z", "cost_basis": "computed",
		"resolution_note": "Birim fiyat ve agirlik tutarli; hatali olan beyan edilen toplam.",
	}), 201)
	orderID := order.str("id")

	// Ciktilar toplami 25'i asiyor (fiziksel imkansiz): but 20 + digerleri => KUTLE_DENGESI.
	over := e.must(e.do("PUT", "/api/v1/cutting-orders/"+orderID+"/outputs",
		cf.outputsBody("20.0", "4.5", "2.5", "3.0", "2.0", "2.5", "3.0")), 200)
	if bd := over.str("balance_diff_kg"); bd[0] != '-' {
		t.Fatalf("balance_diff_kg negatif olmali (cikti > giris): %s", bd)
	}
	if n := count(t, `SELECT COUNT(*) FROM anomaly_alert WHERE ref_type = 'cutting_order' AND ref_id = ? AND alert_type = 'KUTLE_DENGESI'`, orderID); n != 1 {
		t.Fatalf("1 KUTLE_DENGESI uyarisi beklenirdi, %d", n)
	}

	// Lot hala aktif (bloke edilmedi).
	lot := e.must(e.do("GET", "/api/v1/lots/"+cf.lotID, nil), 200)
	if lot.str("status") != "active" {
		t.Fatalf("anomali lotu bloke etmemeli: %s", lot.Raw)
	}

	// Ikinci cagri (hala dengesiz, farkli agirlik): mukerrer kayit OLUSMAMALI, mevcut guncellenmeli.
	e.must(e.do("PUT", "/api/v1/cutting-orders/"+orderID+"/outputs",
		cf.outputsBody("21.0", "4.5", "2.5", "3.0", "2.0", "2.5", "3.0")), 200)
	if n := count(t, `SELECT COUNT(*) FROM anomaly_alert WHERE ref_type = 'cutting_order' AND ref_id = ? AND alert_type = 'KUTLE_DENGESI'`, orderID); n != 1 {
		t.Fatalf("ikinci tetiklemede hala 1 kayit olmali (mukerrer degil), %d", n)
	}

	// Dengeyi duzelt: artik kapanabilir -- acik KUTLE_DENGESI uyarisi olsa DA kapatma basarili olmali.
	fixed := e.must(e.do("PUT", "/api/v1/cutting-orders/"+orderID+"/outputs",
		cf.outputsBody("7.5", "4.5", "2.5", "3.0", "2.0", "2.5", "3.0")), 200)
	if fixed.JSON["closable"] != true {
		t.Fatalf("dengeli girdi sonrasi closable=true beklenirdi: %s", fixed.Raw)
	}
	closed := e.must(e.do("POST", "/api/v1/cutting-orders/"+orderID+"/close", nil), 200)
	if closed.str("status") != "closed" {
		t.Fatalf("acik anomali uyarisi olsa da kapatma basarili olmali: %s", closed.Raw)
	}
}

// 4) GET /anomaly-alerts (filtre+sayfalama), PATCH ack+not, GET /anomaly-alerts/summary dogru sayar.
func TestAnomalyAlertListPatchAndSummary(t *testing.T) {
	e := newEnv(t)
	cf := e.cuttingFixture()
	order := e.must(e.do("POST", "/api/v1/cutting-orders", map[string]any{
		"recipe_id": cf.recipeID, "input_lot_id": cf.lotID, "input_weight_kg": "25", "allocation_method": "value",
		"cut_at": "2026-03-05T10:00:00Z", "cost_basis": "computed",
		"resolution_note": "Birim fiyat ve agirlik tutarli; hatali olan beyan edilen toplam.",
	}), 201)
	orderID := order.str("id")
	e.must(e.do("PUT", "/api/v1/cutting-orders/"+orderID+"/outputs",
		cf.outputsBody("20.0", "4.5", "2.5", "3.0", "2.0", "2.5", "3.0")), 200)

	list := e.must(e.do("GET", "/api/v1/anomaly-alerts?severity=critical&ref_type=cutting_order", nil), 200)
	items, _ := list.JSON["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("1 acik critical uyari beklenirdi: %s", list.Raw)
	}
	alert := items[0].(map[string]any)
	if alert["ref_no"] != order.str("order_no") {
		t.Fatalf("ref_no = siparis numarasi olmali, gelen: %v (beklenen %s)", alert["ref_no"], order.str("order_no"))
	}
	alertID := alert["id"].(string)

	sum := e.must(e.do("GET", "/api/v1/anomaly-alerts/summary", nil), 200)
	if sum.JSON["critical"].(float64) < 1 {
		t.Fatalf("summary.critical >= 1 olmali: %s", sum.Raw)
	}

	ack := e.must(e.do("PATCH", "/api/v1/anomaly-alerts/"+alertID, map[string]any{
		"acknowledged": true, "note": "fiili tartim tekrar girildi",
	}), 200)
	if ack.JSON["acknowledged"] != true || ack.str("note") != "fiili tartim tekrar girildi" {
		t.Fatalf("ack/not guncellenmedi: %s", ack.Raw)
	}

	afterAck := e.must(e.do("GET", "/api/v1/anomaly-alerts?severity=critical&ref_type=cutting_order&acknowledged=false", nil), 200)
	if items2, _ := afterAck.JSON["items"].([]any); len(items2) != 0 {
		t.Fatalf("ack sonrasi acik listede gorunmemeli: %s", afterAck.Raw)
	}

	if bad := e.do("PATCH", "/api/v1/anomaly-alerts/"+alertID, map[string]any{}); bad.Status != 422 {
		t.Fatalf("bos govde 422 olmali: %d %s", bad.Status, bad.Raw)
	}
}

// 5) LOT_HAREKET_TUTARSIZ kavrami NEGATIVE_STOCK_OFFLINE ile BIRLESTIRILDI (iki ayri sistem yok,
// bkz. docs/anomaly-integration.md): ML tamamen kapaliyken (erisilemeyen port) bile offline satis
// stogu negatife dusurdugunde uyari olusur -- bu kontrol ML'e hic bagimli degildir.
func TestNegativeStockRuleIndependentOfML(t *testing.T) {
	e := newEnvML(t) // ML_URL erisilemez
	f := e.sellable(e.fixture())
	cust := e.customer("MKT-A", "Market A")
	lot := e.must(e.do("POST", "/api/v1/lots", f.lotBody("ET-2026-900", "10.000", "9.7000", nil)), 201)

	op := domain.NewID()
	payload := salesSyncOp(op, cust, f.warehouse, f.item, lot.str("id"))
	payload["payload"].(map[string]any)["lines"] = []any{saleLine(f.item, lot.str("id"), "50.000", "24.9000")}
	r := e.must(e.do("POST", "/api/v1/sync/push", map[string]any{
		"device_id": domain.NewID(), "operations": []any{payload},
	}), 200)
	res := r.JSON["results"].([]any)[0].(map[string]any)
	if res["status"] != "flagged" || res["message"] != "NEGATIVE_STOCK_OFFLINE" {
		t.Fatalf("ML kapaliyken de flagged+NEGATIVE_STOCK_OFFLINE beklenirdi: %v", res)
	}
	if n := count(t, `SELECT COUNT(*) FROM anomaly_alert WHERE alert_type = 'NEGATIVE_STOCK_OFFLINE' AND severity = 'critical'`); n != 1 {
		t.Fatalf("ML kapaliyken de 1 anomaly_alert beklenirdi, %d", n)
	}
}
