# Fatura/irsaliye okuma (OCR) — isteğe bağlı özellik

Bir tedarikçi fatura/irsaliyesinin fotoğrafını veya PDF'ini yükleyip ürün, miktar, lot ve maliyet
bilgilerini **öneri** olarak çıkaran, kullanıcı onayı olmadan **hiçbir kayıt oluşturmayan** bir akış.
Vurgu OCR doğruluk oranında değil, **onay akışında**: sistem kendi başına asla bir lot/alış fişi
yazmaz.

## Kesin kural: öneri → düzeltme → onay

```
yükle → ML (Tesseract) öneri çıkarır → kullanıcı ekranda düzeltir → ONAYLA → alış fişi/lot/stok hareketi
                                      → (ya da) VAZGEÇ → hiçbir şey oluşmaz
```

- OCR'ın çıkardığı hiçbir alan "doğru" diye sunulmaz: her alanın bir `confidence` (0–1) ve
  `source_text`'i (okunan ham metin) vardır, ekranda **"öneri"** etiketiyle gösterilir.
- `POST /invoice-imports/{id}/confirm` çağrısının gövdesi OCR çıktısı **DEĞİL**, kullanıcının
  düzelttiği veridir (`CreatePurchaseReceiptRequest` ile birebir aynı şekil). Bu gövde, mevcut
  `POST /purchase-receipts` yazma yolunu (`createPurchaseReceiptInTx` → `createPurchaseLotInTx`)
  **AYNEN** çağırır — **ikinci bir alış fişi oluşturma yolu yoktur**.
- Onay (`confirm`) ve iptal (`discard`) **tek transaction**'da, durum kontrolüyle (`FOR UPDATE`)
  birlikte yapılır: aynı kayıt iki kez onaylanamaz (`409`), iptal edilmiş bir kayıt onaylanamaz
  (`409`). Onaydan önce hiçbir lot/stok hareketi/alış fişi yoktur — bu, entegrasyon testlerinde
  açıkça doğrulanır (`backend/internal/integration/invoice_import_test.go`).
- ML servisi kapalıysa veya dosya işlenemiyorsa: **upload 503 döner, hiçbir kayıt oluşmaz** —
  kullanıcı mevcut "Yeni Alış" formuyla manuel giriş yapmaya devam eder, akış kırılmaz. Bu,
  projedeki diğer ML entegrasyonlarından (üretim/parçalama tahmini, anomali tespiti — hepsi "en iyi
  gayret") **bilinçli bir fark**: onlarda ML'siz de asıl işlem (sipariş/satış/kapanış) başarıyla
  tamamlanır; burada ML olmadan zaten üretilecek bir "öneri" yoktur, o yüzden istek baştan reddedilir.

## ML servisi tarafı (`ml/app/ocr.py`, `POST /extract/invoice`)

**Motor: Tesseract (pytesseract), imajda kurulu.** Harici API, API anahtarı veya internet bağlantısı
**gerektirmez** — jüri kendi makinesinde ek kurulum yapmadan `docker compose up --build` ile çalıştırır.
Model **eğitimi yapılmadı**: çıkarım tamamen kural tabanlı (kelime konumları + etiket/değer kuralları +
`miktar × fiyat = tutar` aritmetiği), üretim/parçalama ML modellerinden farklı olarak burada "öğrenen"
bir model yok — fatura düzeni belge belge çok farklı olduğundan, bu ölçekte küçük bir eğitim verisiyle
genel geçer bir model kurmak gerçekçi değil; kural tabanlı yaklaşım hem şeffaf (her alanın neden o
değeri aldığı `source_text`'ten görülür) hem de jüri/kullanıcı için öngörülebilir.

### Ön işleme (`preprocess_image`)

Deterministik, her adım en iyi gayret (başarısız olursa atlanır):

1. **EXIF yönü** — telefon fotoğrafları yönü EXIF etiketinde taşır; saydam PNG arka planı beyaza basılır.
2. **90/180/270° döndürme** — Tesseract OSD.
3. **Ölçek** — uzun kenar 2500–3600 px aralığına getirilir (Tesseract ~300 DPI'da en iyi okur; çok büyük
   fotoğraflar süre için küçültülür).
4. **Gölge/ışık giderme** — görüntü, yazıdan arındırılmış bulanık kopyasına (kâğıt arka planı) bölünür;
   temiz taramada değişiklik yapmaz.
5. **Eğiklik düzeltme** — ±10°: yazı satırlarının yatay izdüşümünün en keskin olduğu açı aranır.
6. **Tablo/kutu çizgilerini silme** — harf boyunu çok aşan düz yatay/dikey çizgiler beyazlatılır;
   rakamlara yapışık çizgiler Tesseract'ın `|1` gibi okumasına ve hücreleri birleştirmesine yol açıyordu.

### Okuma: birden fazla sayfa düzeni modu

Tesseract aynı görüntüyü farklı "sayfa düzeni" modlarında farklı yerlerde yanlış okur (ölçüldü:
otomatik mod tablo hücresindeki yalnız sayıları — `Miktar`, `20` — atlıyor; tek blok modu bir unvan
satırını `et` okuyabiliyor). Bu yüzden **tek blok (6) ve sütun (4) modları her zaman**, sonuç hâlâ eksikse
otomatik mod (3) de çalıştırılır. Kalem tablosu aritmetiği en iyi tutan moddan, tedarikçi/belge no/tarih
ise her biri o alanı en güvenle (ve en eksiksiz) okuyan moddan alınır. Tipik süre 3–5 sn.

### Çıkarım

**Görsel satırlar.** Tesseract tablo sütunlarını ve etiket/değer kutularını ayrı bloklara bölebilir
(`Fatura No:` bir blokta, numarası başka blokta). Bu yüzden kelime kutucukları (`image_to_data`) aynı
yükseklikteki kelimeler tek satırda olacak şekilde yeniden birleştirilir; geniş boşluklar hücreleri ayırır.

**Etiket → değer.** Her etiket için değer etiketin **sağında, solunda, altında ve üstünde** aranır ve
değer tipine göre doğrulanır; en yüksek puanlı aday seçilir (etiketten sonra `:` varsa sağ güçlüdür,
yoksa sol da güçlü adaydır: `ABC2026000000145   Fatura No` düzeni).

- **Belge no**: `Fatura No` / `No.` / `Numarası` / `Nr` / `Fat. No`, `Belge No`, `İrsaliye No`,
  `Seri-Sıra No`, `Invoice No`, `Evrak/Fiş No`. Değer rakam içermeli; tarih, tutar, ETTN (UUID) ya da başka
  bir etiket olamaz. Matbu fatura `Seri: A  Sıra No: 123456` → `A-123456`. Etiket bulunamazsa belgede
  **GİB e-Fatura numarası** biçimi (`ABC2026000000123`: 3 karakter + yıl + 9 hane) aranır; sıra kısmındaki
  OCR harf/rakam karışıklıkları (`O→0`, `I→1`, `S→5`...) düzeltilir. `Özelleştirme No`, `Sipariş No`,
  `Mersis No` gibi alanlar belge no sayılmaz.
- **Tarih**: `Fatura/Belge/Düzenleme Tarihi`, `Tarih`, `Invoice Date`; **vade, son ödeme, SKT, üretim
  tarihi belge tarihi sayılmaz**. Etiket yoksa tablo dışındaki ilk tarih (vade/SKT yanında olmayan).
- **Tedarikçi**: önce açık etiket (`Satıcı`, `Tedarikçi`, `Gönderen`, `Ünvanı` — `Alıcı Ünvanı` hariç);
  yoksa tablo başlığından önceki bölümde firma ekleri (`A.Ş.`, `Ltd. Şti.`, `Gıda`, `San. Tic.`...) taşıyan
  hücre. Adres, vergi dairesi/VKN, telefon, e-posta, `e-FATURA` başlığı ve **alıcı bloğu (`SAYIN ...`)
  elenir**. İki satıra bölünmüş unvanlar (`... GIDA` / `SANAYİ VE TİCARET LTD. ŞTİ.`) birleştirilir.
- **Para birimi**: `€`/`EUR`, `TL`/`TRY`/`₺`, `$`/`USD` sayımı, varsayılan `EUR` — **sistem tüm tutarları
  € olarak tutar**, bu alan yalnızca bilgilendirme amaçlıdır, `confirm` gövdesine dahil edilmez.

**Kalem tablosu.**

- **Başlık → sütunlar**: `Mal Hizmet`/`Ürün`/`Açıklama`, `Miktar`, `Birim`, `Birim Fiyat`, `İskonto Oranı/
  Tutarı`, `KDV Oranı/Tutarı`, `Mal Hizmet Tutarı`/`Tutar`, `Lot`/`Parti`, `SKT`/`Son Kul. Tar.`, `Sıra No`.
  İki satırlı başlıklar (`Birim` / `Fiyat`) — ortalanmış tek satırlık başlıklarla birlikte üç görsel satıra
  dağılsa bile — birleştirilip öyle sınıflandırılır. Sık OCR bozulmaları (`Tutarı→Tutan`, `Oranı→Oram`)
  tanınır. `Mal Hizmet` başlığı okunamazsa ilk sayı sütununun solu ad sütunu sayılır.
- **Aritmetik**: `miktar × birim fiyat (× (1 − iskonto)) = tutar` tutan üçlü, sütun kaymasına ve KDV/
  iskonto sütunlarına rağmen doğru sayıları seçer. Adın içindeki sayılar (`1 kg'lık`) miktar sanılmaz.
- **İskontolu satır**: liste fiyatı yerine **net birim fiyat** (`tutar / miktar`) önerilir —
  `ISKONTO_UYGULANDI` uyarısıyla (backend de tutarı `miktar × birim fiyat` ile karşılaştırır).
- **Virgülü okunmamış sayı** (`14,80` → `1480`): aritmetik ancak `14.80` ile tutuyorsa düzeltilir,
  **`ONDALIK_DUZELTILDI` uyarısıyla** kullanıcıya gösterilir ve güven yükseltilmez.
- **Alt satıra taşan ürün adı** bir önceki kaleme eklenir; adı sayıların üstündeki satırda olan düzen
  de desteklenir. Bozuk okunmuş yüzde (`%1 → Y1`) ada eklenmez.
- **Lot / SKT**: `LOT:`, `Lot No`, `Parti No`, `Şarj`, `Charge`, `Batch` ve `SKT`, `S.K.T.`, `STT`, `TETT`,
  `Son Kullanma/Tüketim Tarihi` — adın içinde, aynı satırda, alt satırda ya da kendi sütununda. Etiketsiz
  bir kod/tarih lot/SKT diye **uydurulmaz**.
- **Birimler**: `kg/kgs/kgm/kilo`, `gr/g`, `ton`, `adet/ad/C62/NIU/pcs`, `lt/l`, `ml`, `paket/pk`, `koli`, `kutu`.
- **Başlıksız belge**: her satır sıra sezgisiyle denenir; birimli miktar + iki tutar ya da tutan bir
  aritmetik üçlü şarttır.
- **Sayılar**: `1.543,50` / `1,543.50` / `12,5` / `1 543,50` / `€1.018,00` / `12,50TL`. İki ayraç varsa
  sondaki ondalıktır; tek ayraçtan sonra 3 hane gelmiyorsa ondalıktır (`12.50` TR belgede de 12,50);
  tam 3 hanede (`1.500`, `25,000`) belge geneli biçim (`detect_number_format`) karar verir.
  Rakam yerine okunan harfler düzeltilir (`1.543,5O → 1543.50`).
- **Tarihler**: `01.11.2026`, `01/11/26`, `01-11-2026`, `01,11.2026` (OCR virgülü), `2026-11-01`,
  `1 Kasım 2026`, `01 KAS 2026`, `05 Nov 2026`.

### Doğrulama (çıkarımdan SONRA, öneri kalitesini artırmak için)

- **Satır düzeyi**: `miktar × birim fiyat` ile okunan `satır tutarı` karşılaştırılır
  (`0,02 €` tolerans); hiçbir yorum tutmuyorsa `TUTAR_UYUSMAZLIGI` uyarısı eklenir. Bu, dokümandaki
  `KZ-2026-001` tutarsızlığının (25 kg × 11,50 €/kg = 287,50 € ama belgede 351,00 € yazması) OCR
  üzerinden de yakalandığının kanıtıdır — `docs/samples/invoice_mismatch.png` bu senaryoyu birebir
  üretir ve `ml/tests/test_ocr.py::test_tutar_uyusmazligi_ocr_uzerinden_yakalanir` doğrular.
- **Belge düzeyi**: satır tutarlarının toplamı belgedeki `Mal Hizmet Toplam Tutarı`/`Ara Toplam`/
  `KDV Matrahı` ile ya da `Ödenecek Tutar`/`Genel Toplam`/`Vergiler Dahil Toplam` **eksi KDV** (birden
  fazla KDV oranı toplanır) ile karşılaştırılır, iskonto da hesaba katılır. Hiçbiri tutmuyorsa
  `TOPLAM_UYUSMAZLIGI` belge uyarısı eklenir (KDV'li toplam yüzünden yanlış alarm verilmez).
- Hiçbir satır okunamazsa `SATIR_BULUNAMADI`, genel güven eşiğin (`OCR_CONFIDENCE_MEDIUM`, varsayılan
  0.5) altındaysa `DUSUK_GUVEN` belge uyarısı eklenir.

### Yanıt biçimi

Her alan: `{ "value": string|null, "confidence": 0..1, "source_text": "...", "warnings": [...] }`.
Belge düzeyinde: `overall_confidence`, `raw_text` (tam OCR metni — web'de "Ham OCR metnini göster"
açılır bölümünde, jüri "gerçekten okudu mu" diye kontrol edebilsin diye), `warnings[]`.
**Hiçbir alan okunamazsa (bozuk/boş dosya dahil) hata fırlatılmaz**: boş öneri + `confidence: 0.0`
döner (`ml/tests/test_ocr.py::test_bozuk_dosya_hata_firlatmaz_...`, `test_bos_dosya_hata_firlatmaz`).

### Yapılandırma (`ml/app/config.py`, env değişkenli)

| Değişken | Varsayılan | Anlamı |
|---|---|---|
| `OCR_LANGUAGES` | `tur+eng` | Tesseract dil paketi |
| `OCR_CONFIDENCE_HIGH` | `0.80` | Bu eşiğin üstü "yüksek güven" (web'de de aynı eşik kullanılır) |
| `OCR_CONFIDENCE_MEDIUM` | `0.50` | Bu eşiğin altı "düşük güven" (sarı çerçeve + dikkat çekme) |
| `OCR_MAX_FILE_SIZE_BYTES` | `15000000` | Azami dosya boyutu |

## Go backend tarafı

### `invoice_import` tablosu (migration `000010`)

`id, file_name, file_hash, content_type, stored_path, uploaded_at, status
(extracted|confirmed|discarded), ocr_json, overall_confidence, purchase_receipt_id (NULL ta ki
onaylanana kadar), confirmed_at, discarded_at, created_at`. `ocr_json`, ML'in `/extract/invoice`
yanıtının **aynısıdır** (Go alan alan kopyalamaz, `json.RawMessage` olarak saklanır ve web'e aynen
geçirilir) — tek doğruluk kaynağı ML'in ürettiği öneri, Go onu dönüştürmez.

### Uç noktalar

- **`POST /api/v1/invoice-imports`** — `multipart/form-data`, alan adı `file` (jpg/png/pdf, azami
  15 MB — hem Go hem ML tarafında kontrol edilir). ML'e iletir, sonucu `status=extracted` olarak
  kaydeder. **ML kapalıysa/hata verirse 503 `unavailable` döner, HİÇBİR KAYIT OLUŞMAZ** (dosya da
  diske yazılmaz). Dosya `UPLOADS_DIR` altında (`docker-compose.yml`'de kalıcı bir volume,
  `uploads_data:/app/uploads`) `<uuid><uzantı>` adıyla saklanır.
- **`GET /api/v1/invoice-imports`** — liste (`file_name`, `uploaded_at`, `status`,
  `overall_confidence`, onaylandıysa `purchase_receipt_id`/`purchase_receipt_doc_no`).
- **`GET /api/v1/invoice-imports/{id}`** — OCR önerisi (`ocr`, ham) + **taze hesaplanan**
  (DB'de saklanmaz, her istekte yeniden hesaplanır — `cutting-orders` önizlemesiyle aynı desen)
  tedarikçi/ürün eşleştirme adayları.
- **`GET /api/v1/invoice-imports/{id}/file`** — yüklenen görseli/PDF'i döner (web'de sol panel).
- **`POST /api/v1/invoice-imports/{id}/confirm`** — gövde **kullanıcının düzelttiği**
  `CreatePurchaseReceiptRequest` (OCR çıktısı değil). `extracted` dışı bir durumda `409`. Tek
  transaction'da: durum kilidi (`FOR UPDATE`) + `createPurchaseReceiptInTx` (repo/purchase.go ile
  **AYNI** fonksiyon) + `status='confirmed'`.
- **`POST /api/v1/invoice-imports/{id}/discard`** — SADECE `extracted` durumundaki bir kaydı
  `discarded` yapar (aksi halde `409`); hiçbir lot/hareket/fiş etkilenmez.
- **`DELETE /api/v1/invoice-imports/{id}`** — `extracted`/`discarded` kaydı ve dosyasını siler;
  `confirmed` kayıt alış fişinin kaynak belgesi olduğu için silinemez (`409`).

### Eşleştirme yardımı (`repo/fuzzy.go`)

OCR'dan gelen tedarikçi adı ve her satırın ürün adı, mevcut `supplier`/`item` (yalnız aktif ürünler)
kayıtlarıyla **basit normalize (Türkçe karakter sadeleştirme, küçük harf) + Levenshtein** benzerliğiyle
karşılaştırılır (model eğitimi yok). En iyi 3 aday (skor ≥ 0.30) döner; eşleşme bulunamazsa boş dizi —
**otomatik seçim yapılmaz**, kullanıcı adaylardan birini seçer ya da ekrandaki "+ Yeni tedarikçi/ürün"
kısayoluyla yeni kayıt oluşturur (mevcut `POST /suppliers`/`POST /items`'i çağırır).

### OCR satır tutarı → `declared_line_total`

OCR'ın her satırdan okuduğu `line_total` (belgede basılı tutar), `confirm` gövdesinde
`declared_line_total` alanına yerleştirilir; backend bunu her zaman yaptığı gibi
`round2(qty × unit_price)` ile karşılaştırıp `has_mismatch` hesaplar. Yani OCR ile yakalanan bir
tutar uyuşmazlığı, onay sonrası **aynı kırmızı uyarı mekanizmasıyla** (Alışlar sayfasındaki
`KZ-2026-001` gösterimiyle birebir) alış fişinde de görünür — ikinci bir tutarsızlık tespiti
yazılmadı, mevcut mekanizma yeniden kullanıldı.

## Web (React), "Fatura Okuma" sayfası

- **Liste** (`/fatura-okuma`): yükleme geçmişi (dosya adı, tarih, durum rozeti, genel güven,
  onaylandıysa oluşan fişe link) + "Fatura/İrsaliye Yükle" butonu.
- **Detay** (`/fatura-okuma/{id}`), iki panel:
  - **Sol**: yüklenen görsel (`GET /invoice-imports/{id}/file`).
  - **Sağ**: çıkarılan alanların **düzenlenebilir** formu. Her alanın yanında güven rozeti
    (yüksek/orta/düşük renk kodu, `OCR_CONFIDENCE_HIGH`/`MEDIUM` eşikleriyle — ML config'indekiyle
    **aynı** sabitler) ve "öneri" etiketi; düşük güvenli alanlar sarı çerçeveli. Tedarikçi ve ürün
    alanlarında fuzzy eşleşme adayları tıklanabilir rozet olarak, aratılabilir bir `Select` ve
    "+ Yeni ..." kısayolu var. Satır tablosunda miktar × birim fiyat ≠ beyan edilen tutar olan
    satırlar kırmızı vurgulu.
  - Üstte kalıcı bant: **"Bunlar OCR önerileridir. Onaylamadan hiçbir kayıt oluşturulmaz."**
  - Alt: **"Onayla ve alış fişi oluştur"** (başarı → oluşan fişe yönlendirir) ve **"Vazgeç"**.
  - Açılır bölüm: ham OCR metni.

## Demo verisi (`docs/samples/`, `ml/invoicegen/`)

Üç örnek fatura **programatik** üretilir (`python -m invoicegen.cli`, PIL ile): gerçek bir firmanın
faturası **değildir**, hayali firma adları kullanılır.

| Dosya | Senaryo |
|---|---|
| `invoice_clean.png` | Temiz, düzenli — yüksek güven beklenir; iki Türkçe tarih/sayı formatı (`2 Kasım 2026`, binlik nokta `1.018,00`) ayrıca test eder. |
| `invoice_mismatch.png` | `KZ-2026-001` senaryosu: 25 kg × 11,50 €/kg yazıyor ama toplam 351,00 € — `TUTAR_UYUSMAZLIGI` OCR üzerinden de yakalanır. |
| `invoice_low_quality.png` | Eğri taranmış (6°) + gürültülü + bulanık, LOT/SKT hiç yazılmamış — eğiklik düzeltilip okunur, LOT/SKT alanları **boş** döner (sistemin dürüst davrandığının kanıtı: uydurma yok). |

Testler ayrıca **GİB e-Fatura benzeri** belgeler üretir (`invoicegen.cli.build_efatura`, diske yazılmaz):
sağ üstte bilgi kutusu (değer etiketin **sağında**, **solunda** ya da **altında**), iki satırlık unvan,
`SAYIN` alıcı bloğu, ETTN, iki satırlı tablo başlıkları ve ızgara çizgileri, `%10` iskontolu satır, ayrı
birim sütunu seçeneği, alt satıra taşan ve içinde `1 kg'lık` geçen ürün adı, adın içinde LOT/SKT, iki
farklı KDV oranı ve KDV'li toplam kutusu. `build_efatura_photo` aynı belgeyi telefon fotoğrafı gibi
bozar (eğik, köşe gölgesi, bulanık, gürültülü, JPEG).

## Ekran görüntüleri

[`docs/screenshots/fatura-okuma-oneri.jpg`](screenshots/fatura-okuma-oneri.jpg) — onay ekranı (sol
görsel, sağ düzenlenebilir öneri formu, güven rozetleri, fuzzy eşleşme adayları).
[`docs/screenshots/fatura-okuma-sonuc-uyusmazlik.jpg`](screenshots/fatura-okuma-sonuc-uyusmazlik.jpg) —
onay sonrası oluşan alış fişi (`invoice_mismatch.png` örneğinden: OCR'ın yakaladığı tutar
uyuşmazlığı, mevcut "Tutar uyuşmazlığı" rozetiyle aynen görünüyor).

## Sınırlılıklar

- **El yazısı desteklenmez.** Tesseract basılı metin için eğitilmiştir.
- **Kurallar, Tesseract'ın yanlış okuduğu harfi geri getiremez.** Bulanık bir görüntüde `Dana Kıyma`
  `Dene Kıyma`, `14,80` `14.20` okunabilir. Ürün adı backend'deki benzerlik eşleştirmesiyle mevcut ürüne
  bağlanır; yanlış okunan sayı `TUTAR_UYUSMAZLIGI` ile işaretlenir; kullanıcı düzeltir.
- **Alışılmadık (sütunsuz, serbest metin) düzenler başarısız olabilir.** Bu durumda sistem boş öneri
  döner (hata vermez), kullanıcı manuel giriş yapar.
- **Çok sayfalı PDF'in yalnızca ilk sayfası işlenir.** PDF'in metin katmanı (dijital e-Fatura PDF'leri)
  kullanılmaz, sayfa 300 DPI görüntüye çevrilip OCR'lanır.
- **Tek bir global ondalık ayıracı sezilir** (belge genelinden); aynı belgede hem `1.234,56` hem
  `1,234.56` karışık kullanılırsa (gerçek hayatta neredeyse hiç olmaz) yanlış sezilebilir.
- **Para birimi bilgisi yalnızca gösterim amaçlıdır**: sistem tüm tutarları € olarak tutar, çoklu
  para birimi desteği yoktur.
- **Fuzzy eşleştirme basit bir Levenshtein benzerliğidir**, anlamsal (eş anlamlı ürün adları gibi)
  eşleştirme yapmaz.
- **Hiçbir model eğitilmedi.** Bu, bilinçli bir tercihtir: fatura düzeni tedarikçiden tedarikçiye
  çok farklıdır; bu ölçekte bir eğitim verisiyle kurulacak bir model hem güvenilir olmaz hem de
  "kara kutu" bir sonuç üretirdi. Kural tabanlı yaklaşım her alanın `source_text`'ini gösterdiği
  için şeffaftır ve jüri/kullanıcı neden o değerin önerildiğini görebilir.

## İnsan onayı neden zorunlu?

Fatura OCR'ı, özellikle eski/soluk/eğri taranmış belgelerde veya alışılmadık düzenlerde hatalı
okuyabilir — bir miktar, fiyat ya da lot numarasının yanlış okunması doğrudan stok/maliyet
kayıtlarına yansırsa (örnek: "25" yerine "250" okunan bir miktar) telafisi zor bir hata zincirine
yol açar (değiştirilemez `stock_movement` defteri yalnızca `reversal` kaydıyla düzeltilebilir, bkz.
`docs/HANDOFF.md`). Bu yüzden OCR çıktısı hiçbir zaman doğrudan yazılmaz: kullanıcı her alanı görür,
düzeltir ve **açıkça onaylar**; sistem onay almadan tek bir satır bile veritabanına yazmaz — bu kural
kodda (`ConfirmInvoiceImport`'un `extracted` dışı durumları reddetmesi, `CreateInvoiceImport`'un asla
`purchase_receipt` yazmaması) ve testlerde (bkz. yukarıdaki "Kesin kural" bölümü) açıkça uygulanır.
