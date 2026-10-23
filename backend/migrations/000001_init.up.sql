-- 000001_init: ilk sema.
--
-- Kurallar (docs/data-model.md, docs/assumptions.md):
--   * Tum ID'ler UUIDv7, CHAR(36). Insan okunur numaralar ayri UNIQUE kolon.
--   * Tutar / birim maliyet DECIMAL(14,4), miktar DECIMAL(14,3). Float yok.
--   * Zamanlar UTC (sunucu +00:00).
--   * stock_movement DEGISTIRILEMEZ bir defterdir; duzeltme 'reversal' kaydiyla yapilir.

-- ---------------------------------------------------------------------------
-- Ana veriler
-- ---------------------------------------------------------------------------

CREATE TABLE item (
  id                 CHAR(36)      NOT NULL,
  code               VARCHAR(40)   NOT NULL,
  name               VARCHAR(160)  NOT NULL,
  item_type          ENUM('raw','finished','cut_part','by_product','waste') NOT NULL,
  base_uom           ENUM('kg','adet') NOT NULL,
  unit_weight_kg     DECIMAL(14,3) NULL,
  default_sale_price DECIMAL(14,4) NULL,
  is_active          BOOLEAN       NOT NULL DEFAULT TRUE,
  created_at         DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uq_item_code (code)
) ENGINE=InnoDB;

CREATE TABLE supplier (
  id            CHAR(36)     NOT NULL,
  code          VARCHAR(40)  NOT NULL,
  name          VARCHAR(160) NOT NULL,
  supplier_type ENUM('material','external_producer','livestock') NOT NULL,
  contact_name  VARCHAR(120) NULL,
  phone         VARCHAR(40)  NULL,
  email         VARCHAR(160) NULL,
  address       VARCHAR(400) NULL,
  created_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uq_supplier_code (code)
) ENGINE=InnoDB;

CREATE TABLE customer (
  id            CHAR(36)     NOT NULL,
  code          VARCHAR(40)  NOT NULL,
  name          VARCHAR(160) NOT NULL,
  customer_type ENUM('market','wholesale','end_customer') NOT NULL,
  contact_name  VARCHAR(120) NULL,
  phone         VARCHAR(40)  NULL,
  email         VARCHAR(160) NULL,
  address       VARCHAR(400) NULL,
  created_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uq_customer_code (code)
) ENGINE=InnoDB;

CREATE TABLE warehouse (
  id             CHAR(36)     NOT NULL,
  code           VARCHAR(40)  NOT NULL,
  name           VARCHAR(160) NOT NULL,
  warehouse_type ENUM('internal','external_producer','quarantine','vehicle') NOT NULL,
  created_at     DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uq_warehouse_code (code)
) ENGINE=InnoDB;

CREATE TABLE device (
  id           CHAR(36)     NOT NULL,
  name         VARCHAR(120) NOT NULL,
  warehouse_id CHAR(36)     NOT NULL,
  last_sync_at DATETIME(3)  NULL,
  created_at   DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  CONSTRAINT fk_device_warehouse FOREIGN KEY (warehouse_id) REFERENCES warehouse (id)
) ENGINE=InnoDB;

-- ---------------------------------------------------------------------------
-- Lot ve stok
-- ---------------------------------------------------------------------------

CREATE TABLE lot (
  id                     CHAR(36)      NOT NULL,
  lot_no                 VARCHAR(40)   NOT NULL,
  item_id                CHAR(36)      NOT NULL,
  supplier_id            CHAR(36)      NULL,
  origin_type            ENUM('purchase','production','cutting','reprocess','return') NOT NULL,
  origin_id              CHAR(36)      NULL,
  received_or_produced_at DATE         NOT NULL,
  expiry_date            DATE          NULL,
  initial_qty            DECIMAL(14,3) NOT NULL,
  uom                    ENUM('kg','adet') NOT NULL,
  unit_cost              DECIMAL(14,4) NOT NULL,
  status                 ENUM('active','blocked','recalled','consumed') NOT NULL DEFAULT 'active',
  blocked_reason         VARCHAR(400)  NULL,
  created_at             DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uq_lot_no (lot_no),
  KEY ix_lot_item (item_id),
  KEY ix_lot_supplier (supplier_id),
  KEY ix_lot_status (status),
  KEY ix_lot_origin (origin_type, origin_id),
  CONSTRAINT fk_lot_item     FOREIGN KEY (item_id)     REFERENCES item (id),
  CONSTRAINT fk_lot_supplier FOREIGN KEY (supplier_id) REFERENCES supplier (id),
  CONSTRAINT ck_lot_qty CHECK (initial_qty >= 0)
) ENGINE=InnoDB;

-- Soy agaci: cokludan cokluya, miktarli.
CREATE TABLE lot_link (
  id              CHAR(36)      NOT NULL,
  parent_lot_id   CHAR(36)      NOT NULL,
  child_lot_id    CHAR(36)      NOT NULL,
  qty_from_parent DECIMAL(14,3) NOT NULL,
  uom             ENUM('kg','adet') NOT NULL,
  process_type    ENUM('production','cutting','reprocess','repack') NOT NULL,
  process_id      CHAR(36)      NOT NULL,
  created_at      DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY ix_lot_link_parent (parent_lot_id),
  KEY ix_lot_link_child (child_lot_id),
  KEY ix_lot_link_process (process_type, process_id),
  CONSTRAINT fk_lot_link_parent FOREIGN KEY (parent_lot_id) REFERENCES lot (id),
  CONSTRAINT fk_lot_link_child  FOREIGN KEY (child_lot_id)  REFERENCES lot (id),
  CONSTRAINT ck_lot_link_distinct CHECK (parent_lot_id <> child_lot_id)
) ENGINE=InnoDB;

-- DEGISTIRILEMEZ DEFTER. qty isaretli: giris +, cikis -.
CREATE TABLE stock_movement (
  id                    CHAR(36)      NOT NULL,
  lot_id                CHAR(36)      NOT NULL,
  warehouse_id          CHAR(36)      NOT NULL,
  movement_type         ENUM('purchase_in','send_to_producer','production_consume','production_in',
                             'cutting_consume','cutting_in','transfer_out','transfer_in','sale_out',
                             'return_in','waste_out','reprocess_consume','reprocess_in',
                             'adjustment','reversal') NOT NULL,
  qty                   DECIMAL(14,3) NOT NULL,
  unit_cost             DECIMAL(14,4) NOT NULL,
  ref_type              VARCHAR(40)   NOT NULL,
  ref_id                CHAR(36)      NOT NULL,
  reverses_movement_id  CHAR(36)      NULL,
  device_id             CHAR(36)      NULL,
  client_created_at     DATETIME(3)   NULL,
  note                  VARCHAR(400)  NULL,
  created_at            DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  -- Bir hareket en fazla bir kez geri alinabilir (NULL'lar cakismaz).
  UNIQUE KEY uq_stock_movement_reverses (reverses_movement_id),
  KEY ix_stock_movement_lot (lot_id, warehouse_id),
  KEY ix_stock_movement_ref (ref_type, ref_id),
  KEY ix_stock_movement_created (created_at),
  CONSTRAINT fk_sm_lot       FOREIGN KEY (lot_id)               REFERENCES lot (id),
  CONSTRAINT fk_sm_warehouse FOREIGN KEY (warehouse_id)         REFERENCES warehouse (id),
  CONSTRAINT fk_sm_reverses  FOREIGN KEY (reverses_movement_id) REFERENCES stock_movement (id),
  CONSTRAINT fk_sm_device    FOREIGN KEY (device_id)            REFERENCES device (id),
  -- Isaret kurali: giris tipleri +, cikis tipleri -, adjustment/reversal herhangi isaretli ama sifir olamaz.
  CONSTRAINT ck_sm_sign CHECK (
       (movement_type IN ('purchase_in','production_in','cutting_in','transfer_in','return_in','reprocess_in') AND qty > 0)
    OR (movement_type IN ('send_to_producer','production_consume','cutting_consume','transfer_out','sale_out','waste_out','reprocess_consume') AND qty < 0)
    OR (movement_type IN ('adjustment','reversal') AND qty <> 0)
  )
) ENGINE=InnoDB;

CREATE TRIGGER trg_stock_movement_no_update BEFORE UPDATE ON stock_movement
FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'stock_movement is immutable: use a reversal movement';

CREATE TRIGGER trg_stock_movement_no_delete BEFORE DELETE ON stock_movement
FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'stock_movement is immutable: use a reversal movement';

CREATE VIEW v_lot_balance AS
SELECT lot_id, warehouse_id, SUM(qty) AS qty_on_hand
FROM stock_movement
GROUP BY lot_id, warehouse_id;

-- ---------------------------------------------------------------------------
-- Alis
-- ---------------------------------------------------------------------------

CREATE TABLE purchase_receipt (
  id          CHAR(36)    NOT NULL,
  doc_no      VARCHAR(40) NOT NULL,
  supplier_id CHAR(36)    NOT NULL,
  invoice_no  VARCHAR(60) NULL,
  received_at DATE        NOT NULL,
  created_at  DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uq_purchase_receipt_doc (doc_no),
  KEY ix_purchase_receipt_supplier (supplier_id),
  CONSTRAINT fk_pr_supplier FOREIGN KEY (supplier_id) REFERENCES supplier (id)
) ENGINE=InnoDB;

CREATE TABLE purchase_receipt_line (
  id                 CHAR(36)      NOT NULL,
  receipt_id         CHAR(36)      NOT NULL,
  item_id            CHAR(36)      NOT NULL,
  lot_id             CHAR(36)      NOT NULL,
  qty                DECIMAL(14,3) NOT NULL,
  unit_price         DECIMAL(14,4) NOT NULL,
  line_total         DECIMAL(14,4) NOT NULL,           -- hesaplanan: round2(qty * unit_price)
  declared_line_total DECIMAL(14,4) NULL,              -- belgede yazan
  has_mismatch       BOOLEAN       NOT NULL DEFAULT FALSE, -- hesaplanan <> beyan
  created_at         DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uq_prl_lot (lot_id),
  KEY ix_prl_receipt (receipt_id),
  KEY ix_prl_item (item_id),
  KEY ix_prl_mismatch (has_mismatch),
  CONSTRAINT fk_prl_receipt FOREIGN KEY (receipt_id) REFERENCES purchase_receipt (id),
  CONSTRAINT fk_prl_item    FOREIGN KEY (item_id)    REFERENCES item (id),
  CONSTRAINT fk_prl_lot     FOREIGN KEY (lot_id)     REFERENCES lot (id)
) ENGINE=InnoDB;

-- ---------------------------------------------------------------------------
-- Dis uretim (Gorev 1)
-- ---------------------------------------------------------------------------

CREATE TABLE production_order (
  id                   CHAR(36)      NOT NULL,
  order_no             VARCHAR(40)   NOT NULL,
  product_item_id      CHAR(36)      NOT NULL,
  producer_supplier_id CHAR(36)      NOT NULL,
  external_warehouse_id CHAR(36)     NOT NULL,
  planned_qty          DECIMAL(14,3) NOT NULL,
  received_qty         DECIMAL(14,3) NULL,
  service_unit_price   DECIMAL(14,4) NOT NULL,
  service_invoice_no   VARCHAR(60)   NULL,
  service_invoiced_qty DECIMAL(14,3) NULL,
  status               ENUM('draft','materials_sent','received','closed') NOT NULL DEFAULT 'draft',
  output_lot_id        CHAR(36)      NULL,
  material_cost        DECIMAL(14,4) NULL,
  service_cost         DECIMAL(14,4) NULL,
  total_cost           DECIMAL(14,4) NULL,
  ekp_unit             DECIMAL(14,4) NULL,
  sale_price           DECIMAL(14,4) NULL,
  closed_at            DATETIME(3)   NULL,
  created_at           DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uq_production_order_no (order_no),
  KEY ix_po_product (product_item_id),
  KEY ix_po_producer (producer_supplier_id),
  KEY ix_po_status (status),
  CONSTRAINT fk_po_product  FOREIGN KEY (product_item_id)       REFERENCES item (id),
  CONSTRAINT fk_po_producer FOREIGN KEY (producer_supplier_id)  REFERENCES supplier (id),
  CONSTRAINT fk_po_wh       FOREIGN KEY (external_warehouse_id) REFERENCES warehouse (id),
  CONSTRAINT fk_po_out_lot  FOREIGN KEY (output_lot_id)         REFERENCES lot (id)
) ENGINE=InnoDB;

CREATE TABLE production_input (
  id                  CHAR(36)      NOT NULL,
  production_order_id CHAR(36)      NOT NULL,
  lot_id              CHAR(36)      NOT NULL,
  qty_sent            DECIMAL(14,3) NOT NULL,
  qty_consumed        DECIMAL(14,3) NULL,
  unit_cost_at_send   DECIMAL(14,4) NOT NULL,
  created_at          DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY ix_pi_order (production_order_id),
  KEY ix_pi_lot (lot_id),
  CONSTRAINT fk_pi_order FOREIGN KEY (production_order_id) REFERENCES production_order (id),
  CONSTRAINT fk_pi_lot   FOREIGN KEY (lot_id)               REFERENCES lot (id)
) ENGINE=InnoDB;

-- ---------------------------------------------------------------------------
-- Parcalama (Gorev 2)
-- ---------------------------------------------------------------------------

CREATE TABLE cutting_recipe (
  id          CHAR(36)     NOT NULL,
  code        VARCHAR(40)  NOT NULL,
  name        VARCHAR(160) NOT NULL,
  animal_type VARCHAR(40)  NOT NULL,
  created_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uq_cutting_recipe_code (code)
) ENGINE=InnoDB;

CREATE TABLE cutting_recipe_line (
  id                   CHAR(36)      NOT NULL,
  recipe_id            CHAR(36)      NOT NULL,
  part_item_id         CHAR(36)      NOT NULL,
  expected_ratio       DECIMAL(7,4)  NOT NULL,
  reference_sale_price DECIMAL(14,4) NOT NULL,
  is_by_product        BOOLEAN       NOT NULL DEFAULT FALSE,
  is_waste             BOOLEAN       NOT NULL DEFAULT FALSE,
  created_at           DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uq_crl_recipe_part (recipe_id, part_item_id),
  CONSTRAINT fk_crl_recipe FOREIGN KEY (recipe_id)    REFERENCES cutting_recipe (id),
  CONSTRAINT fk_crl_part   FOREIGN KEY (part_item_id) REFERENCES item (id)
) ENGINE=InnoDB;

CREATE TABLE cutting_order (
  id               CHAR(36)      NOT NULL,
  order_no         VARCHAR(40)   NOT NULL,
  recipe_id        CHAR(36)      NOT NULL,
  input_lot_id     CHAR(36)      NOT NULL,
  input_weight_kg  DECIMAL(14,3) NOT NULL,
  total_input_cost DECIMAL(14,4) NOT NULL,
  allocation_method ENUM('quantity','value') NOT NULL,
  status           ENUM('draft','closed') NOT NULL DEFAULT 'draft',
  cut_at           DATETIME(3)   NOT NULL,
  balance_diff_kg  DECIMAL(14,3) NULL,
  notes            TEXT          NULL,
  created_at       DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uq_cutting_order_no (order_no),
  KEY ix_co_input_lot (input_lot_id),
  CONSTRAINT fk_co_recipe FOREIGN KEY (recipe_id)    REFERENCES cutting_recipe (id),
  CONSTRAINT fk_co_lot    FOREIGN KEY (input_lot_id) REFERENCES lot (id)
) ENGINE=InnoDB;

CREATE TABLE cutting_output (
  id                CHAR(36)      NOT NULL,
  cutting_order_id  CHAR(36)      NOT NULL,
  part_item_id      CHAR(36)      NOT NULL,
  output_lot_id     CHAR(36)      NULL,
  actual_weight_kg  DECIMAL(14,3) NOT NULL,
  sale_price_basis  DECIMAL(14,4) NOT NULL,
  allocated_cost    DECIMAL(14,4) NULL,
  unit_cost         DECIMAL(14,4) NULL,
  created_at        DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY ix_cout_order (cutting_order_id),
  CONSTRAINT fk_cout_order FOREIGN KEY (cutting_order_id) REFERENCES cutting_order (id),
  CONSTRAINT fk_cout_part  FOREIGN KEY (part_item_id)     REFERENCES item (id),
  CONSTRAINT fk_cout_lot   FOREIGN KEY (output_lot_id)    REFERENCES lot (id)
) ENGINE=InnoDB;

-- ---------------------------------------------------------------------------
-- Satis, sevkiyat, iade
-- ---------------------------------------------------------------------------

CREATE TABLE sales_order (
  id           CHAR(36)      NOT NULL,
  order_no     VARCHAR(40)   NOT NULL,
  customer_id  CHAR(36)      NOT NULL,
  device_id    CHAR(36)      NULL,
  warehouse_id CHAR(36)      NOT NULL,
  sold_at      DATETIME(3)   NOT NULL,
  status       ENUM('draft','confirmed','cancelled') NOT NULL DEFAULT 'draft',
  total_amount DECIMAL(14,4) NOT NULL DEFAULT 0,
  created_at   DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uq_sales_order_no (order_no),
  KEY ix_so_customer (customer_id),
  CONSTRAINT fk_so_customer  FOREIGN KEY (customer_id)  REFERENCES customer (id),
  CONSTRAINT fk_so_device    FOREIGN KEY (device_id)    REFERENCES device (id),
  CONSTRAINT fk_so_warehouse FOREIGN KEY (warehouse_id) REFERENCES warehouse (id)
) ENGINE=InnoDB;

CREATE TABLE sales_line (
  id                CHAR(36)      NOT NULL,
  sales_order_id    CHAR(36)      NOT NULL,
  item_id           CHAR(36)      NOT NULL,
  lot_id            CHAR(36)      NOT NULL,
  qty               DECIMAL(14,3) NOT NULL,
  unit_price        DECIMAL(14,4) NOT NULL,
  line_total        DECIMAL(14,4) NOT NULL,
  stock_movement_id CHAR(36)      NULL,
  recall_violation  BOOLEAN       NOT NULL DEFAULT FALSE,
  created_at        DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY ix_sl_order (sales_order_id),
  KEY ix_sl_lot (lot_id),
  CONSTRAINT fk_sl_order FOREIGN KEY (sales_order_id)    REFERENCES sales_order (id),
  CONSTRAINT fk_sl_item  FOREIGN KEY (item_id)           REFERENCES item (id),
  CONSTRAINT fk_sl_lot   FOREIGN KEY (lot_id)            REFERENCES lot (id),
  CONSTRAINT fk_sl_mv    FOREIGN KEY (stock_movement_id) REFERENCES stock_movement (id)
) ENGINE=InnoDB;

CREATE TABLE shipment (
  id             CHAR(36)    NOT NULL,
  shipment_no    VARCHAR(40) NOT NULL,
  customer_id    CHAR(36)    NOT NULL,
  sales_order_id CHAR(36)    NULL,
  shipped_at     DATETIME(3) NOT NULL,
  vehicle        VARCHAR(40) NULL,
  created_at     DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uq_shipment_no (shipment_no),
  KEY ix_shipment_customer (customer_id),
  CONSTRAINT fk_sh_customer FOREIGN KEY (customer_id)    REFERENCES customer (id),
  CONSTRAINT fk_sh_order    FOREIGN KEY (sales_order_id) REFERENCES sales_order (id)
) ENGINE=InnoDB;

CREATE TABLE shipment_line (
  id          CHAR(36)      NOT NULL,
  shipment_id CHAR(36)      NOT NULL,
  lot_id      CHAR(36)      NOT NULL,
  qty         DECIMAL(14,3) NOT NULL,
  created_at  DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY ix_shl_shipment (shipment_id),
  KEY ix_shl_lot (lot_id),
  CONSTRAINT fk_shl_shipment FOREIGN KEY (shipment_id) REFERENCES shipment (id),
  CONSTRAINT fk_shl_lot      FOREIGN KEY (lot_id)      REFERENCES lot (id)
) ENGINE=InnoDB;

CREATE TABLE customer_return (
  id            CHAR(36)      NOT NULL,
  return_no     VARCHAR(40)   NOT NULL,
  customer_id   CHAR(36)      NOT NULL,
  sales_line_id CHAR(36)      NULL,
  lot_id        CHAR(36)      NOT NULL,
  qty           DECIMAL(14,3) NOT NULL,
  reason        VARCHAR(400)  NOT NULL,
  returned_at   DATETIME(3)   NOT NULL,
  created_at    DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uq_customer_return_no (return_no),
  KEY ix_cr_lot (lot_id),
  CONSTRAINT fk_cr_customer FOREIGN KEY (customer_id)   REFERENCES customer (id),
  CONSTRAINT fk_cr_line     FOREIGN KEY (sales_line_id) REFERENCES sales_line (id),
  CONSTRAINT fk_cr_lot      FOREIGN KEY (lot_id)        REFERENCES lot (id)
) ENGINE=InnoDB;

-- ---------------------------------------------------------------------------
-- Geri cagirma (Gorev 3)
-- ---------------------------------------------------------------------------

CREATE TABLE recall (
  id          CHAR(36)     NOT NULL,
  recall_no   VARCHAR(40)  NOT NULL,
  root_lot_id CHAR(36)     NOT NULL,
  reason      VARCHAR(400) NOT NULL,
  status      ENUM('open','closed') NOT NULL DEFAULT 'open',
  opened_at   DATETIME(3)  NOT NULL,
  closed_at   DATETIME(3)  NULL,
  created_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uq_recall_no (recall_no),
  KEY ix_recall_root (root_lot_id),
  CONSTRAINT fk_recall_lot FOREIGN KEY (root_lot_id) REFERENCES lot (id)
) ENGINE=InnoDB;

CREATE TABLE recall_lot (
  id         CHAR(36)    NOT NULL,
  recall_id  CHAR(36)    NOT NULL,
  lot_id     CHAR(36)    NOT NULL,
  depth      INT         NOT NULL,
  path       TEXT        NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uq_recall_lot (recall_id, lot_id),
  KEY ix_rl_lot (lot_id),
  CONSTRAINT fk_rl_recall FOREIGN KEY (recall_id) REFERENCES recall (id),
  CONSTRAINT fk_rl_lot    FOREIGN KEY (lot_id)    REFERENCES lot (id)
) ENGINE=InnoDB;

CREATE TABLE recall_customer (
  id             CHAR(36)      NOT NULL,
  recall_id      CHAR(36)      NOT NULL,
  customer_id    CHAR(36)      NOT NULL,
  lot_id         CHAR(36)      NOT NULL,
  qty_shipped    DECIMAL(14,3) NOT NULL,
  qty_returned   DECIMAL(14,3) NOT NULL DEFAULT 0,
  contact_status ENUM('pending','contacted','confirmed') NOT NULL DEFAULT 'pending',
  notes          VARCHAR(400)  NULL,
  created_at     DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uq_rc (recall_id, customer_id, lot_id),
  CONSTRAINT fk_rc_recall   FOREIGN KEY (recall_id)   REFERENCES recall (id),
  CONSTRAINT fk_rc_customer FOREIGN KEY (customer_id) REFERENCES customer (id),
  CONSTRAINT fk_rc_lot      FOREIGN KEY (lot_id)      REFERENCES lot (id)
) ENGINE=InnoDB;

-- ---------------------------------------------------------------------------
-- ML ve uyari
-- ---------------------------------------------------------------------------

CREATE TABLE prediction (
  id             CHAR(36)    NOT NULL,
  model_name     VARCHAR(80) NOT NULL,
  model_version  VARCHAR(40) NOT NULL,
  target_type    ENUM('production','cutting') NOT NULL,
  ref_id         CHAR(36)    NOT NULL,
  input_json     JSON        NOT NULL,
  predicted_json JSON        NOT NULL,
  actual_json    JSON        NULL,
  error_json     JSON        NULL,
  created_at     DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY ix_prediction_ref (target_type, ref_id)
) ENGINE=InnoDB;

CREATE TABLE anomaly_alert (
  id           CHAR(36)     NOT NULL,
  alert_type   VARCHAR(60)  NOT NULL,
  severity     ENUM('info','warning','critical') NOT NULL,
  ref_type     VARCHAR(40)  NOT NULL,
  ref_id       CHAR(36)     NOT NULL,
  message      VARCHAR(500) NOT NULL,
  acknowledged BOOLEAN      NOT NULL DEFAULT FALSE,
  created_at   DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY ix_alert_ref (ref_type, ref_id),
  KEY ix_alert_open (acknowledged, severity)
) ENGINE=InnoDB;

-- ---------------------------------------------------------------------------
-- Senkron
-- ---------------------------------------------------------------------------

-- Idempotency: ayni op_id ikinci kez gelirse islem tekrarlanmaz.
CREATE TABLE processed_request (
  op_id         CHAR(36)    NOT NULL,
  device_id     CHAR(36)    NOT NULL,
  status        VARCHAR(20) NOT NULL,
  response_json JSON        NOT NULL,
  processed_at  DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (op_id),
  KEY ix_processed_device (device_id)
) ENGINE=InnoDB;

-- Sunucudan cihaza degisiklik akisi (pull). seq monoton artar.
CREATE TABLE change_log (
  seq          BIGINT      NOT NULL AUTO_INCREMENT,
  entity       VARCHAR(40) NOT NULL,
  entity_id    CHAR(36)    NOT NULL,
  action       ENUM('upsert','delete') NOT NULL,
  payload_json JSON        NOT NULL,
  created_at   DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (seq),
  KEY ix_change_log_entity (entity, entity_id)
) ENGINE=InnoDB;
