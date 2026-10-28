-- anomaly_alert'i genel okuma uc noktasi + Go tarafindan mukerrer-onleyen upsert icin genisletir
-- (bkz. docs/anomaly-integration.md). evidence_json: ML'den/kural tabanli kontrolden gelen sayisal
-- kanit (yalniz string degerler); note: PATCH /anomaly-alerts/{id} ile okundu isaretlerken eklenen not;
-- updated_at: ayni acik (acknowledged=false) uyari tekrar tetiklendiginde mukerrer kayit yerine
-- guncellenen satirin zamani.
ALTER TABLE anomaly_alert
  ADD COLUMN evidence_json JSON NULL AFTER message,
  ADD COLUMN note VARCHAR(500) NULL AFTER acknowledged,
  ADD COLUMN updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3) AFTER created_at,
  ADD KEY ix_alert_dedupe (ref_type, ref_id, alert_type, acknowledged);
