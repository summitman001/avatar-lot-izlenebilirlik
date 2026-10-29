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
Model **eğitimi yapılmadı**: çıkarım tamamen kural tabanlı (regex + pozisyon sezgileri), üretim/parçalama
ML modellerinden farklı olarak burada "öğrenen" bir model yok — fatura düzeni belge belge çok farklı
olduğundan, bu ölçekte küçük bir eğitim verisiyle genel geçer bir model kurmak gerçekçi değil; kural
tabanlı yaklaşım hem şeffaf (her alanın neden o değeri aldığı `source_text`'ten görülür) hem de
jüri/kullanıcı için öngörülebilir.

### Ön işleme (`preprocess_image`)

Basit ve deterministik: gri tonlama → otomatik döndürme (Tesseract OSD, en iyi gayret — başarısız
olursa atlanır) → kontrast normalizasyonu (`autocontrast`) → küçük görseller 1600 px genişliğe
büyütülür (OCR doğruluğu düşük çözünürlükte belirgin düşer).

### Çıkarım

- **Belge üst bilgisi**: tedarikçi adı (belgenin ilk, salt sayısal/tarih olmayan metin satırı),
  fatura/irsaliye no (`FATURA NO` / `IRSALIYE NO` / `BELGE NO` etiketleri), tarih (bkz. aşağıda),
  para birimi (`€` / `TL`/`TRY`/`₺` sayımı, varsayılan `EUR` — **sistem tüm tutarları € olarak
  tutar**, bu alan yalnızca bilgilendirme amaçlıdır, `confirm` gövdesine dahil edilmez).
- **Satırlar**: ürün adı, miktar + ölçü birimi (`kg`/`adet`/`lt`), birim fiyat, satır tutarı, ve
  (varsa, ayrı bir alt satırda) `LOT:`/`SKT:` etiketli lot no / son kullanma tarihi. Sütun ayrıştırması
  ham metin değil, Tesseract'ın kelime kutucuklarından (`image_to_data`, `left`/`width`) yeniden
  kurulmuş bir düzenle yapılır — `image_to_string`'in kendi düzen çıkarımı geniş sütun boşluklarını
  tek boşluğa indirip komşu sütunları birbirine karıştırabiliyordu (bkz. `ocr.py` `run_tesseract`).
- **Türkçe tarih formatları**: `01.11.2026`, `01/11/2026`, `1 Kasım 2026` (ay adları dahil).
- **Türkçe sayı formatı**: `1.543,50` (binlik nokta, ondalık virgül) **ve** `1,543.50` (ABD formatı)
  ikisi de desteklenir; hangisinin kullanıldığı belge genelinden sezilir (`detect_number_format`:
  `\d,\d{2}$` kalıbı `\d\.\d{2}$` kalıbından daha sık geçiyorsa `tr`, aksi halde `us`).

### Doğrulama (çıkarımdan SONRA, öneri kalitesini artırmak için)

- **Satır düzeyi**: `miktar × birim fiyat` ile okunan `satır tutarı` karşılaştırılır
  (`0,02 €` tolerans); tutmuyorsa `TUTAR_UYUSMAZLIGI` uyarısı eklenir. Bu, dokümandaki
  `KZ-2026-001` tutarsızlığının (25 kg × 11,50 €/kg = 287,50 € ama belgede 351,00 € yazması) OCR
  üzerinden de yakalandığının kanıtıdır — `docs/samples/invoice_mismatch.png` bu senaryoyu birebir
  üretir ve `ml/tests/test_ocr.py::test_tutar_uyusmazligi_ocr_uzerinden_yakalanir` doğrular.
- **Belge düzeyi**: satır tutarlarının toplamı ile belgedeki `TOPLAM`/`GENEL TOPLAM` karşılaştırılır;
  tutmuyorsa `TOPLAM_UYUSMAZLIGI` belge uyarısı eklenir.
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
| `invoice_low_quality.png` | Eğri taranmış (6°) + gürültülü + bulanık, LOT/SKT hiç yazılmamış — düşük güven, bu alanlar **boş** döner (sistemin dürüst davrandığının kanıtı: uydurma yok). |

## Ekran görüntüleri

[`docs/screenshots/fatura-okuma-oneri.jpg`](screenshots/fatura-okuma-oneri.jpg) — onay ekranı (sol
görsel, sağ düzenlenebilir öneri formu, güven rozetleri, fuzzy eşleşme adayları).
[`docs/screenshots/fatura-okuma-sonuc-uyusmazlik.jpg`](screenshots/fatura-okuma-sonuc-uyusmazlik.jpg) —
onay sonrası oluşan alış fişi (`invoice_mismatch.png` örneğinden: OCR'ın yakaladığı tutar
uyuşmazlığı, mevcut "Tutar uyuşmazlığı" rozetiyle aynen görünüyor).

## Sınırlılıklar

- **El yazısı desteklenmez.** Tesseract basılı metin için eğitilmiştir.
- **Çok kötü taranmış/eğri belgeler ve alışılmadık (sütunsuz, serbest metin) düzenler başarısız
  olabilir.** Bu durumda sistem boş öneri döner (hata vermez), kullanıcı manuel giriş yapar —
  `invoice_low_quality.png` örneği bunu bilerek gösterir.
- **Çok sayfalı PDF'in yalnızca ilk sayfası işlenir.**
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
