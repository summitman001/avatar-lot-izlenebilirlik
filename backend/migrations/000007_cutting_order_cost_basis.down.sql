ALTER TABLE cutting_output
  DROP KEY uq_cout_order_part;

ALTER TABLE cutting_order
  DROP COLUMN cost_basis,
  DROP COLUMN resolution_note,
  DROP COLUMN closable,
  DROP COLUMN closed_at;
