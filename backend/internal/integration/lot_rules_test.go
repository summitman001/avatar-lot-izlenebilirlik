// Raf omrunden SKT turetme, tuketilen lotun bakiyesinin cihaza gitmesi ve cihaz satis kurallari.
package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
)

// Raf omru tanimli urunde SKT verilmezse lot tarihinden turetilir; acik SKT her zaman kazanir;
// raf omru tanimsizsa lot SKT'siz kalir.
func TestPurchaseLotExpiryDerivedFromShelfLife(t *testing.T) {
	e := newEnv(t)
	f := e.fixture() // KUZU-BUTUN: raf omru yok
	item := e.must(e.do("POST", "/api/v1/items", map[string]any{
		"code": "ET-RAF", "name": "Raf Omurlu Et", "item_type": "raw", "base_uom": "kg", "shelf_life_days": 7}), 201)
	if item.JSON["shelf_life_days"] != float64(7) {
		t.Fatalf("shelf_life_days donmeli: %s", item.Raw)
	}
	g := fixture{item.str("id"), f.supplier, f.warehouse}

	derived := e.must(e.do("POST", "/api/v1/lots", g.lotBody("ET-RAF-001", "10.000", "9.7000", nil)), 201)
	if got := derived.str("expiry_date"); got != "2026-03-09" { // received_at 2026-03-02 + 7 gun
		t.Fatalf("turetilmis SKT 2026-03-09 beklenirdi: %q", got)
	}
	body := g.lotBody("ET-RAF-002", "10.000", "9.7000", nil)
	body["expiry_date"] = "2026-03-04"
	if got := e.must(e.do("POST", "/api/v1/lots", body), 201).str("expiry_date"); got != "2026-03-04" {
		t.Fatalf("acik SKT korunmali: %q", got)
	}
	if none := e.must(e.do("POST", "/api/v1/lots", f.lotBody("KZ-SKTSIZ", "10.000", "11.5000", nil)), 201); none.JSON["expiry_date"] != nil {
		t.Fatalf("raf omru yoksa SKT bos kalmali: %s", none.Raw)
	}
	if r := e.do("POST", "/api/v1/items", map[string]any{
		"code": "X-RAF", "name": "x", "item_type": "raw", "base_uom": "kg", "shelf_life_days": 0}); r.Status != 422 {
		t.Fatalf("shelf_life_days=0 reddedilmeli: %d %s", r.Status, r.Raw)
	}
}

// Parcalama kapanisi: raf omurlu parcanin lotu SKT alir; tuketilen giris lotunun son change_log
// kaydi 0 bakiye tasir (cihaz parcalanmis karkasi stokta gormez).
func TestCuttingOutputExpiryAndConsumedInputBalanceSynced(t *testing.T) {
	e := newEnv(t)
	cf := e.cuttingFixture()
	but := e.must(e.do("GET", "/api/v1/items/"+cf.partItemIDs["But"], nil), 200).JSON
	but["shelf_life_days"] = 5
	e.must(e.do("PUT", "/api/v1/items/"+cf.partItemIDs["But"], but), 200)

	order := e.must(e.do("POST", "/api/v1/cutting-orders", map[string]any{
		"recipe_id": cf.recipeID, "input_lot_id": cf.lotID, "input_weight_kg": "25", "allocation_method": "value",
		"cut_at": "2026-03-05T10:00:00Z", "cost_basis": "computed", "resolution_note": "beyan edilen toplam hatali",
	}), 201)
	e.must(e.do("PUT", "/api/v1/cutting-orders/"+order.str("id")+"/outputs",
		cf.outputsBody("7.5", "4.5", "2.5", "3.0", "2.0", "2.5", "3.0")), 200)
	closed := e.must(e.do("POST", "/api/v1/cutting-orders/"+order.str("id")+"/close", nil), 200)

	want := time.Now().UTC().AddDate(0, 0, 5).Format("2006-01-02")
	for _, raw := range closed.JSON["outputs"].([]any) {
		o := raw.(map[string]any)
		lot := e.must(e.do("GET", "/api/v1/lots/"+o["output_lot_id"].(string), nil), 200)
		switch o["part_item_code"] {
		case "CUT-But":
			if got := lot.str("expiry_date"); got != want {
				t.Fatalf("But lotu SKT'si bugun+5 (%s) olmali: %q", want, got)
			}
		case "CUT-Kol":
			if lot.JSON["expiry_date"] != nil {
				t.Fatalf("raf omru olmayan parcanin SKT'si bos kalmali: %s", lot.Raw)
			}
		}
	}

	var qty string
	if err := testDB.QueryRow(`SELECT JSON_UNQUOTE(JSON_EXTRACT(payload_json, '$.qty_on_hand')) FROM change_log
	                           WHERE entity = 'lot' AND entity_id = ? ORDER BY seq DESC LIMIT 1`, cf.lotID).Scan(&qty); err != nil || qty != "0.000" {
		t.Fatalf("tuketilen giris lotunun son change_log bakiyesi 0.000 olmali: %q %v", qty, err)
	}
}

// Cihaz yalniz mamul/parca/yan urun satar, yalniz ic depo/aractan: aksi rejected, hicbir kayit yazilmaz.
func TestSyncDeviceSaleRules(t *testing.T) {
	e := newEnv(t)
	f := e.fixture() // KUZU-BUTUN: raw
	cust := e.customer("MKT-A", "Market A")
	rawLot := e.must(e.do("POST", "/api/v1/lots", f.lotBody("KZ-HAM-001", "10.000", "11.5000", nil)), 201)
	p := e.sellable(f)
	quar := e.must(e.do("POST", "/api/v1/warehouses", map[string]any{
		"code": "WH-QUAR", "name": "Karantina", "warehouse_type": "quarantine"}), 201).str("id")
	qLot := e.must(e.do("POST", "/api/v1/lots", fixture{p.item, f.supplier, quar}.lotBody("SUC-Q-001", "10.000", "18.0000", nil)), 201)

	for name, c := range map[string]struct {
		op   map[string]any
		code string
	}{
		"hammadde":  {salesSyncOp(domain.NewID(), cust, f.warehouse, f.item, rawLot.str("id")), "DEVICE_SALE_ITEM_TYPE"},
		"karantina": {salesSyncOp(domain.NewID(), cust, quar, p.item, qLot.str("id")), "DEVICE_SALE_WAREHOUSE"},
	} {
		r := e.must(e.do("POST", "/api/v1/sync/push", map[string]any{"device_id": domain.NewID(), "operations": []any{c.op}}), 200)
		res := r.JSON["results"].([]any)[0].(map[string]any)
		if res["status"] != "rejected" || !strings.Contains(res["message"].(string), c.code) {
			t.Fatalf("%s: rejected + %s beklenirdi: %v", name, c.code, res)
		}
	}
	if n := count(t, `SELECT COUNT(*) FROM sales_order`); n != 0 {
		t.Fatalf("reddedilen satislar yazilmamali: %d", n)
	}
	// Web (REST) satisi kurala tabi degil: ayni hammadde lotu satilabilir.
	e.must(e.do("POST", "/api/v1/sales-orders", salesOrderBody(cust, f.warehouse, saleLine(f.item, rawLot.str("id"), "1.000", "12.0000"))), 201)
}
