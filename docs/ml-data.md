# Sentetik ML Verisi

> **Sınırlılık notu:** Bu veri sentetiktir. Parametreler seçilmiş varsayımlardır ve gerçek işletme verisinden öğrenilmemiştir. Bu veri üzerindeki model başarısı (MAE, MAPE) gerçek dünya performansını kanıtlamaz; yalnızca boru hattının (üretim, eğitim, değerlendirme, servis) uçtan uca çalıştığını gösterir. Veri, üretici fonksiyonların kendi varsayımlarını geri öğrenir; bu yüzden sonuçlar iyimserdir. Gerçek veriyle yeniden eğitilmeden üretimde kullanılmamalıdır.

Çalıştırma (hepsi `ml/` klasöründen):

```bash
python -m data.generate_production
python -m data.generate_cutting
```

Çıktılar `ml/data/out/` altına yazılır: `production_orders.csv`, `lamb_cutting.csv`. Her üreteç sabit seed kullanır; aynı seed aynı CSV baytlarını üretir (testle doğrulanır, SHA-256 eşitliği). Tüm decimal değerler string/Decimal olarak yazılır (float yok); satır sonu `\n`.

## Üretim emirleri (`generate_production.py`)

60 dış üretim emri, 2025-01 ile 2026-10 arası (22 ay), seed `20250101`. Hedef: `delivered_qty` (teslim edilen adet, kg). EKP her emir için `oracle.production_cost` ile hesaplanır (faturalanan adet = teslim edilen adet, satış fiyatı yok).

| Parametre | Değer | Kaynak |
|---|---|---|
| Et miktarı `et_kg` | tamsayı, Uniform 60 ile 150 | TODO |
| Tedarikçi olasılıkları | TED-A 0,35; TED-B 0,40; TED-C 0,25 | TODO |
| Tedarikçi verim etkisi | TED-A +0,02; TED-B 0,00; TED-C −0,02 | TODO |
| Dış üretici olasılıkları | URT-1 0,60; URT-2 0,40 | TODO |
| Hizmet birim fiyatı | URT-1 5,50 €; URT-2 5,80 € (sabit) | TODO |
| Taban verim | 0,85 kg/kg et | TODO |
| Yaz etkisi (Haziran, Temmuz, Ağustos) | −0,01 | TODO |
| Verim gürültüsü | Normal, σ = 0,012 | TODO |
| Teslim edilen adet | yuvarlanmış tamsayı: `et_kg × gerçek verim` | TODO |
| Baharat miktarı | `et_kg × Uniform(0,017; 0,023)`, 0,1 kg'a yuvarlı | TODO |
| Bağırsak miktarı | `et_kg × Uniform(0,040; 0,060)`, 0,1 kg'a yuvarlı | TODO |
| Et birim fiyatı | 8,60 € × (1 + 0,006 × ay_indeksi) × (1 + N(0; 0,015)) | TODO |
| Baharat birim fiyatı | 23,20 € × (1 + 0,004 × ay_indeksi) × (1 + N(0; 0,010)) | TODO |
| Bağırsak birim fiyatı | 11,20 € × (1 + 0,004 × ay_indeksi) × (1 + N(0; 0,010)) | TODO |

`ay_indeksi`: 2025-01 için 0, 2026-10 için 21 (aylık hafif enflasyon trendi). Gerçek verim = taban + tedarikçi etkisi + yaz etkisi + gürültü.

Sütunlar: `order_id, order_date, year, month, month_index, is_summer, supplier_id, producer_id, et_kg, spice_kg, casing_kg, meat_unit_price, spice_unit_price, casing_unit_price, service_unit_price, delivered_qty, yield_ratio, material_cost, service_cost, total_cost, ekp_unit, ekp_display`.

Bilinen basitleştirmeler: dış üretici verimi etkilemez; faturalanan adet her zaman teslim edilene eşittir (`INVOICED_QTY_MISMATCH` yok); fiyat gürültüsü bağımsızdır.

## Kuzu parçalama (`generate_cutting.py`)

200 kuzu, seed `20260101`. Karkas 14 ile 28 kg (Normal, irk ortalaması etrafında, sınırlara kırpılır), 0,1 kg'a yuvarlı. Parça kg değerleri 3 ondalık; dağıtım yuvarlamasıyla parçalar + fire toplamı karkasa birebir eşittir (oranlar toplamı 1).

Her parça için oran: `taban + irk etkisi + cinsiyet etkisi + ağırlık eğimi × (karkas − 21 kg) + gürültü`; negatifler 0,01'e kırpılır, sonra toplam 1 olacak şekilde normalize edilir.

| Parametre | Değer | Kaynak |
|---|---|---|
| Irk olasılıkları | Morkaraman 0,40; Akkaraman 0,35; Merinos 0,25 | TODO |
| Karkas ortalaması (irk) | Morkaraman 22,0; Akkaraman 20,5; Merinos 19,0 kg; σ = 3,0 | TODO |
| Taban oran: but | 0,310 (literatür aralığı ~%29-33) | TODO |
| Taban oran: kol | 0,170 (~%15-19) | TODO |
| Taban oran: sırt | 0,105 | TODO |
| Taban oran: kaburga | 0,120 | TODO |
| Taban oran: gerdan | 0,085 (~%8-9) | TODO |
| Taban oran: kıyma | 0,100 | TODO |
| Taban oran: fire | 0,110 (~%10-12) | TODO |
| Irk etkisi: Morkaraman | but +0,004; kol +0,002; kıyma −0,002; fire −0,003 | TODO |
| Irk etkisi: Akkaraman | kol −0,001; kaburga +0,003; fire +0,003 | TODO |
| Irk etkisi: Merinos | but +0,006; kol −0,004; kaburga −0,002; gerdan −0,002 | TODO |
| Cinsiyet etkisi: erkek (M) | but +0,002; fire −0,001 | TODO |
| Cinsiyet etkisi: dişi (F) | but −0,002; fire +0,001 | TODO |
| Ağırlık eğimi (oran/kg) | but −0,0012; kol −0,0003; sırt 0; kaburga +0,0010; gerdan +0,0003; kıyma +0,0006; fire +0,0004 | TODO |
| Gürültü σ | but 0,006; kol 0,004; sırt 0,003; kaburga 0,003; gerdan 0,002; kıyma 0,003; fire 0,004 | TODO |

Sütunlar: `lamb_id, breed, sex, carcass_kg, but_kg, kol_kg, sirt_kg, kaburga_kg, gerdan_kg, kiyma_kg, fire_kg`.

"Taban oran" ve etki değerleri Türk literatüründeki aralıkları hedefleyen seçilmiş sayılardır; kaynak sütunu literatür eklenince doldurulacak. Yağlı parçaların ağırlıkla artması ve but oranının hafifçe düşmesi bilinçli olarak eklenmiş sistematik etkilerdir.

## Baseline sonuçları

`python -m train.evaluate` sonuçları `ml/reports/baseline_metrics.json` dosyasına yazar. Üretim için zaman sıralı bölme (en yeni %20 test), kuzu için rastgele %80/20 (seed 42) kullanılır. Bu sonuçlar da yukarıdaki sınırlılık notuna tabidir.
