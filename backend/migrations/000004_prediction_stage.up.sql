-- 000004: tahmin kaydina asama bilgisi.
--
-- Dis uretimde tahmin birden fazla asamada alinir/guncellenir (siparis olusturulurken kaba bir
-- tahmin, hammaddeler sevk edilince gercek malzeme maliyetiyle kesinlesen bir tahmin). Her asama
-- AYRI bir prediction satiridir (onceki satir UZERINE YAZILMAZ); en son satir "guncel tahmin"dir.
-- target_type='production' icin 'draft' | 'materials_sent'; diger target_type'lar icin NULL kalabilir.
ALTER TABLE prediction
  ADD COLUMN stage VARCHAR(20) NULL AFTER target_type;
