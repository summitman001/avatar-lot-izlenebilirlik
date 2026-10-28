ALTER TABLE anomaly_alert
  DROP KEY ix_alert_dedupe,
  DROP COLUMN updated_at,
  DROP COLUMN note,
  DROP COLUMN evidence_json;
