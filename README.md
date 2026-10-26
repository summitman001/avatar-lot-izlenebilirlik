# Avatar — Üretim, Maliyet, Satış ve Lot İzlenebilirliği

Et/gıda işletmesi için dış hizmetle üretimde gerçek maliyet (EKP), bütün kuzu parçalama maliyet dağıtımı ve hammaddeden müşteriye lot izlenebilirliği/geri çağırma sistemi: Go API + MySQL, React yönetim paneli, Python ML servisi ve (ayrı geliştirilen) çevrimdışı Kotlin Android satış uygulaması.

## Hızlı başlangıç

Gereken tek şey Docker. Temiz bir clone'da:

```bash
docker compose up --build
```

MySQL → migration → seed → API → web sırasıyla kendiliğinden ayağa kalkar; seed verileri (ürünler, tedarikçiler, müşteriler, alış lotları, kuzu reçetesi) gelir. İlk derleme birkaç dakika sürer. Sıfırlamak için: `make reset`.

| Servis | Adres | Not |
|---|---|---|
| Web paneli | http://localhost:3000 | Ürünler, Lotlar, Dış Üretim, Satış/Sevkiyat, İzlenebilirlik, Geri Çağırma canlı; diğer sayfalar "yakında" |
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

## Klasör yapısı

```
backend/    Go 1.23 API (chi, database/sql, golang-migrate, shopspring/decimal; ORM yok)
  cmd/api, cmd/seed     çalıştırılabilir komutlar
  internal/{config,db,http,domain,repo}
  migrations/           SQL migration'lar (000001_init)
  api/openapi.yaml      elle yazılmış OpenAPI 3.1
web/        React 18 + Vite + TypeScript + TanStack Query + React Router + Mantine
ml/         Python 3.11 FastAPI: maliyet oracle'ı (golden dosyaları üretir) + baseline tahmin modelleri
android/    Kotlin Android uygulaması (boş; ayrı geliştirilecek)
docs/       api-contract.md, data-model.md, sync.md, assumptions.md, ml-data.md, android-integration.md, HANDOFF.md, golden/
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
- Parçalama servisi henüz yok (şema ve hesap kuralları hazır); dış üretim, satış/sevkiyat/iade ve
  izlenebilirlik/geri çağırma (Görev 3) tamamlandı. Bkz. [`docs/HANDOFF.md`](docs/HANDOFF.md).

## Dokümanlar

- [`docs/HANDOFF.md`](docs/HANDOFF.md) — **devralan geliştirici için**: ne bitti, ne stub, neden bu kararlar, sıradaki işler
- [`docs/api-contract.md`](docs/api-contract.md) — endpoint'ler, örnek istek/yanıtlar, decimal-string ve UUIDv7 kuralları
- [`docs/data-model.md`](docs/data-model.md) — tablolar ve ER diyagramı (Mermaid)
- [`docs/sync.md`](docs/sync.md) — çevrimdışı senkron sözleşmesi
- [`docs/assumptions.md`](docs/assumptions.md) — **Varsayımlar ve Tespitler** (yuvarlama kuralları, VKP 24,90 € varsayımı, KZ-2026-001 tutarsızlığı)
- [`docs/traceability.md`](docs/traceability.md) — izlenebilirlik veri modeli, recursive CTE mantığı, geri çağırma akışı
- [`docs/ml-data.md`](docs/ml-data.md) — ML sentetik verisi; `docs/golden/` — hesap doğruluk referansları
- [`docs/android-integration.md`](docs/android-integration.md) — Kotlin geliştirici için: kullanılacak uç noktalar, örnek gövdeler, sync sonuç durumlarına göre istemci davranışı
