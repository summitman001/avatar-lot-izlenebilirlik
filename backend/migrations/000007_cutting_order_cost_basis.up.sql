-- 000007: parcalama siparisi (Gorev 2) icin maliyet esasi/uyusmazlik ve kapanis alanlari.
--
-- cost_basis/resolution_note siparis OLUSTURULURKEN sabitlenir (bkz. docs/assumptions.md, "Gorev 2"):
-- lotun alisinda PURCHASE_TOTAL_MISMATCH varsa ikisi de ZORUNLUDUR, yoksa closable=false kalir
-- (MISMATCH_RESOLUTION_MISSING). closable/balance_diff_kg her outputs guncellemesinde yeniden
-- hesaplanip buraya yazilir (GET listesinde taze AllocateCutting cagirmadan gosterilebilsin diye).
ALTER TABLE cutting_order
  ADD COLUMN cost_basis      ENUM('computed','declared') NULL AFTER allocation_method,
  ADD COLUMN resolution_note VARCHAR(1000) NULL AFTER cost_basis,
  ADD COLUMN closable  BOOLEAN     NOT NULL DEFAULT FALSE AFTER balance_diff_kg,
  ADD COLUMN closed_at DATETIME(3) NULL AFTER closable;

-- Fiili agirliklar draft iken birden cok kez guncellenebilir (UPSERT); parca basina tek satir.
ALTER TABLE cutting_output
  ADD UNIQUE KEY uq_cout_order_part (cutting_order_id, part_item_id);
