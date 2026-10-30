// İzlenebilirlik (ileri/geri/genealogy) ve geri çağırma (Görev 3) entegrasyon testleri.
//
// KRİTİK: hiçbir test ML servisine bağlanmaz (newEnv, prodFixture'daki gibi Store'a ML istemcisi
// BAĞLAMAZ); izleme/recall sonuçlarının kesin kayıt ilişkilerinden (lot_link) geldiğini, ML'e
// dayanmadığını dolaylı olarak kanıtlar (ML kapalıyken de tüm bu testler geçer).
package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
)

// recallScenario: yarışma dokümanındaki ET-2026-001 senaryosu BİREBİR (100 kg -> URT-2026-045 ->
// 85 paket SUC-2026-045 -> Market A/B/C 30/25/30) + ondan bağımsız ikinci bir et lotu ET-2026-002
// (bir hammadde lotunun birden fazla mamul lotuna dönüşmesi: SUC-2026-046 ve SUC-2026-047) + iade --
// gerçek HTTP uç noktalarıyla (POST /lots, /production-orders, /sales-orders, /customer-returns) kurulur.
type recallScenario struct {
	f                      prodFixture
	et1, et2               string
	bhrLot, bgrLot         string
	order1, order2, order3 string
	suc045, suc046, suc047 string
	mktA, mktB, mktC       string
}

func (e *env) buildRecallScenario() recallScenario {
	e.t.Helper()
	f := e.prodFixture()
	mktA := e.customer("MKT-A", "Market A")
	mktB := e.customer("MKT-B", "Market B")
	mktC := e.customer("MKT-C", "Market C")
	supBhr := e.must(e.do("POST", "/api/v1/suppliers", map[string]any{
		"code": "SUP-BHR", "name": "Doğu Baharat", "supplier_type": "material"}), 201).str("id")
	supBgr := e.must(e.do("POST", "/api/v1/suppliers", map[string]any{
		"code": "SUP-BGR", "name": "Anadolu Bağırsak", "supplier_type": "material"}), 201).str("id")

	lotBody := func(lotNo, itemID, supplierID, qty, price string) map[string]any {
		return map[string]any{
			"lot_no": lotNo, "item_id": itemID, "supplier_id": supplierID, "warehouse_id": f.mainWh,
			"received_at": "2026-03-02", "qty": qty, "unit_price": price,
		}
	}
	et1 := e.must(e.do("POST", "/api/v1/lots", lotBody("ET-2026-001", f.meatItem, f.supplier, "100.000", "9.7000")), 201).str("id")
	bhrLot := e.must(e.do("POST", "/api/v1/lots", lotBody("BHR-2026-001", f.spiceItem, supBhr, "2.000", "24.0000")), 201).str("id")
	bgrLot := e.must(e.do("POST", "/api/v1/lots", lotBody("BGR-2026-001", f.casingItem, supBgr, "5.000", "11.6000")), 201).str("id")
	et2 := e.must(e.do("POST", "/api/v1/lots", lotBody("ET-2026-002", f.meatItem, f.supplier, "32.000", "9.7000")), 201).str("id")

	prodOrder := func(orderNo, plannedQty string) string {
		return e.must(e.do("POST", "/api/v1/production-orders", map[string]any{
			"order_no": orderNo, "product_item_id": f.product, "producer_supplier_id": f.producer,
			"external_warehouse_id": f.extWh, "planned_qty": plannedQty, "service_unit_price": "5.5000", "sale_price": "24.9000",
		}), 201).str("id")
	}
	receive := func(orderID, outputLotNo, qty string) string {
		out := e.must(e.do("POST", "/api/v1/production-orders/"+orderID+"/receive", map[string]any{
			"received_qty": qty, "output_lot_no": outputLotNo,
			"service_invoice_no": outputLotNo + "-INV", "service_invoiced_qty": qty,
		}), 200)
		e.must(e.do("POST", "/api/v1/production-orders/"+orderID+"/close", nil), 200)
		return out.str("output_lot_id")
	}

	// URT-2026-045: 100 kg ET + 2 kg BHR + 5 kg BGR -> 85 adet SUC-2026-045.
	order1 := prodOrder("URT-2026-045", "85")
	e.must(e.do("POST", "/api/v1/production-orders/"+order1+"/send-materials", map[string]any{
		"lines": []map[string]any{{"lot_id": et1, "qty": "100.000"}, {"lot_id": bhrLot, "qty": "2.000"}, {"lot_id": bgrLot, "qty": "5.000"}},
	}), 200)
	suc045 := receive(order1, "SUC-2026-045", "85")

	// URT-2026-046: ET-2026-002'nin 12 kg'ı -> 10 adet SUC-2026-046.
	order2 := prodOrder("URT-2026-046", "10")
	e.must(e.do("POST", "/api/v1/production-orders/"+order2+"/send-materials", map[string]any{
		"lines": []map[string]any{{"lot_id": et2, "qty": "12.000"}},
	}), 200)
	suc046 := receive(order2, "SUC-2026-046", "10")

	// URT-2026-047: ET-2026-002'nin kalan 20 kg'ı -> SUC-2026-047. ET-2026-002 böylece İKİ mamul
	// lotuna dönüşür; ikisi de ET-2026-001'den TAMAMEN bağımsızdır (geri çağırma kontrolü).
	order3 := prodOrder("URT-2026-047", "10")
	e.must(e.do("POST", "/api/v1/production-orders/"+order3+"/send-materials", map[string]any{
		"lines": []map[string]any{{"lot_id": et2, "qty": "20.000"}},
	}), 200)
	suc047 := receive(order3, "SUC-2026-047", "10")

	saleLine := func(custID, lotID, qty string) map[string]any {
		return map[string]any{
			"customer_id": custID, "warehouse_id": f.mainWh, "sold_at": "2026-03-10T09:00:00Z",
			"lines": []map[string]any{{"item_id": f.product, "lot_id": lotID, "qty": qty, "unit_price": "24.9000"}},
		}
	}
	// SUC-045 -> MKT-A 30, MKT-B 25, MKT-C 30 (doküman); SUC-046 -> MKT-A 10; SUC-047 -> MKT-C 10.
	for _, v := range []struct{ cust, lot, qty string }{
		{mktA, suc045, "30"}, {mktB, suc045, "25"}, {mktC, suc045, "30"}, {mktA, suc046, "10"}, {mktC, suc047, "10"},
	} {
		e.must(e.do("POST", "/api/v1/sales-orders", saleLine(v.cust, v.lot, v.qty)), 201)
	}
	// MKT-B, SUC-045'ten 2 paket iade eder (MKT-B net sevk edilen 25 kalır, iade AYRI gösterilir).
	e.must(e.do("POST", "/api/v1/customer-returns", map[string]any{
		"customer_id": mktB, "lot_id": suc045, "qty": "2", "reason": "musteri memnun degil",
	}), 201)

	return recallScenario{
		f: f, et1: et1, et2: et2, bhrLot: bhrLot, bgrLot: bgrLot,
		order1: order1, order2: order2, order3: order3,
		suc045: suc045, suc046: suc046, suc047: suc047,
		mktA: mktA, mktB: mktB, mktC: mktC,
	}
}

func insertLotLink(t *testing.T, parent, child, processType string) {
	t.Helper()
	if _, err := testDB.Exec(
		`INSERT INTO lot_link (id, parent_lot_id, child_lot_id, qty_from_parent, uom, process_type, process_id)
		 VALUES (?, ?, ?, 1.000, 'kg', ?, ?)`,
		domain.NewID(), parent, child, processType, domain.NewID()); err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------------------
// trace-forward
// ---------------------------------------------------------------------------

// Doküman senaryosu: ET-2026-001 -> yalnızca SUC-2026-045; ET-2026-002'nin mamulleri (SUC-046/047)
// BULUNMAZ. Müşteri dağılımı dokümandaki gibi: MKT-A 30, MKT-B 25 (2 iade AYRI gösterilir), MKT-C 30.
func TestTraceForwardMultiPathAndCustomerSummary(t *testing.T) {
	e := newEnv(t)
	sc := e.buildRecallScenario()

	r := e.must(e.do("GET", "/api/v1/lots/"+sc.et1+"/trace-forward", nil), 200)
	nodes, _ := r.JSON["nodes"].([]any)
	lotNos := map[string]bool{}
	for _, n := range nodes {
		lotNos[n.(map[string]any)["lot_no"].(string)] = true
	}
	for _, want := range []string{"ET-2026-001", "SUC-2026-045"} {
		if !lotNos[want] {
			t.Errorf("%s bulunmali: %s", want, r.Raw)
		}
	}
	for _, other := range []string{"SUC-2026-046", "SUC-2026-047"} {
		if lotNos[other] {
			t.Errorf("%s baska et lotundan (ET-2026-002), BULUNMAMALI: %s", other, r.Raw)
		}
	}

	summary := r.JSON["summary"].(map[string]any)
	if int(summary["affected_lot_count"].(float64)) != 2 {
		t.Errorf("2 etkilenen lot (ET-001, SUC-045) beklenirdi: %v", summary)
	}
	custs, _ := summary["customers"].([]any)
	got := map[string]struct{ shipped, returned string }{}
	for _, c := range custs {
		m := c.(map[string]any)
		got[m["customer_name"].(string)] = struct{ shipped, returned string }{m["qty_shipped"].(string), m["qty_returned"].(string)}
		if m["downstream_known"] != false || m["downstream_note"] == nil {
			t.Errorf("downstream_known=false + not dolu olmali: %v", m)
		}
	}
	if len(custs) != 3 {
		t.Fatalf("3 musteri (MKT-A/B/C) beklenirdi: %v", custs)
	}
	if got["Market A"].shipped != "30.000" {
		t.Errorf("Market A sevk: 30.000 beklenirdi (SUC-046'nin 10'u DAHIL degil), %s geldi", got["Market A"].shipped)
	}
	if got["Market B"].shipped != "25.000" || got["Market B"].returned != "2.000" {
		t.Errorf("Market B: sevk 25.000/iade 2.000 beklenirdi, %v geldi", got["Market B"])
	}
	if got["Market C"].shipped != "30.000" {
		t.Errorf("Market C sevk: 30.000 beklenirdi (SUC-047'nin 10'u DAHIL degil), %s geldi", got["Market C"].shipped)
	}

	// SUC-2026-045 düğümünün kendi satış/sevkiyat satırları da dolu olmalı.
	var suc045Node map[string]any
	for _, n := range nodes {
		m := n.(map[string]any)
		if m["lot_no"] == "SUC-2026-045" {
			suc045Node = m
		}
	}
	if suc045Node == nil {
		t.Fatal("SUC-2026-045 dugumu bulunamadi")
	}
	sales, _ := suc045Node["sales"].([]any)
	if len(sales) != 3 {
		t.Errorf("SUC-2026-045 icin 3 satis satiri beklenirdi: %v", sales)
	}
	if suc045Node["returned_qty"] != "2.000" {
		t.Errorf("SUC-2026-045 returned_qty=2.000 beklenirdi: %v", suc045Node["returned_qty"])
	}

	// Çok yollu: bir hammadde lotu (ET-2026-002) birden fazla mamul lotuna dönüşür.
	r2 := e.must(e.do("GET", "/api/v1/lots/"+sc.et2+"/trace-forward", nil), 200)
	lotNos2 := map[string]bool{}
	for _, n := range r2.JSON["nodes"].([]any) {
		lotNos2[n.(map[string]any)["lot_no"].(string)] = true
	}
	if !lotNos2["SUC-2026-046"] || !lotNos2["SUC-2026-047"] || lotNos2["SUC-2026-045"] {
		t.Errorf("ET-2026-002 -> SUC-2026-046 ve SUC-2026-047 (SUC-045 degil): %s", r2.Raw)
	}
}

// Döngü koruması: yapay A->B->A döngüsü kurulduğunda sorgu sonsuza girmez, hata vermez.
func TestTraceForwardCycleGuard(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	lotA := e.must(e.do("POST", "/api/v1/lots", f.lotBody("A-2026-001", "10.000", "5.0000", nil)), 201).str("id")
	lotB := e.must(e.do("POST", "/api/v1/lots", f.lotBody("B-2026-001", "10.000", "5.0000", nil)), 201).str("id")
	insertLotLink(t, lotA, lotB, "reprocess")
	insertLotLink(t, lotB, lotA, "reprocess")

	done := make(chan resp, 1)
	go func() { done <- e.do("GET", "/api/v1/lots/"+lotA+"/trace-forward", nil) }()
	select {
	case r := <-done:
		if r.Status != 200 {
			t.Fatalf("dongu korumali sorgu basarisiz: %d %s", r.Status, r.Raw)
		}
		nodes, _ := r.JSON["nodes"].([]any)
		if len(nodes) != 2 {
			t.Fatalf("tam 2 dugum (A, B) beklenirdi, dongu sonsuz genislememeli: %d dugum", len(nodes))
		}
	case <-time.After(15 * time.Second):
		t.Fatal("sorgu zaman asimina ugradi: dongu korumasi calismiyor")
	}
}

// Çok seviyeli (3 derinlik) izleme: parçalama/yeniden işleme servisi henüz olmadığı için lot_link
// kayıtları doğrudan oluşturulur (bkz. görev talimatı).
func TestTraceForwardThreeLevelsDeep(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	var lots [4]string
	for i, lotNo := range []string{"L1-2026-001", "L2-2026-001", "L3-2026-001", "L4-2026-001"} {
		lots[i] = e.must(e.do("POST", "/api/v1/lots", f.lotBody(lotNo, "10.000", "5.0000", nil)), 201).str("id")
	}
	insertLotLink(t, lots[0], lots[1], "cutting")
	insertLotLink(t, lots[1], lots[2], "reprocess")
	insertLotLink(t, lots[2], lots[3], "repack")

	r := e.must(e.do("GET", "/api/v1/lots/"+lots[0]+"/trace-forward", nil), 200)
	nodes, _ := r.JSON["nodes"].([]any)
	if len(nodes) != 4 {
		t.Fatalf("4 dugum (L1..L4) beklenirdi: %s", r.Raw)
	}
	depthByLot := map[string]float64{}
	for _, n := range nodes {
		m := n.(map[string]any)
		depthByLot[m["lot_no"].(string)] = m["depth"].(float64)
	}
	if depthByLot["L4-2026-001"] != 3 {
		t.Fatalf("L4-2026-001 depth=3 (3 seviye derinlik) beklenirdi: %v", depthByLot)
	}
}

// ---------------------------------------------------------------------------
// trace-backward
// ---------------------------------------------------------------------------

// SUC-2026-045 -> 3 hammadde lotu (et, baharat, bağırsak), 3 FARKLI tedarikçi; miktarlar 100/2/5.
func TestTraceBackwardThreeRawLotsThreeSuppliers(t *testing.T) {
	e := newEnv(t)
	sc := e.buildRecallScenario()

	r := e.must(e.do("GET", "/api/v1/lots/"+sc.suc045+"/trace-backward", nil), 200)
	nodes, _ := r.JSON["nodes"].([]any)
	if len(nodes) != 3 {
		t.Fatalf("3 hammadde lotu beklenirdi: %s", r.Raw)
	}
	qtyByLotNo := map[string]string{}
	for _, n := range nodes {
		m := n.(map[string]any)
		qtyByLotNo[m["lot_no"].(string)] = m["qty_used"].(string)
		if m["supplier_name"] == nil {
			t.Errorf("tedarikci adi dolu olmali: %v", m)
		}
	}
	want := map[string]string{"ET-2026-001": "100.000", "BHR-2026-001": "2.000", "BGR-2026-001": "5.000"}
	for lotNo, qty := range want {
		if qtyByLotNo[lotNo] != qty {
			t.Errorf("%s: %s beklenirdi, %s geldi", lotNo, qty, qtyByLotNo[lotNo])
		}
	}
	summary := r.JSON["summary"].(map[string]any)
	if int(summary["raw_lot_count"].(float64)) != 3 {
		t.Errorf("raw_lot_count=3 beklenirdi: %v", summary)
	}
	if int(summary["supplier_count"].(float64)) != 3 {
		t.Errorf("supplier_count=3 beklenirdi: %v", summary)
	}
}

// ---------------------------------------------------------------------------
// genealogy
// ---------------------------------------------------------------------------

func TestGenealogyLevelsAndEdges(t *testing.T) {
	e := newEnv(t)
	sc := e.buildRecallScenario()

	r := e.must(e.do("GET", "/api/v1/lots/"+sc.et2+"/genealogy", nil), 200)
	nodes, _ := r.JSON["nodes"].([]any)
	levelByLot := map[string]float64{}
	for _, n := range nodes {
		m := n.(map[string]any)
		levelByLot[m["lot_no"].(string)] = m["level"].(float64)
	}
	if levelByLot["ET-2026-002"] != 0 {
		t.Errorf("kok lot level=0 olmali: %v", levelByLot)
	}
	if levelByLot["SUC-2026-046"] != 1 || levelByLot["SUC-2026-047"] != 1 {
		t.Errorf("mamul lotlar level=1 olmali: %v", levelByLot)
	}
	edges, _ := r.JSON["edges"].([]any)
	if len(edges) < 2 {
		t.Fatalf("en az 2 kenar (ET-002->SUC-046, ET-002->SUC-047) beklenirdi: %v", edges)
	}
}

// ---------------------------------------------------------------------------
// geri çağırma
// ---------------------------------------------------------------------------

// ET-2026-001 için recall açılınca yalnızca etkilenen lotlar (ET-001, SUC-045) 'recalled' olur;
// ET-2026-002'nin mamulleri (SUC-046/047) etkilenmez.
// Aynı lot için ikinci recall 409.
func TestRecallCreateBlocksOnlyAffectedLots(t *testing.T) {
	e := newEnv(t)
	sc := e.buildRecallScenario()

	rc := e.must(e.do("POST", "/api/v1/recalls", map[string]any{"root_lot_id": sc.et1, "reason": "kalite sikayeti"}), 201)
	if rc.str("status") != "open" || rc.str("root_lot_id") != sc.et1 || rc.str("recall_no") == "" {
		t.Fatalf("recall: %s", rc.Raw)
	}
	affected, _ := rc.JSON["affected_lots"].([]any)
	statusByLot := map[string]string{}
	for _, a := range affected {
		m := a.(map[string]any)
		statusByLot[m["lot_no"].(string)] = m["status"].(string)
	}
	for _, want := range []string{"ET-2026-001", "SUC-2026-045"} {
		if statusByLot[want] != "recalled" {
			t.Errorf("%s recalled olmali: %v", want, statusByLot)
		}
	}
	for _, other := range []string{"SUC-2026-046", "SUC-2026-047"} {
		if _, ok := statusByLot[other]; ok {
			t.Errorf("%s etkilenmemeli: %v", other, statusByLot)
		}
	}
	if n := count(t, `SELECT COUNT(*) FROM lot WHERE id IN (?, ?) AND status = 'active'`, sc.suc046, sc.suc047); n != 2 {
		t.Fatalf("SUC-2026-046/047 DB'de aktif kalmali")
	}
	if n := count(t, `SELECT COUNT(*) FROM lot WHERE id IN (?, ?) AND status = 'recalled'`, sc.et1, sc.suc045); n != 2 {
		t.Fatalf("2 lot DB'de recalled olmali, %d oldu", n)
	}

	custs, _ := rc.JSON["customers"].([]any)
	if len(custs) == 0 {
		t.Fatalf("musteri listesi bos olmamali")
	}

	second := e.do("POST", "/api/v1/recalls", map[string]any{"root_lot_id": sc.et1, "reason": "tekrar"})
	if second.Status != 409 {
		t.Fatalf("ayni lot icin ikinci recall 409 beklenirdi: %d %s", second.Status, second.Raw)
	}
}

// Tüm müşteriler confirmed olmadan close 422; hepsi confirmed olunca close/reopen çalışır.
func TestRecallCloseRequiresAllCustomersConfirmed(t *testing.T) {
	e := newEnv(t)
	sc := e.buildRecallScenario()
	rc := e.must(e.do("POST", "/api/v1/recalls", map[string]any{"root_lot_id": sc.et1, "reason": "test"}), 201)
	recallID := rc.str("id")

	tooEarly := e.do("POST", "/api/v1/recalls/"+recallID+"/close", nil)
	if tooEarly.Status != 422 {
		t.Fatalf("422 beklenirdi: %d %s", tooEarly.Status, tooEarly.Raw)
	}

	custs, _ := rc.JSON["customers"].([]any)
	if len(custs) == 0 {
		t.Fatal("musteri listesi bos")
	}
	for _, c := range custs {
		cid := c.(map[string]any)["id"].(string)
		patched := e.must(e.do("PATCH", "/api/v1/recalls/"+recallID+"/customers/"+cid,
			map[string]any{"contact_status": "confirmed", "notes": "telefonla ulasildi"}), 200)
		_ = patched
	}
	closed := e.must(e.do("POST", "/api/v1/recalls/"+recallID+"/close", nil), 200)
	if closed.str("status") != "closed" {
		t.Fatalf("closed beklenirdi: %s", closed.Raw)
	}
	reopened := e.must(e.do("POST", "/api/v1/recalls/"+recallID+"/reopen", nil), 200)
	if reopened.str("status") != "open" {
		t.Fatalf("reopen basarisiz: %s", reopened.Raw)
	}
}

// Bloke etkisi: recall sonrası GET /sync/pull'daki blocked_lot_ids etkilenen lotları içerir;
// REST üzerinden o lottan satış denemesi 422; offline (/sync/push) ise flagged + recall_violation.
func TestRecallBlockReflectedInSyncAndOfflineViolation(t *testing.T) {
	e := newEnv(t)
	sc := e.buildRecallScenario()
	rc := e.must(e.do("POST", "/api/v1/recalls", map[string]any{"root_lot_id": sc.et1, "reason": "test"}), 201)

	pull := e.must(e.do("GET", "/api/v1/sync/pull?since_seq=0", nil), 200)
	ids, _ := pull.JSON["blocked_lot_ids"].([]any)
	blocked := map[string]bool{}
	for _, id := range ids {
		blocked[id.(string)] = true
	}
	if !blocked[sc.suc045] || !blocked[sc.et1] || blocked[sc.suc046] {
		t.Fatalf("blocked_lot_ids yalniz etkilenen lotlari (ET-001, SUC-045) icermeli: %v", ids)
	}

	rest := e.do("POST", "/api/v1/sales-orders", map[string]any{
		"customer_id": sc.mktA, "warehouse_id": sc.f.mainWh, "sold_at": "2026-03-11T09:00:00Z",
		"lines": []map[string]any{{"item_id": sc.f.product, "lot_id": sc.suc045, "qty": "1.000", "unit_price": "24.9000"}},
	})
	if rest.Status != 422 {
		t.Fatalf("REST satis denemesi 422 beklenirdi: %d %s", rest.Status, rest.Raw)
	}

	device, op := domain.NewID(), domain.NewID()
	push := e.must(e.do("POST", "/api/v1/sync/push", map[string]any{
		"device_id": device,
		"operations": []any{map[string]any{
			"op_id": op, "entity": "sales_order", "action": "create",
			"payload": map[string]any{
				"customer_id": sc.mktA, "warehouse_id": sc.f.mainWh, "sold_at": "2026-03-11T09:00:00Z",
				"lines": []any{map[string]any{"item_id": sc.f.product, "lot_id": sc.suc045, "qty": "1.000", "unit_price": "24.9000"}},
			},
			"client_created_at": "2026-03-11T09:00:00Z",
		}},
	}), 200)
	result := push.JSON["results"].([]any)[0].(map[string]any)
	if result["status"] != "flagged" || !strings.Contains(result["message"].(string), "LOT_BLOCKED_AT_SALE") {
		t.Fatalf("flagged + LOT_BLOCKED_AT_SALE beklenirdi: %s", push.Raw)
	}

	detail := e.must(e.do("GET", "/api/v1/recalls/"+rc.str("id"), nil), 200)
	custs, _ := detail.JSON["customers"].([]any)
	violation := false
	for _, c := range custs {
		m := c.(map[string]any)
		if m["lot_id"] == sc.suc045 && m["has_violation"] == true {
			violation = true
		}
	}
	if !violation {
		t.Fatalf("recall detayinda SUC-2026-045 icin has_violation=true beklenirdi: %s", detail.Raw)
	}
}

// Jüri demosu: CSV iletişim listesi doğru satır sayısı ve kolonları döndürür.
func TestRecallContactListCSV(t *testing.T) {
	e := newEnv(t)
	sc := e.buildRecallScenario()
	rc := e.must(e.do("POST", "/api/v1/recalls", map[string]any{"root_lot_id": sc.et1, "reason": "test"}), 201)

	csvResp := e.do("GET", "/api/v1/recalls/"+rc.str("id")+"/contact-list?format=csv", nil)
	if csvResp.Status != 200 {
		t.Fatalf("csv status 200 beklenirdi: %d %s", csvResp.Status, csvResp.Raw)
	}
	lines := strings.Split(strings.TrimRight(csvResp.Raw, "\n"), "\n")
	custs, _ := rc.JSON["customers"].([]any)
	if len(lines) != len(custs)+1 {
		t.Fatalf("csv satir sayisi %d (beklenen %d, +1 baslik)", len(lines), len(custs)+1)
	}
	if !strings.HasPrefix(lines[0], "musteri,telefon,mail,lot,sevk_miktari,iade_miktari,kalan_risk,durum") {
		t.Fatalf("csv basligi beklenmedik: %s", lines[0])
	}

	// format verilmezse JSON doner
	asJSON := e.must(e.do("GET", "/api/v1/recalls/"+rc.str("id")+"/contact-list", nil), 200)
	if _, ok := asJSON.JSON["items"]; !ok {
		t.Fatalf("format verilmeden JSON {items:[...]} beklenirdi: %s", asJSON.Raw)
	}
}
