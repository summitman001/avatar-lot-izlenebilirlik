package domain

import "time"

// Package domain (prediction.go): GET /predictions(/summary) API gorunumleri. prediction tablosunun
// TUM satirlarini (hem production hem cutting, her asama/siparis ayri satir) duz bir listede gosterir
// -- GET /production-orders/{id} ve /cutting-orders/{id}'deki "en guncel tahmin" gorunumunden farkli
// olarak burada GECMIS tum tahminler (ornek tahminler + dogruluk degerlendirmesi icin) listelenir.
//
// Genel metrik: production icin predicted/actual_delivered_qty (adet) -- docs/ml-report.md'deki
// rolling-origin degerlendirmenin asil hedefi budur (EKP hatasi ayrica ekp_* alanlarinda tasinir).
// cutting icin parca basina kg tahmini/fiili (ortalama mutlak hata/yuzde olarak ozetlenir, ayrintisi
// Parts'tadir).
type PredictionListItem struct {
	ID           string    `json:"id"`
	CreatedAt    time.Time `json:"created_at"`
	TargetType   string    `json:"target_type"` // production | cutting
	Stage        *string   `json:"stage"`       // yalniz production: draft | materials_sent
	RefType      string    `json:"ref_type"`    // production_order | cutting_order
	RefID        string    `json:"ref_id"`
	RefNo        *string   `json:"ref_no"`
	ModelName    string    `json:"model_name"`
	ModelVersion string    `json:"model_version"`
	Baseline     bool      `json:"baseline"`

	// Genel metrik (ozet/MAE-MAPE hesabinda kullanilan): production icin "delivered_qty" (adet),
	// cutting icin "part_weight" (kg, parca basina ortalama).
	Metric         string  `json:"metric"`
	MetricUnit     string  `json:"metric_unit"` // adet | kg
	PredictedValue *string `json:"predicted_value"`
	ActualValue    *string `json:"actual_value"`
	ErrorAbs       *string `json:"error_abs"`
	ErrorPct       *string `json:"error_pct"`
	HasActual      bool    `json:"has_actual"`

	// Hibrit EKP tahmini (yalnizca production, stage=materials_sent'te doludur).
	PredictedEkpUnit *string `json:"predicted_ekp_unit,omitempty"`
	ActualEkpUnit    *string `json:"actual_ekp_unit,omitempty"`
	EkpErrorAbs      *string `json:"ekp_error_abs,omitempty"`
	EkpErrorPct      *string `json:"ekp_error_pct,omitempty"`

	// Parca bazinda kirilim (yalnizca cutting, gercek girilmisse doludur) -- web'de satir genisletilince gosterilir.
	Parts []PredictionPartDiff `json:"parts,omitempty"`
}

// PredictionPartDiff: cutting tahmininde parca basina beklenen/gercek/fark (ML'in kendi kisa adiyla,
// ornek "but"/"kol"/"fire" -- bkz. repo/cutting.go partMLKey).
type PredictionPartDiff struct {
	PartKey     string  `json:"part_key"`
	PredictedKg *string `json:"predicted_kg"`
	ActualKg    *string `json:"actual_kg"`
	DiffKg      *string `json:"diff_kg"`
	DiffAbsPct  *string `json:"diff_pct"`
}

// PredictionSummaryEntry: bir target_type icin kayit sayisi + dogruluk ozeti (bkz. PredictionSummary).
type PredictionSummaryEntry struct {
	Count           int     `json:"count"`
	CountWithActual int     `json:"count_with_actual"`
	MAE             *string `json:"mae"`  // ortalama mutlak hata (metric_unit biriminde); sadece has_actual uzerinden
	MAPE            *string `json:"mape"` // ortalama mutlak yuzde hata (%); sadece has_actual uzerinden
}

// PredictionSummary GET /predictions/summary yanitidir: target_type bazinda ozet (web "Tahminler"
// sayfasinin ust kartlari icin).
type PredictionSummary struct {
	Production PredictionSummaryEntry `json:"production"`
	Cutting    PredictionSummaryEntry `json:"cutting"`
}
