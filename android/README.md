# Android — Lot izlenebilir saha satış uygulaması

Kotlin 2.4 · Jetpack Compose + Material 3 · Navigation Compose · Room (KSP) · Hilt · Retrofit + kotlinx.serialization · OkHttp · WorkManager. Paket: `com.avatar.lotsales`, minSdk 26, compileSdk/targetSdk 37.

Uygulama **offline-first** çalışır: tüm yazmalar önce Room'a gider, internet gelince Go API ile senkronlanır. Sözleşme: [`docs/android-integration.md`](../docs/android-integration.md), [`docs/sync.md`](../docs/sync.md), [`docs/api-contract.md`](../docs/api-contract.md).

## Gereksinimler

- **Android Studio 2025.3.4** veya daha yenisi (AGP 9.2.1). Gradle daemon'u JDK 21 ile çalışır (`gradle/gradle-daemon-jvm.properties`); `JAVA_HOME` daha yeni bir JDK'yı gösterse de sorun olmaz.
- Android SDK: platform 37 ve build-tools. Eksikse AGP kendisi indirir (lisanslar kabul edilmiş olmalı).
- Komut satırından derlemek için `local.properties` dosyasında `sdk.dir` olmalı (Studio otomatik yazar; repoya girmez).

## Çalıştırma

```bash
cd android
./gradlew assembleDebug test        # Windows: gradlew.bat assembleDebug test
```

APK: `app/build/outputs/apk/debug/app-debug.apk` → `adb install -r app/build/outputs/apk/debug/app-debug.apk`.

**İlk açılışta Senkron sekmesinden cihaz deposunu seçin** (satışın yapıldığı depo, örn. satış aracı). Depo seçilmeden satış yapılamaz; üst barda kırmızı "Depo seçilmedi" görünür.

### FakeApi (backend olmadan)

Debug build varsayılan olarak **FakeApi** ile çalışır: OkHttp interceptor'ı istekleri ağa çıkarmadan `app/src/debug/assets/fakeapi/pull.json`'dan başlayan bellek içi bir sunucu gibi cevaplar. Fixture'da depolar `WH-VAN-01` (stoklu araç) ve `WH-MAIN` (stoksuz) var. Lotlar:

- `SUC-2026-045`: aktif, 85 adet
- `SUC-2026-040`: aktif, 20 adet, SKT'si daha yakın (FEFO gösterimi için)
- `SUC-2026-099`: bloke

Müşteriler Market A, B ve C. Demo için `WH-VAN-01`'i seçin.

FakeApi Go sunucusunun push kurallarını taklit eder:

- bilinmeyen alan içeren payload'ı **reddeder** (sunucudaki `DisallowUnknownFields` ile aynı)
- aynı `op_id` tekrar gelirse `duplicate` döner
- bloke lot satışında `flagged` + `LOT_BLOCKED_AT_SALE`, yetersiz bakiyede `flagged` + `NEGATIVE_STOCK_OFFLINE` döner
- satış ve iade depo bakiyesini değiştirir ve pull akışına bakiyeli lot kaydı ekler

Durum bellekte tutulur, uygulama yeniden başlayınca sıfırlanır.

| Ayar (`android/gradle.properties` veya `-P`) | Varsayılan | Anlamı |
|---|---|---|
| `useFakeApi` | `true` | Debug build'de FakeApi açık/kapalı (release'te her zaman kapalı) |
| `apiBaseUrl` | `http://10.0.2.2:8080/api/v1/` | Gerçek backend adresi (sonda `/` olmalı) |

Gerçek backend'e bağlanmak için: `./gradlew installDebug -PuseFakeApi=false`. Seed verisinde stok `WH-MAIN`'dedir; cihaz deposu olarak onu seçin.

- **Emülatör**, bilgisayardaki API'ye `10.0.2.2` üzerinden erişir (emülatörde `localhost` emülatörün kendisidir). Backend hostta `:8080`'de çalışmalı.
- **Gerçek cihaz** için bilgisayarın yerel ağ IP'sini verin: `-PapiBaseUrl=http://192.168.1.20:8080/api/v1/`.
- Debug build'de düz HTTP (cleartext) açıktır, release'te kapalıdır.

### Release APK

İmza anahtarı repoya girmez. `~/.gradle/gradle.properties` dosyasına (veya `-P` ile) şunları verin, sonra `./gradlew assembleRelease` çalıştırın:

```properties
releaseStoreFile=C:/keys/lotsales.jks
releaseStorePassword=...
releaseKeyAlias=lotsales
releaseKeyPassword=...
```

Bu değerler verilmezse release imzasız derlenir.

## Mimari

- **Tek doğruluk kaynağı Room.** UI yalnızca Room Flow'larını gözler; ağ yanıtı doğrudan ekrana gelmez.
- **Para ve miktar `BigDecimal`.** Room'da TEXT (`toPlainString`) olarak saklanır, JSON'da string taşınır. Ölçekler: miktar 3, birim fiyat 4, satır tutarı 2 ondalık, `HALF_UP` (`core/Decimals.kt`, Go'daki `domain.LineTotal` ile aynı). SQL'de bu kolonlarla aritmetik yapılmaz, toplamlar Kotlin'de alınır.
- **ID'ler istemcide üretilen UUIDv7** (`core/Uuid7.kt`): küçük harf ve aynı milisaniye içinde bile kesin artan sırada. Sunucu bu id'leri aynen kullanır.
- **Outbox deseni** (`data/SalesRepository.kt`). Her yazma TEK Room transaction'ıdır, ağ çağrısı içermez:
  - **satış:** `sales_orders` + `sales_lines` + her satır için `local_stock_movements` (`sale_out`, −) + `sync_outbox`
  - **iade:** `customer_returns` + `return_in` (+) + `sync_outbox`

  Kurallar (bloke lot, stok, miktar, iade ≤ satılan − iade edilen) transaction içinde yeniden doğrulanır.
- **Push payload'ları sunucunun istek gövdeleriyle birebir aynıdır** (`CreateSalesOrderRequest`, `CreateCustomerReturnRequest`; `data/net/SyncApi.kt`). Sunucu bilinmeyen alanı reddettiği için bu sınıflara sözleşme dışı alan eklemeyin. `device_id` gönderilmez (cihaz kayıt uç noktası yok); `create_shipment` varsayılan olarak true.
- **Senkron** (`data/SyncRepository.kt`, `sync/SyncWork.kt`): WorkManager ile, bağlantı varken çalışır. Üç tetik vardır: 15 dakikada bir periyodik, uygulama açılışında ve her satış/iadeden sonra tek seferlik. Her turda:
  1. **PUSH**: outbox `created_at` sırasıyla, 500'lük gruplar halinde gönderilir. `applied` ve `duplicate` başarıdır. `flagged` sunucuda kayıtlıdır ama incelemededir; Senkron ekranında Türkçe uyarıyla görünür ve "Tamam" ile kapanır. `rejected` sunucuya hiç yazılmamıştır; outbox'ta kalır, "Vazgeç" seçilince stok etkisi geri alınır.
  2. **PULL**: `since_seq` ile, `has_more` bitene kadar çekilir. Veri ve imleç aynı transaction'da yazılır. `blocked_lot_ids` her seferinde tam liste olarak gelir.
- **Lot bakiyesi** = pull'daki lot payload'ının `balances` kırılımında **cihaz deposunun** değeri (`qty_on_hand_server`). Cihaz deposu değişince bakiyeler sıfırlanır ve imleç 0'a çekilir; tam yeniden çekim yapılır. Bekleyen işlem varken depo değiştirilemez.
- **Yerel kullanılabilir miktar** = `qty_on_hand_server` + `synced=0` yerel hareketler. Sunucuya ulaşmış bir kaydın hareketi, ancak sonraki pull başarılı olunca `synced=1` olur. Arada bir adım başarısız olursa stok iki kez düşülmüş görünür, yani hata hep "eksik göster" yönündedir; fazla satış oluşmaz.
- **Room v2** (`warehouses`, `customer_returns`): v1'den otomatik migration yapılır. v1 pull'da depoları attığı için migration imleci ve bakiyeleri sıfırlar (`Migration1To2`).
- **FEFO**: ürün seçilince SKT'si en yakın, satılabilir ve stoklu lot önerilir. Bloke lotlar listede görünür ama seçilince hata verir.

## Ekranlar

1. **Lotlar**: arama, ürün, lot no, SKT, kullanılabilir miktar ve Türkçe durum rozeti. Bloke lot kırmızıdır ve tıklanamaz; aktif lota dokununca o lotla yeni satış açılır.
2. **Yeni satış**: müşteri → ürün → lot (FEFO önerili) → miktar ve fiyat → satır ekle → onayla.
3. **Satışlar**: satış listesi (Bekliyor / Senkronlandı / İncelemede / Reddedildi). Detayda satırlar ve lotları görünür, satırdan **iade alınır** (miktar + neden).
4. **Senkron**: cihaz deposu seçimi, bekleyen işlem sayısı, son başarılı senkron, son hata, "Şimdi senkronla" butonu, inceleme listesi (flagged → Tamam, rejected → Vazgeç).

Üst barda her zaman bir gösterge vardır: `Çevrimdışı` / `Bekleyen N` / `Güncel`. Gerekirse ek olarak kırmızı `Depo seçilmedi` ve `İnceleme N` görünür. Arayüz metinleri `res/values/strings.xml`'dedir; iş kuralı mesajları ise saf JVM testlerinde doğrulandığı için `domain/SaleValidator.kt`'de durur.

## Tasarım

- Tema `ui/Theme.kt`: marka yeşili `#2E7D32`, krem yüzeyler, yalnız açık tema; durum renkleri `StatusColors` (tonlu rozet zemininde ≥4.8:1 kontrast).
- Font: Plus Jakarta Sans (variable, `res/font/`), lisansı `assets/licenses/OFL-PlusJakartaSans.txt`. İkonlar: Material Symbols Rounded vector drawable'ları (`res/drawable/ic_*.xml`).
- Animasyon yardımcıları `ui/Motion.kt`: ekranlar arası paylaşılan kart (`sharedCard`), sıralı liste girişi, odometre sayaç (`RollingText`), sallanma + titreşim, başarı tiki. Açılış: `core-splashscreen` (API 31+ animasyonlu ikon).
- Sistem ayarında animasyonlar kapalıysa Compose bunlara uyar; ek kod yok.

## Testler

`./gradlew test` (AGP 9'da varsayılan olarak yalnız debug varyantı), toplam 31 test:

- `Uuid7Test`, `DecimalsTest`, `SaleValidatorTest`: UUIDv7 sırası/geçerliliği, TypeConverter round-trip, yuvarlama, satış ve iade kuralları, ihlal kodu metinleri, FEFO.
- `SalesRepositoryTest` (Robolectric): onay 4 tabloya birlikte yazar, transaction ortasında hata olursa hiçbiri yazılmaz; depo zorunlu; iade satılanı aşamaz; reddedilen satıştan vazgeçince stok geri gelir.
- `MigrationTest` (Robolectric): dışa aktarılmış v1 şemasından gerçek bir veritabanı kurar, v2'ye yükseltir.
- `SyncRepositoryTest` (Robolectric + FakeApi, `src/testDebug`):
  - bakiye cihaz deposundan gelir
  - push `applied`; aynı `op_id` tekrarında `duplicate` sayılır ve stok iki kez düşmez
  - çevrimdışı satılan geri çağrılmış lot `flagged` olur
  - negatif stok `flagged` olur
  - iade senkronlanır
  - sözleşme dışı alan `rejected` döner
  - bekleyen işlem varken depo değişmez
- `LotsScreenTest` (Compose, Robolectric): bloke lot tıklanamaz, aktif lot satışı açar.
- `LotVisualsTest`: SKT aciliyet eşikleri (geçti / ≤7 / ≤30 gün) ve Türkçe baş harf avatarı.
- `RealBackendSyncTest`: **gerçek Go API'ye karşı** uçtan uca test (pull bakiyesi, satış, iade, geri çağırma, duplicate, sözleşme dışı alan). Yalnızca `E2E_API_URL` verilince koşar; CI'da atlanır:

  ```bash
  E2E_API_URL=http://127.0.0.1:8080/api/v1/ ./gradlew testDebugUnitTest --tests '*RealBackendSyncTest*'
  ```

Robolectric testleri API 36'da koşar (`src/test/resources/robolectric.properties`).

## Açık konular

- **Cihaz kaydı yok** (backend TODO). `device_id` yalnızca istemcide üretilir; satışın deposu kullanıcının Senkron ekranında seçtiği depodur.
- **Lot bakiyesi her stok değişikliğinde gelmiyor.** Bakiyeli lot kaydı lot oluşunca ve satış, iade veya bloke/açma işlemlerinde yazılır. Depolar arası transfer, üretim sevki ve parçalama tüketimi lot kaydı yazmaz; bu lotların cihazdaki bakiyesi bir sonraki bakiyeli kayda kadar eski kalabilir. Araçta satılan mamul ve parça lotlarını etkilemez.
- `items.is_active` Room'da yok; pasif ürün de listelenir.
- `order_no` cihazda üretilir: `S-` + UUIDv7'nin rastgele kısmının son 10 hanesi. Sunucu bunu aynen kullanır.
- Kimlik doğrulama yok (backend'de de yok).
