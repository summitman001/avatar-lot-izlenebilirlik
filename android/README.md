# Android — Lot izlenebilir saha satış uygulaması

Kotlin 2.4 · Jetpack Compose + Material 3 · Navigation Compose · Room (KSP) · Hilt · Retrofit + kotlinx.serialization · OkHttp · WorkManager. Paket: `com.avatar.lotsales`, minSdk 26, compileSdk/targetSdk 37.

Uygulama **offline-first** çalışır: tüm yazmalar önce Room'a gider, internet gelince Go API ile senkronlanır. Sözleşme: [`docs/api-contract.md`](../docs/api-contract.md), [`docs/sync.md`](../docs/sync.md).

## Gereksinimler

- **Android Studio 2025.3.4** veya daha yenisi (AGP 9.2.1). Gradle'ı Studio'nun JBR'si (JDK 21) ile çalıştırın; JDK 26 gibi çok yeni bir `JAVA_HOME` Gradle 9.7'de desteklenmeyebilir.
- Android SDK: platform 37 ve build-tools. Eksikse AGP kendisi indirir (lisanslar kabul edilmiş olmalı).
- Komut satırından derlemek için `local.properties` dosyasında `sdk.dir` olmalı (Studio otomatik yazar; repoya girmez).

## Çalıştırma

```bash
cd android
./gradlew assembleDebug test        # Windows: gradlew.bat assembleDebug test
```

APK: `app/build/outputs/apk/debug/app-debug.apk` → `adb install -r app/build/outputs/apk/debug/app-debug.apk`.

### FakeApi (backend olmadan)

Debug build varsayılan olarak **FakeApi** ile çalışır: OkHttp interceptor'ı istekleri ağa çıkarmadan `app/src/debug/assets/fakeapi/*.json` fixture'larından cevaplar. Fixture'larda `SUC-2026-045` (Narin Sucuk 1 kg, 85 adet, aktif), `SUC-2026-040` (FEFO gösterimi için SKT'si daha yakın, 20 adet), `SUC-2026-099` (bloke, `blocked_lot_ids` içinde) ve Market A/B/C müşterileri var. FakeApi sunucu kurallarını taklit eder: aynı `op_id` ikinci kez gelince `duplicate`, bloke lot satışı `flagged` döner, satılan miktar bakiyeden düşer. Bu durum bellekte tutulur, uygulama yeniden başlayınca sıfırlanır.

| Ayar (`android/gradle.properties` veya `-P`) | Varsayılan | Anlamı |
|---|---|---|
| `useFakeApi` | `true` | Debug build'de FakeApi açık/kapalı (release'te her zaman kapalı) |
| `apiBaseUrl` | `http://10.0.2.2:8080/api/v1/` | Gerçek backend adresi (sonda `/` olmalı) |

Gerçek backend'e bağlanmak için: `./gradlew installDebug -PuseFakeApi=false`

- **Emülatör**, bilgisayardaki API'ye `10.0.2.2` üzerinden erişir (emülatörde `localhost` emülatörün kendisidir). Backend hostta `:8080`'de çalışmalı (`docker compose up` veya `go run ./cmd/api`).
- **Gerçek cihaz** için bilgisayarın yerel ağ IP'sini verin: `-PapiBaseUrl=http://192.168.1.20:8080/api/v1/`.
- Debug build'de düz HTTP (cleartext) açıktır, release'te kapalıdır.

## Mimari

- **Tek doğruluk kaynağı Room.** UI yalnızca Room Flow'larını gözler; ağ yanıtı doğrudan ekrana gelmez.
- **Para ve miktar `BigDecimal`.** Room'da TEXT (`toPlainString`) olarak saklanır, JSON'da string taşınır. Ölçekler: miktar 3, birim fiyat 4, satır tutarı 2 ondalık, `HALF_UP` (`core/Decimals.kt`, Go'daki `domain.LineTotal` ile aynı). SQL'de bu kolonlarla aritmetik yapılmaz, toplamlar Kotlin'de alınır.
- **ID'ler istemcide üretilen UUIDv7** (`core/Uuid7.kt`): küçük harf ve aynı milisaniye içinde bile kesin artan sırada.
- **Outbox deseni** (`data/SalesRepository.kt`): satış onayı TEK Room transaction'ıdır. `sales_orders`, `sales_lines`, her satır için `local_stock_movements` (`sale_out`, negatif) ve `sync_outbox` birlikte yazılır. Kurallar transaction içinde yeniden doğrulanır: bloke lot, yetersiz stok, miktar ≤ 0.
- **Senkron** (`data/SyncRepository.kt`, `sync/SyncWork.kt`): WorkManager ile, bağlantı varken çalışır. Üç tetik vardır: 15 dakikada bir periyodik, uygulama açılışında ve her satıştan sonra tek seferlik. Her turda:
  1. **PUSH**: outbox `created_at` sırasıyla, 500'lük gruplar halinde gönderilir. `applied`, `duplicate` ve `flagged` sunucuya ulaşmış sayılır; `flagged` ve `rejected` "İnceleme" listesine düşer.
  2. **PULL**: `since_seq` ile, `has_more` bitene kadar çekilir. Veri ve imleç aynı transaction'da yazılır. `blocked_lot_ids` her seferinde tam liste olarak gelir ve `blocked_lots` tablosu onunla değiştirilir.
  3. **Bakiye yenileme**: `GET /lots` ile `qty_on_hand_server` güncellenir.
- **Yerel kullanılabilir miktar** = `qty_on_hand_server` + `synced=0` yerel hareketlerin toplamı. Bir satışın hareketi, sipariş sunucuya ulaştıktan **ve** bakiye yenilemesi başarılı olduktan sonra `synced=1` olur. Arada bir adım başarısız olursa stok iki kez düşülmüş görünür, yani hata hep "eksik göster" yönündedir; fazla satış oluşmaz.
- **FEFO**: ürün seçilince SKT'si en yakın, satılabilir ve stoklu lot önerilir. Bloke lotlar listede görünür ama seçilince hata verir.

## Ekranlar

1. **Lotlar**: arama, ürün, lot no, SKT, kullanılabilir miktar ve durum rozeti. Bloke lot kırmızıdır ve tıklanamaz; aktif lota dokununca o lotla yeni satış açılır.
2. **Yeni satış**: müşteri → ürün → lot (FEFO önerili) → miktar ve fiyat → satır ekle → onayla.
3. **Senkron**: bekleyen işlem sayısı, son başarılı senkron, son hata, "Şimdi senkronla" butonu, flagged/rejected listesi.

Üst barda her zaman bir gösterge vardır: `Çevrimdışı` / `Bekleyen N` / `Güncel`; inceleme bekleyen kayıt varsa ek olarak kırmızı `İnceleme N`.

## Testler

`./gradlew test` (AGP 9'da varsayılan olarak yalnız debug varyantı):

- `Uuid7Test`: sürüm/varyant geçerliliği, aynı milisaniyede ve saat geri gittiğinde artan sıra.
- `DecimalsTest`: TypeConverter round-trip (ölçek korunur), satır tutarı yuvarlaması, girdi ayrıştırma.
- `SaleValidatorTest`: bloke lot, yetersiz stok (taslaktaki satırlar dahil), sıfır/negatif miktar, FEFO sırası, kullanılabilir miktar hesabı.
- `SalesRepositoryTest` (Robolectric): onay 4 tabloya birlikte yazar; transaction ortasında hata olursa hiçbiri yazılmaz.
- `SyncRepositoryTest` (Robolectric + FakeApi, `src/testDebug`): push sonrası `applied`; aynı `op_id` tekrarında `duplicate` başarı sayılır ve stok iki kez düşmez; pull sonrası bloke lot satılamaz; çevrimdışıyken satılan geri çağrılmış lot `flagged` olur.

Robolectric testleri API 36'da koşar (`src/test/resources/robolectric.properties`).

## Sözleşmeden sapmalar ve açık konular

- **Lot bakiyesi pull'da gelmez** (`sync.md`: "lot payload'ı bakiye içermez"). `qty_on_hand_server` bu yüzden `GET /lots` → `qty_on_hand` alanından okunur. Bu değer **tüm depoların toplamıdır**; cihazın (aracın) kendi depo bakiyesi değildir. Backend'e cihaz deposuna göre bakiye veren bir uç nokta (veya pull alanı) gerekiyor.
- **`sales_order` push payload'ı** repo dokümanlarında tanımlı değil (`sync.md`'deki örnek yalnızca `order_no` içeriyor). Görev tanımındaki biçim kullanıldı: `{id, order_no, customer_id, sold_at, lines:[{id, item_id, lot_id, qty, unit_price, line_total, stock_movement_id}]}`. Sunucu tablosu `warehouse_id` istiyor; bunu cihaz kaydından (`device.warehouse_id`) türetmesi gerekecek, ama cihaz kayıt uç noktası henüz yok.
- **Backend push'u bugün stub**: ilk gönderim `flagged` döner. Gerçek backend'le her satış "İnceleme" listesinde görünür. Bu yüzden FakeApi debug'da varsayılan olarak açık.
- Pull'daki `warehouse` değişiklikleri yok sayılır (Room'da tablosu yok). `has_more` ve `entity_id` alanları sözleşmede var ve kullanılıyor.
- `rejected` bir satışın stok düşümü yerelde kalır (temkinli taraf). Düzeltme ve iptal akışı yok.
- `items.is_active` Room şemasında yok; pasif ürün de listelenir.
- `order_no` cihazda üretilir: `S-` + UUIDv7'nin rastgele kısmının son 10 hanesi.
- Kimlik doğrulama yok (backend'de de yok).
