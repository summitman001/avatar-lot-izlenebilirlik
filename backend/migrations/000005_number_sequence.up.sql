-- 000005: atomik numara uretimi (order_no, lot_no gibi "<prefix>-<yil>-NNN" numaralari icin).
--
-- Onceki yaklasim (mevcut en buyuk NNN'i okuyup +1 yapmak) es zamanli iki istekte ayni numarayi
-- uretebiliyordu (ikincisi sadece UNIQUE ihlaliyle 409 aliyordu, otomatik yeniden denemiyordu).
-- Bu tablo + INSERT ... ON DUPLICATE KEY UPDATE deseniyle (bkz. backend/internal/repo/production.go,
-- nextSequenceNo) artis atomiktir: tek bir INSERT satiri kilitler ve artirir, es zamanli istekler
-- sirayla serilesir, hic kimse ayni numarayi almaz.
--
-- Kolon adi NOT: `last_value` degil `seq_value` -- LAST_VALUE() MySQL 8.0'da pencere fonksiyonu
-- oldugu icin ayrilmis (reserved) sozcuktur, backtick'le bile kolon adi olarak kullanilamiyor.
CREATE TABLE number_sequence (
  prefix    VARCHAR(20) NOT NULL,
  year      SMALLINT    NOT NULL,
  seq_value INT         NOT NULL DEFAULT 0,
  PRIMARY KEY (prefix, year)
) ENGINE=InnoDB;
