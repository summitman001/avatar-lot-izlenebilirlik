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
