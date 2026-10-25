# Varsayımlar ve Maliyet Kuralları

Bu dosya `ml/oracle/` içindeki maliyet oracle'ının kurallarını yazar. Go backend ve Android aynı kurallara birebir uymalıdır; doğruluk referansı `docs/golden/*.json` dosyalarıdır (`cd ml && python -m oracle.cli` ile yeniden üretilir).

Genel sayısal kurallar:

- Tüm hesaplar `Decimal`, float yasak (float girdi `TypeError` verir).
- Birim maliyet 4 ondalık, para tutarları 2 ondalık, miktar (kg) 3 ondalık, yüzde 2 ondalık, oran (kg/kg) 4 ondalık. Hepsi ROUND_HALF_UP.
- JSON'da tüm decimal değerler string. Golden dosyalarında sayısal JSON değeri yoktur (yalnızca string, bool, null).
- Golden dosya biçimi: `[{"case_id","description","input":{...},"expected":{...}}]`. Reçete dosyası (`cutting_case.json`) tek nesnedir.

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
4. **Senkron push bugün stub.** `POST /sync/push` yalnızca `op_id`'yi kaydeder; payload'ı **işlemez**. İlk gönderim bu yüzden `applied` değil **`flagged`** döner (istemci "uygulandı" sanıp yerel kaydı silmesin). Aynı `op_id` tekrar gelirse `duplicate`. Payload işleme yazılınca stub kayıtları temizlenmelidir (bkz. `docs/HANDOFF.md`).
5. **Stok defteri.** `stock_movement` değiştirilemez (UPDATE/DELETE trigger ile reddedilir); düzeltme yalnızca `reversal` kaydıyladır. Giriş tipleri (+), çıkış tipleri (−) işaret kuralı veritabanında CHECK ile zorlanır. Geri alma stoğu negatife düşürüyorsa reddedilir.
6. **Kimlik doğrulama yok.** API şimdilik açıktır (CORS `*`); yarışma demosu içindir, üretimde eklenmelidir.

## Dış üretim (Görev 1) servisine ilişkin varsayımlar ve tespitler

Durum makinesi: `draft → materials_sent → received → closed`. Uygulama: `backend/internal/repo/production.go`, uç noktalar `backend/internal/http/handlers.go`, istek/durum kuralları `backend/internal/domain/production.go`. Doğruluk referansı yine `docs/golden/production_cases.json`; `TestProductionGoldenScenarios` (`backend/internal/integration/production_test.go`) golden senaryoları (`UR-001`, `UR-002`, `UR-003`, `UR-006`) **gerçek HTTP uç noktalarıyla** (`POST /production-orders` → `send-materials` → `receive` → `GET .../cost-report`) koşturup sonucu golden dosyasından okunan beklenen değerlerle karşılaştırır.

1. **İade edilmeyen hammadde (proses firesi dahil) mamul maliyetine yüklenir.** `receive` adımında her girdi satırı için `tüketilen = sevk edilen − iade edilen`; tüketilen miktar `production_consume` hareketiyle dış depodan düşülür ve `CalculateProductionCost`'a girdi olur. İade edilmeyen miktarın neden tükendiği (gerçek üretim kaybı mı, sayım farkı mı) ayrıca izlenmez — sistem bunu basitçe "mamulün maliyetine dahil" sayar. Gerçek bir fire/kayıp ayrımı istenirse `production_input`'a ayrı bir `qty_wasted` alanı eklenebilir (TODO).
2. **Hizmet bedeli faturalanan adet üzerinden alınır, EKP ise teslim alınan adede bölünür.** `UR-003` golden senaryosu bunu kanıtlar: 83 teslim alınmış ama üretici 85 adet faturalamışsa (`service_invoiced_qty=85`), `service_cost = round2(85 × hizmet_fiyatı)` ama `ekp_unit = total_cost / 83`. Uyarı: `INVOICED_QTY_MISMATCH`.
3. **%2 miktar farkı eşiği (`QTY_VARIANCE`).** `|teslim_alinan − planlanan| / planlanan × 100 > 2` ise uyarı üretilir (`domain.QtyVarianceThresholdPct`). Golden senaryoların hiçbiri bu uyarıyı içermediği için entegrasyon testinde `planned_qty = delivered_qty` verilerek bu uyarı devre dışı bırakılır (karşılaştırmayı bozmasın diye); gerçek kullanımda `planned_qty` sipariş açılışında girilen bağımsız bir değerdir.
4. **Hammadde kategorisi artık `item.material_category` alanıdır** (migration `000002`, ENUM `meat|spice|casing|other`, yalnızca `item_type=raw` için anlamlı). `CalculateProductionCost` her satır için bu kategoriyi zorunlu kılar; kategorisi olmayan bir hammadde sevk edilip teslim alınmaya çalışılırsa `422` döner. Seed'de `ET-DANA=meat`, `BAHARAT-SUCUK=spice`, `BAGIRSAK=casing` olarak ayarlıdır.
5. **Tek depo varsayımı.** Bir siparişin tüm girdileri **aynı** ana depodan sevk edilmiş olmalı (dış üretici deposuna `send_to_producer`/`transfer_in` çifti bu depodan gider); çıktı mamulü de aynı depoya girer (`production_in`). Girdi lotları farklı depolardan sevk edilmişse `receive` `422` döner. Bir lot birden fazla depoya bölünmüşse (`v_lot_balance` birden fazla satır) ve sevk miktarını karşılayan TEK bir depo yoksa `send-materials` `409` döner (çoklu depodan birleştirilmiş sevkiyat desteklenmez).
6. **`production_input (production_order_id, lot_id)` UNIQUE'dir** (migration `000003`): bir lot aynı siparişe en fazla bir sevk satırıyla gönderilebilir. Bu, `receive` sırasında "bu lot hangi depodan sevk edildi" sorusunun defterden (bkz. 7) tek ve belirsizliksiz cevaplanmasını sağlar. `draft`/`materials_sent` durumunda birden fazla `send-materials` çağrısına (kısmi/çok partili sevkiyat) izin verilir; aynı lot ikinci kez gönderilmeye çalışılırsa `409`.
7. **Sipariş numarası üretimi (`URT-YYYY-NNN`) ve çıktı lot numarası (`SUC-YYYY-NNN`) atomik DEĞİLDİR.** Mevcut en büyük `NNN` okunup +1 yapılır; es zamanlı iki istek teorik olarak aynı numarayı üretip ikincisi `409` (UNIQUE ihlali) alabilir — yeniden otomatik denenmez. Demo kapsamında (tek yazarlı kullanım) kabul edilebilir bir sınırlamadır; üretimde bir sequence tablosu veya `INSERT ... ON DUPLICATE` döngüsü eklenebilir.
8. **"Toplam stok değeri değişmiyor" DEĞİL, hizmet bedeli kadar ARTAR.** Hammadde değeri (`material_cost`) tüketilen miktarla birebir mamule aktarılır, ama mamulün birim maliyeti (`ekp_unit`) hizmet bedelini de içerir. Yani sevk+teslim sonrası toplam envanter değeri, önceki hammadde değerinden tam olarak `service_cost` kadar YÜKSEKTİR (gerçek bir nakit maliyet eklenmiştir, stoktan gelmez). `TestProductionOrderFullLifecycle` bu özdeşliği (`material_cost` tüketilen hammaddenin tam değeri, `total_cost = material_cost + service_cost`) doğrular; "değişmiyor" ifadesi yalnızca hammadde değerinin kaybolmadan aktarıldığı anlamında doğrudur.
9. **Hizmet faturası `close`'da da verilebilir.** `receive`'de `service_invoice_no` opsiyoneldir (fatura genelde mamulden sonra gelir); verilmemişse `POST .../close` gövdesinde `service_invoice_no` ile tamamlanabilir. İkisi de boşsa kapatma `422` ile reddedilir.
10. **Durum geçmişi ayrı bir tablo yerine mevcut zaman damgalarından türetilir** (`GET /production-orders/{id}` → `status_history`): `draft` = siparişin `created_at`'i, `materials_sent` = bu siparise ait ilk `send_to_producer` hareketinin zamanı, `received` = çıktı lotunun `created_at`'i, `closed` = `closed_at`. Ayrı bir durum-geçişi log tablosu eklenmedi (şema sadeliği); gerçek bir denetim izi gerekiyorsa eklenebilir (TODO).
11. **ML entegrasyonu bir maliyet modeli DEĞİLDİR, yalnızca fiziksel verim tahmini yapar.** `ml/` servisinin `/predict/production` uç noktası `predicted_yield_ratio`/`predicted_output_kg`/`predicted_delivered_qty` döner; **EKP tahmini yoktur** (maliyet sunucu tarafında gerçek hammadde/hizmet fiyatlarıyla hesaplanır, ML'in bilgisi dışındadır). Bu yüzden API'de `prediction.predicted_ekp_unit` gibi bir alan kasıtlı olarak **yoktur** — böyle bir alan eklemek olmayan bir "tahmin" uydurmak anlamına gelirdi.
12. **Siparış oluşturulurken ML'e gönderilen `et_kg` tahminidir, gerçek değer değildir.** Oluşturma anında hangi hammaddelerin sevk edileceği henüz bilinmediği için (`send-materials` sonraki adımdır), `et_kg_estimate = planned_qty × unit_weight_kg / 0,85` (varsayılan verim, golden `UR-001..004` senaryolarındaki `yield_meat_ratio=0,8500` ile aynı) kabaca hesaplanıp ML'e gönderilir. Gerçek sonuç (`received_qty`) `receive` adımında bu ilk tahminle karşılaştırılır (`delivered_qty_error_abs/pct`) — tahminin kendisi YENİDEN hesaplanmaz, yalnızca gerçekleşenle kıyaslanır.
13. **ML "en iyi gayret"tir.** `ML_URL` erişilemezse (servis kapalı, zaman aşımı) sipariş oluşturma **başarısız olmaz**; uyarı loglanır ve `prediction` alanı `null` kalır (`backend/internal/repo/production.go`, `predictOnCreate`). `TestProductionMLUnreachableStillCreatesOrder` bunu gerçek bir ağ hatasıyla (sahte değil, dinlemeyen bir porta bağlanarak) doğrular.
