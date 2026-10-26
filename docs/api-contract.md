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

Kurallar: yalnızca tek hareketli belgelerin hareketleri geri alınabilir (bugün `ref_type = purchase_receipt`); çok hareketli bir belgeye bağlı hareket (ör. `production_order` sevk/teslim hareketleri) tek başına geri alınamaz (`422`) — transferin yarısı geri alınırsa stok yoktan oluşur ve belge defterden ayrışır. Bir hareket **en fazla bir kez** geri alınır (tekrar → `409`); bir `reversal` geri alınamaz (`422`); geri alma stoğu **negatife düşürüyorsa** `409` ("geri alma stogu negatife dusurur"); olmayan hareket `404`. İstekte `id` verilip aynı `id` ile tekrar gönderilirse mevcut ters kayıt döner (idempotent).

---

## Satış / Sevkiyat / İade

Ayrıntılı: [`backend/api/openapi.yaml`](../backend/api/openapi.yaml) (`sales` tag'i).

### `POST /sales-orders` — satış oluştur

Online (REST) kurallar: lot `active` olmalı (aksi halde `422`, mesajda `LOT_BLOCKED`), belirtilen depoda
bakiye yeterli olmalı (aksi halde `409`, mesajda `INSUFFICIENT_STOCK`). Aynı istemci `id`'siyle tekrar
gelen istek mevcut siparişi döner (idempotent). `/sync/push`'un `sales_order/create` işlemesiyle **aynı
servis fonksiyonu** kullanılır (bkz. `docs/sync.md`).

```json
{
  "customer_id": "01a1...",
  "warehouse_id": "01a1...",
  "sold_at": "2026-03-10T09:00:00Z",
  "create_shipment": true,
  "lines": [
    { "item_id": "01a1...", "lot_id": "01a1...", "qty": "30.000", "unit_price": "24.9000" }
  ]
}
```

`201` yanıtı satırları (`lines`, her birinde `lot_no`, `line_total`, `stock_movement_id`,
`recall_violation`) ve `create_shipment=true` ise oluşan sevkiyatı (`shipments`, her birinde lot/miktar
satırları) içerir. `order_no` boşsa `SAT-<yıl>-NNN` atomik üretilir; sevkiyat `SHP-<yıl>-NNN`.

### `GET /sales-orders` / `GET /sales-orders/{id}`

Liste filtreleri: `customer_id`, `lot_id`, `from`/`to` (sold_at, `YYYY-MM-DD`). Detay satırları +
sevkiyatları içerir; liste yalnızca `has_violation` (en az bir satırda `recall_violation=true`) bayrağını taşır.

### `POST /customer-returns` — müşteri iadesi

`sales_line_id` veya `lot_id`'den en az biri verilmeli. `return_in` hareketi (+qty) aynı lota geri girer.
Satılan miktarın üstünde iade `422`: `sales_line_id` verilmişse o satırın `qty`'sine, verilmemişse lotun
toplam satışına (tüm `sale_out` hareketleri) göre kontrol edilir. `lot_id`'den depo türetimi: o lotun en
son `sale_out` hareketinin deposu (bu lot hiç satılmadıysa `422`).

```json
{ "customer_id": "01a1...", "sales_line_id": "01a1...", "qty": "4.000", "reason": "musteri memnun degil" }
```

## Bloke lot mekanizması

### `POST /lots/{id}/block` / `POST /lots/{id}/unblock`

`block`: gövdede `{"reason": "..."}` zorunlu; `lot.status='blocked'`, `blocked_reason` güncellenir.
`unblock`: gövde yok; `status='active'`, `blocked_reason=null`. İkisi de `change_log`'a yazar (pull'daki
`blocked_lot_ids` her çekmede güncel tam listedir). Geri çağırma servisi (Görev 3) bu uç noktaları
yeniden kullanacaktır.

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
      "payload": {
        "customer_id": "01a1...", "warehouse_id": "01a1...", "sold_at": "2026-03-02T10:00:00Z",
        "lines": [{ "item_id": "01a1...", "lot_id": "01a1...", "qty": "2.000", "unit_price": "24.9000" }]
      },
      "client_created_at": "2026-03-02T10:00:00Z"
    }
  ]
}
```

Yanıt (`200`):

```json
{
  "results": [
    { "op_id": "0192f6c2-8a00-7000-8000-000000000001", "status": "applied", "message": "satis kaydedildi: SAT-2026-001" }
  ],
  "server_seq": 23
}
```

Aynı `op_id` tekrar gönderilirse: `"status": "duplicate", "message": "op_id daha once islendi (ilk sonuc: applied)"`.
`status` ∈ `applied | duplicate | flagged | rejected`. Desteklenen `entity`/`action`: `sales_order`/`create`,
`customer_return`/`create`. Payload işleme gerçektir (stub değil): ayrıntılı offline kurallar, `flagged`/`rejected`
anlamları ve örnek akış `docs/sync.md`'dedir.

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

## Dış üretim siparişleri (Görev 1)

Durum makinesi: `draft → materials_sent → received → closed`. Geçersiz geçişler `409`. Ayrıntılı kurallar ve örnekler: [`docs/assumptions.md`](assumptions.md) ("Dış üretim (Görev 1) servisine ilişkin varsayımlar ve tespitler").

### `POST /production-orders` — sipariş oluştur (durum `draft`)

```json
{
  "product_item_id": "01a174fc-3594-70aa-9633-15f7e5522093",
  "producer_supplier_id": "01a174fc-2e17-7a08-ac08-30badb2fc575",
  "external_warehouse_id": "01a174fc-2fdf-70da-a64b-a8994d461a3c",
  "planned_qty": "85",
  "service_unit_price": "5.5000",
  "sale_price": "24.9000"
}
```

Opsiyonel: `id`, `order_no` (boşsa `URT-<yıl>-NNN` **atomik** üretilir — eş zamanlı isteklerde de çakışmaz). Oluşturulunca ML servisine (`ML_URL`) en iyi gayretle bir tahmin istenir (`prediction`, `stage: "draft"`; malzeme kompozisyonu henüz bilinmediği için yalnızca adet tahmini, `predicted_ekp_unit: null`); ML kapalıysa sipariş yine oluşturulur, `prediction` boş kalır.

### `POST /production-orders/{id}/send-materials` — hammadde sevk et

`draft` veya `materials_sent` durumunda kabul edilir (kısmi/çok partili sevkiyat için tekrar çağrılabilir):

```json
{ "lines": [
  { "lot_id": "01a174fc-3daa-7e29-a6a6-335611b68b22", "qty": "100.000" },
  { "lot_id": "01a174fc-401c-74de-8157-9942629d7faf", "qty": "2.000" },
  { "lot_id": "01a174fc-428f-7e29-bbdd-f963c4344fe6", "qty": "5.000" }
]}
```

Her satır için `send_to_producer` (−) + `transfer_in` (+) hareket çifti ve bir `production_input` satırı oluşur; durum `materials_sent` olur. Başarılı her çağrıdan sonra (en iyi gayret) ML'e GERÇEK sevk edilen et miktarıyla tekrar sorulup yeni bir tahmin satırı eklenir — artık kesin malzeme maliyeti bilindiği için bu satırda `predicted_ekp_unit` doludur (bkz. `GET .../cost-report` örneği). Hatalar: lot aktif değilse (`blocked`/`recalled`/`consumed`) `422`; lotun ürününde `material_category` yoksa `422` (ör. `KUZU-BUTUN`); lotun tek bir depoda yeterli bakiyesi yoksa `409`; aynı lot bu siparişe ikinci kez gönderilirse `409` (bir lot, bir siparişe en fazla bir kez sevk edilir).

### `POST /production-orders/{id}/receive` — mamulü teslim al

Yalnız `materials_sent` durumunda kabul edilir:

```json
{
  "received_qty": "85",
  "output_lot_no": "SUC-2026-045",
  "service_invoice_no": "URT-2026-045-INV",
  "service_invoiced_qty": "85",
  "returns": [{ "lot_id": "01a174fc-3daa-7e29-a6a6-335611b68b22", "qty": "10.000" }]
}
```

`returns` opsiyoneldir (dış üreticiden geri gelen artan hammadde). İade edilmeyen (fire dahil) miktar `production_consume` ile düşülür ve maliyete girer. `200` yanıtı (kısaltılmış, golden `UR-001` ile birebir):

```json
{
  "status": "received",
  "material_cost": "1076.0000",
  "service_cost": "467.5000",
  "total_cost": "1543.5000",
  "ekp_unit": "18.1588",
  "output_lot_id": "01a174fc-51e3-79b8-b35c-a3b67b3b2efd",
  "output_lot_no": "SUC-2026-045",
  "warnings": []
}
```

`INVOICED_QTY_MISMATCH` (`service_invoiced_qty ≠ received_qty`) ve `QTY_VARIANCE` (`|received_qty − planned_qty| / planned_qty > %2`) uyarıları `warnings`'e eklenir. Hatalar: `materials_sent` dışı durumda `409`; iade miktarı sevk edilenden fazlaysa `422`; bir hammaddenin ürününde `material_category` tanımlı değilse `422`.

### `POST /production-orders/{id}/close` — kapat

Yalnız `received` durumunda kabul edilir. Gövde opsiyonel: `receive`'de `service_invoice_no` verilmediyse burada (`{"service_invoice_no": "..."}`) tamamlanabilir; ikisi de boşsa `422`.

### `GET /production-orders/{id}/cost-report` — maliyet raporu

`received` veya `closed` durumunda (aksi halde `422`):

```json
{
  "order_no": "URT-2026-045",
  "lines": [
    { "label": "Dana Eti", "qty": "100.000", "unit_price": "9.7000", "amount": "970.00" },
    { "label": "Sucuk Baharatı", "qty": "2.000", "unit_price": "24.0000", "amount": "48.00" },
    { "label": "Bağırsak", "qty": "5.000", "unit_price": "11.6000", "amount": "58.00" },
    { "label": "Hizmet bedeli", "qty": "85.000", "unit_price": "5.5000", "amount": "467.50" }
  ],
  "ekp_unit": "18.1588", "ekp_display": "18.16", "ekp_per_kg": "18.1588",
  "yield_input_pct": "79.44", "yield_meat_ratio": "0.8500",
  "revenue": "2116.50", "gross_profit_total": "573.00", "margin_pct": "27.07",
  "qty_variance_pct": "0.00", "warnings": [],
  "prediction": {
    "model_name": "production_baseline_supplier_mean", "baseline": true, "stage": "materials_sent",
    "predicted_delivered_qty": "84.87", "predicted_ekp_unit": "18.1783",
    "actual_delivered_qty": "85.000", "actual_ekp_unit": "18.1588",
    "delivered_qty_error_abs": "0.130", "delivered_qty_error_pct": "0.15",
    "ekp_error_abs": "-0.0195", "ekp_error_pct": "0.11"
  }
}
```

`ekp_display` yalnızca gösterim içindir; değerleme `ekp_unit` veya `total_cost` üzerinden yapılır (bkz. `docs/assumptions.md`, yuvarlama notu). `prediction.predicted_ekp_unit` **hibrit bir tahmindir**: ML'in kendisi maliyet bilmez (yalnızca fiziksel verimi/adedi tahmin eder), bu alan sunucuda `(material_cost + round2(predicted_qty × service_unit_price)) / predicted_qty` formülüyle hesaplanır. İki aşamalı: `stage: "draft"` (sipariş oluşunca, malzeme maliyeti henüz bilinmediği için `predicted_ekp_unit: null`) ve `stage: "materials_sent"` (hammadde sevki tamamlanınca, kesin malzeme maliyetiyle hesaplanır — yukarıdaki örnek bu aşamadandır). Ayrıntı: `docs/assumptions.md` ("Hibrit EKP tahmini").

### `GET /production-orders/{id}/movements`

Bu siparişe ait tüm stok hareketlerini (`send_to_producer`, `transfer_in/out`, `production_consume`, `production_in`) kronolojik döner; biçim `GET /stock-movements` ile aynıdır.

---

## Henüz uygulanmamış (sözleşme taslağı yok)

Parçalama, izlenebilirlik sorguları, geri çağırma (lot bazlı), uyarı listesi uç noktaları ve cihaz kaydı
sonraki işlerdir (bkz. [`docs/HANDOFF.md`](HANDOFF.md)). Satış/sevkiyat/iade ve bloke lot mekanizması bu
görevle tamamlandı. Şemaları `backend/migrations/000001_init.up.sql` içinde hazırdır; parçalama hesap
kuralları Go'da `backend/internal/domain` içinde (`AllocateCutting`) golden testlerle doğrulanmıştır.
