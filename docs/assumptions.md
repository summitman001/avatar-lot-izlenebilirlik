# Varsayımlar ve Maliyet Kuralları

Bu dosya `ml/oracle/` içindeki maliyet oracle'ının kurallarını yazar. Go backend ve Android aynı kurallara birebir uymalıdır; doğruluk referansı `docs/golden/*.json` dosyalarıdır (`cd ml && python -m oracle.cli` ile yeniden üretilir).

Genel sayısal kurallar:

- Tüm hesaplar `Decimal`, float yasak (float girdi `TypeError` verir).
- Birim maliyet 4 ondalık, para tutarları 2 ondalık, miktar (kg) 3 ondalık, yüzde 2 ondalık, oran (kg/kg) 4 ondalık. Hepsi ROUND_HALF_UP.
- JSON'da tüm decimal değerler string. Golden dosyalarında sayısal JSON değeri yoktur (yalnızca string, bool, null).
- Golden dosya biçimi: `[{"case_id","description","input":{...},"expected":{...}}]`. Reçete dosyası (`cutting_case.json`) tek nesnedir.

## Belge numaraları ve fatura belgesi silme

- **Otomatik numara, elle verilmiş numarayı atlar.** Numara alanı boş bırakılınca `number_sequence`
  sayacı kullanılır; sayaç elle verilen numaraları (seed, "Emir No" alanı) bilmediği için üretilen
  numara doluysa bir sonrakine geçilir. Mevcut veritabanlarında da ek işlem gerekmez.
- **Yalnızca onaylanmamış fatura belgesi silinir.** Onaylanmış belge alış fişinin kaynağıdır ve
  izlenebilirlik kaydı sayılır; silme isteği `409` döner.

## Görev 1 maliyet kuralları

Girdi: ürün adı ve `unit_weight_kg` (ürünün birim ağırlığı), sevk edilen lotlar (ad, kategori `meat|spice|casing|other`, miktar kg, birim maliyet), teslim alınan adet, hizmet birim fiyatı (adet başı), faturalanan adet (opsiyonel), satış fiyatı (opsiyonel, adet başı).

**Yuvarlama kuralı (Go ve Android ile aynı):**

1. Her maliyet satırı (her hammadde satırı ve hizmet satırı) ayrı ayrı 2 ondalığa ROUND_HALF_UP ile yuvarlanır: `round2(miktar × birim maliyet)`, `round2(faturalanan adet × hizmet fiyatı)`.
2. `material_cost` yuvarlanmış hammadde satırlarının toplamıdır; `service_cost` yuvarlanmış hizmet satırıdır; `total_cost = material_cost + service_cost`. Yani satır tutarlarının toplamı her zaman `total_cost`'a birebir eşittir (hypothesis ile test edilir).
3. `ekp_unit = round4(total_cost / teslim alınan adet)`.
4. `ekp_display = round2(total_cost / teslim alınan adet)`. 4 ondalıklı `ekp_unit`'ten ikinci kez yuvarlama YAPILMAZ.

| Çıktı | Formül | Yuvarlama |
|---|---|---|
| `lot_lines[i].amount` | miktar × birim maliyet | 2 ondalık, satır başına |
| `material_cost` | yuvarlanmış hammadde satırları toplamı | (toplam) |
| `service_cost` | faturalanan adet × hizmet birim fiyatı | 2 ondalık |
| `total_cost` | `material_cost + service_cost` | (toplam) |
| `ekp_unit` | `total_cost / teslim alınan adet` | 4 ondalık |
| `ekp_display` | `total_cost / teslim alınan adet` | 2 ondalık |
| `output_kg` | teslim alınan adet × `unit_weight_kg` | 3 ondalık |
| `ekp_per_kg` | `total_cost / output_kg` | 4 ondalık |
| `revenue` | satış fiyatı × teslim alınan adet | 2 ondalık |
| `gross_profit_unit` | satış fiyatı − `total_cost / adet` | 4 ondalık |
| `gross_profit_total` | `revenue − total_cost` | (fark) |
| `margin_pct` | `gross_profit_total / revenue × 100` | 2 ondalık |
| `yield_input_pct` | `output_kg` / toplam girdi kg × 100 | 2 ondalık |
| `yield_meat_ratio` | `output_kg` / et kg (kg/kg) | 4 ondalık |

Diğer kurallar:

- Faturalanan adet verilmezse teslim alınan adede eşittir. Farklıysa uyarı `INVOICED_QTY_MISMATCH` döner; hizmet maliyeti faturalanan adet üzerinden hesaplanır, EKP ise teslim alınan adede bölünür.
- Satış (ciro, kâr, marj) teslim alınan adet üzerinden hesaplanır. Satış fiyatı yoksa kâr alanları `null`.
- **Kg/adet dönüşümü:** Verim hesabında teslim alınan adet doğrudan kg sayılmaz; ürünün `unit_weight_kg` alanıyla kg'a çevrilir. Kaynak: yarışma dokümanı, "85 adet 1 kg Narin Sucuk" (Narin Sucuk için `unit_weight_kg = 1.000`). Lot miktarları kg'dır. `unit_weight_kg` verilmezse `output_kg` ve verim alanları `null` döner. 500 g'lık ürün için golden senaryo `UR-006` (170 adet × 0,500 kg = 85 kg; aynı toplam maliyet, adet başı EKP yarıya iner, kg başı EKP aynı kalır).
- Değerleme yalnızca `ekp_unit` (4 ondalık) veya `total_cost` üzerinden yapılır. `ekp_display × adet` kullanmak yanlıştır (aşağıdaki yuvarlama notuna bakın).
- Geçersiz girdi (teslim alınan adet ≤ 0, `unit_weight_kg` ≤ 0, negatif miktar veya fiyat, bilinmeyen kategori) `ValueError` verir.

## Görev 2 dağıtım kuralları

Girdi: giriş ağırlığı (kg), birim maliyet (€/kg, opsiyonel), alış belgesinde beyan edilen toplam (opsiyonel), maliyet esası `cost_basis` (`computed` veya `declared`), uyuşmazlık notu `resolution_note`, yöntem (`quantity` veya `value`), parçalar (ad, fiili kg, referans satış fiyatı €/kg, `is_waste`, `is_by_product`).

- **Maliyet esası:** `computed` ise toplam = giriş kg × birim maliyet (2 ondalık); `declared` ise toplam = belgede beyan edilen tutar.
- **Uyuşmazlık zorunluluğu:** Hesaplanan ve beyan edilen toplam farklıysa uyarı `PURCHASE_TOTAL_MISMATCH` döner. Bu durumda `cost_basis` ve `resolution_note` ZORUNLUDUR. Biri veya ikisi verilmemişse (boş/boşluk notu dahil) uyarı `MISMATCH_RESOLUTION_MISSING` döner ve `closable=false` olur; dağıtım geçici olarak hesaplanan tutar üzerinden gösterilir. Uyuşmazlık yoksa esas ve not gerekmez.
- **Alternatif esas:** Uyuşmazlıkta seçilmeyen esas da hesaplanıp `alternative_basis` altında raporlanır (esas, toplam, parça başı dağıtılan tutar). Kullanıcı seçimine göre iki tutar da desteklenir.
- **Ağırlık dengesi:** `balance_diff_kg = giriş kg − toplam(tüm parçalar, fire ve yan ürün dahil)`. `|fark| > 0,05 kg` ise uyarı `WEIGHT_IMBALANCE` ve `closable=false`. Tam 0,05 kg fark kabul edilir. Dengesiz kayıtta dağıtım yine de hesaplanır (önizleme) ama kapatmaya izin verilmez. `closable`, ağırlık dengesi sağlanmış VE uyuşmazlık çözümlenmiş (veya yok) ise `true`.
- **Fire (`is_waste`):** maliyet taşımaz (`0.00`); maliyeti satılabilir parçalara biner.
- **Yan ürün (`is_by_product`):** kendi satış değeri kadar maliyet taşır (kg × referans fiyat, 2 ondalık). Bu tutar toplam maliyetten düşülür, kalan havuz ana parçalara dağıtılır (net gerçekleşebilir değer yaklaşımı). Yan ürün değeri toplam maliyeti aşarsa `ValueError`.
- **Yöntemler:** `quantity`: havuz ana parçalara kg oranında; `value`: kg × referans fiyat oranında.
- **Dağıtım yuvarlaması** (`oracle/rounding.py`): her satır tam hassasiyetle hesaplanır ve 2 ondalığa yuvarlanır; kalan kuruş farkı en büyük tutarlı satıra eklenir (eşitlikte girdi sırasındaki ilk satır). Toplam dağıtılan tutar toplam maliyete kuruşu kuruşuna eşittir (hypothesis ile test edilir).
- Parça birim maliyeti: son dağıtılan tutar / parça kg (4 ondalık); fire için `0.0000`.
- Parça sırası girdi sırasıdır. Uyarı sırası sabittir: `PURCHASE_TOTAL_MISMATCH`, `MISMATCH_RESOLUTION_MISSING`, `WEIGHT_IMBALANCE`.

## Dokümandaki tutarsızlıklar

1. **287,50 € ile 351,00 €:** Kuzu parçalama senaryosunda sistemin hesabı 25 kg × 11,50 €/kg = 287,50 €, alış belgesinde beyan edilen ise 351,00 €. **Demo için 287,50 € seçildi.** Gerekçe: birim fiyat (11,50 €/kg) ve ağırlık (25 kg) birbiriyle tutarlı; hatalı olan tek alan beyan edilen toplamdır (351,00 / 25 = 14,04 €/kg, hiçbir diğer alanla örtüşmüyor). 351,00 € alternatifi de her senaryoda hesaplanıp `alternative_basis` altında raporlanır; `KZ-2026-003` doğrudan 351,00 € esasıyla koşar. Seçim kayıt başına `cost_basis` + `resolution_note` ile yapılır; ikisi yoksa kayıt kapatılamaz (`KZ-2026-007`). Gerçek iş kuralının hangisi olduğu yarışma sahibine teyit edilmelidir.
2. **22 kg ile 25 kg:** Parçaların toplamı 22 kg, giriş 25 kg; 3,0 kg fark fire kaydı yapılmadığı için açıkta kalır. Bu durum `WEIGHT_IMBALANCE` ve `closable=false` üretir (`KZ-2026-001`). 3,0 kg fire kaydedildiğinde denge tamamdır (`KZ-2026-002`).
3. **Yuvarlama: 18,16 × 85 ≠ 1.543,50:** `18,16 × 85 = 1.543,60`, oysa gerçek toplam 1.543,50 (fark 0,10 €). Sebep: 18,16, 18,158823... değerinin 2 ondalığa yuvarlanmış gösterimidir. Değerleme her zaman 4 ondalıklı `ekp_unit` (18,1588 × 85 = 1.543,498, 2 ondalık 1.543,50) veya doğrudan `total_cost` üzerinden yapılmalıdır; `ekp_display` yalnızca ekranda gösterilir. Golden: `UR-004` (`rounding_proof`).
4. **Senaryo 004 (yan ürünlü) parça ağırlıkları:** Dokümandaki parça kg değerleri 22 kg toplamlıdır (but 7,5 kg, kıyma 2,5 kg). Yan ürün olarak 1,0 kg ciğer eklendiğinde parça toplamı 22 kg kalsın (ve 3,0 kg fire ile giriş 25 kg'a tamamlansın) diye `KZ-2026-004` içinde **but 7,5 → 7,0 kg** ve **kıyma 2,5 → 2,0 kg** indirildi (ana parçalar 21,0 kg + ciğer 1,0 kg = 22,0 kg). Bu, dokümandaki değerlerden bilinçli bir sapmadır; golden açıklamasında da yazılıdır.

## Raf ömrü ve parça satış fiyatı varsayımları (seed)

Yarışma dokümanı raf ömrü ve parça satış fiyatı vermez. Seed verisi soğutulmuş ürün için tipik değerler kullanır; SKT verilmeyen lotlarda SKT bunlardan türetilir (`item.shelf_life_days`, bkz. `docs/api-contract.md`):

| Ürün | Raf ömrü (gün) | Varsayılan satış fiyatı |
|---|---|---|
| Dana eti | 7 | — |
| Sucuk baharatı | 365 | — |
| Bağırsak | 180 | — |
| Narin Sucuk 1 kg | 90 | 24,90 € (aşağıda) |
| Bütün kuzu (karkas) | 5 | — |
| Kuzu but/kol/sırt/kaburga/gerdan | 5 | reçetenin referans satış fiyatı (16 / 15 / 18 / 12 / 10 €) |
| Kuzu kıyma | 2 | 11 € (reçete) |
| Kuzu fire | yok (SKT'siz) | satılmaz |

Parçaların varsayılan satış fiyatı `docs/golden/cutting_case.json` içindeki `reference_sale_price` ile aynıdır (tek kaynak); Android'de satış satırına öneri olarak gelir, satıcı değiştirebilir.

## Cihazdan satış kuralları

Android satış uygulaması yalnızca mamul (`finished`), parça (`cut_part`) ve yan ürün (`by_product`) satar ve yalnızca iç depodan (`internal`) veya araçtan (`vehicle`) satış yapar. Cihaz hammadde/fire lotlarını, cihaz deposunda stoğu olmayan lotları ve karantina/dış üretici depolarını hiç göstermez; sunucu da bu kurala aykırı bir `sales_order` push'unu `rejected` ile reddeder (`DEVICE_SALE_ITEM_TYPE`, `DEVICE_SALE_WAREHOUSE`). Bu bir sözleşme ihlalidir, offline tolerans uygulanmaz: geri çağrılan lot veya yetersiz stok gibi fiziksel olarak gerçekleşmiş satışların aksine, kuralı uygulayan bir cihaz bu satışı hiç oluşturamaz. Web (REST) satışı bu kurala tabi değildir (örn. toptan hammadde satışı).

## VKP varsayımı (24,90 €)

Doküman senaryosunda satış fiyatı (VKP) verilmediği için kâr ve marj örneği 24,90 € varsayımıyla hesaplanır. Bu değer yarışma dokümanından gelmez; `UR-001` ile `UR-004` arası golden senaryolarında kullanılır. `UR-005` satış fiyatsız örnektir (kâr alanları `null`); `UR-006` aynı fiyatın yarısını (12,45 € / 500 g) kullanır.

| Senaryo | ekp_unit | Birim kâr | Toplam kâr | Marj |
|---|---|---|---|---|
| UR-001 (85 teslim) | 18,1588 | 6,7412 | 573,00 | %27,07 |
| UR-002 (83 teslim, 83 fatura) | 18,4639 | 6,4361 | 534,20 | %25,85 |
| UR-003 (83 teslim, 85 fatura) | 18,5964 | 6,3036 | 523,20 | %25,32 |
| UR-006 (170 adet × 500 g) | 9,0794 | 3,3706 | 573,00 | %27,07 |

VKP gerçek değerle değişirse yalnızca `ml/oracle/cli.py` içindeki `VKP` sabiti güncellenir ve golden dosyaları yeniden üretilir.

## Backend uygulamasına ilişkin varsayımlar ve tespitler

Bu bölüm Go backend'in (`backend/`) veri modelini ve API davranışını etkileyen kararları özetler. Sayısal kurallar yukarıdadır; Go bunları `backend/internal/domain` içinde uygular ve `docs/golden/*.json` dosyalarını okuyan testlerle doğrular (`go test ./internal/domain`).

1. **VKP 24,90 € varsayımı.** Yarışma dokümanı satış fiyatı vermediği için `SUCUK-NARIN-1KG` ürününün `default_sale_price` değeri **24.9000** olarak seed'lenir (`backend/cmd/seed`). Bu değer dokümandan gelmez, bir **varsayımdır**; gerçek fiyat netleşince yalnızca ürün kaydı güncellenir (`PUT /api/v1/items/{id}`) ve — golden senaryolar için — `ml/oracle/cli.py` içindeki `VKP` sabiti.
2. **KZ-2026-001 alış belgesi tutarsızlığı.** 25 kg × 11,50 €/kg = **287,50 €** (hesaplanan) iken belgede **351,00 €** (beyan) yazıyor. Sistem bu satırı gizlemez ve sessizce düzeltmez: `purchase_receipt_line.line_total = 287.50`, `declared_line_total = 351.00`, **`has_mismatch = TRUE`**. Lot maliyeti hesaplanan birim fiyattan (11,5000) kurulur; web'de lot listesinde "Tutar uyuşmazlığı" rozeti görünür, `GET /api/v1/lots?has_mismatch=true` ile filtrelenir. Hangi tutarın geçerli olduğu kararı parçalama kaydında `cost_basis` + `resolution_note` ile verilir (yukarıdaki "Görev 2" kuralları). Gerçek iş kuralı yarışma sahibine teyit edilmelidir.
3. **Satır tutarı yuvarlaması.** `line_total = round2(qty × unit_price)` (ROUND_HALF_UP); örn. 18,500 kg × 11,75 = 217,375 → **217,38**. Veritabanında tutarlar `DECIMAL(14,4)` saklanır ama satır tutarları 2 ondalıkta yuvarlanmış değerdir; API tutarları 4 ondalıklı string döner (`"217.3800"`).
4. **Senkron push gerçek işlem yapar.** `POST /sync/push`, `sales_order/create` ve `customer_return/create` işlemlerini REST ile aynı servis fonksiyonları üzerinden gerçekten kaydeder (`backend/internal/repo/sync.go`). Her işlem kendi transaction'ındadır ve sonuç işlem bazında döner: kural ihlali yoksa `applied`; satış bloke/geri çağrılmış lottan yapılmışsa veya stoğu negatife düşürüyorsa satış yine kaydedilir ve `flagged` döner (`LOT_BLOCKED_AT_SALE`, `NEGATIVE_STOCK_OFFLINE`); aynı `op_id` tekrar gelirse uygulanmaz, `duplicate` döner (`processed_request`, ilk sonuç mesajda); bozuk payload, bilinmeyen referans, cihazdan satılamayan ürün/depo veya satılandan fazla iade `rejected` döner ve hiçbir kayıt yazılmaz. Eski taslak dönemin `processed_request` kayıtları migration `000006` ile temizlendi. Ayrıntı: `docs/sync.md`.
5. **Stok defteri.** `stock_movement` değiştirilemez (UPDATE/DELETE trigger ile reddedilir); düzeltme yalnızca `reversal` kaydıyladır. Giriş tipleri (+), çıkış tipleri (−) işaret kuralı veritabanında CHECK ile zorlanır. Geri alma stoğu negatife düşürüyorsa reddedilir.
6. **Kimlik doğrulama yok.** API şimdilik açıktır (CORS `*`); yarışma demosu içindir, üretimde eklenmelidir.
7. **Anomali uyarıları (`anomaly_alert`) SADECE BİLGİLENDİRMEDİR, bir karar mekanizması DEĞİLDİR.**
   `/detect/anomaly` (ML, en iyi gayret) ve iki kural tabanlı kontrol (deterministik `KUTLE_DENGESI`,
   `NEGATIVE_STOCK_OFFLINE`) hiçbir lotu bloke etmez, hiçbir işlemi reddetmez; lot izlenebilirliği,
   geri çağırma ve stok kararları HER ZAMAN kesin kayıt ilişkilerine (stok defteri, `lot_link`)
   dayanır, bir anomali bulgusuna değil. Ayrıntı ve çağrı noktaları: `docs/anomaly-integration.md`
   ("Go entegrasyonu"). Görev tanımındaki "LOT_HAREKET_TUTARSIZ" kavramı, mevcut
   `NEGATIVE_STOCK_OFFLINE` mekanizmasıyla (bkz. "Satış/sevkiyat/iade" bölümü) BİRLEŞTİRİLDİ — iki
   ayrı sistem yok, aynı `alert_type` ve aynı `upsertAnomalyAlert` yolu kullanılıyor.

## Dış üretim (Görev 1) servisine ilişkin varsayımlar ve tespitler

Durum makinesi: `draft → materials_sent → received → closed`. Uygulama: `backend/internal/repo/production.go`, uç noktalar `backend/internal/http/handlers.go`, istek/durum kuralları `backend/internal/domain/production.go`. Doğruluk referansı yine `docs/golden/production_cases.json`; `TestProductionGoldenScenarios` (`backend/internal/integration/production_test.go`) golden senaryoları (`UR-001`, `UR-002`, `UR-003`, `UR-006`) **gerçek HTTP uç noktalarıyla** (`POST /production-orders` → `send-materials` → `receive` → `GET .../cost-report`) koşturup sonucu golden dosyasından okunan beklenen değerlerle karşılaştırır.

1. **İade edilmeyen hammadde (proses firesi dahil) mamul maliyetine yüklenir.** `receive` adımında her girdi satırı için `tüketilen = sevk edilen − iade edilen`; tüketilen miktar `production_consume` hareketiyle dış depodan düşülür ve `CalculateProductionCost`'a girdi olur. İade edilmeyen miktarın neden tükendiği (gerçek üretim kaybı mı, sayım farkı mı) ayrıca izlenmez — sistem bunu basitçe "mamulün maliyetine dahil" sayar. Gerçek bir fire/kayıp ayrımı istenirse `production_input`'a ayrı bir `qty_wasted` alanı eklenebilir (TODO).
2. **Hizmet bedeli faturalanan adet üzerinden alınır, EKP ise teslim alınan adede bölünür.** `UR-003` golden senaryosu bunu kanıtlar: 83 teslim alınmış ama üretici 85 adet faturalamışsa (`service_invoiced_qty=85`), `service_cost = round2(85 × hizmet_fiyatı)` ama `ekp_unit = total_cost / 83`. Uyarı: `INVOICED_QTY_MISMATCH`.
3. **%2 miktar farkı eşiği (`QTY_VARIANCE`).** `|teslim_alinan − planlanan| / planlanan × 100 > 2` ise uyarı üretilir (`domain.QtyVarianceThresholdPct`). Golden senaryoların hiçbiri bu uyarıyı içermediği için entegrasyon testinde `planned_qty = delivered_qty` verilerek bu uyarı devre dışı bırakılır (karşılaştırmayı bozmasın diye); gerçek kullanımda `planned_qty` sipariş açılışında girilen bağımsız bir değerdir.
4. **Hammadde kategorisi artık `item.material_category` alanıdır** (migration `000002`, ENUM `meat|spice|casing|other`, yalnızca `item_type=raw` için anlamlı). `CalculateProductionCost` her satır için bu kategoriyi zorunlu kılar; kategorisi olmayan bir hammaddenin lotu daha `send-materials`'ta `422` ile reddedilir (teslimde reddedilseydi sipariş `materials_sent`'te, stok dış depoda kilitli kalırdı). Seed'de `ET-DANA=meat`, `BAHARAT-SUCUK=spice`, `BAGIRSAK=casing` olarak ayarlıdır.
5. **Tek depo varsayımı.** Bir siparişin tüm girdileri **aynı** ana depodan sevk edilmiş olmalı (dış üretici deposuna `send_to_producer`/`transfer_in` çifti bu depodan gider); çıktı mamulü de aynı depoya girer (`production_in`). Girdi lotları farklı depolardan sevk edilmişse `receive` `422` döner. Bir lot birden fazla depoya bölünmüşse (`v_lot_balance` birden fazla satır) ve sevk miktarını karşılayan TEK bir depo yoksa `send-materials` `409` döner (çoklu depodan birleştirilmiş sevkiyat desteklenmez).
6. **`production_input (production_order_id, lot_id)` UNIQUE'dir** (migration `000003`): bir lot aynı siparişe en fazla bir sevk satırıyla gönderilebilir. Bu, `receive` sırasında "bu lot hangi depodan sevk edildi" sorusunun defterden (bkz. 7) tek ve belirsizliksiz cevaplanmasını sağlar. `draft`/`materials_sent` durumunda birden fazla `send-materials` çağrısına (kısmi/çok partili sevkiyat) izin verilir; aynı lot ikinci kez gönderilmeye çalışılırsa `409`.
7. **Sipariş numarası (`URT-YYYY-NNN`) ve çıktı lot numarası (`SUC-YYYY-NNN`) üretimi ATOMİKTİR.** `number_sequence` tablosu (migration `000005`, PK `(prefix, year)`) + `INSERT ... ON DUPLICATE KEY UPDATE seq_value = seq_value + 1` deseni (`backend/internal/repo/production.go`, `nextSequenceNo`): bu tek satır hem satırı oluşturur/kilitler hem de artırır; es zamanlı iki çağrı aynı satırda **sıraya girer** (ikincisi birincinin transaction'ı commit/rollback olana kadar bekler), bu yüzden hiçbir zaman aynı numara üretilmez — `409` ile reddetme veya yeniden deneme gerekmez. `TestProductionOrderNumberingIsAtomicUnderConcurrency` 10 siparişi **gerçekten aynı anda** (goroutine'lerle) oluşturup hepsinin benzersiz numara aldığını doğrular. (Kolon adı `last_value` değil `seq_value`: `LAST_VALUE()` MySQL 8.0'da pencere fonksiyonu olduğu için ayrılmış sözcüktür, backtick'le bile kolon adı olarak kullanılamıyor — bkz. migration yorumu.)
8. **"Toplam stok değeri değişmiyor" DEĞİL, hizmet bedeli kadar ARTAR.** Hammadde değeri (`material_cost`) tüketilen miktarla birebir mamule aktarılır, ama mamulün birim maliyeti (`ekp_unit`) hizmet bedelini de içerir. Yani sevk+teslim sonrası toplam envanter değeri, önceki hammadde değerinden tam olarak `service_cost` kadar YÜKSEKTİR (gerçek bir nakit maliyet eklenmiştir, stoktan gelmez). `TestProductionOrderFullLifecycle` bu özdeşliği (`material_cost` tüketilen hammaddenin tam değeri, `total_cost = material_cost + service_cost`) doğrular; "değişmiyor" ifadesi yalnızca hammadde değerinin kaybolmadan aktarıldığı anlamında doğrudur.
9. **Hizmet faturası `close`'da da verilebilir.** `receive`'de `service_invoice_no` opsiyoneldir (fatura genelde mamulden sonra gelir); verilmemişse `POST .../close` gövdesinde `service_invoice_no` ile tamamlanabilir. İkisi de boşsa kapatma `422` ile reddedilir.
10. **Durum geçmişi ayrı bir tablo yerine mevcut zaman damgalarından türetilir** (`GET /production-orders/{id}` → `status_history`): `draft` = siparişin `created_at`'i, `materials_sent` = bu siparise ait ilk `send_to_producer` hareketinin zamanı, `received` = çıktı lotunun `created_at`'i, `closed` = `closed_at`. Ayrı bir durum-geçişi log tablosu eklenmedi (şema sadeliği); gerçek bir denetim izi gerekiyorsa eklenebilir (TODO).
11. **Hibrit EKP tahmini.** Yarışma dokümanının zorunlu YZ maddesi A "beklenen mamul miktarını VE birim maliyetini (EKP) tahmin eden bir model" ister. `ml/` servisinin kendisi bir maliyet modeli değildir — yalnızca fiziksel verimi (`predicted_yield_ratio`/`predicted_output_kg`/`predicted_delivered_qty`) tahmin eder, hammadde/hizmet fiyatlarını bilmez. EKP tahmini bu yüzden **hibrit** üretilir: ML'in fiziksel adet tahmini + sunucudaki **kesin** malzeme maliyeti birleştirilir:

   ```
   predicted_ekp_unit = (material_cost + round2(predicted_qty × service_unit_price)) / predicted_qty
   ```

   (4 ondalık, `DivRound`; hizmet bedelinde faturalanan adetin tahmini adetle aynı olduğu varsayılır — tıpkı gerçek EKP hesabında `invoiced_qty` verilmezse `delivered_qty`'ye eşit sayılması gibi.) `material_cost`, o ana kadar sevk edilmiş hammaddelerin KESİN tutarıdır (`round2(qty_sent × unit_cost_at_send)` toplamı); tahmini değildir.
12. **Tahmin iki AŞAMADA alınır, önceki satırın ÜZERİNE YAZILMAZ.** `prediction.stage` ∈ `draft | materials_sent`; her asama ayrı bir satırdır (`backend/internal/repo/production.go`, `insertPrediction`):
    - **`draft`** (sipariş oluşturulunca): hammadde kompozisyonu henüz bilinmediği için `et_kg_estimate = planned_qty × unit_weight_kg / 0,85` (varsayılan verim, golden `UR-001..004`'teki `yield_meat_ratio=0,8500` ile aynı) kabaca hesaplanıp ML'e gönderilir. Malzeme maliyeti henüz bilinmediği için **`predicted_ekp_unit` bu aşamada `null`'dur**.
    - **`materials_sent`** (hammadde sevki tamamlanınca, her `send-materials` çağrısından sonra güncellenir): artık GERÇEK sevk edilen et miktarı (`et_kg`, tahmini değil) ve KESİN `material_cost` bilinir; ML bu gerçek `et_kg` ile tekrar sorulur ve yukarıdaki formülle **`predicted_ekp_unit` hesaplanır**. `GET`/`cost-report` her zaman EN GÜNCEL satırı gösterir (`ORDER BY created_at DESC LIMIT 1`), yani `materials_sent` tahmini varsa `draft`'ınkinin yerini (görünürlükte, veritabanında değil) alır.

    `receive` sırasında (`recordPredictionActual`) gerçekleşen `received_qty` ve hesaplanan `ekp_unit`, en güncel tahminle karşılaştırılıp AYNI satıra `actual_json`/`error_json` olarak yazılır: `actual_ekp_unit`, `ekp_error_abs` (işaretli: `actual − predicted`), `ekp_error_pct` (`round2(|fark| / predicted × 100)`). En güncel tahmin `draft` ise (yani `materials_sent`'te ML kapalıydı) `predicted_ekp_unit` yoktur; bu durumda yalnızca adet sapması yazılır, EKP sapma alanları boş kalır — var olmayan bir tahminle sahte bir karşılaştırma uydurulmaz. `TestProductionHybridEkpPrediction` (mock ML sunucusu, sabit `predicted_qty=84,87`) formülün API'de **elle hesaplanan değerle birebir aynı** sonucu verdiğini ve sapmanın doğru işaretle yazıldığını doğrular.
13. **ML "en iyi gayret"tir, her iki aşamada da.** `ML_URL` erişilemezse (servis kapalı, zaman aşımı) ne sipariş oluşturma ne de hammadde sevki **başarısız olur**; uyarı loglanır ve o aşamanın tahmini atlanır (`predictOnCreate`, `predictOnMaterialsSent`). `TestProductionMLUnreachableStillCreatesOrder` bunu gerçek bir ağ hatasıyla (sahte değil, dinlemeyen bir porta bağlanarak) doğrular.

## Satış/sevkiyat/iade + senkron (Görev 2) servisine ilişkin varsayımlar ve tespitler

Uygulama: `backend/internal/repo/sales.go` (servis), `backend/internal/repo/sync.go` (`/sync/push`
gerçek işleme), `backend/internal/domain/sales.go` (istek/varlık tipleri). Doğruluk referansı
`backend/internal/integration/sales_test.go` (gerçek HTTP uç noktaları + gerçek MySQL ile).

1. **Tek servis fonksiyonu, "online"/"offline" bayrağıyla dallanır.** `Store.createSalesOrderInTx`
   hem `POST /sales-orders` (REST, `SaleOptions{Online: true}`) hem `/sync/push`'un `sales_order/create`
   işlemesi (`SaleOptions{Online: false}`) tarafından çağrılır — iş mantığı tek yerde, tekrar yok.
   `Store.CreateSalesOrder` (REST) kendi transaction'ını açar; sync katmanı (`repo/sync.go`) ZATEN açık
   olan transaction'ı (processed_request rezervasyonuyla birlikte) kullanır — bu yüzden iç fonksiyon
   (`createSalesOrderInTx`) bir `*sql.Tx` alır, kendi açmaz/kapatmaz. Aynı desen `customer_return` için
   `createCustomerReturnInTx`'tedir.
2. **`sales_order` satırı, satırlardan (`sales_line`) ÖNCE yazılır.** `sales_line.sales_order_id` FK'si
   olduğu için ilk sürümde satırlar döngü içinde yazılıp `sales_order` döngüden SONRA ekleniyordu — bu,
   her satışta FK ihlaliyle (`referans verilen kayit bulunamadi`) başarısız oluyordu (entegrasyon
   testleriyle yakalandı). Düzeltme: `sales_order` `total_amount=0` yer tutucuyla ÖNCE yazılır, döngü
   biter bitmez gerçek toplamla `UPDATE` edilir.
3. **`device_id` push zarfından sipariş satırına OTOMATİK kopyalanmaz.** `/sync/push` isteğinin üst
   seviye `device_id`'si yalnızca `processed_request`/`device.last_sync_at` bakımı içindir ve `device`
   tablosunda kayıtlı olması ZORUNLU DEĞİLDİR (cihaz kayıt uç noktası henüz yok, bkz. HANDOFF TODO).
   `sales_order.device_id` ise şemada `device` tablosuna FK'lidir — kayıtsız bir id buraya yazılırsa FK
   ihlaliyle reddedilir. Bu yüzden `payload.device_id` yalnızca PAYLOAD'UN KENDİSİ açıkça verdiyse (ve
   o cihaz kayıtlıysa) kullanılır; push zarfının device_id'si asla sızdırılmaz. Bu da entegrasyon
   testleriyle yakalanan gerçek bir hataydı (ilk sürüm otomatik kopyalıyordu).
4. **Satır içi sorgu, dışarıdaki açık `rows` kapanmadan çalıştırılamaz (`*sql.Tx` tek bağlantıdır).**
   `loadShipmentsForOrder` ilk sürümde her sevkiyat satırını, DIŞARIDAKİ sevkiyat `rows`'u hâlâ açıkken
   (`for rows.Next()` içinde) `loadShipmentLines` ile sorguluyordu; bu `*sql.DB` havuzuyla sorunsuzdu
   ama `*sql.Tx` TEK bir bağlantıya bağlı olduğu için sürücü `invalid connection` hatası veriyordu
   (yalnızca gerçek MySQL'e karşı entegrasyon testinde ortaya çıktı, birim testlerinde görünmez).
   Düzeltme: önce TÜM sevkiyat satırları okunup `rows` kapatılır, SONRA her biri için ayrı sorgu atılır.
   Yeni bir repo fonksiyonu yazarken aynı tx üzerinde iç içe açık `*sql.Rows` kullanmayın.
5. **Online kurallar sert hata, offline kurallar bayrak.** `opts.Online=true` (REST) iken bloke lot
   `422` (mesajda `LOT_BLOCKED`), yetersiz stok `409` (mesajda `INSUFFICIENT_STOCK`) — hiçbir kayıt
   kalmaz (transaction geri alınır). `opts.Online=false` (sync) iken aynı durumlar satışı
   REDDETMEZ: `sales_line.recall_violation=true` / stok negatife düşer + `anomaly_alert`, sonuç
   `violations` listesiyle döner, sync katmanı bunu `flagged` yapar (bkz. `docs/sync.md`, "OFFLINE
   KURALI"). Mesaj kodu tam olarak `LOT_BLOCKED_AT_SALE` / `NEGATIVE_STOCK_OFFLINE`'dır (birden fazlaysa
   virgülle birleştirilir) — Android istemcisi bu sabit string'lere göre dallanabilir.
6. **`sales_order.status` her zaman `'confirmed'`dir.** Şemadaki `draft`/`cancelled` değerleri bu
   görevde kullanılmaz (taslak/iptal akışı kapsam dışı); bir satış oluştuğu anda stok hareketi zaten
   postalanmıştır, geri alınamaz bir "taslak" durumu anlamsızdır.
7. **`customer_return` depo türetimi: `sales_line_id` → siparişin deposu; yalnız `lot_id` → lotun en
   son `sale_out` hareketinin deposu.** Şema `customer_return`'e `warehouse_id` eklemez (görev tanımı
   da istemez); `sales_line_id` verilmişse ilişkili `sales_order.warehouse_id` kullanılır, yalnız
   `lot_id` verilmişse (satış satırı referansı yoksa) lotun en güncel `sale_out` hareketinin deposuna
   `return_in` yazılır (bu lot hiç satılmadıysa `422`).
8. **İade numarası öneki `IAD-<yıl>-NNN`'dir** (atomik, `number_sequence`, aynı desen `SAT`/`SHP`
   gibi) — görev tanımı bu öneki açıkça belirtmediği için seçilen bir varsayımdır.
9. **İade için offline tolerans YOKTUR.** Satış fiziksel olarak zaten gerçekleştiği için offline modda
   hiçbir zaman reddedilmez (en fazla `flagged`); ama iade bir STOK GERİ ALMA işlemidir, fiziksel bir
   zorunluluk taşımaz — satılandan fazla iade her zaman (online veya `/sync/push` üzerinden) `422`/
   `rejected` ile reddedilir, hiçbir kayıt yazılmaz.
10. **Lot `change_log` payload'ı artık bakiye İÇERİR.** Önceki sözleşme "lot payload'ı bakiye içermez"
    diyordu (ana veri değişikliklerinde doğru); ama bir satış/iade/bloke-açma lotun durumunu veya
    (dolaylı olarak) bakiyesini değiştirdiğinde, o lot için YENİDEN `getLot(..., withDetail=true)` ile
    okunan TAM görünüm (`qty_on_hand`/`balances` dahil) `change_log`'a yazılır — Android'in kendi
    offline hesapladığı bakiyeyi sunucunun otoriter değeriyle karşılaştırıp düzeltebilmesi için
    (`docs/sync.md`, "Pull" bölümü). Ayrı bir `qty_on_hand_server` alanı EKLENMEDİ: mevcut `qty_on_hand`
    alanı zaten bu amaca hizmet eder, şema/sözleşme basitliği için aynı alan yeniden kullanıldı.
11. **Bloke lot mekanizması `POST /lots/{id}/block`+`unblock` ile minimaldir.** Durum makinesi yok
    (herhangi bir durumdan `blocked`/`active`'e geçilebilir); Görev 3'ün geri çağırma servisi bu iki
    fonksiyonu (`Store.SetLotBlocked`) olduğu gibi yeniden kullanacaktır. `GET /sync/pull`'daki
    `blocked_lot_ids` zaten her çekimde `status IN ('blocked','recalled')` tam listesini döndüğü için
    (önceki görevden beri) bu uç noktalar için ayrı bir sync değişikliği gerekmedi.
12. **`/sync/push` idempotency + "rejected hiçbir kayıt yazmaz" tek bir desenle sağlanır:**
    `processed_request` satırı önce `'processing'` durumuyla REZERVE edilir (aynı transaction'ın İLK
    adımı); `op_id` PRIMARY KEY olduğu için eş zamanlı aynı `op_id`'ler burada serileşir (ikincisi
    UNIQUE ihlaliyle anında `duplicate` olur, hiçbir yan etkisi commit edilmeden geri alınır). Payload
    işlenirken bir `domain.ValidationError`/`ConflictError` dönerse TÜM transaction (rezervasyon dahil)
    geri alınır → `rejected`, hiçbir iz kalmaz. Başarılıysa rezervasyon satırı aynı transaction'da
    gerçek `status`/`message` ile `UPDATE` edilip commit edilir. Bu, önceki "stub" sürümünün basit
    `INSERT ... ON DUPLICATE` desenini (yalnızca `processed_request`'i yazan, iş mantığı eklenince
    atomiklik garantisi kalmayan) değiştirir.

## İzlenebilirlik ve geri çağırma (Görev 3) servisine ilişkin varsayımlar ve tespitler

Uygulama: `backend/internal/repo/traceability.go`, istek/varlık tipleri
`backend/internal/domain/traceability.go`. Veri modeli ve recursive CTE mantığı ayrıntılı olarak
`docs/traceability.md`'dedir; burada yalnızca BİLİNÇLİ kararlar özetlenir.

1. **ET-2026-001 senaryosu dokümanla birebirdir.** 100 kg, tamamı `URT-2026-045`'te kullanılır →
   85 paket `SUC-2026-045` → Market A/B/C 30/25/30. Dokümandaki "bir hammadde lotu birden fazla mamul
   lotuna dönüşebilir" ilişkisi, ET-2026-001'i değiştirmemek için AYRI bir et lotunda (`ET-2026-002`,
   32 kg → `SUC-2026-046` ve `SUC-2026-047`) gösterilir; bu lotlar ET-2026-001'in geri çağrılmasından
   etkilenmez (bağımsız kontrol).
2. **İkinci kademe dağıtım yalnızca kaydedilmişse gösterilir.** Marketin kendi alıcısına dağıttığı
   miktar `downstream_distribution`'a (migration `000012`) kaydedilir; kayıt yoksa `downstream_known`
   `false`'dur ve sabit `downstream_note` (`domain.DownstreamNoteText`) eşlik eder. Kayıt varsa
   yalnızca kayıtlı alıcılar ve miktarlar kesin olarak gösterilir; kayıtsız kalan miktar için rakam
   uydurulmaz, not döner. Toplam kayıt müşteriye giden net miktarı (sevk − iade) aşamaz. Web bunu kesin veriden (satış/sevkiyat miktarı) görsel olarak AYRI bir rozetle
   (dolgu yeşil = kesin, çerçeveli gri = bilinmiyor) gösterir; aynı renk kullanılmaz.
3. **Geri çağırma, bloke lot mekanizmasını `setLotStatusInTx` ortak yardımcısıyla yeniden kullanır.**
   `SetLotBlocked`'ın (Görev 2) iç mantığı `setLotStatusInTx(tx, lotID, status, reason)`'a çıkarıldı;
   hem `POST /lots/{id}/block` (`status='blocked'`) hem `CreateRecall` (`status='recalled'`) AYNI
   fonksiyonu (aynı `change_log` tam-lot-görünümü deseni) kullanır. `GET /sync/pull`'daki
   `blocked_lot_ids` (`status IN ('blocked','recalled')`) bu sayede HİÇBİR ek değişiklik gerektirmeden
   etkilenen lotları otomatik içerir.
4. **`recall_no` deseni `RCL-<yıl>-NNN`, atomik `number_sequence` ile** (`SAT`/`SHP`/`IAD`/`URT`/`SUC`
   ile aynı desen, bkz. "Dış üretim" bölümü madde 7).
5. **Aynı kök lot için AÇIK bir geri çağırma varsa `CreateRecall` 409 döner** (mesajda mevcut
   `recall_no`); `SELECT ... FOR UPDATE` ile kontrol edilip aynı transaction'da yeni kayıt yazılır, bu
   yüzden eş zamanlı iki çağrı birbirini geçemez. Kapatılmış (eski) bir recall aynı kök lot için YENİ bir
   recall açılmasını ENGELLEMEZ.
6. **`qty_shipped` kaynağı: `sales_line` (sipariş → müşteri) BİRLEŞİMİ siparişe bağlı olmayan
   `shipment_line`'dır** (`shipment.sales_order_id IS NULL`). Bu kod tabanında her satış zaten bir
   sevkiyat doğurduğu için (Görev 2) pratikte kaynak her zaman `sales_line`'dır; birleşim yalnızca
   ileride "siparişsiz sevkiyat" eklenirse veri kaybını önleyen bir güvenlik payıdır.
7. **`CloseRecall`/`ReopenRecall` lotların `recalled` durumunu DEĞİŞTİRMEZ.** Kapatma yalnızca
   `recall.status`'u günceller (tüm müşteriler `confirmed` olmadan `422`); lotları tekrar `active` yapmak
   için AYRI `POST /lots/{id}/unblock` (Görev 2'den beri var) kullanılır -- bir recall'ın idari olarak
   kapanması ile ürünün fiilen piyasaya geri dönmesi (karantina kararı) BİLEREK ayrı işlemlerdir.
   Geri çağrılan lota `/sync/push` ile geç düşen satış, kapalı geri çağırmayı otomatik yeniden açar (bkz.
   `docs/traceability.md`, "Geri çağırma listesi geç satışlarla güncellenir"), çünkü haber verilmemiş
   riskli ürün doğmuştur. Lot durumu yine değişmez.
8. **Parçalama/yeniden işleme servisi henüz yok**, bu yüzden "çok seviyeli izleme" ve "döngü koruması"
   testleri (`TestTraceForwardThreeLevelsDeep`, `TestTraceForwardCycleGuard`) `lot_link` satırlarını
   DOĞRUDAN (ham SQL ile) oluşturur. Gerçek parçalama servisi eklenince bu testler değişmeden
   geçerliliğini korur (yalnızca `lot_link` kaynağı değişir, sorgu mantığı aynıdır).
9. **İzlenebilirlik/geri çağırma sorgularının HİÇBİRİ ML çağrısı yapmaz.** `repo/traceability.go`
   dosyasında `s.ml`'e (ML istemcisi) hiçbir referans yoktur; tüm entegrasyon testleri (`newEnv`) ML
   istemcisi BAĞLANMADAN (nil) çalışır ve geçer -- bu, sonuçların ML'in açık/kapalı olmasından bağımsız,
   tamamen kesin kayıtlara dayandığını kanıtlar.

## Parçalama (Görev 2) servisine ilişkin varsayımlar ve tespitler

Uygulama: `backend/internal/repo/cutting.go` (servis), `backend/internal/domain/cutting_order.go`
(varlıklar/istek gövdeleri), `backend/internal/domain/cutting.go` (`AllocateCutting`/`ResolveCostBasis`,
saf hesap). Dağıtım yöntemleri, fire/yan ürün muamelesi, yuvarlama ve ağırlık dengesi kuralları
`docs/cutting.md`'dedir; burada yalnızca BİLİNÇLİ backend kararları özetlenir. Doğruluk referansı
`backend/internal/integration/cutting_test.go` (`TestCuttingGoldenScenarios` gerçek HTTP uç
noktalarıyla, `docs/golden/cutting_cases.json`'dan okunan beklenen değerlerle).

1. **`cost_basis`/`resolution_note` SİPARİŞ OLUŞTURULURKEN sabitlenir, sonradan değiştirilemez.**
   `POST /cutting-orders`, giriş lotunun alış kaydında (`purchase_receipt_line.declared_line_total`)
   bir uyuşmazlık varsa `domain.ResolveCostBasis`'i O ANDA çağırır ve `total_input_cost`'u kalıcı olarak
   yazar (`cutting_order.total_input_cost` NOT NULL); `cost_basis`/`resolution_note` verilmemişse emir
   yine oluşur ama `closable` hiçbir zaman `true` olamaz (`MISMATCH_RESOLUTION_MISSING`) — düzeltmek
   için aynı lotla yeni bir emir açılmalıdır. Bu, dış üretimdeki "hizmet faturası `close`'da da
   tamamlanabilir" esnekliğinin tam tersidir; burada uyuşmazlık kaynağı (alış belgesi) emirden önce
   zaten sabit olduğu ve geriye dönük değişikliğe izin vermenin "kapanmaz dedik, sonra esası
   değiştirip kapattık" gibi denetim izini bozan bir senaryo açacağı için bilinçli olarak izin
   verilmemiştir.
2. **Giriş lotunun beyan edilen tutarı (`declared_total_cost`) PRORATE edilir.** `cutting_order` kısmi
   bir lotu (lotun tamamından daha az ağırlık) parçalayabilir; bu durumda belgede beyan edilen tutar
   lotun TAMAMI içindir, parçalanan kısım için `declared_line_total × (input_weight_kg / lot.initial_qty)`
   ile oranlanır. Demo senaryosunda (`KZ-2026-001`, 25 kg'ın tamamı) oran 1'dir, beyan direkt 351,00 €
   olarak kullanılır.
3. **Dağıtım önizlemesi (`allocation.quantity`/`allocation.value`) KALICI DEĞİLDİR, her istekte taze
   hesaplanır.** `cutting_output.allocated_cost`/`unit_cost` yalnızca KAPANIŞTA (`CloseCuttingOrder`)
   yazılır (kalıcı, denetim izi); draft iken `GET`/`PUT .../outputs` her çağrıda
   `domain.AllocateCutting`'i YENİDEN çalıştırır (hem `quantity` hem `value` için). `cutting_order`
   tablosunda yalnızca SEÇİLEN yöntemin `balance_diff_kg`/`closable`'ı saklanır (liste görünümü —
   `GET /cutting-orders` — bunu okur, ağır yeniden hesaplamayı GÖRE performans nedeniyle atlar); detay
   (`GET /{id}`) her zaman taze hesaplanmış değeri döner, DB'deki değerden asla SAPMAZ (aynı girdilerle
   aynı çıktı, deterministik).
4. **Hiç fiili ağırlık girilmemiş bir emirde `domain.AllocateCutting` ÇAĞRILMAZ.** Tüm reçete
   satırlarının ağırlığı `0 kg` olduğunda (henüz `PUT .../outputs` çağrılmadıysa) ana parçaların
   ağırlık toplamı sıfır olur; `domain.Distribute` bu durumda hata döner (anlamlı bir orantı yok).
   `repo.buildCuttingPreview` bu durumu özel olarak ele alır: dağıtımı hesaplamadan `allocation=nil`,
   `closable=false`, `balance_diff_kg=input_weight_kg` (tam fark) ve `WEIGHT_IMBALANCE` uyarısını
   döner — "taslak, henüz tartılmamış" durumunun doğal ve hatasız bir temsili. (Bu, entegrasyon
   testleriyle yakalanan gerçek bir hataydı: ilk sürüm `POST /cutting-orders`'ın kendisini `422` ile
   başarısız kılıyordu, çünkü yanıt oluşturulurken önizleme hemen hesaplanmaya çalışılıyordu.)
5. **Çıktı lotu numarası `KUZ-<parça>-<yıl>-NNN`, parça kısaltması ürün kodundan türetilir.** Bu görev
   TEK bir hayvan türüyle (kuzu) sınırlıdır; parça ürün kodları `KUZU-` önekini taşır (`KUZU-BUT` →
   `BUT`, lot no `KUZ-BUT-2026-001`). Aynı kısaltma (küçük harfle) ML'in `/predict/cutting` yanıtındaki
   parça anahtarıyla (`ml/app/datasets.py`, `CUTTING_PARTS = ["but","kol",...]`) eşleştirmede de
   kullanılır (`repo.partMLKey`) — Türkçe karakterli görünen ad (`"Sırt"`) yerine ASCII ürün kodundan
   (`KUZU-SIRT` → `sirt`) türetilir, bu yüzden Türkçe küçük harfe çevirme (ı/İ gibi) sorunu hiç
   oluşmaz. Farklı bir hayvan türü eklenirse bu varsayım (tek önek) güncellenmelidir.
6. **Fire, `waste_out` DEĞİL, normal bir çıktı lotu + `cutting_in` hareketi alır (`unit_cost=0`).**
   Ayrıntı ve gerekçe `docs/cutting.md` madde 2'dedir (ağırlık dengesinin defterde de doğrulanabilir
   olması için). Yan ürün de kendi satış değeriyle normal bir lot olarak kaydedilir.
7. **Giriş lotunun tüketildiği depo, `lotSourceWarehouse`'un (dış üretimle AYNI yardımcı fonksiyon)
   KAPANIŞTA bulduğu depodur**, oluşturma anında SEÇİLİP saklanmaz. Çoklu depoya bölünmüş bir lotun
   tek bir depoda yeterli bakiyesi yoksa kapanış `409` ile reddedilir (dış üretimdeki "tek depo"
   kısıtıyla aynı desen, bkz. "Dış üretim" bölümü madde 5). Oluşturma anında yalnızca TOPLAM bakiye
   (tüm depolar) `input_weight_kg`'a yeterli mi diye bakılır; bu, aynı lottan birden fazla DRAFT
   parçalama emri açılabilmesini (henüz hiçbiri kapanmadığı için stok düşmez) kasıtlı olarak mümkün
   kılar — seed demo senaryosunda (`CUT-2026-002`/`CUT-2026-003`, aynı `KZ-2026-002` lotu) bu durum
   kullanılır.
8. **ML "en iyi gayret"tir, tek aşamalıdır (dış üretimin iki aşamalı tahmininin aksine).** Parçalamada
   malzeme maliyeti girdi anında ZATEN bilinir (lotun `unit_cost`'u); ML'in kendisi de maliyet değil,
   yalnızca fiziksel ağırlık/oran tahmin eder (`/predict/cutting`, `ml/app/main.py`). Bu yüzden dış
   üretimdeki "draft → materials_sent" gibi iki aşamalı bir tahmin gerekmez: tek bir tahmin (siparişin
   OLUŞTURULDUĞU an, `carcass_kg=input_weight_kg` ile) yeterlidir. ML erişilemezse/zaman aşımına
   uğrarsa sipariş oluşturma **başarısız olmaz**, `ml_prediction` boş kalır.
