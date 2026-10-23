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
| Web paneli | http://localhost:3000 | Ürünler ve Lotlar canlı; diğer sayfalar "yakında" |
| API | http://localhost:8080/api/v1 | `GET /health`, `GET /openapi.yaml` |
| ML servisi | http://localhost:8000 | `GET /health`, `GET /models` |
| MySQL | localhost:3306 | kullanıcı `app` / `app`, veritabanları `avatar` ve `avatar_test` |

Port çakışırsa `.env.example`'ı `.env` olarak kopyalayıp portları değiştirin. Hızlı kontrol: `curl "http://localhost:8080/api/v1/lots?has_mismatch=true"` → `KZ-2026-001` (alış belgesinde 287,50 € hesaplanan ≠ 351,00 € beyan).

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
docs/       api-contract.md, data-model.md, sync.md, assumptions.md, ml-data.md, HANDOFF.md, golden/
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

## Dokümanlar

- [`docs/HANDOFF.md`](docs/HANDOFF.md) — **devralan geliştirici için**: ne bitti, ne stub, neden bu kararlar, sıradaki işler
- [`docs/api-contract.md`](docs/api-contract.md) — endpoint'ler, örnek istek/yanıtlar, decimal-string ve UUIDv7 kuralları
- [`docs/data-model.md`](docs/data-model.md) — tablolar ve ER diyagramı (Mermaid)
- [`docs/sync.md`](docs/sync.md) — çevrimdışı senkron sözleşmesi
- [`docs/assumptions.md`](docs/assumptions.md) — **Varsayımlar ve Tespitler** (yuvarlama kuralları, VKP 24,90 € varsayımı, KZ-2026-001 tutarsızlığı)
- [`docs/ml-data.md`](docs/ml-data.md) — ML sentetik verisi; `docs/golden/` — hesap doğruluk referansları
