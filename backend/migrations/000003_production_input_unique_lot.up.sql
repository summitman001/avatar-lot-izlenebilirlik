-- 000003: bir lot, bir uretim siparisine en fazla bir sevk satiriyla gonderilebilir.
--
-- Bu kisit olmadan ayni lot ikinci bir send-materials cagrisinda tekrar sevk edilebilir ve receive
-- asamasinda hangi "qty_sent"in hangi hareket ciftine ait oldugu belirsizlesir (bkz. backend/internal/
-- repo/production.go, sendSourceWarehouse). Ayni lotun kalanini sevk etmek icin yeni bir lot
-- (örn. kismi alim) kullanilmalidir.
ALTER TABLE production_input
  ADD UNIQUE KEY uq_production_input_order_lot (production_order_id, lot_id);
