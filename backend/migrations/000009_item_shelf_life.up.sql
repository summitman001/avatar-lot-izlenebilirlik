-- 000009: urun raf omru (gun). Lot SKT'si verilmezse alis/uretim kabulu/parcalama ciktisinda
-- lot tarihi + shelf_life_days olarak turetilir (bkz. domain.ExpiryFrom, docs/assumptions.md).
-- NULL = raf omru tanimsiz (lot SKT'siz kalir; or. fire).
ALTER TABLE item
  ADD COLUMN shelf_life_days SMALLINT UNSIGNED NULL AFTER default_sale_price;
