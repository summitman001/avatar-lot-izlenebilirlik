-- 000012: Gorev 3 eksikleri (bkz. docs/traceability.md).
--
-- reprocess_order: "yeniden isleme" -- bir veya daha fazla lot tuketilip (reprocess_consume) yeni bir
-- lot uretilir (reprocess_in). Girdi->cikti baglantisi lot_link'e process_type='reprocess' ile yazilir;
-- boylece ileri/geri izleme ve geri cagirma yeniden islenmis lotlari da kapsar.
CREATE TABLE reprocess_order (
  id            CHAR(36)      NOT NULL,
  order_no      VARCHAR(40)   NOT NULL,
  warehouse_id  CHAR(36)      NOT NULL,
  output_lot_id CHAR(36)      NULL,
  total_cost    DECIMAL(14,4) NOT NULL DEFAULT 0,
  note          VARCHAR(400)  NULL,
  created_at    DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uq_reprocess_order_no (order_no),
  CONSTRAINT fk_ro_warehouse  FOREIGN KEY (warehouse_id)  REFERENCES warehouse (id),
  CONSTRAINT fk_ro_output_lot FOREIGN KEY (output_lot_id) REFERENCES lot (id)
) ENGINE=InnoDB;

-- downstream_distribution: "ikinci kademe dagitim" -- bir musterinin (market) aldigi lottan kendi
-- alicilarina dagittigi miktar. Yalnizca KAYDEDILMISSE izleme/geri cagirmada kesin olarak gosterilir.
CREATE TABLE downstream_distribution (
  id              CHAR(36)      NOT NULL,
  customer_id     CHAR(36)      NOT NULL,
  lot_id          CHAR(36)      NOT NULL,
  recipient_name  VARCHAR(160)  NOT NULL,
  recipient_phone VARCHAR(40)   NULL,
  qty             DECIMAL(14,3) NOT NULL,
  distributed_at  DATE          NOT NULL,
  note            VARCHAR(400)  NULL,
  created_at      DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY ix_dd_customer_lot (customer_id, lot_id),
  KEY ix_dd_lot (lot_id),
  CONSTRAINT fk_dd_customer FOREIGN KEY (customer_id) REFERENCES customer (id),
  CONSTRAINT fk_dd_lot      FOREIGN KEY (lot_id)      REFERENCES lot (id),
  CONSTRAINT ck_dd_qty CHECK (qty > 0)
) ENGINE=InnoDB;
