# API sözleşmesi (v1)

Android ve ML geliştiricileri için. Makine okunur karşılığı: [`backend/api/openapi.yaml`](../backend/api/openapi.yaml) (OpenAPI 3.1, `GET /api/v1/openapi.yaml` ile de servis edilir). Aşağıdaki örnekler çalışan API'den alınmıştır (`make up` sonrası seed verisiyle).

- **Base URL:** `http://localhost:8080/api/v1`
- **İçerik:** `Content-Type: application/json; charset=utf-8`. İstek gövdesi en fazla 1 MiB; **bilinmeyen alan 400** ile reddedilir.
- **Kimlik doğrulama:** henüz yok (TODO). CORS: `CORS_ORIGINS` (varsayılan `*`).
- **Sağlık:** `GET /api/v1/health` (kök takma adı `GET /health`) — DB ping'i yapar, DB yoksa `503`.

## Temel kurallar

### 1. Decimal'ler JSON'da STRING taşınır

Para ve miktar **asla JSON number değildir**; `float`'a dönüşmemesi için string gider ve string gelir:

```json
{ "qty": "25.000", "unit_cost": "18.1588" }
```

| Tür | Ölçek | Örnek | Not |
|---|---|---|---|
| Miktar (`Qty`) | 3 ondalık | `"25.000"` | `DECIMAL(14,3)` |
| Tutar / birim maliyet (`Amount`) | 4 ondalık | `"18.1588"`, `"287.5000"` | `DECIMAL(14,4)` |

- Yanıtlarda değerler her zaman **sabit ölçekle** yazılır (`"10"` değil `"10.0000"`).
- İstekte ölçekten **fazla ondalık reddedilir** (sessiz yuvarlama yok): `"qty": "1.0001"` → `400`. Eksik ondalık serbesttir (`"11.5"`).
- İstekte **JSON number reddedilir**: `"qty": 25` → `400 bad_request`.
- Android'de `BigDecimal`, ML'de `decimal.Decimal` kullanın; `Double`/`float` ile hesaplamayın.
- Yuvarlama (ROUND_HALF_UP): satır tutarı 2 ondalık, birim maliyet 4 ondalık; ayrıntı ve golden örnekler `docs/assumptions.md`, `docs/golden/*.json`.

### 2. ID'ler UUIDv7

Tüm `id` alanları UUIDv7 string'idir (`"01a16a05-80ac-7c0e-99be-2765fae80e39"`), **istemci tarafında da üretilebilir**: `POST` gövdesindeki opsiyonel `id` verilirse (UUIDv7 değilse `422`) sunucu onu kullanır, verilmezse üretir. İnsan okunur numaralar (`lot_no`, `code`, `doc_no`) ayrı, benzersiz alanlardır.

### 3. Zaman ve tarih

- Zaman damgaları RFC3339 **UTC**, milisaniyeli: `"2026-10-23T18:28:27.957Z"`.
- Takvim günleri (`received_at`, `expiry_date`): `"YYYY-MM-DD"`.

### 4. Hata formatı

```json
{ "error": { "code": "conflict", "message": "hareket zaten geri alinmis (01a1...)" } }
```

| HTTP | `code` | Ne zaman |
|---|---|---|
| 400 | `bad_request` | Bozuk JSON, bilinmeyen alan, decimal'in number gelmesi / fazla ondalık, geçersiz query parametresi |
| 404 | `not_found` | Kayıt yok |
| 405 | `method_not_allowed` | Metot desteklenmiyor |
| 409 | `conflict` | Tekrarlanan `code`/`lot_no`, zaten geri alınmış hareket, yetersiz stok |
| 422 | `validation_error` | Alan doğrulaması (zorunlu alan, enum, UUIDv7 değil, qty ≤ 0 …) |
| 500 | `internal` | Beklenmeyen hata (ayrıntı sızdırılmaz, sunucu logunda) |
| 503 | `unavailable` | Veritabanı erişilemiyor |

### 5. Liste yanıtları ve sayfalama

Listeler `{ "items": [...] }` döner. `limit` (varsayılan 200, en çok 1000) ve `offset` (varsayılan 0) desteklenir. Silme yoktur (`DELETE` yok); ürünü pasifleştirmek için `is_active=false` kullanın.

---

## Ana veriler: `items`, `suppliers`, `customers`, `warehouses`

Her biri için: `GET /{koleksiyon}`, `POST /{koleksiyon}`, `GET /{koleksiyon}/{id}`, `PUT /{koleksiyon}/{id}` (tam güncelleme; gövde varlığın tamamı).

| Koleksiyon | Liste filtreleri | Zorunlu alanlar |
|---|---|---|
| `/items` | `q` (code/name), `item_type`, `is_active` | `code`, `name`, `item_type`, `base_uom` |
| `/suppliers` | `q`, `supplier_type` | `code`, `name`, `supplier_type` |
| `/customers` | `q`, `customer_type` | `code`, `name`, `customer_type` |
| `/warehouses` | `warehouse_type` | `code`, `name`, `warehouse_type` |

Enum'lar: `item_type` ∈ `raw|finished|cut_part|by_product|waste`, `base_uom` ∈ `kg|adet`, `supplier_type` ∈ `material|external_producer|livestock`, `customer_type` ∈ `market|wholesale|end_customer`, `warehouse_type` ∈ `internal|external_producer|quarantine|vehicle`.

`POST /items` isteği (`is_active` verilmezse `true`):

```json
{
  "code": "SUCUK-NARIN-1KG",
  "name": "Narin Sucuk 1 kg",
  "item_type": "finished",
  "base_uom": "adet",
  "unit_weight_kg": "1.000",
  "default_sale_price": "24.9000"
}
```

`201` yanıtı:

```json
{
  "id": "01a16a05-7ecf-7a76-a746-bda66672b80d",
  "code": "SUCUK-NARIN-1KG",
  "name": "Narin Sucuk 1 kg",
  "item_type": "finished",
  "base_uom": "adet",
  "unit_weight_kg": "1.000",
  "default_sale_price": "24.9000",
  "is_active": true,
  "created_at": "2026-10-23T18:28:27.473Z"
}
```

`unit_weight_kg` (adet → kg çevrimi, verim hesabı için) ve `default_sale_price` `null` olabilir. Müşteri/tedarikçi: `contact_name`, `phone`, `email`, `address` opsiyonel (`null`).

`item`, `customer`, `warehouse` ve `lot` değişiklikleri senkron akışına (`change_log`) da yazılır; tedarikçiler yazılmaz.

---

## Lotlar

### `GET /lots`

Filtreler: `item_id`, `status` (`active|blocked|recalled|consumed`), `q` (lot_no / ürün kodu / ürün adı), `has_mismatch=true|false` (alış belgesinde tutar uyuşmazlığı), `limit`, `offset`. Sıralama: giriş tarihi yeniden eskiye, sonra `lot_no`.

```json
{
  "items": [
    {
      "id": "01a16a05-80ac-7c0e-99be-2765fae80e39",
      "lot_no": "KZ-2026-001",
      "item_id": "01a16a05-7f03-7b9f-bc0a-dc9c3e78d5ff",
      "item_code": "KUZU-BUTUN",
      "item_name": "Bütün Kuzu",
      "supplier_id": "01a16a05-7e3c-7838-98d3-d75a768ebe1a",
      "supplier_name": "Yayla Hayvancılık",
      "origin_type": "purchase",
      "origin_id": "01a16a05-80b3-77e0-9c40-3dc341f5e096",
      "received_or_produced_at": "2026-03-02",
      "expiry_date": null,
      "initial_qty": "25.000",
      "uom": "kg",
      "unit_cost": "11.5000",
      "status": "active",
      "blocked_reason": null,
      "created_at": "2026-10-23T18:28:27.957Z",
      "purchase": {
        "receipt_id": "01a16a05-80b3-77e0-9c40-3dc341f5e096",
        "doc_no": "PR-KZ-2026-001",
        "invoice_no": "HY-F-0001",
        "unit_price": "11.5000",
        "line_total": "287.5000",
        "declared_line_total": "351.0000",
        "has_mismatch": true
      },
      "qty_on_hand": "25.000"
    }
  ]
}
```

- `purchase` yalnızca alış lotlarında doludur (diğerlerinde `null`). `has_mismatch = true` ⇒ belgede beyan edilen tutar (`declared_line_total`) hesaplanandan (`line_total = round2(qty × unit_price)`) farklı. Örnekte 25 kg × 11,50 = **287,50 €** ama belgede **351,00 €**.
- `qty_on_hand`: lotun tüm depolardaki stoğu (`v_lot_balance` toplamı).

### `GET /lots/{id}`

Liste öğesiyle aynı, ek olarak depo kırılımı:

```json
{
  "id": "01a16a05-db3c-7dcd-9490-f5273155112b",
  "lot_no": "KZ-2026-002",
  "qty_on_hand": "18.500",
  "balances": [
    { "warehouse_id": "01a16a05-7e4b-7b00-887a-35c06e0d9266", "warehouse_code": "WH-MAIN", "qty_on_hand": "18.500" }
  ],
  "...": "(diğer alanlar yukarıdaki gibi)"
}
```

### `POST /lots` — alış lotu oluştur

**Tek transaction:** `purchase_receipt` (yoksa) + `lot` + `purchase_receipt_line` + `purchase_in` stok hareketi + `change_log`. Bir adım hata verirse hiçbiri kalmaz.

```json
{
  "lot_no": "KZ-2026-002",
  "item_id": "01a16a05-7f03-7b9f-bc0a-dc9c3e78d5ff",
  "supplier_id": "01a16a05-7e3c-7838-98d3-d75a768ebe1a",
  "warehouse_id": "01a16a05-7e4b-7b00-887a-35c06e0d9266",
  "received_at": "2026-03-03",
  "qty": "18.500",
  "unit_price": "11.7500"
}
```

Opsiyonel alanlar: `id` (istemci UUIDv7), `expiry_date`, `declared_line_total` (belgede yazan tutar; hesaplanandan farklıysa `has_mismatch=true`), `doc_no` (varsayılan `PR-<lot_no>`; aynı `doc_no` + aynı tedarikçi ⇒ mevcut belgeye satır eklenir), `invoice_no`.

`201` yanıtı `GET /lots/{id}` ile aynı biçimdedir; örnek (`18,5 × 11,75 = 217,375 → 217,38` ROUND_HALF_UP):

```json
{
  "lot_no": "KZ-2026-002",
  "initial_qty": "18.500",
  "uom": "kg",
  "unit_cost": "11.7500",
  "status": "active",
  "purchase": { "doc_no": "PR-KZ-2026-002", "unit_price": "11.7500", "line_total": "217.3800", "declared_line_total": null, "has_mismatch": false },
  "qty_on_hand": "18.500",
  "balances": [ { "warehouse_code": "WH-MAIN", "qty_on_hand": "18.500" } ],
  "...": "..."
}
```

Hatalar: `409` (`lot_no` veya `doc_no` çakışması, belge başka tedarikçiye ait), `422` (`item_id/supplier_id/warehouse_id` yok, ürün pasif, `qty ≤ 0`, UUIDv7 değil).

---

## Stok hareketleri (değiştirilemez defter)

### `GET /stock-movements`

Filtreler: `lot_id`, `ref_type`, `warehouse_id`, `movement_type`, `limit`, `offset`. Yeniden eskiye.

```json
{
  "items": [
    {
      "id": "01a16a05-db4e-751a-bd17-92a1dae9df7e",
      "lot_id": "01a16a05-db3c-7dcd-9490-f5273155112b",
      "lot_no": "KZ-2026-002",
      "warehouse_id": "01a16a05-7e4b-7b00-887a-35c06e0d9266",
      "movement_type": "purchase_in",
      "qty": "18.500",
      "unit_cost": "11.7500",
      "ref_type": "purchase_receipt",
      "ref_id": "01a16a05-db46-7075-8429-9c11036898d5",
      "reverses_movement_id": null,
      "device_id": null,
      "client_created_at": null,
      "note": null,
      "created_at": "2026-10-23T18:28:51.151Z"
    }
  ]
}
```

`qty` **işaretlidir**: giriş `+`, çıkış `−`. `movement_type`: `purchase_in, send_to_producer, production_consume, production_in, cutting_consume, cutting_in, transfer_out, transfer_in, sale_out, return_in, waste_out, reprocess_consume, reprocess_in, adjustment, reversal`.

### `POST /stock-movements/{id}/reverse`

Hareketi ters kayıtla geri alır: aynı lot/depo/birim maliyet, işareti ters `qty`, `movement_type = "reversal"`, `reverses_movement_id` = orijinal. Gövde **opsiyoneldir**:

```json
{ "note": "yanlis giris", "id": "<istemci UUIDv7>", "device_id": "<UUIDv7>" }
```

`201` yanıtı:

```json
{
  "id": "01a16a05-e1d9-7735-bc17-e9e659af88bf",
  "lot_id": "01a16a05-db3c-7dcd-9490-f5273155112b",
  "lot_no": "KZ-2026-002",
  "warehouse_id": "01a16a05-7e4b-7b00-887a-35c06e0d9266",
  "movement_type": "reversal",
  "qty": "-18.500",
  "unit_cost": "11.7500",
  "ref_type": "stock_movement",
  "ref_id": "01a16a05-db4e-751a-bd17-92a1dae9df7e",
  "reverses_movement_id": "01a16a05-db4e-751a-bd17-92a1dae9df7e",
  "note": "yanlis giris",
  "created_at": "2026-10-23T18:28:52.826Z"
}
```

Kurallar: bir hareket **en fazla bir kez** geri alınır (tekrar → `409`); bir `reversal` geri alınamaz (`422`); geri alma stoğu **negatife düşürüyorsa** `409` ("geri alma stogu negatife dusurur"); olmayan hareket `404`. İstekte `id` verilip aynı `id` ile tekrar gönderilirse mevcut ters kayıt döner (idempotent).

---

## Senkron

Ayrıntılı akış ve çakışma kuralları: [`docs/sync.md`](sync.md).

### `POST /sync/push`

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

Yanıt (`200`):

```json
{
  "results": [
    { "op_id": "0192f6c2-8a00-7000-8000-000000000001", "status": "flagged", "message": "TODO: payload isleme henuz uygulanmadi; op_id kaydedildi, islem uygulanmadi" }
  ],
  "server_seq": 23
}
```

Aynı `op_id` tekrar gönderilirse: `"status": "duplicate", "message": "op_id daha once islendi (ilk sonuc: flagged)"`. `status` ∈ `applied | duplicate | flagged | rejected`.

> **Bugün stub:** `op_id` kaydedilir, payload işlenmez; bu yüzden ilk gönderim `applied` değil `flagged` döner. Sözleşme (istek/yanıt biçimi, durumlar) kesindir.

### `GET /sync/pull?since_seq=0&device_id=...&limit=500`

```json
{
  "changes": [
    { "seq": 22, "entity": "lot", "entity_id": "01a16a05-80ac-7c0e-99be-2765fae80e39", "action": "upsert", "payload": { "id": "01a16a05-80ac-7c0e-99be-2765fae80e39", "lot_no": "KZ-2026-001", "initial_qty": "25.000", "...": "..." } }
  ],
  "next_seq": 22,
  "has_more": false,
  "blocked_lot_ids": ["01a16a05-80ac-7c0e-99be-2765fae80e39"]
}
```

`entity` ∈ `item | customer | warehouse | lot`; `payload` ilgili varlığın API JSON'udur (lot payload'ında `qty_on_hand`/`balances` yoktur). `blocked_lot_ids`: bloke/geri çağrılmış tüm lotlar (her çekmede tam liste). `next_seq`'i saklayıp sonraki çekmede `since_seq` olarak gönderin.

---

## Henüz uygulanmamış (sözleşme taslağı yok)

Dış üretim, parçalama, satış/sevkiyat/iade, izlenebilirlik sorguları, geri çağırma, tahmin/uyarı uç noktaları ve cihaz kaydı sonraki işlerdir (bkz. [`docs/HANDOFF.md`](HANDOFF.md)). Şemaları `backend/migrations/000001_init.up.sql` içinde hazırdır; hesap kuralları Go'da `backend/internal/domain` içinde (`CalculateProductionCost`, `AllocateCutting`) golden testlerle doğrulanmıştır.
