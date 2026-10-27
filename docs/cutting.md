# Parçalama ve maliyet dağıtımı (Görev 2)

Uygulama: `backend/internal/domain/cutting.go` (saf hesap, `AllocateCutting`/`ResolveCostBasis`),
`backend/internal/domain/cutting_order.go` (varlıklar/istek govdeleri), `backend/internal/repo/cutting.go`
(SQL/transaction + ML entegrasyonu), uç noktalar `backend/internal/http/cutting_handlers.go`. Doğruluk
referansı `docs/golden/cutting_cases.json` (11 senaryo) ve `docs/golden/cutting_case.json` (varsayılan
reçete); `backend/internal/domain/golden_test.go` (`TestGoldenCutting`) bunları doğrudan çağırarak,
`backend/internal/integration/cutting_test.go` (`TestCuttingGoldenScenarios`) ise **gerçek HTTP uç
noktalarıyla** (reçete + alış lotu + `POST /cutting-orders` + `PUT .../outputs`) doğrular.

## 1. Dağıtım yöntemleri: `quantity` vs `value`

- **`quantity`** (miktar): havuz (ana parçaların toplam maliyeti) parçalar arasında **kg oranında**
  dağıtılır. Her parçanın birim maliyeti aynı olur (`havuz / toplam_kg`, ufak kuruş farkları hariç).
- **`value`** (değer, net gerçekleşebilir değer yöntemi): havuz **kg × referans satış fiyatı** oranında
  dağıtılır. Değerli parçalar (ör. but) kg'na göre daha fazla maliyet taşır, ucuz parçalar (ör. gerdan)
  daha az — bu, gerçek muhasebe pratiğine (ortak maliyetin nispi satış değerine göre dağıtımı) daha
  yakındır ve parça bazlı kârlılık raporlamasını anlamlı kılar (`quantity` ile her parçanın birim
  maliyeti ~aynı çıktığından kâr/zarar tamamen satış fiyatı farkından ibaret görünür, bu yanıltıcıdır).
- **Varsayılan: `value`.** Kesim işletmeleri için parçalar arası fiyat farkı büyüktür (bu dokümanın
  senaryosunda but 16 €/kg, gerdan 10 €/kg); `value` yöntemi her parçanın gerçek kârlılığını (satış
  fiyatı − dağıtılan maliyet) görünür kılar, `quantity` ise bunu gizler. Kullanıcı her zaman **her iki**
  yöntemi de yan yana görebilir (`PUT /cutting-orders/{id}/outputs` ve `GET /cutting-orders/{id}`
  yanıtlarında `allocation.quantity` + `allocation.value`); hangisinin kapanışta kullanılacağı
  `allocation_method` ile emir oluşturulurken seçilir, sonradan değiştirilemez (toplam maliyetin
  sabitlenmesiyle aynı tasarım kararı — bkz. madde 3).

Her iki yöntemde de **toplam dağıtılan tutar, toplam giriş maliyetine kuruşu kuruşuna eşittir**
(`domain.Distribute`, `docs/assumptions.md`'deki yuvarlama kuralı).

## 2. Fire ve yan ürün muamelesi

- **Fire (`is_waste=true`):** maliyet TAŞIMAZ (`allocated_cost=0.00`, `unit_cost=0.0000`); maliyeti
  kaybolmaz, satılabilir (ana) parçalara biner (fire'ın payı diğer parçalara dağıtılır). Fakat fire
  **fiziksel olarak çıkan bir üründür** — kapanışta normal bir lot (ör. `KUZ-FIRE-2026-001`) ve
  `cutting_in` hareketi alır, `waste_out` DEĞİL. Bilinçli bir karar: fire'ı deftere hiç yazmasak
  (yalnızca "kayıp" sayıp hiçbir kayıt oluşturmasak) ağırlık dengesi (`balance_diff_kg`) hiçbir zaman
  fire'ı göstermez ve "25 kg girdi, 22 kg çıktı, 3 kg nereye gitti" sorusu defterde cevapsız kalırdı —
  tam da dokümandaki 22/25 kg tutarsızlığının kök nedeni. Fire'ı bir lot olarak kaydetmek, hem ağırlık
  dengesini defterde de doğrulanabilir kılar hem de ileride fire'ın imhası/satışı ayrı bir iş akışı
  olarak eklenebilir bırakır (bu görev kapsamında fire lotu üzerinde başka bir işlem yapılmaz).
- **Yan ürün (`is_by_product=true`, ör. ciğer):** kendi satış değeri kadar maliyet taşır
  (`kg × reference_sale_price`, 2 ondalık — **net gerçekleşebilir değer** yöntemi). Bu tutar toplam
  maliyetten düşülür (`allocation_pool = total_input_cost − by_product_value`), kalan havuz ana
  parçalara dağıtılır. Yan ürünün değeri toplam maliyeti aşarsa (`by_product_value > total_input_cost`)
  `422` (`ValueError`) — bu, girilen referans fiyatının gerçekçi olmadığının bir işaretidir.
- Bir parça aynı anda hem fire hem yan ürün olamaz (`422`).

## 3. Kuruş yuvarlama kuralı

Her parçanın tam hassasiyetli payı (`havuz × ağırlık(veya değer) / toplam`) **2 ondalığa** yuvarlanır
(`ROUND_HALF_UP`); kalan kuruş farkı (yuvarlama sonrası toplam − havuz) **en büyük tutarlı satıra**
eklenir (eşitlikte girdi sırasındaki ilk satır — bkz. `docs/golden/cutting_cases.json`, `KZ-2026-005`:
100,00 € / 3 eşit parça = 33,33 × 3 = 99,99, 0,01 fark ilk parçaya eklenir, o parça 33,34 € olur).
Sonuç: `total_allocated` HER ZAMAN `total_input_cost`'a birebir eşittir — asla kuruş kaybı/kazancı
olmaz. Birim maliyet (`unit_cost`) ise **dağıtılan tutar / ağırlık** (4 ondalık) olarak AYRICA
hesaplanır, ikinci bir yuvarlama yapılmaz.

## 4. Ağırlık dengesi kontrolü

`balance_diff_kg = input_weight_kg − Σ(tüm parçaların + fire'ın + yan ürünün fiili ağırlığı)`.
`|balance_diff_kg| > 0,05 kg` ⇒ `WEIGHT_IMBALANCE` uyarısı ve `closable=false` — denge tutmuyorsa emir
kapatılamaz (dağıtım önizleme olarak yine hesaplanır ve gösterilir, ama kapanış reddedilir, `422`).
Tam `0,05 kg` fark KABUL edilir (eşitlik, tolerans dahil — `docs/golden/cutting_cases.json`,
`KZ-2026-006`). Bu kontrol, henüz hiçbir fiili ağırlık girilmemiş bir emirde de (tüm parçalar `0 kg`
sayılır) otomatik olarak `WEIGHT_IMBALANCE` üretir — "taslak, henüz tartılmamış" durumunun doğal
sonucu, hatalı bir durum değildir; kullanıcı `PUT .../outputs` ile ağırlıkları girdikçe fark küçülür.

## 5. Dokümandaki tutarsızlıkların sistemde nasıl yakalandığı

Her iki tutarsızlık da dokümanın verdiği örnek senaryodan (bütün kuzu, 25 kg, 11,50 €/kg) gelir ve
`docs/assumptions.md`'de ayrıntılı tartışılır; burada yalnızca **sistemin ne yaptığı** özetlenir:

1. **287,50 € (hesaplanan) vs 351,00 € (beyan edilen).** `POST /cutting-orders` sırasında giriş
   lotunun alış kaydında (`purchase_receipt_line.declared_line_total`) beyan edilen tutar varsa ve
   hesaplanandan (`input_weight_kg × lot.unit_cost`) farklıysa, `PURCHASE_TOTAL_MISMATCH` uyarısı
   üretilir ve `cost_basis` (`computed`/`declared`) + `resolution_note` **ZORUNLU** hale gelir —
   verilmezse emir `closable=false` kalır (`MISMATCH_RESOLUTION_MISSING`), hiçbir zaman sessizce bir
   tarafı seçmez. Demo senaryosunda (`CUT-2026-001`, seed) `cost_basis=computed` seçilmiştir: birim
   fiyat (11,50 €/kg) ve ağırlık (25 kg) birbiriyle tutarlıdır, hatalı olan tek alan beyan edilen
   toplamdır (351,00 / 25 = 14,04 €/kg, hiçbir diğer alanla örtüşmüyor). Seçilmeyen esas da
   `alternative_basis` altında (her iki uçta da) raporlanır — kullanıcı isterse `declared` ile yeni bir
   emir açıp `351,00 €` esaslı dağıtımı da görebilir (`docs/golden/cutting_cases.json`, `KZ-2026-003`).
2. **22 kg (parçaların toplamı) vs 25 kg (giriş).** Fire kaydedilmezse 3,0 kg fark açıkta kalır;
   `PUT .../outputs`'a fire satırı (`is_waste=true`) eklenmeden kapanış denenirse `balance_diff_kg=3.000`,
   `WEIGHT_IMBALANCE`, `closable=false` (`docs/golden/cutting_cases.json`, `KZ-2026-001-QTY/VAL`). Fire
   3,0 kg olarak girilince denge tam olur (`KZ-2026-002-QTY/VAL`) — bkz. madde 2 (fire'ın neden bir lot
   olarak kaydedildiği).

## 6. Diğer kurallar (özet, ayrıntı `docs/assumptions.md`)

- Parça sırası girdi (reçete satırı) sırasıdır; uyarı sırası sabittir: `PURCHASE_TOTAL_MISMATCH`,
  `MISMATCH_RESOLUTION_MISSING`, `WEIGHT_IMBALANCE`.
- `cost_basis`/`resolution_note` yalnızca **emir oluşturulurken** belirlenebilir (sonradan
  düzeltilemez) — bu, dış üretimdeki "hizmet faturası `close`'da da verilebilir" kararının tersi bir
  seçimdir: burada uyuşmazlık kaynağı (alış belgesi) emirden önce zaten sabittir, geriye dönük
  değişikliğe izin vermek "önce kapanmaz dedik, sonra esası değiştirip kapattık" gibi denetim izi
  bozan bir senaryoya yol açardı. Web arayüzü bu alanları yalnızca "Yeni Parçalama Emri" formunda sorar;
  emir oluşturulduktan sonra uyuşmazlık çözümlenmemişse emir kapanamaz ve yeniden oluşturulmalıdır.
- ML (`/predict/cutting`) en iyi gayrettir: kapalıysa/zaman aşımına uğrarsa emir oluşturma
  **başarısız olmaz**, `ml_prediction` boş kalır (bkz. `docs/assumptions.md`).
