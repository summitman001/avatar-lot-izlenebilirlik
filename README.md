# Avatar — Üretim, Maliyet, Satış ve Lot İzlenebilirliği

Et/gıda işletmesi için dış hizmetle üretimde gerçek maliyet (EKP), bütün kuzu parçalama maliyet dağıtımı ve hammaddeden müşteriye lot izlenebilirliği/geri çağırma sistemi: Go API + MySQL, React yönetim paneli, Python ML servisi ve (ayrı geliştirilen) çevrimdışı Kotlin Android satış uygulaması.

## Hızlı başlangıç

Gereken tek şey Docker. Temiz bir clone'da:

```bash
docker compose up --build
```

MySQL → migration → seed → API → web sırasıyla kendiliğinden ayağa kalkar; seed verileri (ürünler, tedarikçiler, müşteriler, alış lotları, kuzu reçetesi, parçalama emirleri) gelir. İlk derleme birkaç dakika sürer. Sıfırlamak için: `make reset`.

| Servis | Adres | Not |
|---|---|---|
| Web paneli | http://localhost:3000 | Ürünler, Lotlar, Dış Üretim, Parçalama, Satış/Sevkiyat, İzlenebilirlik, Geri Çağırma canlı; diğer sayfalar "yakında" |
| API | http://localhost:8080/api/v1 | `GET /health`, `GET /openapi.yaml` |
| ML servisi | http://localhost:8000 | `GET /health`, `GET /models` |
| MySQL | localhost:3306 | kullanıcı `app` / `app`, veritabanları `avatar` ve `avatar_test` |

Port çakışırsa `.env.example`'ı `.env` olarak kopyalayıp portları değiştirin. Hızlı kontrol: `curl "http://localhost:8080/api/v1/lots?has_mismatch=true"` → `KZ-2026-001` (alış belgesinde 287,50 € hesaplanan ≠ 351,00 € beyan).

## Demo senaryosu: ET-2026-001'den geri çağırma

Seed verisi, bir et lotundan (`ET-2026-001`) başlayıp sucuk üretimi + satış zincirinin tamamını önceden
kurar (ayrıntı: [`docs/traceability.md`](docs/traceability.md)). Jüri aşağıdaki adımlarla aynı sonucu
tekrar üretebilir:

1. **Web panelinde** sol menüden **İzlenebilirlik**'e tıklayın, arama kutusuna `ET-2026-001` yazın ve
   sonuçtan lotu seçin.
2. Üstte açılan hiyerarşik grafikte `ET-2026-001`'in **iki** mamul lotuna (`SUC-2026-045`,
   `SUC-2026-046`) daldığını görün; alttaki "İleri İzleme" sekmesinde etkilenen lotlar ve müşteri
   dağılımını (Market A 40, Market B 25 − 2 iade, Market C 30) inceleyin.
3. **"Bu lottan geri çağırma başlat"** butonuna tıklayıp bir sebep yazın, başlatın — otomatik olarak
   **Geri Çağırma** detay sayfasına yönlendirilirsiniz.
4. Detay sayfasında **Etkilenen Lotlar** tablosunda `ET-2026-001`, `SUC-2026-045`, `SUC-2026-046`'nın
   bloke edildiğini, **Müşteri/Market Tablosu**'nda her marketin sevk/iade/kalan risk miktarını görün
   (`SUC-2026-047`/Market C'nin bu tabloda **olmadığını** da doğrulayın — bağımsız kontrol lotu).
5. **"İletişim Listesi (CSV)"** butonuyla market bazlı iletişim listesini indirin.
6. Her müşterinin durumunu sırayla **Onaylandı**'ya çevirip **"Kapat"** butonuyla geri çağırmayı
   kapatın (tüm müşteriler onaylanmadan kapatma `422` ile reddedilir).
7. `curl -s "http://localhost:8080/api/v1/lots/<ET-2026-001 id>/trace-forward" | jq .summary` ile aynı
   özeti API'den de doğrulayabilirsiniz.

## Demo senaryosu: bütün kuzu parçalama ve maliyet dağıtımı

Seed verisi, dokümandaki bütün kuzu senaryosunu (25 kg, 11,50 €/kg, alış belgesinde 351,00 € beyan
edilmiş) baştan sona kurar (ayrıntı: [`docs/cutting.md`](docs/cutting.md)):

1. Sol menüden **Parçalama**'ya tıklayın: `CUT-2026-001` (kapandı), `CUT-2026-002` ve `CUT-2026-003`
   (taslak) listede görünür.
2. `CUT-2026-001`'i açın: giriş kartında hesaplanan (287,50 €) ve beyan edilen (351,00 €) toplamın
   farklı olduğunu, çözüm notunun (287,50 € seçildi) ve "Değer" yönteminin seçili olduğunu görün.
   Dağıtım önizlemesinde **Miktar** ve **Değer** yöntemlerinin yan yana, her ikisinin toplamının da
   287,50 €'ya birebir eşit olduğunu doğrulayın; altta oluşan 7 lotu (6 parça + fire) inceleyin.
3. `CUT-2026-002`'yi açın: reçeteyle tam orantılı (22 kg, dengeli) ağırlıklar girilmiş ama emir
   **bilerek kapatılmamış** — ağırlık dengesi çubuğunun yeşil (tam denge) olduğunu, "Kapat" butonunun
   aktif olduğunu görün.
4. `CUT-2026-003`'ü açın: AYNI lot (`KZ-2026-002`) kullanılmış ama fire girilmemiş — ağırlık dengesi
   çubuğu kırmızı, "Denge tutmuyor, kapatılamaz" uyarısı ve "Kapat" butonu devre dışı.
5. `curl -s "http://localhost:8080/api/v1/cutting-orders?status=draft" | jq .` ile taslak emirleri
   API'den de doğrulayabilirsiniz.

## İsteğe bağlı özellikler

### Fatura/irsaliye okuma (OCR → insan onayı)

Sol menüden **Fatura Okuma**'ya tıklayıp bir fatura/irsaliye görseli yükleyin (örnek belgeler:
[`docs/samples/`](docs/samples/) — `invoice_clean.png`, `invoice_mismatch.png` (`KZ-2026-001`
tutarsızlığının OCR üzerinden yakalanışı), `invoice_low_quality.png`). Açılan ekranda solda yüklenen
görsel, sağda **Tesseract OCR**'ın çıkardığı alanlar güven rozetleriyle (öneri, "dogru" diye
sunulmaz) düzenlenebilir biçimde görünür; **"Onayla ve alış fişi oluştur"**'a basmadan hiçbir
lot/stok hareketi/alış fişi oluşmaz. Ayrıntı: [`docs/invoice-ocr.md`](docs/invoice-ocr.md).

### Anomali tespiti

ML servisinin tahmin kalıntılarına ve basit eşik kurallarına dayanan 5 kural (`VERIM_DUSUK`,
`FIRE_YUKSEK`, `MALIYET_ARTISI`, `EKP_SAPMA`, `KUTLE_DENGESI`); sol menüde **Uyarılar** sayfası.
Ayrıntı: [`docs/anomaly-integration.md`](docs/anomaly-integration.md).

## Klasör yapısı

```
backend/    Go 1.23 API (chi, database/sql, golang-migrate, shopspring/decimal; ORM yok)
  cmd/api, cmd/seed     çalıştırılabilir komutlar
  internal/{config,db,http,domain,repo}
  migrations/           SQL migration'lar (000001_init)
  api/openapi.yaml      elle yazılmış OpenAPI 3.1
web/        React 18 + Vite + TypeScript + TanStack Query + React Router + Mantine
ml/         Python 3.11 FastAPI: maliyet oracle'ı (golden dosyaları üretir) + baseline tahmin modelleri + fatura/irsaliye OCR (Tesseract, isteğe bağlı)
android/    Kotlin Android saha satış uygulaması (Compose, Room, WorkManager; offline-first) — android/README.md
docs/       api-contract.md, data-model.md, sync.md, cutting.md, assumptions.md, ml-data.md, android-integration.md, HANDOFF.md, golden/
docker/     MySQL ilk kurulum betiği (test veritabanı)
```

## Make komutları

`make` Windows'ta `winget install ezwinports.make` ile kurulur (veya Git Bash / WSL).

| Komut | Ne yapar |
|---|---|
| `make up` | Her şeyi derleyip arka planda başlatır |
| `make down` | Servisleri durdurur (veri kalır) |
| `make reset` | MySQL volume'unu **siler**, sıfırdan kurar (migration + seed) |
| `make migrate` | Bekleyen migration'ları uygular |
| `make seed` | Seed verisini yükler (idempotent) |
| `make test` / `make test-backend` | Go testleri: golden hesap testleri + gerçek MySQL'de migration/trigger/API testleri (Go ve Docker gerekir) |
| `make test-web` | Web tip kontrolü + build |
| `make lint` | `go vet` + `tsc --noEmit` |
| `make logs` | Tüm servis logları |

Backend testleri `TEST_DATABASE_URL` ile Docker'daki ayrı `avatar_test` veritabanına bağlanır (veritabanı adı `_test` ile bitmezse reddeder; geliştirme verisine dokunmaz).

## Bilinen sınırlamalar

- **Kimlik doğrulama yok.** API herkese açıktır ve CORS `CORS_ORIGINS=*` ile her origin'e izin verir. Yarışma demosu içindir; üretimde kimlik doğrulama eklenmeli ve CORS daraltılmalıdır.
- **`POST /sync/push` gerçek işler** (`sales_order`/`create`, `customer_return`/`create`); offline kuralı satışı reddetmez, kaydeder ve ihlalleri `flagged` ile işaretler. Ayrıntı: [`docs/sync.md`](docs/sync.md).
- **MySQL `log_bin_trust_function_creators=1` gerekir.** MySQL 8.4'te binlog açıkken `CREATE TRIGGER` SUPER yetkisi ister; uygulama kullanıcısı SUPER değildir. Değiştirilemez `stock_movement` defteri trigger'larla korunduğu için bu ayar compose'ta (`mysqld` bayrağı) ve CI'da (`SET GLOBAL`) açıktır. Yönetilen bir MySQL'de aynı parametreyi açın veya migration'ı yetkili kullanıcıyla çalıştırın.
- Kimlik doğrulama/yetki ve cihaz kayıt uç noktası dışında Görev 1–3 tamamlandı (dış üretim, bütün
  kuzu parçalama, satış/sevkiyat/iade, izlenebilirlik/geri çağırma). Bkz. [`docs/HANDOFF.md`](docs/HANDOFF.md).

## Dokümanlar

- [`docs/HANDOFF.md`](docs/HANDOFF.md) — **devralan geliştirici için**: ne bitti, ne stub, neden bu kararlar, sıradaki işler
- [`docs/api-contract.md`](docs/api-contract.md) — endpoint'ler, örnek istek/yanıtlar, decimal-string ve UUIDv7 kuralları
- [`docs/data-model.md`](docs/data-model.md) — tablolar ve ER diyagramı (Mermaid)
- [`docs/sync.md`](docs/sync.md) — çevrimdışı senkron sözleşmesi
- [`docs/assumptions.md`](docs/assumptions.md) — **Varsayımlar ve Tespitler** (yuvarlama kuralları, VKP 24,90 € varsayımı, KZ-2026-001 tutarsızlığı)
- [`docs/traceability.md`](docs/traceability.md) — izlenebilirlik veri modeli, recursive CTE mantığı, geri çağırma akışı
- [`docs/cutting.md`](docs/cutting.md) — parçalama: dağıtım yöntemleri, fire/yan ürün muamelesi, yuvarlama, ağırlık dengesi
- [`docs/ml-data.md`](docs/ml-data.md) — ML sentetik verisi; `docs/golden/` — hesap doğruluk referansları
- [`docs/android-integration.md`](docs/android-integration.md) — Kotlin geliştirici için: kullanılacak uç noktalar, örnek gövdeler, sync sonuç durumlarına göre istemci davranışı
- [`docs/anomaly-integration.md`](docs/anomaly-integration.md) — anomali tespiti: kurallar, eşikler, Go entegrasyonu
- [`docs/invoice-ocr.md`](docs/invoice-ocr.md) — fatura/irsaliye okuma: OCR yaklaşımı, onay akışı, sınırlılıklar
