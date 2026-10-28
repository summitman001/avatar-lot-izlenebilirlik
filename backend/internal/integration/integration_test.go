// Package integration: gercek MySQL (TEST_DATABASE_URL) uzerinde migration, defter trigger'lari ve HTTP API testleri.
//
// Calistirma: `make test-backend` (docker'daki avatar_test veritabanini kullanir).
// TEST_DATABASE_URL yoksa testler atlanir. Guvenlik: veritabani adi "_test" ile bitmelidir, cunku testler
// semayi sifirlar.
package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/db"
	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
	apihttp "github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/http"
	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/repo"
)

var (
	testDB        *sql.DB
	migrationsDir string
)

func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil))) // istek loglarini sustur
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		fmt.Println("TEST_DATABASE_URL tanimli degil: entegrasyon testleri ATLANDI (make test-backend kullanin)")
		os.Exit(0)
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		fmt.Println("TEST_DATABASE_URL cozumlenemedi:", err)
		os.Exit(1)
	}
	if !strings.HasSuffix(cfg.DBName, "_test") {
		fmt.Printf("GUVENLIK: test veritabani adi _test ile bitmeli (su an %q); testler semayi sifirlar\n", cfg.DBName)
		os.Exit(1)
	}
	migrationsDir, _ = filepath.Abs(filepath.Join("..", "..", "migrations"))

	testDB, err = db.Open(context.Background(), dsn, true) // migrate icin multiStatements
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	// Temiz baslangic: onceki kosudan kalan sema varsa indir, yeniden kur.
	if err := db.MigrateDown(testDB, migrationsDir); err != nil {
		fmt.Println("migrate down:", err)
		os.Exit(1)
	}
	if err := db.MigrateUp(testDB, migrationsDir); err != nil {
		fmt.Println("migrate up:", err)
		os.Exit(1)
	}
	code := m.Run()
	testDB.Close()
	os.Exit(code)
}

// ---------------------------------------------------------------------------
// yardimcilar
// ---------------------------------------------------------------------------

// reset tum tablolari bosaltir (migration tablosu haric). TRUNCATE trigger tetiklemez; FK kontrolu gecici kapatilir.
func reset(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	conn, err := testDB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	rows, err := conn.QueryContext(ctx,
		`SELECT table_name FROM information_schema.tables
		 WHERE table_schema = DATABASE() AND table_type = 'BASE TABLE' AND table_name <> 'schema_migrations'`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, n)
	}
	rows.Close()
	if _, err := conn.ExecContext(ctx, `SET FOREIGN_KEY_CHECKS = 0`); err != nil {
		t.Fatal(err)
	}
	for _, tb := range tables {
		if _, err := conn.ExecContext(ctx, "TRUNCATE TABLE `"+tb+"`"); err != nil {
			t.Fatalf("truncate %s: %v", tb, err)
		}
	}
	if _, err := conn.ExecContext(ctx, `SET FOREIGN_KEY_CHECKS = 1`); err != nil {
		t.Fatal(err)
	}
}

type env struct {
	t   *testing.T
	srv *httptest.Server
}

func newEnv(t *testing.T) *env {
	t.Helper()
	reset(t)
	h := apihttp.NewRouter(apihttp.Deps{DB: testDB, Store: repo.New(testDB), CORSOrigins: []string{"*"}})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &env{t: t, srv: srv}
}

type resp struct {
	Status int
	Raw    string
	JSON   map[string]any
}

func (e *env) do(method, path string, body any) resp {
	e.t.Helper()
	var rdr io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rdr = strings.NewReader(b) // ham JSON (ornegin JSON number gondermek icin)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			e.t.Fatal(err)
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, e.srv.URL+path, rdr)
	if err != nil {
		e.t.Fatal(err)
	}
	if rdr != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := resp{Status: res.StatusCode, Raw: string(raw)}
	_ = json.Unmarshal(raw, &out.JSON)
	return out
}

func (e *env) must(r resp, want int) resp {
	e.t.Helper()
	if r.Status != want {
		e.t.Fatalf("status %d (beklenen %d): %s", r.Status, want, r.Raw)
	}
	return r
}

func (r resp) str(path ...string) string {
	var cur any = r.JSON
	for _, p := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = m[p]
	}
	s, _ := cur.(string)
	return s
}

func count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := testDB.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// fixture: bir urun, tedarikci ve depo olusturur ve ID'lerini doner.
type fixture struct{ item, supplier, warehouse string }

func (e *env) fixture() fixture {
	e.t.Helper()
	item := e.must(e.do("POST", "/api/v1/items", map[string]any{
		"code": "KUZU-BUTUN", "name": "Bütün Kuzu", "item_type": "raw", "base_uom": "kg"}), 201)
	sup := e.must(e.do("POST", "/api/v1/suppliers", map[string]any{
		"code": "SUP-HAY", "name": "Yayla Hayvancılık", "supplier_type": "livestock"}), 201)
	wh := e.must(e.do("POST", "/api/v1/warehouses", map[string]any{
		"code": "WH-MAIN", "name": "Ana Depo", "warehouse_type": "internal"}), 201)
	return fixture{item.str("id"), sup.str("id"), wh.str("id")}
}

func (f fixture) lotBody(lotNo, qty, price string, declared *string) map[string]any {
	b := map[string]any{
		"lot_no": lotNo, "item_id": f.item, "supplier_id": f.supplier, "warehouse_id": f.warehouse,
		"received_at": "2026-03-02", "qty": qty, "unit_price": price,
	}
	if declared != nil {
		b["declared_line_total"] = *declared
	}
	return b
}

func sp(s string) *string { return &s }

// ---------------------------------------------------------------------------
// testler
// ---------------------------------------------------------------------------

func tableCount(t *testing.T) int {
	return count(t, `SELECT COUNT(*) FROM information_schema.tables
	                  WHERE table_schema = DATABASE() AND table_name <> 'schema_migrations'`)
}

func TestMigrationsUpDownUp(t *testing.T) {
	if err := db.MigrateDown(testDB, migrationsDir); err != nil {
		t.Fatalf("down: %v", err)
	}
	if n := tableCount(t); n != 0 {
		t.Fatalf("down sonrasi %d tablo/view kaldi", n)
	}
	if err := db.MigrateUp(testDB, migrationsDir); err != nil {
		t.Fatalf("up: %v", err)
	}
	afterFirstUp := tableCount(t)
	if afterFirstUp < 29 {
		t.Fatalf("up sonrasi beklenenden az tablo/view: %d", afterFirstUp)
	}
	// down + up tekrar: sema ayni kalmali
	if err := db.MigrateDown(testDB, migrationsDir); err != nil {
		t.Fatalf("down(2): %v", err)
	}
	if err := db.MigrateUp(testDB, migrationsDir); err != nil {
		t.Fatalf("up(2): %v", err)
	}
	if n := tableCount(t); n != afterFirstUp {
		t.Fatalf("ikinci up sonrasi tablo sayisi %d != %d", n, afterFirstUp)
	}
	for _, name := range []string{"v_lot_balance", "stock_movement", "change_log", "processed_request"} {
		if count(t, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?`, name) != 1 {
			t.Errorf("%s yok", name)
		}
	}
}

// Defter degistirilemez: UPDATE ve DELETE trigger ile reddedilir (SQLSTATE 45000).
func TestStockMovementIsImmutable(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	lot := e.must(e.do("POST", "/api/v1/lots", f.lotBody("KZ-2026-001", "25.000", "11.5000", nil)), 201)
	var mvID string
	if err := testDB.QueryRow(`SELECT id FROM stock_movement WHERE lot_id = ?`, lot.str("id")).Scan(&mvID); err != nil {
		t.Fatal(err)
	}

	for name, stmt := range map[string]string{
		"UPDATE": `UPDATE stock_movement SET qty = 999 WHERE id = ?`,
		"DELETE": `DELETE FROM stock_movement WHERE id = ?`,
	} {
		_, err := testDB.Exec(stmt, mvID)
		var me *mysql.MySQLError
		if !errors.As(err, &me) {
			t.Fatalf("%s: MySQL hatasi beklenirdi, gelen: %v", name, err)
		}
		if string(me.SQLState[:]) != "45000" || !strings.Contains(me.Message, "immutable") {
			t.Errorf("%s: SQLSTATE 45000 / immutable beklenirdi: %v", name, me)
		}
	}
	var qty string
	if err := testDB.QueryRow(`SELECT qty FROM stock_movement WHERE id = ?`, mvID).Scan(&qty); err != nil || qty != "25.000" {
		t.Fatalf("hareket degismemeli: qty=%q err=%v", qty, err)
	}
}

// Isaret kurali: giris tipi negatif miktarla yazilamaz (CHECK).
func TestStockMovementSignRule(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	lot := e.must(e.do("POST", "/api/v1/lots", f.lotBody("ET-2026-001", "10.000", "9.7000", nil)), 201)
	_, err := testDB.Exec(`INSERT INTO stock_movement (id, lot_id, warehouse_id, movement_type, qty, unit_cost, ref_type, ref_id)
	                       VALUES (?, ?, ?, 'purchase_in', -1.000, 9.7, 'test', ?)`,
		domain.NewID(), lot.str("id"), f.warehouse, domain.NewID())
	if err == nil {
		t.Fatal("purchase_in negatif miktarla yazilabildi")
	}
}

// Decimal'ler JSON'da string gidip gelir; JSON number reddedilir.
func TestDecimalStringsOverHTTP(t *testing.T) {
	e := newEnv(t)
	r := e.must(e.do("POST", "/api/v1/items", `{"code":"SUCUK-NARIN-1KG","name":"Narin Sucuk 1 kg","item_type":"finished",
		"base_uom":"adet","unit_weight_kg":"1.000","default_sale_price":"24.9000"}`), 201)
	if r.str("default_sale_price") != "24.9000" || r.str("unit_weight_kg") != "1.000" {
		t.Fatalf("decimal string bekleniyordu: %s", r.Raw)
	}
	got := e.must(e.do("GET", "/api/v1/items/"+r.str("id"), nil), 200)
	if !strings.Contains(got.Raw, `"default_sale_price":"24.9000"`) || !strings.Contains(got.Raw, `"unit_weight_kg":"1.000"`) {
		t.Fatalf("round-trip bozuldu: %s", got.Raw)
	}
	// JSON number (float riski) ve fazla ondalik reddedilir
	for name, body := range map[string]string{
		"number":      `{"code":"X1","name":"x","item_type":"raw","base_uom":"kg","default_sale_price":24.9}`,
		"5 ondalik":   `{"code":"X2","name":"x","item_type":"raw","base_uom":"kg","default_sale_price":"24.90001"}`,
		"unit_weight": `{"code":"X3","name":"x","item_type":"raw","base_uom":"kg","unit_weight_kg":1}`,
	} {
		bad := e.do("POST", "/api/v1/items", body)
		if bad.Status != 400 || bad.str("error", "code") != "bad_request" {
			t.Errorf("%s: 400 bad_request beklenirdi: %d %s", name, bad.Status, bad.Raw)
		}
	}
}

func TestErrorFormat(t *testing.T) {
	e := newEnv(t)
	nf := e.do("GET", "/api/v1/items/"+domain.NewID(), nil)
	if nf.Status != 404 || nf.str("error", "code") != "not_found" || nf.str("error", "message") == "" {
		t.Fatalf("404 not_found beklenirdi: %s", nf.Raw)
	}
	inv := e.do("POST", "/api/v1/items", map[string]any{"code": "", "name": "x", "item_type": "raw", "base_uom": "kg"})
	if inv.Status != 422 || inv.str("error", "code") != "validation_error" {
		t.Fatalf("422 validation_error beklenirdi: %s", inv.Raw)
	}
	e.must(e.do("POST", "/api/v1/items", map[string]any{"code": "A", "name": "a", "item_type": "raw", "base_uom": "kg"}), 201)
	dup := e.do("POST", "/api/v1/items", map[string]any{"code": "A", "name": "b", "item_type": "raw", "base_uom": "kg"})
	if dup.Status != 409 || dup.str("error", "code") != "conflict" {
		t.Fatalf("409 conflict beklenirdi: %s", dup.Raw)
	}
}

// POST /lots: lot + belge satiri + purchase_in tek transaction'da; v_lot_balance dogru.
func TestCreateLotCreatesMovementAtomically(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	lot := e.must(e.do("POST", "/api/v1/lots", f.lotBody("ET-2026-001", "100.000", "9.7000", nil)), 201)

	if lot.str("status") != "active" || lot.str("initial_qty") != "100.000" || lot.str("unit_cost") != "9.7000" ||
		lot.str("qty_on_hand") != "100.000" || lot.str("purchase", "line_total") != "970.0000" {
		t.Fatalf("beklenmeyen lot yaniti: %s", lot.Raw)
	}
	if n := count(t, `SELECT COUNT(*) FROM stock_movement WHERE lot_id = ? AND movement_type = 'purchase_in' AND qty = 100.000`, lot.str("id")); n != 1 {
		t.Fatalf("tam 1 purchase_in beklenirdi, %d", n)
	}
	var onHand string
	if err := testDB.QueryRow(`SELECT qty_on_hand FROM v_lot_balance WHERE lot_id = ? AND warehouse_id = ?`, lot.str("id"), f.warehouse).Scan(&onHand); err != nil || onHand != "100.000" {
		t.Fatalf("v_lot_balance = %q err=%v", onHand, err)
	}
	// GET /lots/{id} depo kirilimi
	got := e.must(e.do("GET", "/api/v1/lots/"+lot.str("id"), nil), 200)
	bal, _ := got.JSON["balances"].([]any)
	if len(bal) != 1 || bal[0].(map[string]any)["qty_on_hand"] != "100.000" || bal[0].(map[string]any)["warehouse_code"] != "WH-MAIN" {
		t.Fatalf("balances beklenmeyen: %s", got.Raw)
	}
	// lot change_log'a yazilmis (sync pull icin)
	if n := count(t, `SELECT COUNT(*) FROM change_log WHERE entity = 'lot' AND entity_id = ?`, lot.str("id")); n != 1 {
		t.Fatalf("lot icin 1 change_log beklenirdi, %d", n)
	}
	// payload bakiyeli: cihaz yeni lotun depo stogunu pull'dan ogrenir
	var payloadQty, payloadWhQty string
	if err := testDB.QueryRow(`SELECT JSON_UNQUOTE(JSON_EXTRACT(payload_json, '$.qty_on_hand')),
	                                  JSON_UNQUOTE(JSON_EXTRACT(payload_json, '$.balances[0].qty_on_hand'))
	                           FROM change_log WHERE entity = 'lot' AND entity_id = ?`, lot.str("id")).Scan(&payloadQty, &payloadWhQty); err != nil ||
		payloadQty != "100.000" || payloadWhQty != "100.000" {
		t.Fatalf("change_log payload bakiyesi = %q / %q err=%v", payloadQty, payloadWhQty, err)
	}
}

// Ikinci adimda hata olursa onceki adimlar geri alinir (belge, lot, hareket kalmaz).
func TestCreateLotRollsBackOnFailure(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	e.must(e.do("POST", "/api/v1/lots", f.lotBody("ET-2026-001", "100.000", "9.7000", nil)), 201)

	// ayni lot_no, farkli belge: belge INSERT'i basarili olur, lot INSERT'i UNIQUE'e carpar
	body := f.lotBody("ET-2026-001", "5.000", "9.7000", nil)
	body["doc_no"] = "PR-BASKA"
	dup := e.do("POST", "/api/v1/lots", body)
	if dup.Status != 409 {
		t.Fatalf("409 beklenirdi: %d %s", dup.Status, dup.Raw)
	}
	if n := count(t, `SELECT COUNT(*) FROM purchase_receipt`); n != 1 {
		t.Errorf("belge geri alinmadi: %d", n)
	}
	if n := count(t, `SELECT COUNT(*) FROM stock_movement`); n != 1 {
		t.Errorf("hareket sayisi 1 olmali: %d", n)
	}
	if n := count(t, `SELECT COUNT(*) FROM lot`); n != 1 {
		t.Errorf("lot sayisi 1 olmali: %d", n)
	}
}

// has_mismatch: 25 x 11.50 = 287.50 != 351.00 => TRUE
func TestPurchaseLineMismatch(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()

	kz := e.must(e.do("POST", "/api/v1/lots", f.lotBody("KZ-2026-001", "25.000", "11.5000", sp("351.0000"))), 201)
	if kz.JSON["purchase"].(map[string]any)["has_mismatch"] != true {
		t.Fatalf("has_mismatch true beklenirdi: %s", kz.Raw)
	}
	if kz.str("purchase", "line_total") != "287.5000" || kz.str("purchase", "declared_line_total") != "351.0000" {
		t.Fatalf("tutarlar: %s", kz.Raw)
	}
	if n := count(t, `SELECT COUNT(*) FROM purchase_receipt_line WHERE lot_id = ? AND has_mismatch = TRUE`, kz.str("id")); n != 1 {
		t.Fatalf("DB'de has_mismatch=TRUE beklenirdi")
	}

	ok := e.must(e.do("POST", "/api/v1/lots", f.lotBody("KZ-2026-002", "25.000", "11.5000", sp("287.5000"))), 201)
	if ok.JSON["purchase"].(map[string]any)["has_mismatch"] != false {
		t.Fatalf("beyan esitse has_mismatch false olmali: %s", ok.Raw)
	}
	none := e.must(e.do("POST", "/api/v1/lots", f.lotBody("KZ-2026-003", "25.000", "11.5000", nil)), 201)
	if none.JSON["purchase"].(map[string]any)["has_mismatch"] != false {
		t.Fatalf("beyan yoksa has_mismatch false olmali: %s", none.Raw)
	}

	// filtre
	list := e.must(e.do("GET", "/api/v1/lots?has_mismatch=true", nil), 200)
	items, _ := list.JSON["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["lot_no"] != "KZ-2026-001" {
		t.Fatalf("has_mismatch filtresi: %s", list.Raw)
	}
}

func TestListLotsFilters(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	e.must(e.do("POST", "/api/v1/lots", f.lotBody("ET-2026-001", "100.000", "9.7000", nil)), 201)
	l2 := e.must(e.do("POST", "/api/v1/lots", f.lotBody("ET-2026-002", "10.000", "9.7000", nil)), 201)
	if _, err := testDB.Exec(`UPDATE lot SET status = 'blocked', blocked_reason = 'test' WHERE id = ?`, l2.str("id")); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]int{
		"/api/v1/lots":                           2,
		"/api/v1/lots?status=blocked":            1,
		"/api/v1/lots?q=002":                     1,
		"/api/v1/lots?item_id=" + f.item:         2,
		"/api/v1/lots?item_id=" + domain.NewID(): 0,
	} {
		r := e.must(e.do("GET", path, nil), 200)
		if items, _ := r.JSON["items"].([]any); len(items) != want {
			t.Errorf("%s: %d kayit bekleniyordu, %d geldi", path, want, len(items))
		}
	}
	if bad := e.do("GET", "/api/v1/lots?status=nope", nil); bad.Status != 400 {
		t.Errorf("gecersiz status 400 olmali: %d", bad.Status)
	}
}

func TestReverseMovement(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	lot := e.must(e.do("POST", "/api/v1/lots", f.lotBody("ET-2026-001", "10.000", "9.7000", nil)), 201)
	list := e.must(e.do("GET", "/api/v1/stock-movements?lot_id="+lot.str("id"), nil), 200)
	mvID := list.JSON["items"].([]any)[0].(map[string]any)["id"].(string)

	rev := e.must(e.do("POST", "/api/v1/stock-movements/"+mvID+"/reverse", map[string]any{"note": "yanlis giris"}), 201)
	if rev.str("movement_type") != "reversal" || rev.str("qty") != "-10.000" || rev.str("reverses_movement_id") != mvID || rev.str("ref_type") != "stock_movement" {
		t.Fatalf("ters kayit: %s", rev.Raw)
	}
	var onHand string
	if err := testDB.QueryRow(`SELECT qty_on_hand FROM v_lot_balance WHERE lot_id = ?`, lot.str("id")).Scan(&onHand); err != nil || onHand != "0.000" {
		t.Fatalf("bakiye 0 olmali: %q %v", onHand, err)
	}
	// ikinci geri alma 409, ters kaydin geri alinmasi 422, olmayan hareket 404
	if r := e.do("POST", "/api/v1/stock-movements/"+mvID+"/reverse", nil); r.Status != 409 {
		t.Errorf("zaten geri alinmis: 409 beklenirdi: %d %s", r.Status, r.Raw)
	}
	if r := e.do("POST", "/api/v1/stock-movements/"+rev.str("id")+"/reverse", nil); r.Status != 422 {
		t.Errorf("ters kayit geri alinamaz: 422 beklenirdi: %d %s", r.Status, r.Raw)
	}
	if r := e.do("POST", "/api/v1/stock-movements/"+domain.NewID()+"/reverse", nil); r.Status != 404 {
		t.Errorf("404 beklenirdi: %d", r.Status)
	}
}

func TestReverseRefusesNegativeStock(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	lot := e.must(e.do("POST", "/api/v1/lots", f.lotBody("ET-2026-001", "10.000", "9.7000", nil)), 201)
	list := e.must(e.do("GET", "/api/v1/stock-movements?lot_id="+lot.str("id"), nil), 200)
	purchaseIn := list.JSON["items"].([]any)[0].(map[string]any)["id"].(string)
	// 4 kg satildi (sale_out); alis girisi geri alinirsa stok -4 olur
	if _, err := testDB.Exec(`INSERT INTO stock_movement (id, lot_id, warehouse_id, movement_type, qty, unit_cost, ref_type, ref_id)
	                          VALUES (?, ?, ?, 'sale_out', -4.000, 9.7, 'test', ?)`,
		domain.NewID(), lot.str("id"), f.warehouse, domain.NewID()); err != nil {
		t.Fatal(err)
	}
	r := e.do("POST", "/api/v1/stock-movements/"+purchaseIn+"/reverse", nil)
	if r.Status != 409 || !strings.Contains(r.str("error", "message"), "negatife") {
		t.Fatalf("409 (negatif stok) beklenirdi: %d %s", r.Status, r.Raw)
	}
	if n := count(t, `SELECT COUNT(*) FROM stock_movement WHERE movement_type = 'reversal'`); n != 0 {
		t.Fatalf("reddedilen geri alma kayit birakmamali")
	}
}

// salesSyncOp gecerli bir sales_order/create push operasyonu olusturur.
func salesSyncOp(opID, customerID, warehouseID, itemID, lotID string) map[string]any {
	return map[string]any{
		"op_id": opID, "entity": "sales_order", "action": "create",
		"payload": map[string]any{
			"customer_id": customerID, "warehouse_id": warehouseID, "sold_at": "2026-03-02T10:00:00Z",
			"lines": []any{map[string]any{"item_id": itemID, "lot_id": lotID, "qty": "1.000", "unit_price": "24.9000"}},
		},
		"client_created_at": "2026-03-02T10:00:00Z",
	}
}

// Ayni op_id iki kez gelirse ikincisi "duplicate"; ikinci kez stok DUSMEZ (bkz. TestSyncPushSalesOrder).
func TestSyncPushDuplicateOpID(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	cust := e.must(e.do("POST", "/api/v1/customers", map[string]any{"code": "MKT-A", "name": "Market A", "customer_type": "market"}), 201)
	lot := e.must(e.do("POST", "/api/v1/lots", f.lotBody("ET-2026-001", "100.000", "9.7000", nil)), 201)

	device, op := domain.NewID(), domain.NewID()
	syncOp := func(opID string) map[string]any {
		return salesSyncOp(opID, cust.str("id"), f.warehouse, f.item, lot.str("id"))
	}
	push := func(opID string) resp {
		return e.must(e.do("POST", "/api/v1/sync/push", map[string]any{
			"device_id": device, "operations": []any{syncOp(opID)}}), 200)
	}
	status := func(r resp) string {
		return r.JSON["results"].([]any)[0].(map[string]any)["status"].(string)
	}
	first, second := push(op), push(op)
	if status(first) != "applied" {
		t.Fatalf("ilk gonderim applied olmali: %s", first.Raw)
	}
	if status(second) != "duplicate" {
		t.Fatalf("ikinci gonderim duplicate olmali: %s", second.Raw)
	}
	if _, ok := second.JSON["server_seq"]; !ok {
		t.Fatalf("server_seq eksik: %s", second.Raw)
	}
	if n := count(t, `SELECT COUNT(*) FROM processed_request WHERE op_id = ?`, op); n != 1 {
		t.Fatalf("processed_request'te tek kayit olmali: %d", n)
	}
	// ikinci kez (duplicate) stok dusmedi: tek sale_out hareketi var, bakiye tek dusus kadar.
	if n := count(t, `SELECT COUNT(*) FROM stock_movement WHERE lot_id = ? AND movement_type = 'sale_out'`, lot.str("id")); n != 1 {
		t.Fatalf("duplicate op_id ikinci kez stok dusurmemeli: %d sale_out hareketi var", n)
	}

	// ayni istekte iki kez ayni op_id
	op2 := domain.NewID()
	both := e.must(e.do("POST", "/api/v1/sync/push", map[string]any{
		"device_id": device, "operations": []any{syncOp(op2), syncOp(op2)}}), 200)
	rs := both.JSON["results"].([]any)
	if rs[0].(map[string]any)["status"] == "duplicate" || rs[1].(map[string]any)["status"] != "duplicate" {
		t.Fatalf("batch icinde tekrar: %s", both.Raw)
	}
	// gecersiz op_id reddedilir, istek 200 kalir
	bad := e.must(e.do("POST", "/api/v1/sync/push", map[string]any{
		"device_id": device, "operations": []any{syncOp("abc")}}), 200)
	if bad.JSON["results"].([]any)[0].(map[string]any)["status"] != "rejected" {
		t.Fatalf("gecersiz op_id rejected olmali: %s", bad.Raw)
	}
	// device_id zorunlu
	if r := e.do("POST", "/api/v1/sync/push", map[string]any{"device_id": "x", "operations": []any{syncOp(domain.NewID())}}); r.Status != 422 {
		t.Errorf("gecersiz device_id 422 olmali: %d", r.Status)
	}
}

func TestSyncPull(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	cust := e.must(e.do("POST", "/api/v1/customers", map[string]any{"code": "MKT-A", "name": "Market A", "customer_type": "market"}), 201)
	lot := e.must(e.do("POST", "/api/v1/lots", f.lotBody("ET-2026-001", "100.000", "9.7000", nil)), 201)
	// suppliers sync'e acik degil
	pull := e.must(e.do("GET", "/api/v1/sync/pull?since_seq=0", nil), 200)
	changes := pull.JSON["changes"].([]any)
	entities := map[string]int{}
	for _, c := range changes {
		entities[c.(map[string]any)["entity"].(string)]++
	}
	if entities["item"] != 1 || entities["warehouse"] != 1 || entities["customer"] != 1 || entities["lot"] != 1 || entities["supplier"] != 0 {
		t.Fatalf("entity dagilimi: %v", entities)
	}
	next := int64(pull.JSON["next_seq"].(float64))
	if next != int64(len(changes)) || pull.JSON["has_more"] != false {
		t.Fatalf("next_seq/has_more: %s", pull.Raw)
	}
	// payload API JSON'unun aynisi (decimal string)
	for _, c := range changes {
		m := c.(map[string]any)
		if m["entity"] == "lot" && m["payload"].(map[string]any)["initial_qty"] != "100.000" {
			t.Fatalf("lot payload: %v", m)
		}
	}

	// next_seq'ten sonra degisiklik yok
	empty := e.must(e.do("GET", fmt.Sprintf("/api/v1/sync/pull?since_seq=%d", next), nil), 200)
	if len(empty.JSON["changes"].([]any)) != 0 || int64(empty.JSON["next_seq"].(float64)) != next {
		t.Fatalf("bos cekme: %s", empty.Raw)
	}
	// guncelleme yeni seq uretir
	e.must(e.do("PUT", "/api/v1/customers/"+cust.str("id"), map[string]any{"code": "MKT-A", "name": "Market A Yeni", "customer_type": "market"}), 200)
	upd := e.must(e.do("GET", fmt.Sprintf("/api/v1/sync/pull?since_seq=%d", next), nil), 200)
	if len(upd.JSON["changes"].([]any)) != 1 {
		t.Fatalf("1 degisiklik beklenirdi: %s", upd.Raw)
	}
	// limit + has_more
	page := e.must(e.do("GET", "/api/v1/sync/pull?since_seq=0&limit=2", nil), 200)
	if len(page.JSON["changes"].([]any)) != 2 || page.JSON["has_more"] != true || page.JSON["next_seq"].(float64) != 2 {
		t.Fatalf("sayfalama: %s", page.Raw)
	}
	// bloke lotlar
	if _, err := testDB.Exec(`UPDATE lot SET status = 'recalled' WHERE id = ?`, lot.str("id")); err != nil {
		t.Fatal(err)
	}
	blocked := e.must(e.do("GET", "/api/v1/sync/pull?since_seq=999999", nil), 200)
	ids := blocked.JSON["blocked_lot_ids"].([]any)
	if len(ids) != 1 || ids[0] != lot.str("id") {
		t.Fatalf("blocked_lot_ids: %s", blocked.Raw)
	}
}

// Istemci uretimli UUIDv7 kabul edilir; v4 reddedilir.
func TestClientGeneratedIDs(t *testing.T) {
	e := newEnv(t)
	id := domain.NewID()
	r := e.must(e.do("POST", "/api/v1/warehouses", map[string]any{"id": id, "code": "W1", "name": "W", "warehouse_type": "vehicle"}), 201)
	if r.str("id") != id {
		t.Fatalf("istemci id'si korunmali: %s", r.Raw)
	}
	bad := e.do("POST", "/api/v1/warehouses", map[string]any{"id": "6ba7b810-9dad-41d1-80b4-00c04fd430c8", "code": "W2", "name": "W", "warehouse_type": "vehicle"})
	if bad.Status != 422 {
		t.Fatalf("UUIDv4 reddedilmeli: %d %s", bad.Status, bad.Raw)
	}
}

func TestHealthAndOpenAPI(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/health", "/api/v1/health"} {
		if r := e.do("GET", p, nil); r.Status != 200 || r.str("status") != "ok" {
			t.Errorf("%s: %d %s", p, r.Status, r.Raw)
		}
	}
	spec := e.do("GET", "/api/v1/openapi.yaml", nil)
	if spec.Status != 200 || !strings.HasPrefix(spec.Raw, "openapi: 3.1") {
		t.Errorf("openapi: %d %.40s", spec.Status, spec.Raw)
	}
}
