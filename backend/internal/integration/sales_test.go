// Satış/sevkiyat/iade + bloke lot + /sync/push gerçek işleme testleri (Görev 2).
package integration

import (
	"strconv"
	"strings"
	"testing"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
)

// customer POST /api/v1/customers kisayolu, id doner.
func (e *env) customer(code, name string) string {
	e.t.Helper()
	return e.must(e.do("POST", "/api/v1/customers", map[string]any{
		"code": code, "name": name, "customer_type": "market"}), 201).str("id")
}

func salesOrderBody(customerID, warehouseID string, lines ...map[string]any) map[string]any {
	return map[string]any{
		"customer_id": customerID, "warehouse_id": warehouseID, "sold_at": "2026-03-10T09:00:00Z",
		"lines": lines,
	}
}

func saleLine(itemID, lotID, qty, unitPrice string) map[string]any {
	return map[string]any{"item_id": itemID, "lot_id": lotID, "qty": qty, "unit_price": unitPrice}
}

// REST satış: bakiye doğru düşüyor, shipment oluşuyor, sales_line stock_movement'a bağlı.
func TestSalesOrderRestFlow(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	cust := e.customer("MKT-A", "Market A")
	lot := e.must(e.do("POST", "/api/v1/lots", f.lotBody("ET-2026-001", "100.000", "9.7000", nil)), 201)

	order := e.must(e.do("POST", "/api/v1/sales-orders", salesOrderBody(cust, f.warehouse,
		saleLine(f.item, lot.str("id"), "10.000", "24.9000"))), 201)
	if order.str("status") != "confirmed" || order.str("total_amount") != "249.0000" {
		t.Fatalf("siparis: %s", order.Raw)
	}
	if order.str("order_no") == "" {
		t.Fatalf("order_no atomik uretilmeli: %s", order.Raw)
	}
	lines, _ := order.JSON["lines"].([]any)
	if len(lines) != 1 {
		t.Fatalf("1 satir beklenirdi: %s", order.Raw)
	}
	line := lines[0].(map[string]any)
	if line["recall_violation"] != false || line["stock_movement_id"] == nil {
		t.Fatalf("satir: %v", line)
	}
	shipments, _ := order.JSON["shipments"].([]any)
	if len(shipments) != 1 {
		t.Fatalf("1 sevkiyat beklenirdi (create_shipment varsayilan true): %s", order.Raw)
	}
	shLines, _ := shipments[0].(map[string]any)["lines"].([]any)
	if len(shLines) != 1 || shLines[0].(map[string]any)["qty"] != "10.000" {
		t.Fatalf("sevkiyat satiri: %s", order.Raw)
	}

	var onHand string
	if err := testDB.QueryRow(`SELECT qty_on_hand FROM v_lot_balance WHERE lot_id = ? AND warehouse_id = ?`,
		lot.str("id"), f.warehouse).Scan(&onHand); err != nil || onHand != "90.000" {
		t.Fatalf("bakiye 90.000 olmali: %q %v", onHand, err)
	}
	if n := count(t, `SELECT COUNT(*) FROM stock_movement WHERE lot_id = ? AND movement_type = 'sale_out' AND qty = -10.000`, lot.str("id")); n != 1 {
		t.Fatalf("tam 1 sale_out beklenirdi")
	}
	if n := count(t, `SELECT COUNT(*) FROM change_log WHERE entity = 'lot' AND entity_id = ?`, lot.str("id")); n < 2 {
		t.Fatalf("lot icin en az 2 change_log beklenirdi (olusturma + satis bakiye guncellemesi): %d", n)
	}

	// GET detay
	got := e.must(e.do("GET", "/api/v1/sales-orders/"+order.str("id"), nil), 200)
	if got.str("order_no") != order.str("order_no") {
		t.Fatalf("detay: %s", got.Raw)
	}

	// GET liste: customer_id ve lot_id filtreleri
	list := e.must(e.do("GET", "/api/v1/sales-orders?customer_id="+cust, nil), 200)
	if items, _ := list.JSON["items"].([]any); len(items) != 1 {
		t.Fatalf("customer_id filtresi: %s", list.Raw)
	}
	byLot := e.must(e.do("GET", "/api/v1/sales-orders?lot_id="+lot.str("id"), nil), 200)
	if items, _ := byLot.JSON["items"].([]any); len(items) != 1 {
		t.Fatalf("lot_id filtresi: %s", byLot.Raw)
	}
	noMatch := e.must(e.do("GET", "/api/v1/sales-orders?customer_id="+domain.NewID(), nil), 200)
	if items, _ := noMatch.JSON["items"].([]any); len(items) != 0 {
		t.Fatalf("eslesmeyen customer_id bos donmeli: %s", noMatch.Raw)
	}
}

// Online REST: bloke lot 422 (LOT_BLOCKED), hicbir kayit yazilmaz.
func TestSalesOrderRestBlockedLot422(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	cust := e.customer("MKT-A", "Market A")
	lot := e.must(e.do("POST", "/api/v1/lots", f.lotBody("ET-2026-001", "100.000", "9.7000", nil)), 201)
	e.must(e.do("POST", "/api/v1/lots/"+lot.str("id")+"/block", map[string]any{"reason": "kalite şikayeti"}), 200)

	r := e.do("POST", "/api/v1/sales-orders", salesOrderBody(cust, f.warehouse, saleLine(f.item, lot.str("id"), "5.000", "24.9000")))
	if r.Status != 422 || !strings.Contains(r.str("error", "message"), "LOT_BLOCKED") {
		t.Fatalf("422 LOT_BLOCKED beklenirdi: %d %s", r.Status, r.Raw)
	}
	if n := count(t, `SELECT COUNT(*) FROM sales_order`); n != 0 {
		t.Fatalf("basarisiz satis kayit birakmamali: %d sales_order", n)
	}
	if n := count(t, `SELECT COUNT(*) FROM stock_movement WHERE movement_type = 'sale_out'`); n != 0 {
		t.Fatalf("basarisiz satis stok hareketi birakmamali")
	}
}

// Online REST: yetersiz stok 409 (INSUFFICIENT_STOCK), hicbir kayit yazilmaz.
func TestSalesOrderRestInsufficientStock409(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	cust := e.customer("MKT-A", "Market A")
	lot := e.must(e.do("POST", "/api/v1/lots", f.lotBody("ET-2026-001", "10.000", "9.7000", nil)), 201)

	r := e.do("POST", "/api/v1/sales-orders", salesOrderBody(cust, f.warehouse, saleLine(f.item, lot.str("id"), "50.000", "24.9000")))
	if r.Status != 409 || !strings.Contains(r.str("error", "message"), "INSUFFICIENT_STOCK") {
		t.Fatalf("409 INSUFFICIENT_STOCK beklenirdi: %d %s", r.Status, r.Raw)
	}
	if n := count(t, `SELECT COUNT(*) FROM sales_order`); n != 0 {
		t.Fatalf("basarisiz satis kayit birakmamali")
	}
	var onHand string
	if err := testDB.QueryRow(`SELECT qty_on_hand FROM v_lot_balance WHERE lot_id = ?`, lot.str("id")).Scan(&onHand); err != nil || onHand != "10.000" {
		t.Fatalf("bakiye degismemeli: %q %v", onHand, err)
	}
}

// Ayni istemci id'siyle tekrar gelen istek mevcut siparisi doner (yeniden islenmez).
func TestSalesOrderRestIdempotentSameID(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	cust := e.customer("MKT-A", "Market A")
	lot := e.must(e.do("POST", "/api/v1/lots", f.lotBody("ET-2026-001", "100.000", "9.7000", nil)), 201)

	id := domain.NewID()
	body := salesOrderBody(cust, f.warehouse, saleLine(f.item, lot.str("id"), "10.000", "24.9000"))
	body["id"] = id
	first := e.must(e.do("POST", "/api/v1/sales-orders", body), 201)
	second := e.must(e.do("POST", "/api/v1/sales-orders", body), 201)
	if first.str("id") != id || second.str("id") != id || first.str("order_no") != second.str("order_no") {
		t.Fatalf("idempotent olmali: %s / %s", first.Raw, second.Raw)
	}
	if n := count(t, `SELECT COUNT(*) FROM sales_order WHERE id = ?`, id); n != 1 {
		t.Fatalf("tek sales_order kaydi olmali: %d", n)
	}
	if n := count(t, `SELECT COUNT(*) FROM stock_movement WHERE lot_id = ? AND movement_type = 'sale_out'`, lot.str("id")); n != 1 {
		t.Fatalf("stok ikinci kez dusmemeli: %d sale_out", n)
	}
}

// Satis senaryosu: uc ayri siparisle bir lotu sifira kadar satmak (bkz. docs/HANDOFF.md seed senaryosu).
// SUC-2026-045 85 adet -> MKT-A 30, MKT-B 25, MKT-C 30; bakiye 0, toplam ciro 85 x 24.90 = 2116.50.
func TestSalesScenarioThreeWaySplit(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	finished := e.must(e.do("POST", "/api/v1/items", map[string]any{
		"code": "SUCUK-NARIN-1KG", "name": "Narin Sucuk 1 kg", "item_type": "finished", "base_uom": "adet",
		"unit_weight_kg": "1.000", "default_sale_price": "24.9000"}), 201)
	lot := e.must(e.do("POST", "/api/v1/lots", map[string]any{
		"lot_no": "SUC-2026-045", "item_id": finished.str("id"), "supplier_id": f.supplier, "warehouse_id": f.warehouse,
		"received_at": "2026-03-10", "qty": "85.000", "unit_price": "18.1588"}), 201)

	total := "0"
	for _, v := range []struct{ code, qty string }{{"MKT-A", "30"}, {"MKT-B", "25"}, {"MKT-C", "30"}} {
		cust := e.customer(v.code, v.code)
		e.must(e.do("POST", "/api/v1/sales-orders", salesOrderBody(cust, f.warehouse,
			saleLine(finished.str("id"), lot.str("id"), v.qty+".000", "24.9000"))), 201)
		_ = total
	}
	if n := count(t, `SELECT COUNT(*) FROM shipment WHERE sales_order_id IN (SELECT id FROM sales_order)`); n != 3 {
		t.Fatalf("3 sevkiyat beklenirdi: %d", n)
	}
	var onHand string
	if err := testDB.QueryRow(`SELECT qty_on_hand FROM v_lot_balance WHERE lot_id = ?`, lot.str("id")).Scan(&onHand); err != nil || onHand != "0.000" {
		t.Fatalf("bakiye 0 olmali: %q %v", onHand, err)
	}
	var revenue string
	if err := testDB.QueryRow(`SELECT SUM(total_amount) FROM sales_order`).Scan(&revenue); err != nil || revenue != "2116.5000" {
		t.Fatalf("toplam ciro 2116.5000 olmali: %q %v", revenue, err)
	}
}

// Offline (sync): bloke lot satisi YINE KAYDEDILIR, flagged + recall_violation=true.
func TestSyncSalesOrderBlockedLotFlagged(t *testing.T) {
	e := newEnv(t)
	f := e.sellable(e.fixture())
	cust := e.customer("MKT-A", "Market A")
	lot := e.must(e.do("POST", "/api/v1/lots", f.lotBody("ET-2026-001", "100.000", "9.7000", nil)), 201)
	e.must(e.do("POST", "/api/v1/lots/"+lot.str("id")+"/block", map[string]any{"reason": "geri cagirma"}), 200)

	op := domain.NewID()
	r := e.must(e.do("POST", "/api/v1/sync/push", map[string]any{
		"device_id":  domain.NewID(),
		"operations": []any{salesSyncOp(op, cust, f.warehouse, f.item, lot.str("id"))},
	}), 200)
	res := r.JSON["results"].([]any)[0].(map[string]any)
	if res["status"] != "flagged" || res["message"] != "LOT_BLOCKED_AT_SALE" {
		t.Fatalf("flagged + LOT_BLOCKED_AT_SALE beklenirdi: %v", res)
	}
	if n := count(t, `SELECT COUNT(*) FROM sales_line WHERE lot_id = ? AND recall_violation = TRUE`, lot.str("id")); n != 1 {
		t.Fatalf("satis kaydedilmeli ve recall_violation=TRUE olmali")
	}
	if n := count(t, `SELECT COUNT(*) FROM stock_movement WHERE lot_id = ? AND movement_type = 'sale_out'`, lot.str("id")); n != 1 {
		t.Fatalf("stok hareketi yine olusmali (satis fiziksel olarak gerceklesti)")
	}
}

// Offline (sync): yetersiz bakiye YINE KAYDEDILIR, flagged, stok negatife duser, anomaly_alert acilir.
func TestSyncSalesOrderNegativeStockFlagged(t *testing.T) {
	e := newEnv(t)
	f := e.sellable(e.fixture())
	cust := e.customer("MKT-A", "Market A")
	lot := e.must(e.do("POST", "/api/v1/lots", f.lotBody("ET-2026-001", "10.000", "9.7000", nil)), 201)

	op := domain.NewID()
	payload := salesSyncOp(op, cust, f.warehouse, f.item, lot.str("id"))
	payload["payload"].(map[string]any)["lines"] = []any{saleLine(f.item, lot.str("id"), "50.000", "24.9000")}
	r := e.must(e.do("POST", "/api/v1/sync/push", map[string]any{
		"device_id": domain.NewID(), "operations": []any{payload},
	}), 200)
	res := r.JSON["results"].([]any)[0].(map[string]any)
	if res["status"] != "flagged" || res["message"] != "NEGATIVE_STOCK_OFFLINE" {
		t.Fatalf("flagged + NEGATIVE_STOCK_OFFLINE beklenirdi: %v", res)
	}
	var onHand string
	if err := testDB.QueryRow(`SELECT qty_on_hand FROM v_lot_balance WHERE lot_id = ?`, lot.str("id")).Scan(&onHand); err != nil || onHand != "-40.000" {
		t.Fatalf("bakiye negatife dusmeli (-40.000): %q %v", onHand, err)
	}
	if n := count(t, `SELECT COUNT(*) FROM anomaly_alert WHERE alert_type = 'NEGATIVE_STOCK_OFFLINE' AND severity = 'critical'`); n != 1 {
		t.Fatalf("1 anomaly_alert beklenirdi")
	}

	// Web listesi ve detayi ihlali gosterir: liste rozeti (has_violation) ve satir bayragi.
	list := e.must(e.do("GET", "/api/v1/sales-orders", nil), 200)
	orders := list.JSON["items"].([]any)
	if len(orders) != 1 || orders[0].(map[string]any)["has_violation"] != true {
		t.Fatalf("listede has_violation=true beklenirdi: %s", list.Raw)
	}
	detail := e.must(e.do("GET", "/api/v1/sales-orders/"+orders[0].(map[string]any)["id"].(string), nil), 200)
	line := detail.JSON["lines"].([]any)[0].(map[string]any)
	if line["negative_stock_violation"] != true || line["recall_violation"] != false {
		t.Fatalf("satirda negative_stock_violation=true, recall_violation=false beklenirdi: %s", detail.Raw)
	}
}

// Eksi stoklu offline satis: uyari okununca "reviewed", tamami iade edilince "returned"; her ikisi de
// pull'a sales_order degisikligi, web iadesi customer_return degisikligi olarak duser (cihaz bildirimi).
func TestNegativeStockSaleReviewAndReturnReachDevice(t *testing.T) {
	e := newEnv(t)
	f := e.sellable(e.fixture())
	cust := e.customer("MKT-A", "Market A")
	lot := e.must(e.do("POST", "/api/v1/lots", f.lotBody("ET-2026-001", "10.000", "9.7000", nil)), 201)

	op := domain.NewID()
	payload := salesSyncOp(op, cust, f.warehouse, f.item, lot.str("id"))
	payload["payload"].(map[string]any)["lines"] = []any{saleLine(f.item, lot.str("id"), "50.000", "24.9000")}
	e.must(e.do("POST", "/api/v1/sync/push", map[string]any{"device_id": domain.NewID(), "operations": []any{payload}}), 200)
	orderID := e.must(e.do("GET", "/api/v1/sales-orders", nil), 200).JSON["items"].([]any)[0].(map[string]any)["id"].(string)

	detail := func() (map[string]any, map[string]any) {
		d := e.must(e.do("GET", "/api/v1/sales-orders/"+orderID, nil), 200)
		return d.JSON, d.JSON["lines"].([]any)[0].(map[string]any)
	}
	pullSince := func(seq int64) []map[string]any {
		r := e.must(e.do("GET", "/api/v1/sync/pull?since_seq="+strconv.FormatInt(seq, 10), nil), 200)
		var out []map[string]any
		for _, c := range r.JSON["changes"].([]any) {
			out = append(out, c.(map[string]any))
		}
		return out
	}
	reviewChange := func(changes []map[string]any) any {
		var status any
		for _, c := range changes {
			if c["entity"] == "sales_order" && c["entity_id"] == orderID {
				status = c["payload"].(map[string]any)["review_status"]
			}
		}
		return status
	}
	seqNow := func() (seq int64) {
		if err := testDB.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM change_log`).Scan(&seq); err != nil {
			t.Fatal(err)
		}
		return seq
	}

	order, line := detail()
	if order["review_status"] != "open" || line["returned_qty"] != "0.000" {
		t.Fatalf("baslangic: review_status=open, returned_qty=0.000 beklenirdi: %v", order)
	}

	var alertID string
	if err := testDB.QueryRow(`SELECT id FROM anomaly_alert WHERE ref_type = 'sales_line' AND ref_id = ?`, line["id"]).Scan(&alertID); err != nil {
		t.Fatal(err)
	}
	seq := seqNow()
	e.must(e.do("PATCH", "/api/v1/anomaly-alerts/"+alertID, map[string]any{"acknowledged": true}), 200)
	if order, _ = detail(); order["review_status"] != "reviewed" {
		t.Fatalf("uyari okununca reviewed beklenirdi: %v", order["review_status"])
	}
	if got := reviewChange(pullSince(seq)); got != "reviewed" {
		t.Fatalf("pull'da sales_order review_status=reviewed beklenirdi: %v", got)
	}

	seq = seqNow()
	ret := e.must(e.do("POST", "/api/v1/customer-returns", map[string]any{
		"customer_id": cust, "sales_line_id": line["id"], "qty": "50.000", "reason": "eksi stok duzeltme",
	}), 201)
	order, line = detail()
	if order["review_status"] != "returned" || line["returned_qty"] != "50.000" {
		t.Fatalf("tam iade sonrasi returned / 50.000 beklenirdi: %v", order)
	}
	changes := pullSince(seq)
	if got := reviewChange(changes); got != "returned" {
		t.Fatalf("pull'da sales_order review_status=returned beklenirdi: %v", got)
	}
	foundReturn := false
	for _, c := range changes {
		if c["entity"] == "customer_return" && c["entity_id"] == ret.str("id") {
			p := c["payload"].(map[string]any)
			foundReturn = p["sales_line_id"] == line["id"] && p["qty"] == "50.000"
		}
	}
	if !foundReturn {
		t.Fatalf("pull'da web iadesi customer_return olarak bulunamadi: %v", changes)
	}
}

// Bozuk payload / bilinmeyen referans: rejected, hicbir kayit yazilmaz (processed_request dahil).
func TestSyncRejectedUnknownReferenceWritesNothing(t *testing.T) {
	e := newEnv(t)
	f := e.sellable(e.fixture())
	lot := e.must(e.do("POST", "/api/v1/lots", f.lotBody("ET-2026-001", "10.000", "9.7000", nil)), 201)

	op := domain.NewID()
	r := e.must(e.do("POST", "/api/v1/sync/push", map[string]any{
		"device_id":  domain.NewID(),
		"operations": []any{salesSyncOp(op, domain.NewID() /* bilinmeyen musteri */, f.warehouse, f.item, lot.str("id"))},
	}), 200)
	res := r.JSON["results"].([]any)[0].(map[string]any)
	if res["status"] != "rejected" {
		t.Fatalf("rejected beklenirdi: %v", res)
	}
	if n := count(t, `SELECT COUNT(*) FROM processed_request WHERE op_id = ?`, op); n != 0 {
		t.Fatalf("rejected icin hicbir kayit yazilmamali (processed_request dahil): %d", n)
	}
	if n := count(t, `SELECT COUNT(*) FROM sales_order`); n != 0 {
		t.Fatalf("rejected icin sales_order yazilmamali")
	}
	// Ayni op_id tekrar gonderilebilir (reddedilen islemler icin dedup yoktur), hala rejected doner.
	r2 := e.must(e.do("POST", "/api/v1/sync/push", map[string]any{
		"device_id":  domain.NewID(),
		"operations": []any{salesSyncOp(op, domain.NewID(), f.warehouse, f.item, lot.str("id"))},
	}), 200)
	if r2.JSON["results"].([]any)[0].(map[string]any)["status"] != "rejected" {
		t.Fatalf("tekrar rejected beklenirdi: %s", r2.Raw)
	}
}

// 3 operasyonlu batch'te ortadaki rejected olsa da digerleri islenir (operasyon basina ayri transaction).
func TestSyncBatchPartialRejection(t *testing.T) {
	e := newEnv(t)
	f := e.sellable(e.fixture())
	cust := e.customer("MKT-A", "Market A")
	lot := e.must(e.do("POST", "/api/v1/lots", f.lotBody("ET-2026-001", "100.000", "9.7000", nil)), 201)

	opA, opBad, opC := domain.NewID(), domain.NewID(), domain.NewID()
	ops := []any{
		salesSyncOp(opA, cust, f.warehouse, f.item, lot.str("id")),
		salesSyncOp(opBad, domain.NewID(), f.warehouse, f.item, lot.str("id")), // bilinmeyen musteri
		salesSyncOp(opC, cust, f.warehouse, f.item, lot.str("id")),
	}
	r := e.must(e.do("POST", "/api/v1/sync/push", map[string]any{"device_id": domain.NewID(), "operations": ops}), 200)
	results := r.JSON["results"].([]any)
	statuses := make([]string, len(results))
	for i, res := range results {
		statuses[i] = res.(map[string]any)["status"].(string)
	}
	if statuses[0] != "applied" || statuses[1] != "rejected" || statuses[2] != "applied" {
		t.Fatalf("beklenen [applied rejected applied], gelen: %v (%s)", statuses, r.Raw)
	}
	if n := count(t, `SELECT COUNT(*) FROM sales_order`); n != 2 {
		t.Fatalf("2 basarili satis beklenirdi: %d", n)
	}
}

// Pull: satis sonrasi lot'un guncel bakiyesi (qty_on_hand) change_log'a dusuyor.
func TestSyncPullReflectsBalanceAfterSale(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	cust := e.customer("MKT-A", "Market A")
	lot := e.must(e.do("POST", "/api/v1/lots", f.lotBody("ET-2026-001", "100.000", "9.7000", nil)), 201)
	before := e.must(e.do("GET", "/api/v1/sync/pull?since_seq=0", nil), 200)
	sinceSeq := int64(before.JSON["next_seq"].(float64))

	e.must(e.do("POST", "/api/v1/sales-orders", salesOrderBody(cust, f.warehouse, saleLine(f.item, lot.str("id"), "30.000", "24.9000"))), 201)

	after := e.must(e.do("GET", "/api/v1/sync/pull?since_seq="+strconv.FormatInt(sinceSeq, 10), nil), 200)
	changes, _ := after.JSON["changes"].([]any)
	found := false
	for _, c := range changes {
		m := c.(map[string]any)
		if m["entity"] == "lot" && m["entity_id"] == lot.str("id") {
			if m["payload"].(map[string]any)["qty_on_hand"] == "70.000" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("satis sonrasi lot pull payload'inda qty_on_hand=70.000 bulunamadi: %s", after.Raw)
	}
}

// Bloke lot mekanizmasi: block/unblock lot.status ve blocked_lot_ids'i gunceller.
func TestBlockUnblockLot(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	lot := e.must(e.do("POST", "/api/v1/lots", f.lotBody("ET-2026-001", "10.000", "9.7000", nil)), 201)

	blocked := e.must(e.do("POST", "/api/v1/lots/"+lot.str("id")+"/block", map[string]any{"reason": "supheli parti"}), 200)
	if blocked.str("status") != "blocked" || blocked.str("blocked_reason") != "supheli parti" {
		t.Fatalf("blokla: %s", blocked.Raw)
	}
	pull := e.must(e.do("GET", "/api/v1/sync/pull?since_seq=999999", nil), 200)
	ids, _ := pull.JSON["blocked_lot_ids"].([]any)
	if len(ids) != 1 || ids[0] != lot.str("id") {
		t.Fatalf("blocked_lot_ids: %s", pull.Raw)
	}
	if r := e.do("POST", "/api/v1/lots/"+lot.str("id")+"/block", map[string]any{"reason": ""}); r.Status != 422 {
		t.Fatalf("bos sebep 422 olmali: %d", r.Status)
	}

	unblocked := e.must(e.do("POST", "/api/v1/lots/"+lot.str("id")+"/unblock", nil), 200)
	if unblocked.str("status") != "active" || unblocked.JSON["blocked_reason"] != nil {
		t.Fatalf("ac: %s", unblocked.Raw)
	}
	pull2 := e.must(e.do("GET", "/api/v1/sync/pull?since_seq=999999", nil), 200)
	ids2, _ := pull2.JSON["blocked_lot_ids"].([]any)
	if len(ids2) != 0 {
		t.Fatalf("unblock sonrasi blocked_lot_ids bos olmali: %s", pull2.Raw)
	}
}

// Musteri iadesi: bakiye artar, fazla iade 422.
func TestCustomerReturn(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	cust := e.customer("MKT-A", "Market A")
	lot := e.must(e.do("POST", "/api/v1/lots", f.lotBody("ET-2026-001", "100.000", "9.7000", nil)), 201)
	order := e.must(e.do("POST", "/api/v1/sales-orders", salesOrderBody(cust, f.warehouse, saleLine(f.item, lot.str("id"), "10.000", "24.9000"))), 201)
	lineID := order.JSON["lines"].([]any)[0].(map[string]any)["id"].(string)

	ret := e.must(e.do("POST", "/api/v1/customer-returns", map[string]any{
		"customer_id": cust, "sales_line_id": lineID, "qty": "4.000", "reason": "musteri memnun degil",
	}), 201)
	if ret.str("qty") != "4.000" || ret.str("return_no") == "" {
		t.Fatalf("iade: %s", ret.Raw)
	}
	var onHand string
	if err := testDB.QueryRow(`SELECT qty_on_hand FROM v_lot_balance WHERE lot_id = ?`, lot.str("id")).Scan(&onHand); err != nil || onHand != "94.000" {
		t.Fatalf("bakiye 94.000 olmali (100-10+4): %q %v", onHand, err)
	}

	over := e.do("POST", "/api/v1/customer-returns", map[string]any{
		"customer_id": cust, "sales_line_id": lineID, "qty": "10.000", "reason": "fazla iade",
	})
	if over.Status != 422 {
		t.Fatalf("satilandan fazla iade 422 olmali (4 iade edilmis, 10 satilmis, 10 istenirse 14>10): %d %s", over.Status, over.Raw)
	}
}
