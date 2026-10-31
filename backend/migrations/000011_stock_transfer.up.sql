-- 000011: depolar arasi transfer (ornek: ana depodan arac deposuna). Belge basligi burada; satirlar
-- ayri tablo DEGILDIR -- her satir bir transfer_out (kaynak) + transfer_in (hedef) stok hareket
-- ciftidir (ref_type='stock_transfer', ref_id=bu tablonun id'si). Defter tek dogruluk kaynagi kalir.
CREATE TABLE stock_transfer (
  id                CHAR(36)     NOT NULL,
  transfer_no       VARCHAR(40)  NOT NULL,
  from_warehouse_id CHAR(36)     NOT NULL,
  to_warehouse_id   CHAR(36)     NOT NULL,
  note              VARCHAR(400) NULL,
  created_at        DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uq_stock_transfer_no (transfer_no),
  KEY ix_stock_transfer_from (from_warehouse_id),
  KEY ix_stock_transfer_to (to_warehouse_id),
  CONSTRAINT fk_st_from FOREIGN KEY (from_warehouse_id) REFERENCES warehouse (id),
  CONSTRAINT fk_st_to   FOREIGN KEY (to_warehouse_id)   REFERENCES warehouse (id),
  CONSTRAINT ck_st_distinct CHECK (from_warehouse_id <> to_warehouse_id)
) ENGINE=InnoDB;
