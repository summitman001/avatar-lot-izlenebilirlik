# Offline senkron sözleşmesi

Android uygulaması (Room/SQLite) çevrimdışı çalışır; MySQL ile iki uçlu senkron eder:

- **push**: cihaz çevrimdışıyken ürettiği işlemleri sunucuya gönderir (`POST /api/v1/sync/push`).
- **pull**: cihaz sunucudaki ana veri ve lot değişikliklerini çeker (`GET /api/v1/sync/pull`).

Makine okunur tanım: `backend/api/openapi.yaml`. Genel kurallar (decimal string, UUIDv7): `docs/api-contract.md`.

> **Durum:** Sözleşme kesindir. **Sunucu tarafı push işleme henüz yazılmadı** (stub): `op_id` kaydedilir, payload işlenmez. Pull tamdır (item, customer, warehouse, lot).

## Temel fikirler

1. **İstemci ID üretir.** Her kayıt UUIDv7 taşır; cihaz offline iken `sales_order.id`, `op_id` vb. üretir. Sunucuda çakışma olmaz.
2. **Her işlem idempotenttir.** Her push işlemi bir `op_id` (UUIDv7) taşır. Ağ kesilip aynı işlem tekrar gönderilirse sunucu `duplicate` döner, ikinci kez uygulamaz. (`processed_request` tablosu, `op_id` PK.)
3. **Sunucu hakemdir.** Stok defteri sunucudadır ve değiştirilemez. Cihazın satışı sunucuda stok/geri çağırma kurallarına takılırsa `flagged` veya `rejected` döner.
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
      "payload": { "order_no": "S-0001" },
      "client_created_at": "2026-03-02T10:00:00Z"
    }
  ]
}
```

- `device_id`: UUIDv7. `operations`: 1–500 işlem, **sırayla** işlenir.
- `action`: `create | update | delete`. `payload`: ilgili varlığın API JSON'u (decimal'ler string).
- `client_created_at`: RFC3339 UTC; cihazın işlemi yaptığı an. Bugün yalnızca doğrulanır; payload işleme yazılınca `stock_movement.client_created_at`'e aktarılır (TODO).

Yanıt (her zaman 200; işlem bazlı sonuç):

```json
{
  "results": [
    { "op_id": "0192f6c2-8a00-7000-8000-000000000001", "status": "applied", "message": "" }
  ],
  "server_seq": 123
}
```

| `status` | Anlamı | Cihaz ne yapar |
|---|---|---|
| `applied` | İşlem uygulandı. | Yerel kaydı "senkronlandı" işaretle. |
| `duplicate` | Bu `op_id` daha önce işlendi; tekrar uygulanmadı. | `applied` gibi davran (ilk sonucu `message` taşır). |
| `flagged` | Kabul edildi ama insan incelemesi gerekiyor (örn. geri çağrılmış lotun satışı, `recall_violation`). **Bugünkü stub'ın ilk gönderimi de `flagged` döner** (işlem uygulanmadı). | Kullanıcıya uyarı göster, kaydı "incelemede" tut. |
| `rejected` | Geçersiz (op_id UUIDv7 değil, bilinmeyen alan, kural ihlali). Tekrar gönderilmemeli. | Kullanıcıya hata göster; kaydı düzeltmeden tekrar gönderme. |

`server_seq` isteğin sonunda `change_log`'daki en büyük `seq`'tir.

İstek düzeyinde hatalar (`422 validation_error`): `device_id` UUIDv7 değil, `operations` boş veya 500'den fazla. Bozuk JSON `400 bad_request`.

### Bugünkü davranış (stub) ve TODO

`backend/internal/repo/sync.go` içinde `pushOne`:

1. `op_id` UUIDv7 değilse, `entity` boşsa, `action` geçersizse veya `payload` geçerli JSON değilse → `rejected` (kayıt yazılmaz).
2. Aksi halde `processed_request`'e `INSERT` (`status = 'flagged'`, `response_json.stub = true`). PK çakışırsa → `duplicate`.
3. `TODO:` payload işleme — `entity`'ye göre aynı transaction'da: `sales_order` → `sales_line` + `sale_out` hareketleri; bloke/geri çağrılmış lot → `flagged` + `sales_line.recall_violation`; stok yetersizse → `flagged`.

Payload işleme yazıldığında bugünkü stub kayıtlarını temizleyin, yoksa cihazların yeniden gönderdiği işlemler `duplicate` sayılıp hiç işlenmez:

```sql
DELETE FROM processed_request WHERE JSON_EXTRACT(response_json, '$.stub') = TRUE;
```

## GET /api/v1/sync/pull?since_seq=0&device_id=...&limit=500

```json
{
  "changes": [
    {
      "seq": 22,
      "entity": "lot",
      "entity_id": "01a16a05-80ac-7c0e-99be-2765fae80e39",
      "action": "upsert",
      "payload": { "id": "01a16a05-80ac-7c0e-99be-2765fae80e39", "lot_no": "KZ-2026-001", "status": "active", "initial_qty": "25.000", "...": "..." }
    }
  ],
  "next_seq": 22,
  "has_more": false,
  "blocked_lot_ids": ["01a16a05-80ac-7c0e-99be-2765fae80e39"]
}
```

- `changes`: `seq > since_seq`, `seq` sırasıyla, en fazla `limit` (varsayılan 500, en çok 1000). Bugün `entity`: `item`, `customer`, `warehouse`, `lot`. Bu tablolara yazan her repo fonksiyonu `change_log` kaydını **aynı transaction'da** atar; yazma geri alınırsa kayıt da gitmez.
- `action`: `upsert | delete`. Bugün yalnızca `upsert` (silme yok).
- `payload`: varlığın API JSON'u, yazıldığı andaki hâli. Lot payload'ı **bakiye içermez** (`qty_on_hand`/`balances` yok): bakiye hareketle değişir, varlıkla senkronlanmaz.
- Aynı varlık birden çok kez değişmişse hepsi sırayla gelir; cihaz `upsert`'leri sırayla uygular (son yazan kazanır).
- `next_seq`: bir sonraki çekmede `since_seq` olarak gönderin. Değişiklik yoksa `since_seq` aynen döner.
- `has_more = true` ise `next_seq` ile hemen tekrar çekin.
- `blocked_lot_ids`: durumu `blocked` veya `recalled` olan **tüm** lotlar (her çekmede tam liste, artımlı değil). Cihaz satış sırasında bu listeye karşı kontrol eder; yine de satarsa sunucu `recall_violation` işaretler (TODO).
- `device_id` (opsiyonel, UUIDv7): kayıtlı cihazsa `device.last_sync_at` güncellenir. Cihaz kaydı için uç nokta henüz yok (TODO).

### Önerilen istemci döngüsü

```
loop:
  1. push: kuyruktaki işlemleri (client_created_at sırasıyla) 500'lük gruplar halinde gönder
  2. her sonuca göre yerel durumu güncelle (applied/duplicate -> senkron; flagged -> incelemede; rejected -> hata)
  3. pull: since_seq = saklı imleç; has_more olduğu sürece tekrarla; next_seq'i kalıcı sakla
  4. blocked_lot_ids'i yerel tabloya yaz (eski listenin yerine)
```

Push'u pull'dan önce yapmak, cihazın kendi işlemlerinin sonuçlarını (ör. sunucunun ürettiği düzeltmeleri) aynı turda görmesini sağlar.

## Çakışma kuralları (tasarım)

| Durum | Karar |
|---|---|
| Aynı `op_id` tekrar | `duplicate`, hiçbir şey yapılmaz. |
| Stok yetersiz | Satış yine kaydedilir (satış gerçekleşti), `flagged`; negatif stok uyarısı `anomaly_alert` üretir (TODO). |
| Geri çağrılmış/bloke lot satışı | Kaydedilir, `sales_line.recall_violation = TRUE`, `flagged` (TODO). |
| Ana veri çakışması (aynı `code`) | Cihaz ana veri oluşturmaz; ana veri yalnızca sunucudan gelir (pull). |
| Hareket düzeltme | Defter değiştirilemez; düzeltme `reversal` hareketiyle (`POST /stock-movements/{id}/reverse`, `id` verilirse idempotent). |
