# Varsayımlar ve Maliyet Kuralları

Bu dosya `ml/oracle/` içindeki maliyet oracle'ının kurallarını yazar. Go backend ve Android aynı kurallara birebir uymalıdır; doğruluk referansı `docs/golden/*.json` dosyalarıdır (`cd ml && python -m oracle.cli` ile yeniden üretilir).

Genel sayısal kurallar:

- Tüm hesaplar `Decimal`, float yasak (float girdi `TypeError` verir).
- Birim maliyet 4 ondalık, para toplamları 2 ondalık, miktar (kg) 3 ondalık, yüzde 2 ondalık, oran (kg/kg) 4 ondalık. Hepsi ROUND_HALF_UP.
- Ara değerler yuvarlanmaz; yuvarlama yalnızca çıktı üretilirken yapılır.
- JSON'da tüm decimal değerler string. Golden dosyalarında sayısal JSON değeri yoktur (yalnızca string, bool, null).
- Golden dosya biçimi: `[{"case_id","description","input":{...},"expected":{...}}]`. `recete` dosyası (`cutting_case.json`) tek nesnedir.

## Görev 1 maliyet kuralları

Girdi: sevk edilen lotlar (ad, kategori `meat|spice|casing|other`, miktar kg, birim maliyet), teslim alınan adet, hizmet birim fiyatı, faturalanan adet (opsiyonel), satış fiyatı (opsiyonel).

| Çıktı | Formül | Yuvarlama |
|---|---|---|
| `material_cost` | toplam(miktar × birim maliyet) | 2 ondalık |
| `service_cost` | faturalanan adet × hizmet birim fiyatı | 2 ondalık |
| `total_cost` | tam hassasiyetli (malzeme + hizmet) | 2 ondalık |
| `ekp_unit` | tam hassasiyetli total / teslim alınan adet | 4 ondalık |
| `ekp_display` | tam hassasiyetli total / teslim alınan adet | 2 ondalık |
| `revenue` | satış fiyatı × teslim alınan adet | 2 ondalık |
| `gross_profit_unit` | satış fiyatı − tam hassasiyetli EKP | 4 ondalık |
| `gross_profit_total` | revenue(tam) − total(tam) | 2 ondalık |
| `margin_pct` | gross_profit_total(tam) / revenue(tam) × 100 | 2 ondalık |
| `yield_input_pct` | teslim alınan adet / toplam girdi kg × 100 | 2 ondalık |
| `yield_meat_ratio` | teslim alınan adet / et kg (kg/kg) | 4 ondalık |

Kurallar:

- Faturalanan adet verilmezse teslim alınan adede eşittir. Farklıysa uyarı `INVOICED_QTY_MISMATCH` döner; hizmet maliyeti faturalanan adet üzerinden hesaplanır, EKP ise teslim alınan adede bölünür.
- Satış (ciro, kâr, marj) teslim alınan adet üzerinden hesaplanır. Satış fiyatı yoksa kâr alanları `null`.
- `total_cost`, yuvarlanmış `material_cost` ile `service_cost` toplamından değil, tam hassasiyetli toplamdan yuvarlanır. Yani `material_cost + service_cost` ile `total_cost` farkı en fazla 0,01 olabilir. Golden senaryolarda fark yoktur.
- `ekp_display` tam hassasiyetli değerden yuvarlanır, 4 ondalıklı `ekp_unit`'ten ikinci kez yuvarlanmaz. Çift yuvarlama farkı çıkabilecek durumlarda Go tarafı da tam değerden yuvarlamalıdır.
- Verim hesabında tüm lot miktarları kg kabul edilir; teslim alınan adet de kg karşılığıdır (kg/kg verim).
- Değerleme yalnızca `ekp_unit` (4 ondalık) veya `total_cost` üzerinden yapılır. `ekp_display × adet` kullanmak yanlıştır (aşağıdaki yuvarlama notuna bakın).
- Geçersiz girdi (teslim alınan adet ≤ 0, negatif miktar veya fiyat, bilinmeyen kategori) `ValueError` verir.

## Görev 2 dağıtım kuralları

Girdi: giriş ağırlığı (kg), birim maliyet (€/kg, opsiyonel), alış belgesinde beyan edilen toplam (opsiyonel), maliyet esası (`computed` veya `declared`), yöntem (`quantity` veya `value`), parçalar (ad, fiili kg, referans satış fiyatı €/kg, `is_waste`, `is_by_product`).

- Maliyet esası: `computed` ise toplam = giriş kg × birim maliyet (2 ondalık); `declared` ise toplam = belgede beyan edilen tutar. İki tutar da verilmişse ve farklıysa uyarı `PURCHASE_TOTAL_MISMATCH` döner (esas hangisi seçilmiş olursa olsun). Kullanıcı seçimine göre iki tutar da desteklenir.
- Ağırlık dengesi: `balance_diff_kg = giriş kg − toplam(tüm parçalar, fire ve yan ürün dahil)`. `|fark| > 0,05 kg` ise uyarı `WEIGHT_IMBALANCE` ve `closable=false`. Tam 0,05 kg fark kabul edilir (`closable=true`). Dengesiz kayıtta dağıtım yine de hesaplanır (önizleme) ama kapatmaya izin verilmez.
- Fire (`is_waste`): maliyet taşımaz (`0.00`); maliyeti satılabilir parçalara biner.
- Yan ürün (`is_by_product`): kendi satış değeri kadar maliyet taşır (kg × referans fiyat, 2 ondalık). Bu tutar toplam maliyetten düşülür, kalan havuz ana parçalara dağıtılır (net gerçekleşebilir değer yaklaşımı). Yan ürün değeri toplam maliyeti aşarsa `ValueError`.
- `quantity` yöntemi: havuz, ana parçalara kg oranında dağıtılır. `value` yöntemi: kg × referans fiyat oranında.
- Dağıtım yuvarlaması (`oracle/rounding.py`): her satır tam hassasiyetle hesaplanır ve 2 ondalığa yuvarlanır; kalan kuruş farkı en büyük tutarlı satıra eklenir (eşitlikte girdi sırasındaki ilk satır). Toplam dağıtılan tutar toplam maliyete kuruşu kuruşuna eşittir (hypothesis ile rastgele parça setlerinde test edilir).
- Parça birim maliyeti: son dağıtılan tutar / parça kg (4 ondalık); fire için `0.0000`.
- Çıktıdaki parça sırası girdi sırasıdır. Uyarı sırası sabittir: `PURCHASE_TOTAL_MISMATCH`, sonra `WEIGHT_IMBALANCE`.

## Dokümandaki tutarsızlıklar

1. **287,50 € ile 351,00 €:** Kuzu parçalama senaryosunda sistemin hesabı 25 kg × 11,50 €/kg = 287,50 €, alış belgesinde beyan edilen ise 351,00 €. İkisi de desteklenir: `cost_basis` ile seçilir ve uyarı `PURCHASE_TOTAL_MISMATCH` döner. Golden: `KZ-2026-001` ve `KZ-2026-002` için 287,50 €; `KZ-2026-003` için 351,00 €. Hangisinin esas alınacağı iş kuralı kararıdır (TODO: yarışma sahibine sorulacak).
2. **22 kg ile 25 kg:** Parçaların toplamı 22 kg, giriş 25 kg; 3,0 kg fark fire kaydı yapılmadığı için açıkta kalır. Bu durum `WEIGHT_IMBALANCE` ve `closable=false` üretir (`KZ-2026-001`). 3,0 kg fire kaydedildiğinde denge tamamdır (`KZ-2026-002`).
3. **Yuvarlama: 18,16 × 85 ≠ 1.543,50:** `18,16 × 85 = 1.543,60`, oysa gerçek toplam 1.543,50 (fark 0,10 €). Sebep: 18,16, 18,158823... değerinin 2 ondalığa yuvarlanmış gösterimidir. Değerleme her zaman 4 ondalıklı `ekp_unit` (18,1588 × 85 = 1.543,498, 2 ondalık 1.543,50) veya doğrudan `total_cost` üzerinden yapılmalıdır; `ekp_display` yalnızca ekranda gösterilir. Golden: `UR-004` (`rounding_proof`).
4. **Birim belirsizliği:** Dokümanda 85 "adet" teslim, aynı satırda verim "85 kg" olarak geçiyor. Oracle teslim adedini kg karşılığı sayar (varsayım, doğrulanmadı).

## VKP varsayımı (24,90 €)

Doküman senaryosunda satış fiyatı (VKP) verilmediği için kâr ve marj örneği 24,90 € varsayımıyla hesaplanır. Bu değer yarışma dokümanından gelmez, tüm `UR-001`, `UR-002`, `UR-003`, `UR-004` golden senaryolarında kullanılır. `UR-005` satış fiyatsız örnektir (kâr alanları `null`).

| Senaryo | ekp_unit | Birim kâr | Toplam kâr | Marj |
|---|---|---|---|---|
| UR-001 (85 teslim) | 18,1588 | 6,7412 | 573,00 | %27,07 |
| UR-002 (83 teslim, 83 fatura) | 18,4639 | 6,4361 | 534,20 | %25,85 |
| UR-003 (83 teslim, 85 fatura) | 18,5964 | 6,3036 | 523,20 | %25,32 |

VKP gerçek değerle değişirse yalnızca `ml/oracle/cli.py` içindeki `VKP` sabiti güncellenir ve golden dosyaları yeniden üretilir.
