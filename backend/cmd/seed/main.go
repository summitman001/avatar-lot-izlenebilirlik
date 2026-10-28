// Komut seed: ana veriler + alis belgeleri/lotlari/purchase_in hareketleri. IDEMPOTENT: tekrar calistirmak
// var olan kayitlari atlar (kod / lot_no ile bulunur). Uretim ve satis senaryosu sonraki gunlerde
// servisler uzerinden eklenecek. Tum firma isimleri hayalidir.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/shopspring/decimal"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/config"
	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/db"
	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/mlclient"
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
	store := repo.New(conn).SetMLClient(mlclient.New(cfg.MLURL))
	s := &seeder{store: store, db: conn, ids: map[string]string{}}

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
	if err := s.cuttingDemo(ctx); err != nil {
		return err
	}
	if err := s.externalProduction(ctx); err != nil {
		return err
	}
	if err := s.salesAndShipments(ctx); err != nil {
		return err
	}
	if err := s.secondProductionPath(ctx); err != nil {
		return err
	}
	if err := s.secondPathSaleAndReturn(ctx); err != nil {
		return err
	}
	if err := s.independentControlLot(ctx); err != nil {
		return err
	}
	if err := s.anomalyProductionDemo(ctx); err != nil {
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
		// material_category Gorev 1 maliyet hesabinin kategori alanidir (meat|spice|casing|other);
		// bkz. migrations/000002_item_material_category, docs/assumptions.md.
		{Code: "ET-DANA", Name: "Dana Eti", ItemType: "raw", BaseUOM: "kg", MaterialCategory: str("meat")},
		{Code: "BAHARAT-SUCUK", Name: "Sucuk Baharatı", ItemType: "raw", BaseUOM: "kg", MaterialCategory: str("spice")},
		{Code: "BAGIRSAK", Name: "Bağırsak", ItemType: "raw", BaseUOM: "kg", MaterialCategory: str("casing")},
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

// recipe REC-KUZU-01 reçetesini SERVİS ÜZERİNDEN (repo.Store.CreateCuttingRecipe/UpdateCuttingRecipe,
// ham SQL yazmadan) oluşturur; idempotenttir -- tekrar çalıştırmada satırlar golden dosyasındaki
// oran/fiyatla UpdateCuttingRecipe (tam değiştirme) ile güncellenir.
func (s *seeder) recipe(ctx context.Context, goldenDir string) error {
	const code = "REC-KUZU-01"
	lines := make([]domain.CuttingRecipeLine, 0, 7)
	for _, p := range loadRecipe(goldenDir) {
		itemCode, ok := recipePartItem[p.Part]
		if !ok {
			return errors.New("recete parcasi icin urun kodu tanimsiz: " + p.Part)
		}
		lines = append(lines, domain.CuttingRecipeLine{
			PartItemID:         s.must(ctx, "item", itemCode),
			ExpectedRatio:      amt(p.ExpectedRatio),
			ReferenceSalePrice: amt(p.ReferenceSalePrice),
			IsByProduct:        p.IsByProduct,
			IsWaste:            p.IsWaste,
		})
	}
	recipe := domain.CuttingRecipe{Code: code, Name: "Bütün kuzu parçalama", AnimalType: "lamb", Lines: lines}

	existing, err := s.lookup(ctx, "cutting_recipe", code)
	if err != nil {
		return err
	}
	if existing == "" {
		out, err := s.store.CreateCuttingRecipe(ctx, recipe)
		if err != nil {
			return err
		}
		s.record("cutting_recipe", code, out.ID, false)
		return nil
	}
	if _, err := s.store.UpdateCuttingRecipe(ctx, existing, recipe); err != nil {
		return err
	}
	s.record("cutting_recipe", code, existing, true)
	return nil
}

func (s *seeder) purchases(ctx context.Context) error {
	day := domain.NewDate(2026, 3, 2)
	for _, v := range []struct {
		lotNo, item, supplier, qty, price string
		declared                          *domain.Amount
		invoice                           string
	}{
		// VARSAYIM: 112 kg (100 kg değil) -- URT-2026-045'te 100 kg, URT-2026-046'da (izlenebilirlik
		// demo senaryosu, bkz. docs/assumptions.md) kalan 12 kg kullanılır; aynı hammadde lotunun İKİ
		// farklı üretim emrinde (iki ayrı dalda) kullanıldığını kanıtlamak için.
		{"ET-2026-001", "ET-DANA", "SUP-ET", "112.000", "9.7000", nil, "ET-F-0001"},
		// Bağımsız kontrol lotu (ET-2026-001'den TAMAMEN ayrı zincir): geri çağırma bunu ETKİLEMEMELİ.
		{"ET-2026-002", "ET-DANA", "SUP-ET", "20.000", "9.7000", nil, "ET-F-0002"},
		{"BHR-2026-001", "BAHARAT-SUCUK", "SUP-BHR", "2.000", "24.0000", nil, "BH-F-0001"},
		{"BGR-2026-001", "BAGIRSAK", "SUP-BGR", "5.000", "11.6000", nil, "BG-F-0001"},
		// Dokümandaki KASITLI tutarsızlık: 25 kg x 11.50 = 287.50 hesaplanan, belgede 351.00 yazıyor => has_mismatch.
		{"KZ-2026-001", "KUZU-BUTUN", "SUP-HAY", "25.000", "11.5000", amtP("351.0000"), "HY-F-0001"},
		// Parçalama demosu (Görev 2) için ikinci kuzu: tutarsızlık yok, "dengeli" senaryoları için.
		{"KZ-2026-002", "KUZU-BUTUN", "SUP-HAY", "22.000", "11.5000", nil, "HY-F-0002"},
		// Anomali demosu (bkz. docs/anomaly-integration.md, anomalyProductionDemo): URT-2026-048 icin
		// hammaddeler -- ayni fiyatlar, yalniz dusuk-verim senaryosunu olusturmak icin AYRI lotlar.
		{"ET-2026-900", "ET-DANA", "SUP-ET", "100.000", "9.7000", nil, "ET-F-0900"},
		{"BHR-2026-900", "BAHARAT-SUCUK", "SUP-BHR", "2.000", "24.0000", nil, "BH-F-0900"},
		{"BGR-2026-900", "BAGIRSAK", "SUP-BGR", "5.000", "11.6000", nil, "BG-F-0900"},
		// Anomali demosu: MALIYET_ARTISI -- aynı hammadde (ET-DANA), son alışların (9,70 €/kg)
		// belirgin üstünde bir fiyatla (ML ayaktaysa /detect/anomaly bunu yakalar).
		{"ET-2026-901", "ET-DANA", "SUP-ET", "15.000", "13.9000", nil, "ET-F-0901"},
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

// cuttingDemo Görev 2 parçalama demo senaryosu, SERVİSLER ÜZERİNDEN (CreateCuttingOrder/
// UpdateCuttingOutputs/CloseCuttingOrder, ham SQL yazmadan):
//   - CUT-2026-001: KZ-2026-001 (25 kg, mismatch'li) üzerinde "value" yöntemiyle, cost_basis=computed +
//     resolution_note ile tam dengeli parçalama + kapanış; çıkan KUZU-BUT lotundan MKT-A'ya satış
//     (parçalama -> satış zincirinin de izlemede görünmesi için).
//   - CUT-2026-002: KZ-2026-002 (22 kg, tutarsızlık yok) üzerinde reçeteyle TAM orantılı (dengeli)
//     ağırlıklar girilir ama BİLEREK KAPATILMAZ (draft kalır) -- "kapanmaya hazır ama kapatılmamış
//     sipariş" durumunu gösterir.
//   - CUT-2026-003: AYNI lot (KZ-2026-002, henüz tüketilmedi) üzerinde yeni bir emir; fire BİLEREK
//     girilmez, 2,64 kg fark (>0,05 kg tolerans) WEIGHT_IMBALANCE + closable=false üretir, draft kalır
//     -- "denge tutmuyorsa kapatılamaz" demosu.
func (s *seeder) cuttingDemo(ctx context.Context) error {
	recipeID := s.must(ctx, "cutting_recipe", "REC-KUZU-01")
	parts := func(but, kol, sirt, kaburga, gerdan, kiyma string, fire *string) []domain.UpdateCuttingOutputsLine {
		lines := []domain.UpdateCuttingOutputsLine{
			{PartItemID: s.must(ctx, "item", "KUZU-BUT"), ActualWeightKg: qty(but)},
			{PartItemID: s.must(ctx, "item", "KUZU-KOL"), ActualWeightKg: qty(kol)},
			{PartItemID: s.must(ctx, "item", "KUZU-SIRT"), ActualWeightKg: qty(sirt)},
			{PartItemID: s.must(ctx, "item", "KUZU-KABURGA"), ActualWeightKg: qty(kaburga)},
			{PartItemID: s.must(ctx, "item", "KUZU-GERDAN"), ActualWeightKg: qty(gerdan)},
			{PartItemID: s.must(ctx, "item", "KUZU-KIYMA"), ActualWeightKg: qty(kiyma)},
		}
		if fire != nil {
			lines = append(lines, domain.UpdateCuttingOutputsLine{PartItemID: s.must(ctx, "item", "KUZU-FIRE"), ActualWeightKg: qty(*fire)})
		}
		return lines
	}

	const orderNo1 = "CUT-2026-001"
	var existingID string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM cutting_order WHERE order_no = ?`, orderNo1).Scan(&existingID)
	switch {
	case err == nil:
		s.skipped++
	case !errors.Is(err, sql.ErrNoRows):
		return err
	default:
		kz001, err := s.store.LotByNo(ctx, "KZ-2026-001")
		if err != nil {
			return fmt.Errorf("CUT-2026-001 icin KZ-2026-001 bulunamadi: %w", err)
		}
		note := "Birim fiyat (11,50 €/kg) ve ağırlık (25 kg) tutarlı; hatalı olan tek alan beyan edilen toplam (351.00 €)."
		order, err := s.store.CreateCuttingOrder(ctx, domain.CreateCuttingOrderRequest{
			OrderNo: str(orderNo1), RecipeID: recipeID, InputLotID: kz001.ID, InputWeightKg: qty("25.000"),
			AllocationMethod: "value", CutAt: time.Now().UTC(), CostBasis: str("computed"), ResolutionNote: str(note),
		})
		if err != nil {
			return fmt.Errorf("CUT-2026-001 olusturulamadi: %w", err)
		}
		s.created++
		slog.Info("parcalama siparisi olusturuldu", "order_no", order.OrderNo, "total_input_cost", order.TotalInputCost)

		fire := "3.0"
		order, err = s.store.UpdateCuttingOutputs(ctx, order.ID, domain.UpdateCuttingOutputsRequest{
			Outputs: parts("7.5", "4.5", "2.5", "3.0", "2.0", "2.5", &fire),
		})
		if err != nil {
			return fmt.Errorf("CUT-2026-001 fiili agirliklar yazilamadi: %w", err)
		}
		slog.Info("parcalama fiili agirliklar girildi", "order_no", order.OrderNo, "closable", order.Closable)

		order, err = s.store.CloseCuttingOrder(ctx, order.ID)
		if err != nil {
			return fmt.Errorf("CUT-2026-001 kapatilamadi: %w", err)
		}
		s.created++
		slog.Info("parcalama siparisi kapandi", "order_no", order.OrderNo, "status", order.Status)

		var butLotID string
		for _, o := range order.Outputs {
			if o.PartItemCode == "KUZU-BUT" && o.OutputLotID != nil {
				butLotID = *o.OutputLotID
			}
		}
		if butLotID == "" {
			return errors.New("KUZU-BUT cikti lotu olusmadi")
		}
		saleOrder, _, err := s.store.CreateSalesOrder(ctx, domain.CreateSalesOrderRequest{
			CustomerID: s.must(ctx, "customer", "MKT-A"), WarehouseID: s.must(ctx, "warehouse", "WH-MAIN"),
			SoldAt: time.Now().UTC(),
			Lines: []domain.CreateSalesOrderLine{
				{ItemID: s.must(ctx, "item", "KUZU-BUT"), LotID: butLotID, Qty: qty("5"), UnitPrice: amt("16.0000")},
			},
		}, repo.SaleOptions{Online: true})
		if err != nil {
			return fmt.Errorf("KUZU-BUT satisi olusturulamadi: %w", err)
		}
		s.created++
		slog.Info("parcalanmis urun satisi olusturuldu", "order_no", saleOrder.OrderNo, "musteri", "MKT-A")
	}

	kz002, err := s.store.LotByNo(ctx, "KZ-2026-002")
	if err != nil {
		return fmt.Errorf("KZ-2026-002 bulunamadi: %w", err)
	}

	const orderNo2 = "CUT-2026-002"
	err = s.db.QueryRowContext(ctx, `SELECT id FROM cutting_order WHERE order_no = ?`, orderNo2).Scan(&existingID)
	switch {
	case err == nil:
		s.skipped++
	case !errors.Is(err, sql.ErrNoRows):
		return err
	default:
		order, err := s.store.CreateCuttingOrder(ctx, domain.CreateCuttingOrderRequest{
			OrderNo: str(orderNo2), RecipeID: recipeID, InputLotID: kz002.ID, InputWeightKg: qty("22.000"),
			AllocationMethod: "quantity", CutAt: time.Now().UTC(),
		})
		if err != nil {
			return fmt.Errorf("CUT-2026-002 olusturulamadi: %w", err)
		}
		s.created++
		fire := "2.64"
		order, err = s.store.UpdateCuttingOutputs(ctx, order.ID, domain.UpdateCuttingOutputsRequest{
			Outputs: parts("6.6", "3.96", "2.2", "2.64", "1.76", "2.2", &fire),
		})
		if err != nil {
			return fmt.Errorf("CUT-2026-002 fiili agirliklar yazilamadi: %w", err)
		}
		slog.Info("parcalama siparisi olusturuldu (dengeli, bilerek draft birakildi)",
			"order_no", order.OrderNo, "closable", order.Closable)
	}

	const orderNo3 = "CUT-2026-003"
	err = s.db.QueryRowContext(ctx, `SELECT id FROM cutting_order WHERE order_no = ?`, orderNo3).Scan(&existingID)
	if err == nil {
		s.skipped++
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	order, err := s.store.CreateCuttingOrder(ctx, domain.CreateCuttingOrderRequest{
		OrderNo: str(orderNo3), RecipeID: recipeID, InputLotID: kz002.ID, InputWeightKg: qty("22.000"),
		AllocationMethod: "quantity", CutAt: time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("CUT-2026-003 olusturulamadi: %w", err)
	}
	s.created++
	// Fire KASITLI girilmez: denge 2.64 kg tutmaz (> 0.05 kg tolerans) -> WEIGHT_IMBALANCE, closable=false.
	order, err = s.store.UpdateCuttingOutputs(ctx, order.ID, domain.UpdateCuttingOutputsRequest{
		Outputs: parts("6.6", "3.96", "2.2", "2.64", "1.76", "2.2", nil),
	})
	if err != nil {
		return fmt.Errorf("CUT-2026-003 fiili agirliklar yazilamadi: %w", err)
	}
	slog.Info("parcalama siparisi olusturuldu (dengesiz, draft)",
		"order_no", order.OrderNo, "closable", order.Closable, "balance_diff_kg", order.BalanceDiffKg)
	return nil
}

// externalProduction URT-2026-045 senaryosunu SERVISLER UZERINDEN (repo fonksiyonlariyla, ham SQL
// ile degil) baştan sona olusturur: hammadde sevki -> 85 adet kabul -> kapanis. Golden UR-001 ile
// ayni girdileri kullanir (ekp_unit = 18.1588, bkz. docs/golden/production_cases.json).
func (s *seeder) externalProduction(ctx context.Context) error {
	const orderNo = "URT-2026-045"
	var existingID string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM production_order WHERE order_no = ?`, orderNo).Scan(&existingID)
	if err == nil {
		s.skipped++
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	order, err := s.store.CreateProductionOrder(ctx, domain.CreateProductionOrderRequest{
		OrderNo:             str(orderNo),
		ProductItemID:       s.must(ctx, "item", "SUCUK-NARIN-1KG"),
		ProducerSupplierID:  s.must(ctx, "supplier", "SUP-URT"),
		ExternalWarehouseID: s.must(ctx, "warehouse", "WH-EXT-01"),
		PlannedQty:          qty("85"),
		ServiceUnitPrice:    amt("5.5000"),
		SalePrice:           amtP("24.9000"), // VARSAYIM: docs/assumptions.md "VKP varsayımı"
	})
	if err != nil {
		return err
	}
	s.created++
	slog.Info("dis uretim siparisi olusturuldu", "order_no", order.OrderNo)

	// Hammaddeler: ET-2026-001'den 100 kg (112 kg'lik lotun TAMAMI DEĞİL -- kalan 12 kg
	// secondProductionPath'te URT-2026-046'ya gider, bkz. docs/assumptions.md "Görev 3" madde 1),
	// BHR-2026-001 (2 kg) ve BGR-2026-001 (5 kg) lotlarının TAMAMI (golden UR-001 ile aynı kompozisyon).
	lines := []domain.SendMaterialsLine{}
	for _, v := range []struct{ lotNo, qty string }{
		{"ET-2026-001", "100.000"}, {"BHR-2026-001", "2.000"}, {"BGR-2026-001", "5.000"},
	} {
		lot, err := s.store.LotByNo(ctx, v.lotNo)
		if err != nil {
			return fmt.Errorf("sevk icin lot bulunamadi %s: %w", v.lotNo, err)
		}
		lines = append(lines, domain.SendMaterialsLine{LotID: lot.ID, Qty: qty(v.qty)})
	}
	order, err = s.store.SendMaterials(ctx, order.ID, domain.SendMaterialsRequest{Lines: lines})
	if err != nil {
		return err
	}
	slog.Info("hammaddeler dis ureticiye sevk edildi", "order_no", order.OrderNo, "satir", len(lines))

	order, err = s.store.ReceiveProduction(ctx, order.ID, domain.ReceiveProductionRequest{
		ReceivedQty:        qty("85"),
		OutputLotNo:        str("SUC-2026-045"),
		ServiceInvoiceNo:   str("URT-2026-045-INV"),
		ServiceInvoicedQty: qtyP("85"),
	})
	if err != nil {
		return err
	}
	slog.Info("mamul teslim alindi", "order_no", order.OrderNo, "output_lot_no", *order.OutputLotNo,
		"ekp_unit", order.EkpUnit.StringFixed(domain.AmountScale))

	order, err = s.store.CloseProductionOrder(ctx, order.ID, domain.CloseProductionOrderRequest{})
	if err != nil {
		return err
	}
	slog.Info("dis uretim siparisi kapandi", "order_no", order.OrderNo, "status", order.Status)
	return nil
}

// salesAndShipments URT-2026-045'in ciktisi SUC-2026-045'i ucuncu bir senaryoda satar: MKT-A'ya 30,
// MKT-B'ye 25, MKT-C'ye 30 paket (unit_price 24.90, create_shipment true). Sonuc: SUC-2026-045
// bakiyesi 0, uc sevkiyat, toplam ciro 85 x 24.90 = 2116.50. Servisler uzerinden (CreateSalesOrder,
// online=true) olusturulur; idempotency lotun sales_line sayisina bakilarak saglanir.
func (s *seeder) salesAndShipments(ctx context.Context) error {
	outputLot, err := s.store.LotByNo(ctx, "SUC-2026-045")
	if err != nil {
		return fmt.Errorf("satis senaryosu icin cikti lotu bulunamadi: %w", err)
	}
	var lineCount int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sales_line WHERE lot_id = ?`, outputLot.ID).Scan(&lineCount); err != nil {
		return err
	}
	if lineCount >= 3 {
		s.skipped++
		return nil
	}

	warehouseID := s.must(ctx, "warehouse", "WH-MAIN")
	for _, v := range []struct {
		customer string
		qty      string
	}{
		{"MKT-A", "30"}, {"MKT-B", "25"}, {"MKT-C", "30"},
	} {
		order, _, err := s.store.CreateSalesOrder(ctx, domain.CreateSalesOrderRequest{
			CustomerID:  s.must(ctx, "customer", v.customer),
			WarehouseID: warehouseID,
			SoldAt:      time.Now().UTC(),
			Lines: []domain.CreateSalesOrderLine{
				{ItemID: outputLot.ItemID, LotID: outputLot.ID, Qty: qty(v.qty), UnitPrice: amt("24.9000")},
			},
		}, repo.SaleOptions{Online: true})
		if err != nil {
			return fmt.Errorf("satis olusturulamadi (%s): %w", v.customer, err)
		}
		s.created++
		slog.Info("satis olusturuldu", "order_no", order.OrderNo, "musteri", v.customer, "qty", v.qty)
	}
	return nil
}

// ---------------------------------------------------------------------------
// İzlenebilirlik/geri çağırma demo senaryosu (Görev 3): ikinci yol, ikinci kademe satış, iade,
// bağımsız kontrol lotu. Hepsi SERVİSLER ÜZERİNDEN (ham SQL yazılmadan) oluşturulur.
// ---------------------------------------------------------------------------

// secondProductionPath: ET-2026-001 lotundan KALAN 12 kg (112 kg - URT-045'te tüketilen 100 kg),
// İKİNCİ bir üretim emrinde (URT-2026-046) kullanılır -> 10 adet SUC-2026-046. İzleme grafının aynı
// hammadde lotundan ÇIKAN İKİ farklı dalı (SUC-045 ve SUC-046) doğru bulduğunu kanıtlar.
func (s *seeder) secondProductionPath(ctx context.Context) error {
	const orderNo = "URT-2026-046"
	var existingID string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM production_order WHERE order_no = ?`, orderNo).Scan(&existingID)
	if err == nil {
		s.skipped++
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	order, err := s.store.CreateProductionOrder(ctx, domain.CreateProductionOrderRequest{
		OrderNo:             str(orderNo),
		ProductItemID:       s.must(ctx, "item", "SUCUK-NARIN-1KG"),
		ProducerSupplierID:  s.must(ctx, "supplier", "SUP-URT"),
		ExternalWarehouseID: s.must(ctx, "warehouse", "WH-EXT-01"),
		PlannedQty:          qty("10"),
		ServiceUnitPrice:    amt("5.5000"),
		SalePrice:           amtP("24.9000"),
	})
	if err != nil {
		return err
	}
	s.created++
	slog.Info("ikinci dis uretim siparisi olusturuldu", "order_no", order.OrderNo)

	etLot, err := s.store.LotByNo(ctx, "ET-2026-001")
	if err != nil {
		return fmt.Errorf("ikinci uretim yolu icin ET-2026-001 bulunamadi: %w", err)
	}
	order, err = s.store.SendMaterials(ctx, order.ID, domain.SendMaterialsRequest{
		Lines: []domain.SendMaterialsLine{{LotID: etLot.ID, Qty: *etLot.QtyOnHand}},
	})
	if err != nil {
		return err
	}
	slog.Info("ikinci yol hammadde sevk edildi", "order_no", order.OrderNo, "et_kg", etLot.QtyOnHand.StringFixed(domain.QtyScale))

	order, err = s.store.ReceiveProduction(ctx, order.ID, domain.ReceiveProductionRequest{
		ReceivedQty:        qty("10"),
		OutputLotNo:        str("SUC-2026-046"),
		ServiceInvoiceNo:   str("URT-2026-046-INV"),
		ServiceInvoicedQty: qtyP("10"),
	})
	if err != nil {
		return err
	}
	slog.Info("ikinci yol mamulu teslim alindi", "order_no", order.OrderNo, "output_lot_no", *order.OutputLotNo)

	order, err = s.store.CloseProductionOrder(ctx, order.ID, domain.CloseProductionOrderRequest{})
	if err != nil {
		return err
	}
	slog.Info("ikinci dis uretim siparisi kapandi", "order_no", order.OrderNo, "status", order.Status)
	return nil
}

// secondPathSaleAndReturn: SUC-2026-046'dan MKT-A'ya 10 paket satılır (MKT-A toplamı 30+10=40 olur);
// MKT-B, SUC-2026-045'ten 2 paket iade eder (MKT-B net 25-2=23 kalır -- iade sevk edileni DÜŞÜRMEZ,
// trace-forward/recall ayrı qty_returned alanında gösterir, bkz. docs/traceability.md).
func (s *seeder) secondPathSaleAndReturn(ctx context.Context) error {
	suc046, err := s.store.LotByNo(ctx, "SUC-2026-046")
	if err != nil {
		return fmt.Errorf("SUC-2026-046 bulunamadi: %w", err)
	}
	var lineCount int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sales_line WHERE lot_id = ?`, suc046.ID).Scan(&lineCount); err != nil {
		return err
	}
	if lineCount == 0 {
		order, _, err := s.store.CreateSalesOrder(ctx, domain.CreateSalesOrderRequest{
			CustomerID:  s.must(ctx, "customer", "MKT-A"),
			WarehouseID: s.must(ctx, "warehouse", "WH-MAIN"),
			SoldAt:      time.Now().UTC(),
			Lines: []domain.CreateSalesOrderLine{
				{ItemID: suc046.ItemID, LotID: suc046.ID, Qty: qty("10"), UnitPrice: amt("24.9000")},
			},
		}, repo.SaleOptions{Online: true})
		if err != nil {
			return fmt.Errorf("SUC-2026-046 satisi olusturulamadi: %w", err)
		}
		s.created++
		slog.Info("ikinci yol satisi olusturuldu", "order_no", order.OrderNo, "musteri", "MKT-A", "qty", "10")
	} else {
		s.skipped++
	}

	suc045, err := s.store.LotByNo(ctx, "SUC-2026-045")
	if err != nil {
		return fmt.Errorf("SUC-2026-045 bulunamadi: %w", err)
	}
	var returnCount int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM customer_return WHERE lot_id = ?`, suc045.ID).Scan(&returnCount); err != nil {
		return err
	}
	if returnCount == 0 {
		ret, err := s.store.CreateCustomerReturn(ctx, domain.CreateCustomerReturnRequest{
			CustomerID: s.must(ctx, "customer", "MKT-B"),
			LotID:      str(suc045.ID),
			Qty:        qty("2"),
			Reason:     "musteri memnun degil",
		})
		if err != nil {
			return fmt.Errorf("MKT-B iadesi olusturulamadi: %w", err)
		}
		s.created++
		slog.Info("iade olusturuldu", "return_no", ret.ReturnNo, "musteri", "MKT-B", "qty", "2")
	} else {
		s.skipped++
	}
	return nil
}

// independentControlLot: ET-2026-002 (ET-2026-001'den TAMAMEN bağımsız bir et lotu) ile
// URT-2026-047/SUC-2026-047 üretilip MKT-C'ye satılır. ET-2026-001 geri çağrıldığında bu lot
// ETKİLENMEMELİDİR (ayrı bir soy ağacı) -- bkz. backend/internal/integration/traceability_test.go.
func (s *seeder) independentControlLot(ctx context.Context) error {
	const orderNo = "URT-2026-047"
	var existingID string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM production_order WHERE order_no = ?`, orderNo).Scan(&existingID)
	if err == nil {
		s.skipped++
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	order, err := s.store.CreateProductionOrder(ctx, domain.CreateProductionOrderRequest{
		OrderNo:             str(orderNo),
		ProductItemID:       s.must(ctx, "item", "SUCUK-NARIN-1KG"),
		ProducerSupplierID:  s.must(ctx, "supplier", "SUP-URT"),
		ExternalWarehouseID: s.must(ctx, "warehouse", "WH-EXT-01"),
		PlannedQty:          qty("10"),
		ServiceUnitPrice:    amt("5.5000"),
		SalePrice:           amtP("24.9000"),
	})
	if err != nil {
		return err
	}
	s.created++
	slog.Info("bagimsiz kontrol uretim siparisi olusturuldu", "order_no", order.OrderNo)

	etLot2, err := s.store.LotByNo(ctx, "ET-2026-002")
	if err != nil {
		return fmt.Errorf("ET-2026-002 bulunamadi: %w", err)
	}
	order, err = s.store.SendMaterials(ctx, order.ID, domain.SendMaterialsRequest{
		Lines: []domain.SendMaterialsLine{{LotID: etLot2.ID, Qty: *etLot2.QtyOnHand}},
	})
	if err != nil {
		return err
	}

	order, err = s.store.ReceiveProduction(ctx, order.ID, domain.ReceiveProductionRequest{
		ReceivedQty:        qty("10"),
		OutputLotNo:        str("SUC-2026-047"),
		ServiceInvoiceNo:   str("URT-2026-047-INV"),
		ServiceInvoicedQty: qtyP("10"),
	})
	if err != nil {
		return err
	}
	slog.Info("bagimsiz kontrol lotu uretildi", "order_no", order.OrderNo, "output_lot_no", *order.OutputLotNo)

	order, err = s.store.CloseProductionOrder(ctx, order.ID, domain.CloseProductionOrderRequest{})
	if err != nil {
		return err
	}

	outputLot, err := s.store.GetLot(ctx, *order.OutputLotID)
	if err != nil {
		return err
	}
	saleOrder, _, err := s.store.CreateSalesOrder(ctx, domain.CreateSalesOrderRequest{
		CustomerID:  s.must(ctx, "customer", "MKT-C"),
		WarehouseID: s.must(ctx, "warehouse", "WH-MAIN"),
		SoldAt:      time.Now().UTC(),
		Lines: []domain.CreateSalesOrderLine{
			{ItemID: outputLot.ItemID, LotID: outputLot.ID, Qty: qty("10"), UnitPrice: amt("24.9000")},
		},
	}, repo.SaleOptions{Online: true})
	if err != nil {
		return fmt.Errorf("bagimsiz kontrol lotu satisi olusturulamadi: %w", err)
	}
	s.created++
	slog.Info("bagimsiz kontrol lotu satildi", "order_no", saleOrder.OrderNo, "musteri", "MKT-C", "qty", "10")
	return nil
}

// anomalyProductionDemo: URT-2026-048, 100 kg etten SADECE 70 adet teslim alinan (normalde ~85 adet
// beklenir -- diger senaryolarla ayni kompozisyon) dusuk verimli bir dis uretim siparisi. ML servisi
// ayaktaysa receive sonrasi best-effort /detect/anomaly cagrisi VERIM_DUSUK (ve genelde EKP_SAPMA)
// uretir; ML kapaliysa siparis yine basariyla olusur/kapanir, sadece uyari olusmaz (bkz.
// docs/anomaly-integration.md). İkinci demo anomalisi (MALIYET_ARTISI) purchases()'taki ET-2026-901
// satirindadir.
func (s *seeder) anomalyProductionDemo(ctx context.Context) error {
	const orderNo = "URT-2026-048"
	var existingID string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM production_order WHERE order_no = ?`, orderNo).Scan(&existingID)
	if err == nil {
		s.skipped++
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	order, err := s.store.CreateProductionOrder(ctx, domain.CreateProductionOrderRequest{
		OrderNo:             str(orderNo),
		ProductItemID:       s.must(ctx, "item", "SUCUK-NARIN-1KG"),
		ProducerSupplierID:  s.must(ctx, "supplier", "SUP-URT"),
		ExternalWarehouseID: s.must(ctx, "warehouse", "WH-EXT-01"),
		PlannedQty:          qty("85"),
		ServiceUnitPrice:    amt("5.5000"),
		SalePrice:           amtP("24.9000"),
	})
	if err != nil {
		return err
	}
	s.created++
	slog.Info("anomali demosu: dusuk verimli uretim siparisi olusturuldu", "order_no", order.OrderNo)

	lines := []domain.SendMaterialsLine{}
	for _, v := range []struct{ lotNo, qty string }{
		{"ET-2026-900", "100.000"}, {"BHR-2026-900", "2.000"}, {"BGR-2026-900", "5.000"},
	} {
		lot, err := s.store.LotByNo(ctx, v.lotNo)
		if err != nil {
			return fmt.Errorf("sevk icin lot bulunamadi %s: %w", v.lotNo, err)
		}
		lines = append(lines, domain.SendMaterialsLine{LotID: lot.ID, Qty: qty(v.qty)})
	}
	order, err = s.store.SendMaterials(ctx, order.ID, domain.SendMaterialsRequest{Lines: lines})
	if err != nil {
		return err
	}

	// 100 kg etten SADECE 70 adet: normalde (ayni kompozisyonla, bkz. URT-2026-045) ~85 adet cikar.
	order, err = s.store.ReceiveProduction(ctx, order.ID, domain.ReceiveProductionRequest{
		ReceivedQty:        qty("70"),
		OutputLotNo:        str("SUC-2026-048"),
		ServiceInvoiceNo:   str("URT-2026-048-INV"),
		ServiceInvoicedQty: qtyP("70"),
	})
	if err != nil {
		return err
	}
	slog.Info("anomali demosu: dusuk verimli mamul teslim alindi", "order_no", order.OrderNo,
		"ekp_unit", order.EkpUnit.StringFixed(domain.AmountScale))

	order, err = s.store.CloseProductionOrder(ctx, order.ID, domain.CloseProductionOrderRequest{})
	if err != nil {
		return err
	}
	slog.Info("anomali demosu: siparis kapandi", "order_no", order.OrderNo, "status", order.Status)
	return nil
}
