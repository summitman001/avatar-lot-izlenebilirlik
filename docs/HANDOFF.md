# Devir notu (HANDOFF)

Bu dosya, backend iskeletini devralan full-stack geliştirici içindir. Branch'ler: `feat/backend-skeleton` (iskelet) ve `feat/task1-external-production` (Görev 1, bu üzerine). Önce [`README.md`](../README.md), sonra burası; ayrıntılar `docs/` altındadır.

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
- **Dış üretim (Görev 1)** — `backend/internal/repo/production.go`, durum/istek kuralları `backend/internal/domain/production.go`: `draft → materials_sent → received → closed`. `POST /production-orders` (+`URT-YYYY-NNN` otomatik numaralandırma), `send-materials` (kısmi/çok partili, bloke lot/yetersiz bakiye kontrolü), `receive` (iade desteği, `domain.CalculateProductionCost` ile EKP, çıktı lotu + `lot_link` + `production_in`, `INVOICED_QTY_MISMATCH`/`QTY_VARIANCE` uyarıları), `close` (fatura no zorunlu), `GET .../cost-report`, `GET .../movements`. `item.material_category` (migration `000002`) ve `production_input` lot-başına-tek-sevk kısıtı (migration `000003`) eklendi.
- **ML entegrasyonu (en iyi gayret)** — `backend/internal/mlclient`: sipariş oluşturulunca `/predict/production` çağrılır (kaba `et_kg` tahminiyle, bkz. `docs/assumptions.md` madde 12), `receive`'de gerçekleşenle karşılaştırılıp `prediction` tablosu güncellenir. ML kapalıysa/zaman aşımına uğrarsa işlem bozulmaz, `prediction` boş kalır (`repo.Store.SetMLClient`, `ML_URL` env).
- **Web** — `web/`: Mantine sol menü iskeleti (11 sayfa), Ürünler/Lotlar/**Dış Üretim** canlı (Dış Üretim: liste + yeni sipariş formu + detay sayfası — durum adımları, sevk/kabul/kapatma formları, maliyet raporu kartı, tahmin-vs-gerçek kartı, hareket zaman çizelgesi, girdi lotları tablosu), diğerleri "yakında"; API durum rozeti. Ekran görüntüsü: [`docs/screenshots/urt-2026-045-cost-report.jpg`](screenshots/urt-2026-045-cost-report.jpg) (temiz `docker compose up --build` sonrası seed senaryosu).
- **Altyapı**: `docker-compose.yml` (mysql→migrate→seed→api→web, ml; `api`/`seed` artık `ML_URL=http://ml:8000` ile), `Makefile`, `.env.example`, CI (`ml-test` ve `ml-docker-build` aynen duruyor; eklenenler: `backend-test`, `web-build`, `compose-smoke`).
- **Testler** (`make test`): domain golden + dec/rounding birim testleri, 15+6 entegrasyon testi — önceki paket (migration up/down/up, trigger, decimal string round-trip, `POST /lots` atomikliği ve geri alma, `has_mismatch`, reverse kuralları, sync duplicate/pull/has_more/blocked, hata formatı, istemci UUIDv7) + yeni (`TestProductionGoldenScenarios`: golden `UR-001/002/003/006` gerçek HTTP uç noktalarıyla çalıştırılıp sonuç golden dosyasından okunarak karşılaştırılır; tam yaşam döngüsü + defter/`lot_link` doğrulaması; bloke lot/yetersiz bakiye/kısmi iade/ML kapalı senaryoları).

## 2. Stub / TODO (bilerek yapılmadı)

| Alan | Durum |
|---|---|
| `POST /sync/push` payload işleme | **Stub.** `op_id` kaydedilir, payload işlenmez; ilk gönderim `flagged` döner (uygulanmadı). `repo/sync.go` içindeki `TODO` yeri. Yazılınca stub kayıtlarını silin: `DELETE FROM processed_request WHERE JSON_EXTRACT(response_json,'$.stub') = TRUE;` |
| Cihaz kaydı | `device` tablosu var, kayıt uç noktası yok. `pull`/`push` yalnızca kayıtlı cihazın `last_sync_at`'ini günceller. |
| Parçalama, satış/sevkiyat/iade, izlenebilirlik, geri çağırma, uyarı | Tablolar var; **servis/uç nokta yok**. Hesap kuralları (`AllocateCutting`) hazır ve golden'la doğrulu, bağlanmayı bekliyor. |
| ML entegrasyonu — yalnız dış üretim | `prediction` yalnızca `production` için yazılır (`anomaly_alert` hiç yazılmıyor; parçalama/satış ML'i de yok). ML yalnızca fiziksel verim tahmin eder, EKP tahmini **yoktur** (bkz. `docs/assumptions.md` madde 11). |
| Kimlik doğrulama / yetki | Yok. API açık, CORS `*`. |
| Web | Ürünler/Lotlar salt okunur; Dış Üretim formlu ama diğer modüller yok. Web testi yok (yalnızca `tsc` + build). |
| Sipariş/lot numarası üretimi atomik değil | `URT-YYYY-NNN`/`SUC-YYYY-NNN`: MAX+1, UNIQUE çakışmasında otomatik yeniden denemez (409). Demo/tek-yazarlı kullanım için yeterli (bkz. `docs/assumptions.md` madde 7). |
| Çok depolu dış üretim desteklenmiyor | Bir siparişin tüm girdileri aynı depodan sevk edilmeli; çıktı da oraya girer (bkz. `docs/assumptions.md` madde 5). |
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
9. **Test veritabanı ayrı (`avatar_test`).** Taze volume'da `docker/mysql-init/01-test-db.sh` oluşturur; mevcut volume'larda `make test-backend` önce `docker compose run --rm test-db` ile (root, `CREATE DATABASE IF NOT EXISTS` + GRANT) oluşturur, `make reset` gerekmez. Testler `_test` ile bitmeyen veritabanında çalışmayı reddeder.
10. **Test migration'ı `iofs` kaynağıyla** (`db/migrate.go`): `file://` URL'i Windows sürücü harfleriyle bozuluyor.
11. **Go `go 1.23`**, yerelde 1.27 ile geliştirildi; Docker imajı 1.23. Yeni bağımlılık eklerken `go.mod`'daki `go` satırının yükselmediğini kontrol edin.
12. **Silme yok.** Spesifikasyon gereği; ürünü pasifleştirmek için `is_active=false`. İzlenebilirlik için kayıt silinmemeli.
13. **Hata mesajları Türkçe, `code` değerleri İngilizce sabit** — istemciler `code`'a bakmalı, mesaja değil.
14. **Seed'deki VKP 24,90 € ve `KZ-2026-001` tutarsızlığı** varsayım/tespit olarak `docs/assumptions.md`'de.
15. **Seed artık servisler üzerinden de yazıyor.** `cmd/seed/externalProduction`: `URT-2026-045` senaryosu `repo.Store`'un gerçek `CreateProductionOrder`/`SendMaterials`/`ReceiveProduction`/`CloseProductionOrder` fonksiyonlarıyla, ham SQL YAZMADAN oluşturulur (yalnızca idempotency kontrolü ve reçete satırları SQL'dir — ikincisi bir "ana veri" tablosu, iş akışı değil). Yeni servis eklerken aynı deseni izleyin.
16. **`item.material_category` ve `production_input` UNIQUE kısıtı ayrı migration'lardır** (`000002`, `000003`) — `000001` zaten `main`'e merge edildiği için üzerine yazılmadı, ekleme yapıldı (bkz. `docs/assumptions.md` madde 4 ve 6).
17. **Hizmet faturası `receive`'de opsiyonel, `close`'da zorunlu.** Gerçek iş akışında fatura genelde mamulden sonra gelir; `close` gövdesi `service_invoice_no` ile tamamlanmasına izin verir (bkz. `docs/assumptions.md` madde 9). Bu, orijinal görev tanımının "receive: service_invoice_no" ile "close: fatura no yoksa 422" arasındaki örtük çelişkiyi çözen bilinçli bir yorumdur.

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

Ortam değişkenleri: `DB_HOST/DB_PORT/DB_NAME/DB_USER/DB_PASSWORD` (veya `DATABASE_URL`), `HTTP_ADDR`, `CORS_ORIGINS`, `GOLDEN_DIR` (seed), `ML_URL` (varsayılan `http://localhost:8000`; compose'ta `http://ml:8000`), `TEST_DATABASE_URL` (testler).

## 5. Sıradaki 5 iş (öneri sırası)

1. **Parçalama servisi (Görev 2)** — `cutting_order/cutting_output`: `domain.AllocateCutting` ile önizleme (`closable` değilse kapatma), kapanışta parça lotları (`cutting_in`), giriş lotu `cutting_consume`, `lot_link`, `balance_diff_kg`. `REC-KUZU-01` reçetesi seed'de hazır. Dış üretim servisiyle aynı desenleri (tek transaction, `FOR UPDATE`, golden-karşılaştırmalı entegrasyon testi) izleyin. Golden: `KZ-2026-001…007`.
2. **Sync push payload işleme** — `sales_order` (+satır) → `sale_out` hareketleri; bloke/geri çağrılmış lot → `flagged` + `sales_line.recall_violation`; yetersiz stok → `flagged` + `anomaly_alert`; cihaz kayıt uç noktası. Önce stub `processed_request` kayıtlarını temizleyin. Sözleşme: `docs/sync.md`.
3. **İzlenebilirlik ve geri çağırma (Görev 3)** — `lot_link` üzerinde recursive CTE ile ileri/geri izleme (ileri: hammadde lotu → mamul → sevkiyat → müşteri; geri: mamul → hammadde → tedarikçi); `recall` açınca `recall_lot` (derinlik + yol), `recall_customer`, ilgili lotları `blocked/recalled` yapıp `change_log`'a yaz (cihazlara `blocked_lot_ids` zaten gidiyor); karantina deposuna transfer.
4. **Satış/sevkiyat/iade + kimlik doğrulama** — `sales_order/shipment/customer_return` uç noktaları ve web sayfaları; API'ye kimlik doğrulama/rol (CORS'u daralt).
5. **Parçalama ML'i + `anomaly_alert`** — `/predict/cutting` için `mlclient`'a benzer entegrasyon; stok/maliyet anomalilerinde `anomaly_alert` yazımı; web'de "Tahminler"/"Uyarılar" sayfaları.

## 6. Dikkat edilecekler

- `backend/internal/repo/sync.go` içinde `Push` stub'ı; `docs/sync.md` ile birlikte okuyun.
- `TRUNCATE stock_movement` trigger'ı tetiklemez (testler bunu `reset()` için kullanır); üretimde uygulama kullanıcısına `TRUNCATE/DROP` verilmemeli.
- Yeni yazma fonksiyonu sync'e açılacaksa `appendChange(...)`'ı **aynı `tx` içinde** çağırın.
- Yeni stok hareketi eklerken `insertMovement` kullanın ve bakiye gerektiren işlemlerde lot satırını `FOR UPDATE` kilitleyin (`ReverseMovement` örnek).
- Tutarlarda `float` kullanmayın; `shopspring/decimal` ve `domain.Round*` / `LineTotal` / `Distribute`.
- CI'daki `ml-test` job'ı `docs/golden/` dosyalarının güncel olduğunu kontrol eder: golden'ı elle değiştirmeyin, `cd ml && python -m oracle.cli` ile üretin.
- Yeni bir uç nokta/servis golden'a dayanıyorsa, testi Go fonksiyonunu doğrudan çağırarak DEĞİL, `backend/internal/integration/production_test.go`'daki `TestProductionGoldenScenarios` gibi **gerçek HTTP uç noktalarından geçirip** golden dosyadan okunan beklenen değerlerle karşılaştırarak yazın (repo/http katmanındaki hataları da yakalar).
- `domain.Qty`/`Amount` HTTP yanıtlarında her zaman sabit ölçekle döner (`"85.000"`); ama `docs/golden/*.json` dosyaları girdinin kendi ölçeğini korur (`"85"`). İkisi sayısal olarak eşittir, string olarak değil — golden'a karşı test yazarken `decimal.Decimal.Equal` kullanın, string karşılaştırması yapmayın (istisna: `round2`/`round4` gibi hesaplanan çıktı alanları, ki onlar zaten her iki tarafta da sabit ölçeklidir ve string karşılaştırması güvenlidir).
