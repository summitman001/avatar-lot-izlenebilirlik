package domain

import "time"

// Anomali uyarilari (anomaly_alert): bes ML kurali (VERIM_DUSUK, FIRE_YUKSEK, MALIYET_ARTISI,
// EKP_SAPMA, KUTLE_DENGESI - bkz. docs/anomaly-integration.md) + Go tarafinda her zaman calisan iki
// kural tabanli kontrol (KUTLE_DENGESI deterministik hali, NEGATIVE_STOCK_OFFLINE). SADECE
// BILGILENDIRMEDIR: hicbir anomali bir lotu bloke etmez veya bir islemi reddetmez (bkz.
// docs/assumptions.md).

var AlertSeverities = set("info", "warning", "critical")

// AlertRefTypes: anomaly_alert.ref_type icin gecerli degerler (ref_no cozumlemesinde de kullanilir,
// bkz. repo/anomaly.go loadAlertRefNo).
var AlertRefTypes = set("production_order", "cutting_order", "purchase_receipt", "sales_line")

// AnomalyAlert GET /anomaly-alerts(/{id}) ve ilgili emir detaylarina (production/cutting-orders)
// gömülen görünümdür.
type AnomalyAlert struct {
	ID           string            `json:"id"`
	AlertType    string            `json:"alert_type"`
	Severity     string            `json:"severity"`
	RefType      string            `json:"ref_type"`
	RefID        string            `json:"ref_id"`
	RefNo        *string           `json:"ref_no"`
	Message      string            `json:"message"`
	Evidence     map[string]string `json:"evidence,omitempty"`
	Acknowledged bool              `json:"acknowledged"`
	Note         *string           `json:"note"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

// AnomalyAlertSummary GET /anomaly-alerts/summary yanitidir: severity bazinda ACIK (acknowledged=false)
// uyari sayisi (dashboard ozet karti icin).
type AnomalyAlertSummary struct {
	Critical int `json:"critical"`
	Warning  int `json:"warning"`
	Info     int `json:"info"`
	Total    int `json:"total"`
}

// PatchAnomalyAlertRequest PATCH /anomaly-alerts/{id} govdesi.
type PatchAnomalyAlertRequest struct {
	Acknowledged *bool   `json:"acknowledged"`
	Note         *string `json:"note"`
}

func (r *PatchAnomalyAlertRequest) Validate() error {
	trimPtr(&r.Note)
	if r.Note != nil && len(*r.Note) > 500 {
		return Invalid("note en fazla 500 karakter olabilir")
	}
	if r.Acknowledged == nil && r.Note == nil {
		return Invalid("acknowledged veya note verilmeli")
	}
	return nil
}
