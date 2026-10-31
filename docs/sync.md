# Offline senkron sözleşmesi

Android uygulaması (Room/SQLite) çevrimdışı çalışır; MySQL ile iki uçlu senkron eder:

- **push**: cihaz çevrimdışıyken ürettiği işlemleri sunucuya gönderir (`POST /api/v1/sync/push`).
- **pull**: cihaz sunucudaki ana veri ve lot değişikliklerini çeker (`GET /api/v1/sync/pull`).

Makine okunur tanım: `backend/api/openapi.yaml`. Genel kurallar (decimal string, UUIDv7): `docs/api-contract.md`.

> **Durum:** Sözleşme kesindir VE **sunucu tarafı push işleme gerçektir** (stub değil): `sales_order/create`
> ve `customer_return/create` gerçekten işlenir, stok defterine yazar, offline kurallarını uygular.
> Pull tamdır (item, customer, warehouse, lot — lot bakiyesi dahil).

## Temel fikirler

1. **İstemci ID üretir.** Her kayıt UUIDv7 taşır; cihaz offline iken `sales_order.id`, `op_id` vb. üretir. Sunucuda çakışma olmaz.
2. **Her işlem idempotenttir.** Her push işlemi bir `op_id` (UUIDv7) taşır. Ağ kesilip aynı işlem tekrar gönderilirse sunucu `duplicate` döner, ikinci kez uygulamaz. (`processed_request` tablosu, `op_id` PK.)
3. **Sunucu hakemdir, ama satışı reddetmez.** Stok defteri sunucudadır ve değiştirilemez. Satış cihazda fiziksel olarak zaten gerçekleşmiştir; sunucu kural ihlali bulsa bile satışı kaydeder (`flagged`), sadece kaydı sonradan değerlendirilmesi için işaretler. Yalnızca bozuk/anlamsız istekler (`rejected`) hiç kaydedilmez.
4. **Pull imleçlidir.** Sunucudaki her değişiklik `change_log`'a monoton artan `seq` ile yazılır. Cihaz son aldığı `next_seq`'i saklar.

## POST /api/v1/sync/push

İstek:

```json
{
  "device_id": "0192f6c2-7b1e-7c3a-9d2e-5a1f0c4b8e21",
  "operations": [
    {
      "op_id": "0192f6c2-8a00-7000-8000-000000000001",
      "entity": "sales_order",
      "action": "create",
      "payload": {
        "customer_id": "01a1...", "warehouse_id": "01a1...", "sold_at": "2026-03-02T10:00:00Z",
        "lines": [{ "item_id": "01a1...", "lot_id": "01a1...", "qty": "2.000", "unit_price": "24.9000" }]
      },
      "client_created_at": "2026-03-02T10:00:00Z"
    }
  ]
}
```

- `device_id`: UUIDv7. **Not:** cihaz kayıt uç noktası henüz yok (TODO); bu `device_id` sadece
  `processed_request`/`device.last_sync_at` bakımı içindir, FK zorunluluğu yoktur. `payload` içindeki
  satış kendi `device_id`'sini (opsiyonel) taşıyabilir, ama bu alan `device` tablosunda KAYITLI bir
  satıra işaret etmelidir (şemadaki FK nedeniyle) — kayıtsızsa boş bırakın.
- `operations`: 1–500 işlem, **sırayla ve her biri kendi transaction'ında** işlenir (bir operasyonun
  hatası diğerlerini etkilemez).
- `entity`/`action`: bugün desteklenen tek çift `sales_order`/`create` ve `customer_return`/`create`dir.
  Başka bir entity/action `rejected` döner.
- `payload`: ilgili REST isteğiyle (`POST /sales-orders` / `POST /customer-returns`) **aynı gövde**
  (decimal'ler string). Bilinmeyen alan `rejected` (bu görevde `400` yerine, çünkü push her zaman `200`
  döner ve hata işlem bazlıdır).
- `client_created_at`: RFC3339 UTC; cihazın işlemi yaptığı an. `sales_order` için satır bazlı
  `stock_movement.client_created_at`'e yazılır (satışın ne zaman GERÇEKTEN yapıldığını defterde saklar).

Yanıt (her zaman `200`; işlem bazlı sonuç):

```json
{
  "results": [
    { "op_id": "0192f6c2-8a00-7000-8000-000000000001", "status": "applied", "message": "satis kaydedildi: SAT-2026-001" }
  ],
  "server_seq": 123
}
```

| `status` | Anlamı | Cihaz ne yapar |
|---|---|---|
| `applied` | İşlem uygulandı, hiçbir kural ihlali yok. | Yerel outbox kaydını **sil**. |
| `duplicate` | Bu `op_id` daha önce işlendi; tekrar uygulanmadı (stok ikinci kez düşmez). | `applied` gibi davran: **sil**. |
| `flagged` | Kabul edildi, **kaydedildi**, ama insan incelemesi gerekiyor (`message` ihlal kodlarını taşır: `LOT_BLOCKED_AT_SALE`, `NEGATIVE_STOCK_OFFLINE`, virgülle ayrık). | Yerel kaydı **sil** (satış sunucuda kalıcı) ve kullanıcıya uyarı göster. |
| `rejected` | Geçersiz (op_id UUIDv7 değil, bilinmeyen alan, bilinmeyen lot/müşteri/ürün, kural ihlali — hiçbir kayıt yazılmadı). | Yerel kaydı **sakla**, kullanıcıya hata göster; düzeltmeden tekrar gönderme (ama tekrar gönderilirse yine `rejected` döner, zararsızdır — dedup uygulanmaz çünkü hiç kayıt yazılmadı). |

`server_seq` isteğin sonunda `change_log`'daki en büyük `seq`'tir.

İstek düzeyinde hatalar (`422 validation_error`): `device_id` UUIDv7 değil, `operations` boş veya 500'den fazla. Bozuk JSON `400 bad_request`.

### OFFLINE KURALI (kritik)

Satış cihazda fiziksel olarak zaten oldu: sunucu reddetmez, kaydeder ve kuralları SONRADAN değerlendirir.

- **Lot sunucuda `blocked`/`recalled` ise:** satış yine kaydedilir (`sale_out` hareketi yazılır),
  `sales_line.recall_violation = TRUE`, sonuç `status: "flagged"`, `message: "LOT_BLOCKED_AT_SALE"`.
- **Bakiye yetersizse:** satış yine kaydedilir, stok **negatife düşer**, bir `anomaly_alert`
  (`severity: critical`, `alert_type: NEGATIVE_STOCK_OFFLINE`, `ref_type: sales_line`) açılır, sonuç
  `flagged` (`message: "NEGATIVE_STOCK_OFFLINE"`; iki ihlal birden varsa virgülle: `"LOT_BLOCKED_AT_SALE,NEGATIVE_STOCK_OFFLINE"`).
- **Cihazdan satılamayan ürün tipi veya depo:** hammadde/fire satışı (`DEVICE_SALE_ITEM_TYPE`) ya da
  karantina/dış üretici deposundan satış (`DEVICE_SALE_WAREHOUSE`) `"rejected"`, hiçbir kayıt yazılmaz
  (bkz. `docs/assumptions.md`, "Cihazdan satış kuralları").
- **Bozuk payload veya bilinmeyen lot/müşteri/ürün/sales_line:** `"rejected"` ve açık mesaj, **hiçbir
  kayıt yazılmaz** (ne `sales_order`/`sales_line`/`stock_movement`, ne `processed_request`).
- **Satılan miktarın üstünde `customer_return`:** aynı şekilde `rejected` (offline tolerans yalnızca
  satış için tanımlıdır, iade için değil — iade bir stok iadesidir, fiziksel zorunluluk yoktur).
- Hepsi sorunsuzsa `"applied"`.
- Bir operasyonun hatası diğerlerini etkilemez (her operasyon ayrı transaction); sıra korunur.
- Sunucu stok bakiyesini ve geri çağırma durumunu kendisi hesaplar, istemcinin gönderdiği bakiyeye güvenmez.

**Uygulama notu (ilgi çekebilir):** `rejected` ile sonuçlanan bir operasyon `processed_request`'e hiç
yazılmaz — bu yüzden dedup edilmez ve tekrar gönderilebilir (zararsızdır, daima aynı sonucu üretir).
`applied`/`flagged` ise `processed_request` satırı (önce "rezervasyon" olarak, sonra güncellenmiş halde)
AYNI transaction'da commit edilir: bu, `op_id`'nin PRIMARY KEY olmasından faydalanan bir desen — iki eş
zamanlı istek aynı `op_id`'yi aynı anda işlemeye çalışırsa, rezervasyon satırının `INSERT`'i ikincisinde
UNIQUE ihlaliyle çarpışır, o transaction tamamen geri alınır (hiçbir yan etkisi commit edilmez) ve
`duplicate` döner.

## GET /api/v1/sync/pull?since_seq=0&device_id=...&limit=500

Lot bakiyesi satış, iade, üretim ve depolar arası transfer (`POST /stock-transfers`) sonrasında
bakiyeli olarak yeniden yazılır.

```json
{
  "changes": [
    {
      "seq": 22,
      "entity": "lot",
      "entity_id": "01a16a05-80ac-7c0e-99be-2765fae80e39",
      "action": "upsert",
      "payload": { "id": "01a16a05-80ac-7c0e-99be-2765fae80e39", "lot_no": "KZ-2026-001", "status": "active", "initial_qty": "25.000", "qty_on_hand": "15.000", "...": "..." }
    }
  ],
  "next_seq": 22,
  "has_more": false,
  "blocked_lot_ids": ["01a16a05-80ac-7c0e-99be-2765fae80e39"]
}
```

- `changes`: `seq > since_seq`, `seq` sırasıyla, en fazla `limit` (varsayılan 500, en çok 1000). Bugün `entity`: `item`, `customer`, `warehouse`, `lot`. Bu tablolara yazan her repo fonksiyonu `change_log` kaydını **aynı transaction'da** atar; yazma geri alınırsa kayıt da gitmez.
- `action`: `upsert | delete`. Bugün yalnızca `upsert` (silme yok).
- `payload`: varlığın API JSON'u, yazıldığı andaki hâli. **Lot payload'ı `qty_on_hand`/`balances` İÇERİR**
  (önceki sözleşmenin aksine): lot oluştuğunda (alış, üretim çıktısı, parçalama çıktısı) ve lotun
  bakiyesini veya durumunu değiştiren her işlemde (satış, iade, bloke/açma, dış üreticiye sevk, üretim
  kabulündeki iade/tüketim, parçalamada giriş lotunun tüketimi, hareket geri alma) o lot için TAM detaylı bir
  `upsert` kaydı yazılır — cihaz böylece kendi offline
  hesapladığı bakiyeyi sunucunun otoriter değeriyle karşılaştırıp düzeltebilir (`qty_on_hand_server`
  niyetiyle; alan adı sözleşmedeki mevcut `qty_on_hand` alanıyla aynıdır, ayrı bir alan EKLENMEDİ).
  Ana veri (item/customer/warehouse) değişiklikleri bakiyesizdir (zaten yok).
- Aynı varlık birden çok kez değişmişse hepsi sırayla gelir; cihaz `upsert`'leri sırayla uygular (son yazan kazanır).
- `next_seq`: bir sonraki çekmede `since_seq` olarak gönderin. Değişiklik yoksa `since_seq` aynen döner.
- `has_more = true` ise `next_seq` ile hemen tekrar çekin.
- `blocked_lot_ids`: durumu `blocked` veya `recalled` olan **tüm** lotlar (her çekimde tam liste, artımlı değil; `POST /lots/{id}/block`/`unblock` ile yönetilir). Cihaz satış sırasında bu listeye karşı kontrol eder; yine de satarsa sunucu `recall_violation` işaretler (yukarıdaki OFFLINE KURALI).
- `device_id` (opsiyonel, UUIDv7): kayıtlı cihazsa `device.last_sync_at` güncellenir. Cihaz kaydı için uç nokta henüz yok (TODO).

### Önerilen istemci döngüsü

```
loop:
  1. push: kuyruktaki işlemleri (client_created_at sırasıyla) 500'lük gruplar halinde gönder
  2. her sonuca göre yerel durumu güncelle (applied/duplicate -> sil; flagged -> sil + uyarı göster; rejected -> sakla + hata göster)
  3. pull: since_seq = saklı imleç; has_more olduğu sürece tekrarla; next_seq'i kalıcı sakla
  4. blocked_lot_ids'i yerel tabloya yaz (eski listenin yerine)
```

Push'u pull'dan önce yapmak, cihazın kendi işlemlerinin sonuçlarını (ör. sunucunun ürettiği düzeltmeleri) aynı turda görmesini sağlar.

## Örnek akış (adım adım)

1. Cihaz offline, `SAT-LOCAL-1` adında bir satış outbox'a yazar: `op_id=X`, lot `L1` (cihazın son
   bildiği durumu: aktif, 10 kg bakiye), 3 kg satılır.
2. Ağ geri gelir. Önce **pull** (son `since_seq`): sunucu bu arada `L1`'i `blocked` yapmış
   (`blocked_lot_ids` içinde `L1` görünür) ve bakiyesi gerçekte 2 kg'dır (başka bir cihaz satmış).
   Cihaz bunu görür ama outbox'taki `X` zaten kuyrukta, geri alınamaz (satış fiziksel olarak oldu).
3. Cihaz **push** eder: `operations: [{op_id: X, entity: sales_order, action: create, payload: {...3 kg...}}]`.
4. Sunucu: lot `L1` bloke → satış YİNE kaydedilir (`sale_out -3kg`, bakiye 2-3=-1 kg, negatife düşer) →
   HEM `LOT_BLOCKED_AT_SALE` HEM `NEGATIVE_STOCK_OFFLINE` ihlali → `anomaly_alert` açılır →
   `status: "flagged", message: "LOT_BLOCKED_AT_SALE,NEGATIVE_STOCK_OFFLINE"`.
5. Cihaz: outbox'taki `X`'i siler (flagged = kaydedildi), kullanıcıya "bu satış incelemede: lot bloke
   ve stok negatif" uyarısı gösterir.
6. Cihaz tekrar **pull** eder: `L1`'in güncel `qty_on_hand=-1.000` değerini görür, yerel önbelleğini düzeltir.

## Çakışma kuralları (özet)

| Durum | Karar |
|---|---|
| Aynı `op_id` tekrar | `duplicate`, hiçbir şey yapılmaz (ikinci kez stok düşmez). |
| Stok yetersiz (satış) | Satış yine kaydedilir, `flagged`, negatif stok uyarısı `anomaly_alert` üretir. |
| Geri çağrılmış/bloke lot satışı | Kaydedilir, `sales_line.recall_violation = TRUE`, `flagged`. |
| Satılandan fazla `customer_return` | `rejected`, hiçbir kayıt yazılmaz (iade için offline tolerans yok). |
| Bilinmeyen müşteri/ürün/lot/sales_line | `rejected`, hiçbir kayıt yazılmaz. |
| Hammadde/fire satışı veya karantina/dış üretici deposundan satış | `rejected` (`DEVICE_SALE_ITEM_TYPE` / `DEVICE_SALE_WAREHOUSE`), hiçbir kayıt yazılmaz. |
| Ana veri çakışması (aynı `code`) | Cihaz ana veri oluşturmaz; ana veri yalnızca sunucudan gelir (pull). |
| Hareket düzeltme | Defter değiştirilemez; düzeltme `reversal` hareketiyle (`POST /stock-movements/{id}/reverse`, `id` verilirse idempotent). |
