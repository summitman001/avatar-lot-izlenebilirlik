# Sentetik ML Verisi

> **Sınırlılık notu:** Bu veri sentetiktir. Parametreler seçilmiş varsayımlardır ve gerçek işletme verisinden öğrenilmemiştir. **Model bu etkileri üreteçten öğrenir; gerçek dünya performansını kanıtlamaz.** Bu veri üzerindeki başarı (MAE, MAPE) yalnızca boru hattının (üretim, eğitim, değerlendirme, servis) uçtan uca çalıştığını ve modelin bilinen etkileri yakalayabildiğini gösterir. Gerçek veriyle yeniden eğitilmeden üretimde kullanılmamalıdır.

Çalıştırma (hepsi `ml/` klasöründen):

```bash
python -m data.generate_production
python -m data.generate_cutting
```

Çıktılar `ml/data/out/` altına yazılır ve repoya commitlenir (örnek kayıtlar): `production_orders.csv`, `cutting_records.csv`. Her üreteç sabit seed kullanır; aynı seed aynı CSV baytlarını üretir. Testler hem iki ardışık üretimin hem de commitlenmiş dosyaların üreteç çıktısıyla birebir aynı olduğunu doğrular. Tüm decimal değerler Decimal olarak üretilip string yazılır (float yok); satır sonu `\n`.

## Üretim emirleri (`generate_production.py`)

120 dış üretim emri, 2025-01 ile 2026-10 arası (22 ay), seed `20250101`. Ürün: Narin Sucuk (`unit_weight_kg = 1.000`). Hedef: `delivered_qty` (teslim edilen adet). EKP her emir için `oracle.production_cost` ile hesaplanır (satır bazlı yuvarlama kuralı, faturalanan adet = teslim edilen adet, satış fiyatı yok).

**Neden 120 (60 değil):** Haftada bir üretim yapan (yılda ~50 emir) bir işletme için 2+ yıllık bir dönemde 100-120 emir gerçekçidir; 60 emir (yaklaşık ayda 3) gerçek işletme ritmine göre azdı ve rolling-origin CV protokolünün (8 katman, her katman 10 test kaydı, ilk katman 40 eğitim kaydıyla başlıyor) anlamlı sayıda katman üretmesi için de yetersiz kalıyordu. Sabit seed (`20250101`) ve mevcut etkiler/gürültü (σ=0,012) DEĞİŞTİRİLMEDİ; yalnızca emir sayısı arttı. Kuzu parçalama verisi (200 kayıt) zaten 5-fold CV için yeterli olduğundan değiştirilmedi.

Gerçek verim (kg çıktı / kg et) şu şekilde kurulur, sonra `teslim adedi = round(et_kg × verim / unit_weight_kg)`:

`verim = 0,85 + tedarikçi etkisi + tedarikçi × üretici etkileşimi + yaz etkisi + et lotu yaşı etkisi + gürültü`

### Verim etkileri

| Etki | Değer | Baseline yakalar mı? | Kaynak |
|---|---|---|---|
| Taban verim | 0,85 kg/kg et | evet (ortalama) | TODO |
| Tedarikçi: TED-A / TED-B / TED-C | +0,02 / 0,00 / −0,02 | evet (tedarikçi ortalaması) | TODO |
| Mevsim: Haziran, Temmuz, Ağustos | −0,01 | hayır | TODO |
| Tedarikçi × dış üretici etkileşimi | TED-A × URT-2: −0,015; TED-B × URT-2: +0,008; TED-C × URT-1: +0,010 | hayır | TODO |
| Et lotunun alıştan üretime geçen gün sayısı (1 ile 10 gün, tamsayı) | her 3 gün −0,003 (doğrusal: −0,001 / gün) | hayır | TODO |
| Gürültü | Normal, σ = 0,012 | hayır (indirgenemez hata) | TODO |

### Diğer üretim parametreleri

| Parametre | Değer | Kaynak |
|---|---|---|
| Et miktarı `et_kg` | tamsayı, Uniform 60 ile 150 | TODO |
| Tedarikçi olasılıkları | TED-A 0,35; TED-B 0,40; TED-C 0,25 | TODO |
| Dış üretici olasılıkları | URT-1 0,60; URT-2 0,40 | TODO |
| Hizmet birim fiyatı | URT-1 5,50 €; URT-2 5,80 € (sabit) | TODO |
| Baharat miktarı | `et_kg × Uniform(0,017; 0,023)`, 0,1 kg'a yuvarlı | TODO |
| Bağırsak miktarı | `et_kg × Uniform(0,040; 0,060)`, 0,1 kg'a yuvarlı | TODO |
| Et birim fiyatı | 8,60 € × (1 + 0,006 × ay_indeksi) × (1 + N(0; 0,015)) | TODO |
| Baharat birim fiyatı | 23,20 € × (1 + 0,004 × ay_indeksi) × (1 + N(0; 0,010)) | TODO |
| Bağırsak birim fiyatı | 11,20 € × (1 + 0,004 × ay_indeksi) × (1 + N(0; 0,010)) | TODO |

`ay_indeksi`: 2025-01 için 0, 2026-10 için 21 (aylık hafif enflasyon trendi).

Sütunlar: `order_id, order_date, meat_purchase_date, lot_age_days, product, unit_weight_kg, year, month, month_index, is_summer, supplier_id, producer_id, et_kg, spice_kg, casing_kg, meat_unit_price, spice_unit_price, casing_unit_price, service_unit_price, delivered_qty, yield_ratio, material_cost, service_cost, total_cost, ekp_unit, ekp_display`.

Bilinen basitleştirmeler: faturalanan adet her zaman teslim edilene eşittir (`INVOICED_QTY_MISMATCH` yok); fiyat gürültüsü bağımsızdır; et lotu yaşı etkisi doğrusal kabul edilmiştir.

## Kuzu parçalama (`generate_cutting.py`)

200 kuzu, seed `20260101`. Karkas 14 ile 28 kg (Normal, irk ortalaması etrafında, sınırlara kırpılır), 0,1 kg'a yuvarlı. Parça kg değerleri 3 ondalık; dağıtım yuvarlamasıyla parçalar + fire toplamı karkasa birebir eşittir (oranlar toplamı 1).

Her parça için oran: `taban + irk etkisi + cinsiyet etkisi + ağırlık eğimi × irk çarpanı × (karkas − 21 kg) + gürültü`; negatifler 0,01'e kırpılır, sonra toplam 1 olacak şekilde normalize edilir.

### Cutting etkileri

| Etki | Değer | Baseline yakalar mı? | Kaynak |
|---|---|---|---|
| Irk: Morkaraman | but +0,004; kol +0,002; kıyma −0,002; fire −0,003 | evet (ırk ortalaması) | TODO |
| Irk: Akkaraman | kol −0,001; kaburga +0,003; fire +0,003 | evet | TODO |
| Irk: Merinos | but +0,006; kol −0,004; kaburga −0,002; gerdan −0,002 | evet | TODO |
| Cinsiyet: erkek (M) | but +0,003; kol +0,001; kaburga −0,002; kıyma −0,001; fire −0,001 | hayır | TODO |
| Cinsiyet: dişi (F) | but −0,003; kol −0,001; kaburga +0,002; kıyma +0,001; fire +0,001 | hayır | TODO |
| Karkas ağırlığı eğimi (oran / kg, 21 kg referans) | but −0,0012; kol −0,0006; sırt 0; kaburga +0,0010; gerdan +0,0003; kıyma +0,0008; fire +0,0004 | hayır | TODO |
| Irk × ağırlık etkileşimi (eğim çarpanı) | Morkaraman ×0,7; Akkaraman ×1,0; Merinos ×1,5 | hayır | TODO |
| Gürültü σ | but 0,006; kol 0,004; sırt 0,003; kaburga 0,003; gerdan 0,002; kıyma 0,003; fire 0,004 | hayır (indirgenemez hata) | TODO |

Yani ağırlık arttıkça but ve kol oranı hafif düşer, kaburga ve kıyma hafif artar; bu düşüş/artış Merinos'ta daha hızlı, Morkaraman'da daha yavaştır.

### Diğer kuzu parametreleri

| Parametre | Değer | Kaynak |
|---|---|---|
| Irk olasılıkları | Morkaraman 0,40; Akkaraman 0,35; Merinos 0,25 | TODO |
| Karkas ortalaması (ırk) | Morkaraman 22,0; Akkaraman 20,5; Merinos 19,0 kg; σ = 3,0 | TODO |
| Taban oran: but | 0,310 (literatür aralığı ~%29-33) | TODO |
| Taban oran: kol | 0,170 (~%15-19) | TODO |
| Taban oran: sırt | 0,105 | TODO |
| Taban oran: kaburga | 0,120 | TODO |
| Taban oran: gerdan | 0,085 (~%8-9) | TODO |
| Taban oran: kıyma | 0,100 | TODO |
| Taban oran: fire | 0,110 (~%10-12) | TODO |

Sütunlar: `lamb_id, breed, sex, carcass_kg, but_kg, kol_kg, sirt_kg, kaburga_kg, gerdan_kg, kiyma_kg, fire_kg`.

"Taban oran" ve etki değerleri Türk literatüründeki aralıkları hedefleyen seçilmiş sayılardır; kaynak sütunu literatür eklenince doldurulacak.

## Baseline sonuçları ve öğrenme eğrisi

`python -m train.evaluate` iki rapor yazar:

- `ml/reports/baseline_metrics.json`: baseline MAE ve MAPE. Üretim için zaman sıralı bölme (en yeni %20 test), kuzu için rastgele %80/20 (seed 42).
- `ml/reports/learning_curve.json`: baseline MAE/MAPE'sinin eğitim kaydı sayısıyla değişimi (üretim: 5, 10, 20, 40; kuzu: 10, 25, 50, 100, 160). Üretimde eğitim kayıtları eğitim bölmesinin en yeni n kaydıdır; kuzuda sabit seed ile karıştırılmış iç içe altkümelerdir. Yeni modeller `app.registry.register` ile eklenince aynı fonksiyondan geçer (`--all`).

Bu sonuçlar da yukarıdaki sınırlılık notuna tabidir: baseline'ın yakalayamadığı etkiler (mevsim, etkileşim, lot yaşı, karkas ağırlığı) üreteçte bilinçli olarak vardır, böylece yarın eklenecek Ridge ve gradient boosting modellerinin baseline'a karşı kazancı ölçülebilir.
