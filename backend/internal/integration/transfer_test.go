// Depolar arasi transfer (repo/transfer.go): bakiye iki depo arasinda tasinir, defter cift hareket
// yazar, idempotency, yetersiz stok 409, bloke lot yalnizca karantinaya.
package integration

import (
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
