-- 000010_invoice_import: Fatura/irsaliye OCR (bkz. docs/invoice-ocr.md). OCR'in cikardigi hicbir
-- alan "dogru" diye yazilmaz; sadece ocr_json'a ONERI olarak kaydedilir. INSAN ONAYI (confirm)
-- olmadan hicbir lot/stok hareketi/alis fisi olusmaz -- purchase_receipt_id confirm'e kadar NULL'dur.
CREATE TABLE invoice_import (
  id                  CHAR(36)      NOT NULL,
  file_name           VARCHAR(255)  NOT NULL,
  file_hash           CHAR(64)      NOT NULL,      -- sha256 (ayni dosya tekrar yuklenirse fark edilsin diye)
  content_type        VARCHAR(100)  NOT NULL,
  stored_path         VARCHAR(255)  NOT NULL,       -- uploads/ altinda dosya adi (bkz. repo/invoice_import.go)
  uploaded_at         DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  status              ENUM('extracted','confirmed','discarded') NOT NULL DEFAULT 'extracted',
  ocr_json            JSON          NOT NULL,       -- ML /extract/invoice yaniti (tam, degistirilmeden)
  overall_confidence  DECIMAL(5,4)  NOT NULL,
  purchase_receipt_id CHAR(36)      NULL,           -- SADECE confirm sonrasi dolar
  confirmed_at        DATETIME(3)   NULL,
  discarded_at        DATETIME(3)   NULL,
  created_at          DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY ix_invoice_import_status (status),
  CONSTRAINT fk_invoice_import_receipt FOREIGN KEY (purchase_receipt_id) REFERENCES purchase_receipt (id)
) ENGINE=InnoDB;
