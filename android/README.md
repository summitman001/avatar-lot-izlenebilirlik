# Android (Kotlin, Room/SQLite, offline)

Bu klasör şimdilik boş (`.gitkeep`). İçeriğini Android geliştiricisi yazacak.

Okunacaklar:
- `docs/api-contract.md` — endpoint'ler, decimal-string ve UUIDv7 kuralları
- `docs/sync.md` — offline senkron sözleşmesi (`/sync/push`, `/sync/pull`, `op_id`, `blocked_lot_ids`)
- `docs/assumptions.md` — yuvarlama kuralları (Go ile birebir aynı olmalı) ve golden dosyalar (`docs/golden/*.json`)
- `backend/api/openapi.yaml` — OpenAPI 3.1 sözleşmesi

Kritik kurallar: para/miktar asla `Double`/`Float` değil (`BigDecimal`, Room'da TEXT/INTEGER-kuruş); tüm ID'ler UUIDv7 ve cihazda üretilebilir; her yazma işlemi bir `op_id` taşır (idempotency).
