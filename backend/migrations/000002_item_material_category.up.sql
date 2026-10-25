-- 000002: hammadde kategorisi (Gorev 1 maliyet hesabi icin).
--
-- docs/assumptions.md Gorev 1 kurallari: her sevk edilen hammadde satiri meat|spice|casing|other
-- kategorisinde olmak zorunda (domain.CalculateProductionCost bunu zorunlu kilar). Kategori urunun
-- kendi ozelligidir (lot veya siparise gore degismez), bu yuzden item tablosuna eklenir.
--
-- Yalnizca item_type='raw' icin anlamlidir; digerlerinde NULL kalir.
ALTER TABLE item
  ADD COLUMN material_category ENUM('meat','spice','casing','other') NULL AFTER base_uom;
