// Gorev 3 eksikleri (repo/lot_ops.go): fire (waste_out), yeniden isleme (reprocess_consume/in +
// lot_link 'reprocess') ve kayitli ikinci kademe dagitimin izleme/geri cagirmada gosterilmesi.
package integration

import (
	"strings"
	"testing"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
)

func TestRecordWaste(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	lot := e.must(e.do("POST", "/api/v1/lots", f.lotBody("ET-2026-001", "100.000", "9.7000", nil)), 201).str("id")

	id := domain.NewID()
	body := map[string]any{"id": id, "warehouse_id": f.warehouse, "qty": "5.000", "reason": "bozulma"}
	m := e.must(e.do("POST", "/api/v1/lots/"+lot+"/waste", body), 201)
	if m.str("id") != id || m.str("movement_type") != "waste_out" || m.str("qty") != "-5.000" ||
		m.str("unit_cost") != "9.7000" || m.str("note") != "bozulma" {
		t.Fatalf("fire hareketi: %s", m.Raw)
	}
	e.must(e.do("POST", "/api/v1/lots/"+lot+"/waste", body), 201) // tekrar deneme: ayni hareket
	if b := balance(t, lot, f.warehouse); b != "95.000" {
		t.Fatalf("bakiye 95.000 olmali (fire bir kez dusulmeli): %s", b)
	}
	if r := e.do("POST", "/api/v1/lots/"+lot+"/waste", map[string]any{"warehouse_id": f.warehouse, "qty": "96.000", "reason": "x"}); r.Status != 409 {
		t.Fatalf("bakiyeden fazla fire 409 olmali: %d %s", r.Status, r.Raw)
	}
	if r := e.do("POST", "/api/v1/lots/"+lot+"/waste", map[string]any{"warehouse_id": f.warehouse, "qty": "1.000", "reason": " "}); r.Status != 422 {
		t.Fatalf("sebep zorunlu (422): %d", r.Status)
	}
	// Bloke/geri cagrilmis lotun imhasi da fire olarak kaydedilir.
	e.must(e.do("POST", "/api/v1/lots/"+lot+"/block", map[string]any{"reason": "geri cagirma"}), 200)
	e.must(e.do("POST", "/api/v1/lots/"+lot+"/waste", map[string]any{"warehouse_id": f.warehouse, "qty": "95.000", "reason": "imha"}), 201)
	if b := balance(t, lot, f.warehouse); b != "0.000" {
		t.Fatalf("imha sonrasi bakiye 0 olmali: %s", b)
	}
	list := e.must(e.do("GET", "/api/v1/stock-movements?lot_id="+lot+"&movement_type=waste_out", nil), 200)
	if items, _ := list.JSON["items"].([]any); len(items) != 2 {
		t.Fatalf("lot bazinda 2 fire hareketi beklenirdi: %s", list.Raw)
	}
}

func TestReprocessOrder(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	lotA := e.must(e.do("POST", "/api/v1/lots", f.lotBody("ET-2026-001", "100.000", "9.7000", nil)), 201).str("id")
	lotB := e.must(e.do("POST", "/api/v1/lots", f.lotBody("ET-2026-002", "50.000", "11.0000", nil)), 201).str("id")
	out := e.must(e.do("POST", "/api/v1/items", map[string]any{
		"code": "KIYMA", "name": "Kıyma", "item_type": "finished", "base_uom": "kg"}), 201).str("id")

	req := map[string]any{
		"warehouse_id": f.warehouse, "note": "kucuk parcalar kiymaya",
		"inputs": []map[string]any{{"lot_id": lotA, "qty": "10.000"}, {"lot_id": lotB, "qty": "5.000"}},
		"output": map[string]any{"item_id": out, "qty": "14.000"},
	}
	ro := e.must(e.do("POST", "/api/v1/reprocess-orders", req), 201)
	// maliyet: round2(10 x 9.70) + round2(5 x 11.00) = 97.00 + 55.00 = 152.00; birim = round4(152 / 14)
	if ro.str("total_cost") != "152.0000" || ro.str("output_unit_cost") != "10.8571" || ro.str("output_qty") != "14.000" {
		t.Fatalf("yeniden isleme maliyeti: %s", ro.Raw)
	}
	if !strings.HasPrefix(ro.str("order_no"), "YIS-") || !strings.HasPrefix(ro.str("output_lot_no"), "YIL-") {
		t.Fatalf("numaralar: %s", ro.Raw)
	}
	if inputs, _ := ro.JSON["inputs"].([]any); len(inputs) != 2 {
		t.Fatalf("2 girdi beklenirdi: %s", ro.Raw)
	}
	outLot := ro.str("output_lot_id")
	for lot, want := range map[string]string{lotA: "90.000", lotB: "45.000", outLot: "14.000"} {
		if b := balance(t, lot, f.warehouse); b != want {
			t.Errorf("lot %s bakiyesi %s olmali, %s", lot, want, b)
		}
	}
	if n := count(t, `SELECT COUNT(*) FROM stock_movement WHERE ref_type = 'reprocess_order' AND movement_type IN ('reprocess_consume','reprocess_in')`); n != 3 {
		t.Fatalf("2 reprocess_consume + 1 reprocess_in beklenirdi: %d", n)
	}

	// Izleme: girdi lotundan ileriye cikti lotu, cikti lotundan geriye iki girdi ve emir numarasi.
	fwd := e.must(e.do("GET", "/api/v1/lots/"+lotA+"/trace-forward", nil), 200)
	if !strings.Contains(fwd.Raw, ro.str("output_lot_no")) {
		t.Fatalf("ileri izleme yeniden islenmis lotu icermeli: %s", fwd.Raw)
	}
	bwd := e.must(e.do("GET", "/api/v1/lots/"+outLot+"/trace-backward", nil), 200)
	nodes, _ := bwd.JSON["nodes"].([]any)
	if len(nodes) != 2 || nodes[0].(map[string]any)["order_no"] != ro.str("order_no") {
		t.Fatalf("geri izleme iki girdiyi ve YIS emrini gostermeli: %s", bwd.Raw)
	}

	// Ayni id: ayni emir; bloke girdi 422; yetersiz stok 409.
	req["id"] = domain.NewID()
	first := e.must(e.do("POST", "/api/v1/reprocess-orders", req), 201)
	again := e.must(e.do("POST", "/api/v1/reprocess-orders", req), 201)
	if first.str("id") != again.str("id") {
		t.Fatalf("ayni id ayni emri donmeli")
	}
	e.must(e.do("POST", "/api/v1/lots/"+lotB+"/block", map[string]any{"reason": "numune"}), 200)
	delete(req, "id")
	if r := e.do("POST", "/api/v1/reprocess-orders", req); r.Status != 422 {
		t.Fatalf("bloke girdi 422 olmali: %d %s", r.Status, r.Raw)
	}
	req["inputs"] = []map[string]any{{"lot_id": lotA, "qty": "999.000"}}
	if r := e.do("POST", "/api/v1/reprocess-orders", req); r.Status != 409 {
		t.Fatalf("yetersiz stok 409 olmali: %d %s", r.Status, r.Raw)
	}
}

func TestDownstreamDistributionShownOnlyWhenRecorded(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	cust := e.customer("MKT-A", "Market A")
	lot := e.must(e.do("POST", "/api/v1/lots", f.lotBody("ET-2026-001", "100.000", "9.7000", nil)), 201).str("id")
	e.must(e.do("POST", "/api/v1/sales-orders", salesOrderBody(cust, f.warehouse, saleLine(f.item, lot, "10.000", "24.9000"))), 201)

	customerSummary := func() map[string]any {
		r := e.must(e.do("GET", "/api/v1/lots/"+lot+"/trace-forward", nil), 200)
		return r.JSON["summary"].(map[string]any)["customers"].([]any)[0].(map[string]any)
	}
	if c := customerSummary(); c["downstream_known"] != false || c["downstream_note"] != domain.DownstreamNoteText {
		t.Fatalf("kayit yokken ikinci kademe bilinmiyor olmali: %v", c)
	}

	if r := e.do("POST", "/api/v1/downstream-distributions", map[string]any{
		"customer_id": cust, "lot_id": lot, "recipient_name": "Şube 1", "qty": "11.000", "distributed_at": "2026-03-11"}); r.Status != 422 {
		t.Fatalf("sevk edilenden fazla dagitim 422 olmali: %d %s", r.Status, r.Raw)
	}
	e.must(e.do("POST", "/api/v1/downstream-distributions", map[string]any{
		"customer_id": cust, "lot_id": lot, "recipient_name": "Şube 1", "recipient_phone": "0555 000 00 01",
		"qty": "4.000", "distributed_at": "2026-03-11"}), 201)

	c := customerSummary()
	ds, _ := c["downstream"].([]any)
	if c["downstream_known"] != true || len(ds) != 1 || c["downstream_note"] != domain.DownstreamPartialNoteText {
		t.Fatalf("kayitli kisim kesin gosterilmeli, kalan icin not: %v", c)
	}

	rc := e.must(e.do("POST", "/api/v1/recalls", map[string]any{"root_lot_id": lot, "reason": "kalite"}), 201)
	got := e.must(e.do("GET", "/api/v1/recalls/"+rc.str("id"), nil), 200)
	rcCust := got.JSON["customers"].([]any)[0].(map[string]any)
	if rds, _ := rcCust["downstream"].([]any); rcCust["downstream_known"] != true || len(rds) != 1 {
		t.Fatalf("geri cagirma musteri satirinda kayitli dagitim olmali: %v", rcCust)
	}
	csv := e.do("GET", "/api/v1/recalls/"+rc.str("id")+"/contact-list?format=csv", nil)
	if !strings.Contains(csv.Raw, "Şube 1,0555 000 00 01,,ET-2026-001,4.000,,4.000,,2,Market A") {
		t.Fatalf("iletisim listesinde ikinci kademe alici olmali:\n%s", csv.Raw)
	}
}
