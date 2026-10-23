# Devir notu (HANDOFF)

Bu dosya, backend iskeletini devralan full-stack geliştirici içindir. Branch: `feat/backend-skeleton`. Önce [`README.md`](../README.md), sonra burası; ayrıntılar `docs/` altındadır.

## 1. Tamamlananlar (çalışıyor, test edildi)

- **Şema**: `backend/migrations/000001_init.{up,down}.sql` — spesifikasyondaki tüm tablolar, FK/index'ler, değiştirilemez `stock_movement` (UPDATE/DELETE trigger'ı + işaret CHECK'i + `UNIQUE reverses_movement_id`), `v_lot_balance` view'u. Up/down/up testli.
- **Domain (saf Go, I/O yok)** — `backend/internal/domain`:
  - `dec.go`: `Qty` (3 ondalık) / `Amount` (4 ondalık) / `Date` tipleri; JSON'da **sadece string** kabul eder, JSON number ve fazla ondalığı reddeder.
  - `rounding.go`: `Round2/4`, `LineTotal`, `HasMismatch`, `Distribute` (satır başı yuvarla, kuruş farkını en büyük satıra ekle; toplam kaynağa birebir eşit).
  - `production_cost.go` (`CalculateProductionCost`): Görev 1 EKP — her satır ayrı 2 ondalık, `total_cost` = satır toplamı, `ekp_unit` 4 ondalık, `ekp_display` = `round2(total/adet)`.
  - `cutting.go` (`AllocateCutting`): Görev 2 dağıtım — miktar/değer yöntemi, fire, yan ürün, ağırlık dengesi (±0,05 kg), alış tutarsızlığı çözümlemesi (`cost_basis` + `resolution_note`), alternatif esas.
  - **Doğrulama**: `golden_test.go`, `docs/golden/production_cases.json` (6 senaryo) ve `cutting_cases.json` (11 senaryo) dosyalarını okur ve Go çıktısını **JSON olarak birebir** karşılaştırır. Golden'ı Python oracle (`ml/oracle`) üretir; Go ile aynı sonucu verdiği test edildi.
- **Repo (SQL)** — `backend/internal/repo`: items/suppliers/customers/warehouses CRUD, lots (liste/filtre/detay/bakiye), `CreatePurchaseLot` (tek transaction: belge + lot + belge satırı + `purchase_in` hareketi + change_log), stok hareketi listesi ve `ReverseMovement`, sync push/pull. item/customer/warehouse/lot'a yazan her fonksiyon `change_log` kaydını aynı transaction'da atar.
- **HTTP** — `backend/internal/http`: chi router, JSON hata formatı, CORS, istek logu, panic kurtarma. Uç noktalar `docs/api-contract.md`'de; sözleşme `backend/api/openapi.yaml`.
- **Seed** — `backend/cmd/seed` (idempotent): tedarikçiler, depolar, müşteriler, ürünler, alış belgeleri/lotları/`purchase_in` hareketleri (`KZ-2026-001` `has_mismatch=TRUE`), kuzu reçetesi `REC-KUZU-01` (oran ve fiyatlar `docs/golden/cutting_case.json`'dan okunur; dosya yoksa uyarıyla varsayılan değerler).
- **Web** — `web/`: Mantine sol menü iskeleti (11 sayfa), Ürünler ve Lotlar canlı (arama/filtre, `has_mismatch` rozeti), diğerleri "yakında"; API durum rozeti.
- **Altyapı**: `docker-compose.yml` (mysql→migrate→seed→api→web, ml), `Makefile`, `.env.example`, CI (`ml-test` ve `ml-docker-build` aynen duruyor; eklenenler: `backend-test`, `web-build`, `compose-smoke`).
- **Testler** (`make test`): domain golden + dec/rounding birim testleri, 15 entegrasyon testi (migration up/down/up, trigger, decimal string round-trip, `POST /lots` atomikliği ve geri alma, `has_mismatch`, reverse kuralları, sync duplicate/pull/has_more/blocked, hata formatı, istemci UUIDv7).

## 2. Stub / TODO (bilerek yapılmadı)

| Alan | Durum |
|---|---|
| `POST /sync/push` payload işleme | **Stub.** `op_id` kaydedilir, payload işlenmez; ilk gönderim `flagged` döner (uygulanmadı). `repo/sync.go` içindeki `TODO` yeri. Yazılınca stub kayıtlarını silin: `DELETE FROM processed_request WHERE JSON_EXTRACT(response_json,'$.stub') = TRUE;` |
| Cihaz kaydı | `device` tablosu var, kayıt uç noktası yok. `pull`/`push` yalnızca kayıtlı cihazın `last_sync_at`'ini günceller. |
| Dış üretim, parçalama, satış/sevkiyat/iade, izlenebilirlik, geri çağırma, tahmin, uyarı | Tablolar var; **servis/uç nokta yok**. Hesap kuralları (`CalculateProductionCost`, `AllocateCutting`) hazır ve golden'la doğrulu, bağlanmayı bekliyor. |
| ML entegrasyonu | `ml/` servisi (başka biri yazdı) ayrı çalışır; Go ondan tahmin çağırmıyor, `prediction`/`anomaly_alert` yazılmıyor. |
| Kimlik doğrulama / yetki | Yok. API açık, CORS `*`. |
| Web | Salt okunur iki sayfa; form yok, web testi yok (yalnızca `tsc` + build). |
| `suppliers` sync | Senkron akışına dahil değil (spec: item/customer/warehouse/lot). |
| Android | `android/` boş (`.gitkeep` + README). |

## 3. Alınan kararlar ve nedenleri

1. **Go hesabı, golden dosyalarından türetildi.** Yarışmada "doğru maliyet" en önemli ölçüt; Python oracle zaten kanıtlanmış olduğundan Go onunla JSON-birebir test edilir. Oracle/golden değişirse `go test ./internal/domain` kırılır — iyi bir şey.
2. **Para/miktar JSON'da string, tip düzeyinde zorlanır.** `Qty/Amount` `UnmarshalJSON`'u number'ı reddeder; yanıtta sabit ölçek (`"10.0000"`). Bu, Android/ML tarafında float kaymasını yapısal olarak engeller.
3. **`ekp_display` yalnızca gösterim.** Değerleme `ekp_unit` (4 ondalık) veya `total_cost` üzerinden; `UR-004` golden'ı neden `ekp_display × adet ≠ total_cost` olduğunu kanıtlar.
4. **`POST /lots` = belge + lot + satır + hareket tek transaction.** Her alış lotunun bir belgesi olmalı ki `has_mismatch` hesaplanabilsin. Aynı `doc_no` + aynı tedarikçi ile çok satırlı belge desteklenir.
5. **Stok bakiyesi saklanmaz, `v_lot_balance` hesaplar.** Tek doğruluk kaynağı defter; bakiye senkron dışı kalma riski yok.
6. **Defter bütünlüğü veritabanında.** Trigger (UPDATE/DELETE yasak), işaret CHECK'i, `UNIQUE (reverses_movement_id)` — uygulama hatalı olsa bile kayıt bozulmaz. `ReverseMovement` lot satırını `FOR UPDATE` kilitleyip bakiyeyi kontrol eder (negatif stok yok).
7. **Push stub `applied` değil `flagged` döner.** İstemci "uygulandı" sanıp yerel kaydı silmesin.
8. **`log_bin_trust_function_creators=1`** (compose'ta mysqld bayrağı, CI'da `SET GLOBAL`): MySQL 8.4'te binlog açıkken `CREATE TRIGGER` SUPER ister; uygulama kullanıcısı SUPER değil. Yönetilen bir MySQL'de bu parametreyi açın ya da migration'ı yetkili kullanıcıyla çalıştırın.
9. **Test veritabanı ayrı (`avatar_test`)**, `docker/mysql-init/01-test-db.sh` ile oluşur — **yalnızca boş volume'da çalışır**. Eski volume'unuz varsa `make reset`. Testler `_test` ile bitmeyen veritabanında çalışmayı reddeder.
10. **Test migration'ı `iofs` kaynağıyla** (`db/migrate.go`): `file://` URL'i Windows sürücü harfleriyle bozuluyor.
11. **Go `go 1.23`**, yerelde 1.27 ile geliştirildi; Docker imajı 1.23. Yeni bağımlılık eklerken `go.mod`'daki `go` satırının yükselmediğini kontrol edin.
12. **Silme yok.** Spesifikasyon gereği; ürünü pasifleştirmek için `is_active=false`. İzlenebilirlik için kayıt silinmemeli.
13. **Hata mesajları Türkçe, `code` değerleri İngilizce sabit** — istemciler `code`'a bakmalı, mesaja değil.
14. **Seed'deki VKP 24,90 € ve `KZ-2026-001` tutarsızlığı** varsayım/tespit olarak `docs/assumptions.md`'de.

## 4. Nasıl çalıştırılır

```bash
docker compose up --build          # her şey; web :3000, api :8080, ml :8000
make reset                         # sıfırdan kur (volume silinir)
make test                          # Go testleri (Go 1.23+ ve Docker gerekir)
make lint                          # go vet + tsc
```

Geliştirirken (hot reload yok, basit):

```bash
docker compose up -d mysql && make migrate && make seed   # DB hazır
cd backend && DB_HOST=127.0.0.1 go run ./cmd/api          # API :8080
cd web && npm install && npm run dev                      # web :5173 (VITE_API_URL varsayılan http://localhost:8080/api/v1)
```

Windows notu: `make` için `winget install ezwinports.make`; Go için `winget install GoLang.Go`. PowerShell'de `go run` ortam değişkenini `$env:DB_HOST="127.0.0.1"` ile verin.

Ortam değişkenleri: `DB_HOST/DB_PORT/DB_NAME/DB_USER/DB_PASSWORD` (veya `DATABASE_URL`), `HTTP_ADDR`, `CORS_ORIGINS`, `GOLDEN_DIR` (seed), `TEST_DATABASE_URL` (testler).

## 5. Sıradaki 5 iş (öneri sırası)

1. **Dış üretim servisi (Görev 1)** — `production_order` yaşam döngüsü: hammadde sevki (`send_to_producer` hareketleri, `WH-EXT-01`'e), teslim alma (`received_qty`), kapanış: `domain.CalculateProductionCost` ile `material_cost/service_cost/total_cost/ekp_unit` yaz, çıktı lotu (`origin_type=production`, `unit_cost = ekp_unit`) + `lot_link` kayıtları + `production_in` hareketi. Uç noktalar + web "Dış Üretim" sayfası. Golden: `UR-001…UR-006`.
2. **Parçalama servisi (Görev 2)** — `cutting_order/cutting_output`: `domain.AllocateCutting` ile önizleme (`closable` değilse kapatma), kapanışta parça lotları (`cutting_in`), giriş lotu `cutting_consume`, `lot_link`, `balance_diff_kg`. `REC-KUZU-01` reçetesi seed'de hazır. Golden: `KZ-2026-001…007`.
3. **Sync push payload işleme** — `sales_order` (+satır) → `sale_out` hareketleri; bloke/geri çağrılmış lot → `flagged` + `sales_line.recall_violation`; yetersiz stok → `flagged` + `anomaly_alert`; cihaz kayıt uç noktası. Önce stub `processed_request` kayıtlarını temizleyin. Sözleşme: `docs/sync.md`.
4. **İzlenebilirlik ve geri çağırma (Görev 3)** — `lot_link` üzerinde recursive CTE ile ileri/geri izleme (ileri: hammadde lotu → mamul → sevkiyat → müşteri; geri: mamul → hammadde → tedarikçi); `recall` açınca `recall_lot` (derinlik + yol), `recall_customer`, ilgili lotları `blocked/recalled` yapıp `change_log`'a yaz (cihazlara `blocked_lot_ids` zaten gidiyor); karantina deposuna transfer.
5. **Satış/sevkiyat/iade + kimlik doğrulama + ML bağlama** — `sales_order/shipment/customer_return` uç noktaları ve web sayfaları; API'ye kimlik doğrulama/rol (CORS'u daralt); Go'dan `ml` servisine tahmin çağrısı ve `prediction`/`anomaly_alert` yazımı; web formları (ürün/lot oluşturma) ve web testleri.

## 6. Dikkat edilecekler

- `backend/internal/repo/sync.go` içinde `Push` stub'ı; `docs/sync.md` ile birlikte okuyun.
- `TRUNCATE stock_movement` trigger'ı tetiklemez (testler bunu `reset()` için kullanır); üretimde uygulama kullanıcısına `TRUNCATE/DROP` verilmemeli.
- Yeni yazma fonksiyonu sync'e açılacaksa `appendChange(...)`'ı **aynı `tx` içinde** çağırın.
- Yeni stok hareketi eklerken `insertMovement` kullanın ve bakiye gerektiren işlemlerde lot satırını `FOR UPDATE` kilitleyin (`ReverseMovement` örnek).
- Tutarlarda `float` kullanmayın; `shopspring/decimal` ve `domain.Round*` / `LineTotal` / `Distribute`.
- CI'daki `ml-test` job'ı `docs/golden/` dosyalarının güncel olduğunu kontrol eder: golden'ı elle değiştirmeyin, `cd ml && python -m oracle.cli` ile üretin.
