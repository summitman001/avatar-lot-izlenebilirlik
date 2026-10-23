// Komut seed: ana veriler + alis belgeleri/lotlari/purchase_in hareketleri. IDEMPOTENT: tekrar calistirmak
// var olan kayitlari atlar (kod / lot_no ile bulunur). Uretim ve satis senaryosu sonraki gunlerde
// servisler uzerinden eklenecek. Tum firma isimleri hayalidir.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/shopspring/decimal"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/config"
	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/db"
	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/repo"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))
	if err := run(context.Background()); err != nil {
		slog.Error("seed basarisiz", "err", err)
		os.Exit(1)
	}
}

func str(s string) *string { return &s }

func qty(s string) domain.Qty      { return domain.NewQty(decimal.RequireFromString(s)) }
func amt(s string) domain.Amount   { return domain.NewAmount(decimal.RequireFromString(s)) }
func amtP(s string) *domain.Amount { a := amt(s); return &a }
func qtyP(s string) *domain.Qty    { q := qty(s); return &q }

func run(ctx context.Context) error {
	cfg := config.Load()
	conn, err := db.Open(ctx, cfg.DSN, false)
	if err != nil {
		return err
	}
	defer conn.Close()
	s := &seeder{store: repo.New(conn), db: conn, ids: map[string]string{}}

	if err := s.suppliers(ctx); err != nil {
		return err
	}
	if err := s.warehouses(ctx); err != nil {
		return err
	}
	if err := s.customers(ctx); err != nil {
		return err
	}
	if err := s.items(ctx); err != nil {
		return err
	}
	if err := s.recipe(ctx, cfg.GoldenDir); err != nil {
		return err
	}
	if err := s.purchases(ctx); err != nil {
		return err
	}
	slog.Info("seed tamam", "olusturulan", s.created, "atlanan", s.skipped)
	return nil
}

type seeder struct {
	store            *repo.Store
	db               *sql.DB
	ids              map[string]string // "tablo:kod" -> id
	created, skipped int
}

// lookup tablo+kod ile id arar; yoksa "" doner.
func (s *seeder) lookup(ctx context.Context, table, code string) (string, error) {
	if id, ok := s.ids[table+":"+code]; ok {
		return id, nil
	}
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM `+table+` WHERE code = ?`, code).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err == nil {
		s.ids[table+":"+code] = id
	}
	return id, err
}

func (s *seeder) must(ctx context.Context, table, code string) string {
	id, err := s.lookup(ctx, table, code)
	if err != nil || id == "" {
		panic("seed bagimliligi bulunamadi: " + table + " " + code)
	}
	return id
}

func (s *seeder) record(table, code, id string, existed bool) {
	s.ids[table+":"+code] = id
	if existed {
		s.skipped++
	} else {
		s.created++
		slog.Info("olusturuldu", "tablo", table, "kod", code)
	}
}

func (s *seeder) suppliers(ctx context.Context) error {
	for _, v := range []domain.Supplier{
		{Code: "SUP-ET", Name: "Palandöken Et Tedarik", SupplierType: "material", ContactName: str("Kemal Aydoğan"), Phone: str("+90 555 010 0001"), Email: str("siparis@palandoken-et.example"), Address: str("Organize Sanayi Bölgesi 3. Cadde No:12, Erzurum")},
		{Code: "SUP-BHR", Name: "Doğu Baharat", SupplierType: "material", ContactName: str("Selin Koç"), Phone: str("+90 555 010 0002"), Email: str("satis@dogu-baharat.example"), Address: str("Baharat Çarşısı Sok. No:7, Malatya")},
		{Code: "SUP-BGR", Name: "Anadolu Bağırsak", SupplierType: "material", ContactName: str("Hasan Yıldız"), Phone: str("+90 555 010 0003"), Email: str("info@anadolu-bagirsak.example"), Address: str("Sanayi Mah. 5. Sok. No:21, Kayseri")},
		{Code: "SUP-URT", Name: "Narin Dış Üretim Atölyesi", SupplierType: "external_producer", ContactName: str("Narin Çelik"), Phone: str("+90 555 010 0004"), Email: str("atolye@narin-uretim.example"), Address: str("Küçük Sanayi Sitesi B Blok No:4, Kayseri")},
		{Code: "SUP-HAY", Name: "Yayla Hayvancılık", SupplierType: "livestock", ContactName: str("Murat Arslan"), Phone: str("+90 555 010 0005"), Email: str("ciftlik@yayla-hayvancilik.example"), Address: str("Yayla Köyü Yolu 8. km, Kars")},
	} {
		existing, err := s.lookup(ctx, "supplier", v.Code)
		if err != nil {
			return err
		}
		if existing != "" {
			s.record("supplier", v.Code, existing, true)
			continue
		}
		out, err := s.store.CreateSupplier(ctx, v)
		if err != nil {
			return err
		}
		s.record("supplier", v.Code, out.ID, false)
	}
	return nil
}

func (s *seeder) warehouses(ctx context.Context) error {
	for _, v := range []domain.Warehouse{
		{Code: "WH-MAIN", Name: "Ana Depo", WarehouseType: "internal"},
		{Code: "WH-EXT-01", Name: "Narin Atölyesi Deposu (dış üretici)", WarehouseType: "external_producer"},
		{Code: "WH-QUAR", Name: "Karantina Deposu", WarehouseType: "quarantine"},
	} {
		existing, err := s.lookup(ctx, "warehouse", v.Code)
		if err != nil {
			return err
		}
		if existing != "" {
			s.record("warehouse", v.Code, existing, true)
			continue
		}
		out, err := s.store.CreateWarehouse(ctx, v)
		if err != nil {
			return err
		}
		s.record("warehouse", v.Code, out.ID, false)
	}
	return nil
}

func (s *seeder) customers(ctx context.Context) error {
	for _, v := range []domain.Customer{
		{Code: "MKT-A", Name: "Market A", CustomerType: "market", ContactName: str("Ayşe Demir"), Phone: str("+90 555 020 0001"), Email: str("satinalma@market-a.example"), Address: str("Cumhuriyet Cad. No:15, Kayseri")},
		{Code: "MKT-B", Name: "Market B", CustomerType: "market", ContactName: str("Burak Şahin"), Phone: str("+90 555 020 0002"), Email: str("siparis@market-b.example"), Address: str("Atatürk Bul. No:102, Ankara")},
		{Code: "MKT-C", Name: "Market C", CustomerType: "market", ContactName: str("Canan Özkan"), Phone: str("+90 555 020 0003"), Email: str("tedarik@market-c.example"), Address: str("İstiklal Sok. No:9, Konya")},
	} {
		existing, err := s.lookup(ctx, "customer", v.Code)
		if err != nil {
			return err
		}
		if existing != "" {
			s.record("customer", v.Code, existing, true)
			continue
		}
		out, err := s.store.CreateCustomer(ctx, v)
		if err != nil {
			return err
		}
		s.record("customer", v.Code, out.ID, false)
	}
	return nil
}

func (s *seeder) items(ctx context.Context) error {
	for _, v := range []domain.Item{
		{Code: "ET-DANA", Name: "Dana Eti", ItemType: "raw", BaseUOM: "kg"},
		{Code: "BAHARAT-SUCUK", Name: "Sucuk Baharatı", ItemType: "raw", BaseUOM: "kg"},
		{Code: "BAGIRSAK", Name: "Bağırsak", ItemType: "raw", BaseUOM: "kg"},
		// VARSAYIM: default_sale_price 24.90 yarışma dokümanından gelmez (docs/assumptions.md, "VKP varsayımı").
		{Code: "SUCUK-NARIN-1KG", Name: "Narin Sucuk 1 kg", ItemType: "finished", BaseUOM: "adet", UnitWeightKg: qtyP("1.000"), DefaultSalePrice: amtP("24.9000")},
		{Code: "KUZU-BUTUN", Name: "Bütün Kuzu", ItemType: "raw", BaseUOM: "kg"},
		{Code: "KUZU-BUT", Name: "Kuzu But", ItemType: "cut_part", BaseUOM: "kg"},
		{Code: "KUZU-KOL", Name: "Kuzu Kol", ItemType: "cut_part", BaseUOM: "kg"},
		{Code: "KUZU-SIRT", Name: "Kuzu Sırt", ItemType: "cut_part", BaseUOM: "kg"},
		{Code: "KUZU-KABURGA", Name: "Kuzu Kaburga", ItemType: "cut_part", BaseUOM: "kg"},
		{Code: "KUZU-GERDAN", Name: "Kuzu Gerdan", ItemType: "cut_part", BaseUOM: "kg"},
		{Code: "KUZU-KIYMA", Name: "Kuzu Kıyma", ItemType: "cut_part", BaseUOM: "kg"},
		{Code: "KUZU-FIRE", Name: "Kuzu Fire", ItemType: "waste", BaseUOM: "kg"},
	} {
		v.IsActive = true
		existing, err := s.lookup(ctx, "item", v.Code)
		if err != nil {
			return err
		}
		if existing != "" {
			s.record("item", v.Code, existing, true)
			continue
		}
		out, err := s.store.CreateItem(ctx, v)
		if err != nil {
			return err
		}
		s.record("item", v.Code, out.ID, false)
	}
	return nil
}

// recipePart golden dosyasindaki bir recete satiri.
type recipePart struct {
	Part               string `json:"part"`
	ExpectedRatio      string `json:"expected_ratio"`
	ReferenceSalePrice string `json:"reference_sale_price"`
	IsWaste            bool   `json:"is_waste"`
	IsByProduct        bool   `json:"is_by_product"`
}

// Golden dosyasi bulunamazsa kullanilan degerler (docs/golden/cutting_case.json ile ayni olmali).
var fallbackRecipe = []recipePart{
	{"But", "0.3000", "16.00", false, false}, {"Kol", "0.1800", "15.00", false, false},
	{"Sırt", "0.1000", "18.00", false, false}, {"Kaburga", "0.1200", "12.00", false, false},
	{"Gerdan", "0.0800", "10.00", false, false}, {"Kıyma", "0.1000", "11.00", false, false},
	{"Fire", "0.1200", "0.00", true, false},
}

// recipePartItem golden'daki parca adini urun koduna esler.
var recipePartItem = map[string]string{
	"But": "KUZU-BUT", "Kol": "KUZU-KOL", "Sırt": "KUZU-SIRT", "Kaburga": "KUZU-KABURGA",
	"Gerdan": "KUZU-GERDAN", "Kıyma": "KUZU-KIYMA", "Fire": "KUZU-FIRE",
}

// loadRecipe expected_ratio ve reference_sale_price degerlerini docs/golden/cutting_case.json'dan okur.
func loadRecipe(goldenDir string) []recipePart {
	raw, err := os.ReadFile(filepath.Join(goldenDir, "cutting_case.json"))
	if err != nil {
		slog.Warn("cutting_case.json okunamadi, varsayilan recete kullaniliyor (TODO: golden dosyasini baglayin)", "err", err)
		return fallbackRecipe
	}
	var doc struct {
		Expected struct {
			Parts []recipePart `json:"parts"`
		} `json:"expected"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil || len(doc.Expected.Parts) == 0 {
		slog.Warn("cutting_case.json cozumlenemedi, varsayilan recete kullaniliyor", "err", err)
		return fallbackRecipe
	}
	return doc.Expected.Parts
}

func (s *seeder) recipe(ctx context.Context, goldenDir string) error {
	const code = "REC-KUZU-01"
	existing, err := s.lookup(ctx, "cutting_recipe", code)
	if err != nil {
		return err
	}
	recipeID := existing
	if recipeID == "" {
		recipeID = domain.NewID()
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO cutting_recipe (id, code, name, animal_type) VALUES (?, ?, ?, 'lamb')`,
			recipeID, code, "Bütün kuzu parçalama"); err != nil {
			return err
		}
		s.record("cutting_recipe", code, recipeID, false)
	} else {
		s.record("cutting_recipe", code, recipeID, true)
	}
	for _, p := range loadRecipe(goldenDir) {
		itemCode, ok := recipePartItem[p.Part]
		if !ok {
			return errors.New("recete parcasi icin urun kodu tanimsiz: " + p.Part)
		}
		// INSERT ... AS: tekrar calistirmada oran/fiyat golden'dan guncellenir (idempotent).
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO cutting_recipe_line (id, recipe_id, part_item_id, expected_ratio, reference_sale_price, is_by_product, is_waste)
			 VALUES (?, ?, ?, ?, ?, ?, ?) AS new
			 ON DUPLICATE KEY UPDATE expected_ratio = new.expected_ratio, reference_sale_price = new.reference_sale_price,
			                         is_by_product = new.is_by_product, is_waste = new.is_waste`,
			domain.NewID(), recipeID, s.must(ctx, "item", itemCode), p.ExpectedRatio, p.ReferenceSalePrice,
			p.IsByProduct, p.IsWaste); err != nil {
			return err
		}
	}
	return nil
}

func (s *seeder) purchases(ctx context.Context) error {
	day := domain.NewDate(2026, 3, 2)
	for _, v := range []struct {
		lotNo, item, supplier, qty, price string
		declared                          *domain.Amount
		invoice                           string
	}{
		{"ET-2026-001", "ET-DANA", "SUP-ET", "100.000", "9.7000", nil, "ET-F-0001"},
		{"BHR-2026-001", "BAHARAT-SUCUK", "SUP-BHR", "2.000", "24.0000", nil, "BH-F-0001"},
		{"BGR-2026-001", "BAGIRSAK", "SUP-BGR", "5.000", "11.6000", nil, "BG-F-0001"},
		// Dokümandaki KASITLI tutarsızlık: 25 kg x 11.50 = 287.50 hesaplanan, belgede 351.00 yazıyor => has_mismatch.
		{"KZ-2026-001", "KUZU-BUTUN", "SUP-HAY", "25.000", "11.5000", amtP("351.0000"), "HY-F-0001"},
	} {
		if _, err := s.store.LotByNo(ctx, v.lotNo); err == nil {
			s.skipped++
			continue
		} else if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		lot, err := s.store.CreatePurchaseLot(ctx, domain.PurchaseLotRequest{
			LotNo:             v.lotNo,
			ItemID:            s.must(ctx, "item", v.item),
			SupplierID:        s.must(ctx, "supplier", v.supplier),
			WarehouseID:       s.must(ctx, "warehouse", "WH-MAIN"),
			ReceivedAt:        day,
			Qty:               qty(v.qty),
			UnitPrice:         amt(v.price),
			DeclaredLineTotal: v.declared,
			InvoiceNo:         str(v.invoice),
		})
		if err != nil {
			return err
		}
		s.created++
		slog.Info("alis lotu olusturuldu", "lot_no", lot.LotNo, "has_mismatch", lot.Purchase.HasMismatch)
	}
	return nil
}
