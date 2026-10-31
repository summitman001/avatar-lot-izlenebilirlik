// Depolar arasi transfer (repo/transfer.go): bakiye iki depo arasinda tasinir, defter cift hareket
// yazar, idempotency, yetersiz stok 409, bloke lot yalnizca karantinaya.
package integration

import (
	"strings"
	"testing"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
)

func balance(t *testing.T, lotID, warehouseID string) string {
	t.Helper()
	var q string
	if err := testDB.QueryRow(`SELECT COALESCE(SUM(qty), 0) FROM stock_movement WHERE lot_id = ? AND warehouse_id = ?`,
		lotID, warehouseID).Scan(&q); err != nil {
		t.Fatal(err)
	}
	return q
}

func TestStockTransfer(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	vehicle := e.must(e.do("POST", "/api/v1/warehouses", map[string]any{
		"code": "WH-ARAC-1", "name": "Araç 1", "warehouse_type": "vehicle"}), 201).str("id")
	quarantine := e.must(e.do("POST", "/api/v1/warehouses", map[string]any{
		"code": "WH-KAR", "name": "Karantina", "warehouse_type": "quarantine"}), 201).str("id")
	lot := e.must(e.do("POST", "/api/v1/lots", f.lotBody("ET-2026-001", "100.000", "9.7000", nil)), 201).str("id")

	id := domain.NewID()
	body := map[string]any{
		"id": id, "from_warehouse_id": f.warehouse, "to_warehouse_id": vehicle, "note": "sabah yuklemesi",
		"lines": []map[string]any{{"lot_id": lot, "qty": "30.000"}},
	}
	tr := e.must(e.do("POST", "/api/v1/stock-transfers", body), 201)
	if tr.str("id") != id || tr.str("transfer_no") == "" || tr.str("to_warehouse_code") != "WH-ARAC-1" {
		t.Fatalf("transfer: %s", tr.Raw)
	}
	e.must(e.do("POST", "/api/v1/stock-transfers", body), 201) // tekrar deneme: ayni transfer
	if b := balance(t, lot, f.warehouse); b != "70.000" {
		t.Fatalf("kaynak bakiye 70.000 olmali: %s", b)
	}
	if b := balance(t, lot, vehicle); b != "30.000" {
		t.Fatalf("hedef bakiye 30.000 olmali: %s", b)
	}
	if n := count(t, `SELECT COUNT(*) FROM stock_movement WHERE ref_type = 'stock_transfer' AND ref_id = ?`, id); n != 2 {
		t.Fatalf("transfer_out + transfer_in beklenirdi: %d", n)
	}
	if n := count(t, `SELECT COUNT(*) FROM change_log WHERE entity = 'lot' AND entity_id = ?`, lot); n < 2 {
		t.Fatalf("bakiye degisimi senkrona yazilmali: %d", n)
	}
	got := e.must(e.do("GET", "/api/v1/stock-transfers?warehouse_id="+vehicle, nil), 200)
	if items, _ := got.JSON["items"].([]any); len(items) != 1 {
		t.Fatalf("liste: %s", got.Raw)
	}

	// Kaynakta yetmeyen miktar 409, hicbir sey yazilmaz.
	over := e.do("POST", "/api/v1/stock-transfers", map[string]any{
		"from_warehouse_id": vehicle, "to_warehouse_id": f.warehouse,
		"lines": []map[string]any{{"lot_id": lot, "qty": "31.000"}},
	})
	if over.Status != 409 {
		t.Fatalf("yetersiz stok 409 olmali: %d %s", over.Status, over.Raw)
	}
	if n := count(t, `SELECT COUNT(*) FROM stock_transfer`); n != 1 {
		t.Fatalf("reddedilen transfer kalmamali: %d", n)
	}

	long := e.do("POST", "/api/v1/stock-transfers", map[string]any{
		"from_warehouse_id": f.warehouse, "to_warehouse_id": vehicle, "note": strings.Repeat("a", 401),
		"lines": []map[string]any{{"lot_id": lot, "qty": "1.000"}},
	})
	if long.Status != 422 {
		t.Fatalf("400 karakteri asan not 422 olmali: %d %s", long.Status, long.Raw)
	}

	// Bloke lot arac deposuna gidemez, karantinaya gidebilir.
	e.must(e.do("POST", "/api/v1/lots/"+lot+"/block", map[string]any{"reason": "numune bekleniyor"}), 200)
	blocked := e.do("POST", "/api/v1/stock-transfers", map[string]any{
		"from_warehouse_id": f.warehouse, "to_warehouse_id": vehicle,
		"lines": []map[string]any{{"lot_id": lot, "qty": "10.000"}},
	})
	if blocked.Status != 422 {
		t.Fatalf("bloke lot arac deposuna 422 olmali: %d %s", blocked.Status, blocked.Raw)
	}
	e.must(e.do("POST", "/api/v1/stock-transfers", map[string]any{
		"from_warehouse_id": f.warehouse, "to_warehouse_id": quarantine,
		"lines": []map[string]any{{"lot_id": lot, "qty": "70.000"}},
	}), 201)
	if b := balance(t, lot, quarantine); b != "70.000" {
		t.Fatalf("karantina bakiyesi 70.000 olmali: %s", b)
	}
}

// Dis uretici deposundaki stok uretim emrine aittir: transfer, fire ve yeniden isleme ona dokunamaz.
// Aksi halde teslimdeki tuketim dis depoyu eksiye dusururdu (ayni et hem tuketilmis hem ana depoda).
func TestExternalProducerStockOnlyMovesWithProductionOrder(t *testing.T) {
	e := newEnv(t)
	f := e.prodFixture()
	lotBody := func(no, qty string, item string) map[string]any {
		return map[string]any{"lot_no": no, "item_id": item, "supplier_id": f.supplier, "warehouse_id": f.mainWh,
			"received_at": "2026-03-02", "qty": qty, "unit_price": "10.0000"}
	}
	meat := e.must(e.do("POST", "/api/v1/lots", lotBody("ET-2026-090", "50.000", f.meatItem)), 201).str("id")
	spice := e.must(e.do("POST", "/api/v1/lots", lotBody("BHR-2026-090", "1.000", f.spiceItem)), 201).str("id")
	other := e.must(e.do("POST", "/api/v1/lots", lotBody("ET-2026-091", "10.000", f.meatItem)), 201).str("id")
	order := e.must(e.do("POST", "/api/v1/production-orders", map[string]any{
		"product_item_id": f.product, "producer_supplier_id": f.producer, "external_warehouse_id": f.extWh,
		"planned_qty": "40", "service_unit_price": "2.0000",
	}), 201).str("id")
	e.must(e.do("POST", "/api/v1/production-orders/"+order+"/send-materials", map[string]any{
		"lines": []map[string]any{{"lot_id": meat, "qty": "50.000"}, {"lot_id": spice, "qty": "1.000"}},
	}), 200)

	for name, r := range map[string]resp{
		"dis depodan transfer": e.do("POST", "/api/v1/stock-transfers", map[string]any{
			"from_warehouse_id": f.extWh, "to_warehouse_id": f.mainWh, "lines": []map[string]any{{"lot_id": meat, "qty": "50.000"}}}),
		"dis depoya transfer": e.do("POST", "/api/v1/stock-transfers", map[string]any{
			"from_warehouse_id": f.mainWh, "to_warehouse_id": f.extWh, "lines": []map[string]any{{"lot_id": other, "qty": "5.000"}}}),
		"dis depoda fire": e.do("POST", "/api/v1/lots/"+meat+"/waste", map[string]any{
			"warehouse_id": f.extWh, "qty": "1.000", "reason": "bozulma"}),
		"dis depoda yeniden isleme": e.do("POST", "/api/v1/reprocess-orders", map[string]any{
			"warehouse_id": f.extWh, "inputs": []map[string]any{{"lot_id": meat, "qty": "10.000"}},
			"output": map[string]any{"item_id": f.meatItem, "qty": "10.000"}}),
	} {
		if r.Status != 422 {
			t.Errorf("%s 422 olmali: %d %s", name, r.Status, r.Raw)
		}
	}

	e.must(e.do("POST", "/api/v1/production-orders/"+order+"/receive", map[string]any{"received_qty": "40"}), 200)
	if b := balance(t, meat, f.extWh); b != "0.000" {
		t.Fatalf("teslimden sonra dis depo 0 olmali (eksi olmamali): %s", b)
	}
	if b := balance(t, meat, f.mainWh); b != "0.000" {
		t.Fatalf("sevk edilen et ana depoda gorunmemeli: %s", b)
	}
}
