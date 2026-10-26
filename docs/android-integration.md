# Android entegrasyon rehberi

Bu dosya Kotlin geliştirici içindir: Android uygulamasının (Room/SQLite tabanlı, offline-first,
outbox desenli) backend ile konuştuğu uç noktalar, örnek gövdeler ve sonuç durumlarına göre istemci
davranışı. Genel sözleşme (decimal string, UUIDv7, hata biçimi): `docs/api-contract.md`. Senkron
akışının tam detayı (idempotency, offline kuralı, çakışma tablosu): `docs/sync.md`. Makine okunur
şema: `backend/api/openapi.yaml`.

## 1. Android'in kullanacağı uç noktalar

| Uç nokta | Ne zaman |
|---|---|
| `GET /api/v1/sync/pull?since_seq=&device_id=` | Her senkron turunda: ana veri (item/customer/warehouse) ve lot (durum + bakiye) değişikliklerini çek. |
| `POST /api/v1/sync/push` | Outbox'taki bekleyen işlemleri (bugün: `sales_order`/`create`, `customer_return`/`create`) gönder. |
| `GET /api/v1/lots?status=active&item_id=...` | Satış ekranında ürüne göre satılabilir (aktif) lotları listele. |
| `GET /api/v1/items`, `/customers`, `/warehouses` | İlk kurulumda veya pull akışı dışında ana veri referans listesi (opsiyonel; genelde pull yeterli). |

Android **asla** `POST /sales-orders` veya `POST /customer-returns`'u doğrudan çağırmaz — bunlar REST
uç noktalarıdır (web/ops aracı içindir); cihaz HER ZAMAN `/sync/push` üzerinden gönderir, çünkü push
offline kuyruk + idempotency + "sunucu reddetmez, işaretler" garantisini taşır. Doğrudan REST
çağrısı online kuralları (bloke lot → sert `422`, yetersiz stok → sert `409`) uygular, bu da offline
kullanım için uygun DEĞİLDİR.

## 2. Outbox işlemi oluşturma (satış)

Kullanıcı ekranda bir satış tamamladığında, Android **yerel olarak** şunu yapar (henüz ağ çağrısı yok):

1. `op_id` üret (UUIDv7).
2. Satış için `sales_order.id` üret (UUIDv7) — bu id sunucuda da aynen kullanılacak.
3. Outbox tablosuna şu JSON'u (gelecekteki push gövdesi) yaz:

```json
{
  "op_id": "0192f6c2-8a00-7000-8000-000000000001",
  "entity": "sales_order",
  "action": "create",
  "payload": {
    "id": "0192f6c2-8a00-7000-8000-000000000002",
    "customer_id": "01a1...",
    "warehouse_id": "01a1...",
    "sold_at": "2026-03-10T09:00:00.000Z",
    "create_shipment": true,
    "lines": [
      { "id": "0192f6c2-8a00-7000-8000-000000000003", "item_id": "01a1...", "lot_id": "01a1...", "qty": "2.000", "unit_price": "24.9000" }
    ]
  },
  "client_created_at": "2026-03-10T09:00:00.000Z"
}
```

- `lot_id`: kullanıcının seçtiği lot, cihazın **son pull'dan bildiği** aktif lotlardan biri. Cihaz
  satış anında `blocked_lot_ids` listesine karşı kontrol edebilir (kullanıcıyı uyarmak için), ama bu
  kontrol YALNIZCA bir ön-uyarıdır — asıl karar sunucudadır (offline kuralı, aşağıda).
- `qty`/`unit_price`: STRING, sabit ondalık (miktar 3, tutar 4; eksik ondalık serbest, `BigDecimal`
  kullanın, `Double` KULLANMAYIN — `docs/api-contract.md`).
- `sold_at`/`client_created_at`: cihazın gerçek saati (RFC3339 UTC), ileride "bu satış ne zaman GERÇEKTEN
  yapıldı" sorusuna cevap verir (defterde `stock_movement.client_created_at`'e yazılır).

Benzer şekilde müşteri iadesi için `entity: "customer_return"`:

```json
{
  "op_id": "0192f6c2-8a00-7000-8000-000000000004",
  "entity": "customer_return",
  "action": "create",
  "payload": {
    "id": "0192f6c2-8a00-7000-8000-000000000005",
    "customer_id": "01a1...",
    "sales_line_id": "0192f6c2-8a00-7000-8000-000000000003",
    "qty": "1.000",
    "reason": "musteri memnun degil"
  },
  "client_created_at": "2026-03-10T11:00:00.000Z"
}
```

## 3. Push gönderimi ve sonuç durumlarına göre istemci davranışı

`POST /api/v1/sync/push` gövdesi: `{"device_id": "<cihazin UUIDv7'si>", "operations": [...]}` (en fazla
500 işlem; `client_created_at` sırasıyla göndermeniz önerilir). Yanıt HER ZAMAN `200`'dür; her işlem
için ayrı bir `status` döner:

| `status` | Android davranışı |
|---|---|
| `applied` | Outbox kaydını **sil**. İşlem kesin uygulandı, hiçbir ihlal yok. |
| `duplicate` | Outbox kaydını **sil** (zaten daha önce uygulanmış; `message` ilk sonucu taşır). |
| `flagged` | Outbox kaydını **sil** (satış/iade SUNUCUDA KAYDEDİLDİ, geri alınamaz) VE kullanıcıya uyarı göster: `message` içindeki kodlara göre metin seç — `LOT_BLOCKED_AT_SALE` → "Bu lot geri çağrılmış/bloke, satış incelemeye alındı"; `NEGATIVE_STOCK_OFFLINE` → "Stok yetersizdi, negatif bakiye oluştu, incelemeye alındı". Birden fazla kod virgülle ayrılabilir (`"LOT_BLOCKED_AT_SALE,NEGATIVE_STOCK_OFFLINE"`). |
| `rejected` | Outbox kaydını **SAKLA** (silme!) ve kullanıcıya hata göster (`message` serbest metindir, ekranda gösterilebilir). Kayıt düzeltilmeden otomatik tekrar gönderilmemelidir (örn. yanlış `lot_id`); ama zararsızdır — tekrar gönderilse de hep aynı `rejected` sonucu döner, hiçbir kayıt yazılmaz. |

```kotlin
when (result.status) {
    "applied", "duplicate" -> outboxDao.delete(result.opId)
    "flagged" -> { outboxDao.delete(result.opId); notifyUser(parseViolationCodes(result.message)) }
    "rejected" -> showError(result.opId, result.message) // outbox'ta kalir
}
```

## 4. Pull ile senkron

```kotlin
var sinceSeq = localCursorStore.get()
do {
    val page = api.pull(sinceSeq, deviceId)
    for (change in page.changes) {
        when (change.entity) {
            "lot" -> lotDao.upsert(change.payload)       // qty_on_hand/balances DAHIL (bkz. docs/sync.md)
            "item" -> itemDao.upsert(change.payload)
            "customer" -> customerDao.upsert(change.payload)
            "warehouse" -> warehouseDao.upsert(change.payload)
        }
    }
    localBlockedLotIds.replaceAll(page.blockedLotIds) // TAM liste, artımlı değil
    sinceSeq = page.nextSeq
    localCursorStore.save(sinceSeq)
} while (page.hasMore)
```

Önemli noktalar:

- `lot` payload'ı `qty_on_hand` (toplam) ve `balances` (depo kırılımı) İÇERİR. Bir satış/iade sonrası
  bu alan güncellenmiş olarak gelir — cihaz kendi offline tuttuğu tahmini bakiyeyi bu DEĞERLE
  değiştirmelidir (sunucu otoritedir, cihazın kendi hesabı yalnızca bağlantı kesikken bir tahmindir).
- `blocked_lot_ids` her çekimde TAM listedir (eskisinin üzerine yazın, birleştirmeyin).
- Push'u pull'dan ÖNCE yapın: böylece aynı turda kendi gönderdiğiniz satışın güncellediği lot
  bakiyesini de görürsünüz.

## 5. Sık yapılan hatalar (kontrol listesi)

- [ ] Tutar/miktar alanlarında `Double`/`Float` DEĞİL, `BigDecimal` + `toPlainString()` (sabit ondalık,
      JSON number değil string).
- [ ] `id` alanları her zaman UUIDv7 (rastgele UUIDv4 değil) — sunucu `422` ile reddeder.
- [ ] `/sync/push`'a doğrudan REST hatası (422/409) BEKLEMEYİN — push her zaman `200` döner, sonuç
      `results[i].status`'tadır.
- [ ] `flagged` sonucunu "başarısız" sanıp outbox'ta BIRAKMAYIN — flagged = kaydedildi, yalnızca
      incelemeye alındı.
- [ ] `rejected` sonucunu sessizce yutup outbox'tan SİLMEYİN — kullanıcı kaydı düzeltmeli.
