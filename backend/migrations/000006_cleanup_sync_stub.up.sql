-- 000006: /sync/push gercek islemeye gectiginde eski stub kayitlarini temizler.
--
-- Stub donemde (bkz. HANDOFF.md, "Stub / TODO") her push islemi payload islenmeden
-- processed_request'e response_json.stub=true ile yaziliyordu. Bu kayitlar kalirsa cihazlarin
-- yeniden gonderecegi ayni op_id'ler "duplicate" sayilip hic islenmez. Idempotent: ikinci calistirmada
-- satir bulunmaz, no-op.
DELETE FROM processed_request WHERE JSON_EXTRACT(response_json, '$.stub') = TRUE;
