package repo

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"

	"github.com/shopspring/decimal"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/mlclient"
)

// Package repo (predictions.go): GET /predictions ve GET /predictions/summary. prediction tablosunun
// TUM satirlarini (gecmis dahil, her asama/siparis ayri satir -- bkz. production.go/cutting.go'daki
// "en guncel tahmin" gorunumunden farkli) duz bir listeye cevirir; "ornek tahminler ve dogruluk
// degerlendirmesi" (bkz. docs/ml-report.md) icin kullanilir.

const predictionRefNoExpr = `(CASE p.target_type
	WHEN 'production' THEN (SELECT po.order_no FROM production_order po WHERE po.id = p.ref_id)
	WHEN 'cutting' THEN (SELECT co.order_no FROM cutting_order co WHERE co.id = p.ref_id)
	ELSE NULL
END)`

const predictionCols = `p.id, p.model_name, p.model_version, p.target_type, p.stage, p.ref_id, ` +
	predictionRefNoExpr + `, p.predicted_json, p.actual_json, p.error_json, p.created_at`

// scanPrediction bir prediction satirini okur ve target_type'a gore (production/cutting) uygun
// JSON govdelerini cozumleyip API gorunumune cevirir.
func scanPrediction(row interface{ Scan(...any) error }) (*domain.PredictionListItem, error) {
	var it domain.PredictionListItem
	var stage, refNo sql.NullString
	var predictedJSON, actualJSON, errorJSON []byte
	if err := row.Scan(&it.ID, &it.ModelName, &it.ModelVersion, &it.TargetType, &stage, &it.RefID, &refNo,
		&predictedJSON, &actualJSON, &errorJSON, &it.CreatedAt); err != nil {
		return nil, mapErr(err)
	}
	if stage.Valid {
		it.Stage = &stage.String
	}
	if refNo.Valid {
		it.RefNo = &refNo.String
	}

	switch it.TargetType {
	case "production":
		it.RefType = "production_order"
		if err := fillProductionPredictionItem(&it, predictedJSON, actualJSON, errorJSON); err != nil {
			return nil, err
		}
	case "cutting":
		it.RefType = "cutting_order"
		if err := fillCuttingPredictionItem(&it, predictedJSON, actualJSON, errorJSON); err != nil {
			return nil, err
		}
	}
	return &it, nil
}

// fillProductionPredictionItem: metrik = teslim edilen adet (predicted_delivered_qty vs
// actual_delivered_qty) -- docs/ml-report.md'deki rolling-origin degerlendirmenin asil hedefi.
// Hibrit EKP tahmini (yalnizca materials_sent'te dolu) ayrica ekp_* alanlarina yazilir.
func fillProductionPredictionItem(it *domain.PredictionListItem, predictedJSON, actualJSON, errorJSON []byte) error {
	it.Metric, it.MetricUnit = "delivered_qty", "adet"
	var pred storedPrediction
	if len(predictedJSON) > 0 {
		if err := json.Unmarshal(predictedJSON, &pred); err != nil {
			return err
		}
	}
	it.Baseline = pred.Baseline
	v := pred.PredictedDeliveredQty
	it.PredictedValue = &v
	it.PredictedEkpUnit = pred.PredictedEkpUnit

	if len(actualJSON) > 0 {
		var actual struct {
			DeliveredQty string  `json:"delivered_qty"`
			EkpUnit      *string `json:"ekp_unit"`
		}
		if err := json.Unmarshal(actualJSON, &actual); err == nil {
			it.ActualValue = &actual.DeliveredQty
			it.ActualEkpUnit = actual.EkpUnit
			it.HasActual = true
		}
	}
	if len(errorJSON) > 0 {
		var e struct {
			DeliveredQtyErrorAbs string  `json:"delivered_qty_error_abs"`
			DeliveredQtyErrorPct string  `json:"delivered_qty_error_pct"`
			EkpErrorAbs          *string `json:"ekp_error_abs"`
			EkpErrorPct          *string `json:"ekp_error_pct"`
		}
		if err := json.Unmarshal(errorJSON, &e); err == nil {
			it.ErrorAbs, it.ErrorPct = &e.DeliveredQtyErrorAbs, &e.DeliveredQtyErrorPct
			it.EkpErrorAbs, it.EkpErrorPct = e.EkpErrorAbs, e.EkpErrorPct
		}
	}
	return nil
}

// fillCuttingPredictionItem: metrik = parca basina kg (ozet deger = parcalar arasi ORTALAMA mutlak
// hata/yuzde; ayrinti Parts'ta). predicted_value/actual_value = tum parcalarin (fire dahil) toplam kg'si.
func fillCuttingPredictionItem(it *domain.PredictionListItem, predictedJSON, actualJSON, errorJSON []byte) error {
	it.Metric, it.MetricUnit = "part_weight", "kg"
	var pred mlclient.CuttingPrediction
	if len(predictedJSON) > 0 {
		if err := json.Unmarshal(predictedJSON, &pred); err != nil {
			return err
		}
	}
	it.Baseline = pred.Baseline
	predictedKg := map[string]string{}
	for _, p := range pred.Parts {
		predictedKg[p.Part] = p.Kg
	}
	if pred.ExpectedFireKg != "" {
		predictedKg["fire"] = pred.ExpectedFireKg
	}
	predictedSum := sumDecimalStrings(mapValues(predictedKg))
	if predictedSum != nil {
		it.PredictedValue = predictedSum
	}

	if len(actualJSON) == 0 {
		return buildCuttingParts(it, predictedKg, nil, nil)
	}
	var actual map[string]string
	if err := json.Unmarshal(actualJSON, &actual); err != nil || len(actual) == 0 {
		return buildCuttingParts(it, predictedKg, nil, nil)
	}
	it.HasActual = true
	actualSum := sumDecimalStrings(mapValues(actual))
	it.ActualValue = actualSum

	var diffs map[string]map[string]string
	if len(errorJSON) > 0 {
		_ = json.Unmarshal(errorJSON, &diffs)
	}
	// Ozet: parcalar arasi ortalama |diff_kg| ve ortalama diff_pct (bkz. repo/cutting.go
	// recordCuttingPredictionActual -- pct her zaman non-negative).
	var sumAbsDiff, sumPct decimal.Decimal
	n := 0
	for _, d := range diffs {
		if dk, err := decimal.NewFromString(d["diff_kg"]); err == nil {
			sumAbsDiff = sumAbsDiff.Add(dk.Abs())
			n++
		}
		if dp, err := decimal.NewFromString(d["diff_pct"]); err == nil {
			sumPct = sumPct.Add(dp)
		}
	}
	if n > 0 {
		abs, pct := domain.Round3(sumAbsDiff.DivRound(decimal.NewFromInt(int64(n)), 6)), domain.Round2(sumPct.DivRound(decimal.NewFromInt(int64(n)), 6))
		absStr, pctStr := abs.StringFixed(3), pct.StringFixed(2)
		it.ErrorAbs, it.ErrorPct = &absStr, &pctStr
	}
	return buildCuttingParts(it, predictedKg, actual, diffs)
}

func buildCuttingParts(it *domain.PredictionListItem, predictedKg, actualKg map[string]string, diffs map[string]map[string]string) error {
	keys := make([]string, 0, len(predictedKg))
	for k := range predictedKg {
		keys = append(keys, k)
	}
	for k := range actualKg {
		if _, ok := predictedKg[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]domain.PredictionPartDiff, 0, len(keys))
	for _, k := range keys {
		pd := domain.PredictionPartDiff{PartKey: k}
		if v, ok := predictedKg[k]; ok {
			pd.PredictedKg = &v
		}
		if v, ok := actualKg[k]; ok {
			pd.ActualKg = &v
		}
		if d, ok := diffs[k]; ok {
			if v, ok := d["diff_kg"]; ok {
				pd.DiffKg = &v
			}
			if v, ok := d["diff_pct"]; ok {
				pd.DiffAbsPct = &v
			}
		}
		parts = append(parts, pd)
	}
	it.Parts = parts
	return nil
}

func mapValues(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

// sumDecimalStrings decimal string'lerin toplamini (3 ondalik) doner; hicbiri gecerli degilse nil.
func sumDecimalStrings(vals []string) *string {
	sum := decimal.Zero
	any := false
	for _, v := range vals {
		d, err := decimal.NewFromString(v)
		if err != nil {
			continue
		}
		sum = sum.Add(d)
		any = true
	}
	if !any {
		return nil
	}
	s := sum.StringFixed(domain.QtyScale)
	return &s
}

// ---------------------------------------------------------------------------
// GET /predictions, GET /predictions/summary
// ---------------------------------------------------------------------------

type PredictionFilter struct {
	TargetType string
	ModelName  string
	From, To   string // created_at tarih araligi (YYYY-MM-DD)
	HasActual  *bool
	Page       Page
}

func (s *Store) ListPredictions(ctx context.Context, f PredictionFilter) ([]domain.PredictionListItem, error) {
	var b query
	if f.TargetType != "" {
		b.add(`p.target_type = ?`, f.TargetType)
	}
	if f.ModelName != "" {
		b.add(`p.model_name = ?`, f.ModelName)
	}
	if f.From != "" {
		b.add(`p.created_at >= ?`, f.From)
	}
	if f.To != "" {
		b.add(`p.created_at < DATE_ADD(?, INTERVAL 1 DAY)`, f.To)
	}
	if f.HasActual != nil {
		if *f.HasActual {
			b.add(`p.actual_json IS NOT NULL`)
		} else {
			b.add(`p.actual_json IS NULL`)
		}
	}
	limit, offset := f.Page.normalized()
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+predictionCols+` FROM prediction p`+b.sql()+` ORDER BY p.created_at DESC LIMIT ? OFFSET ?`,
		append(b.args, limit, offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.PredictionListItem{}
	for rows.Next() {
		it, err := scanPrediction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

// GetPredictionsSummary GET /predictions/summary: target_type bazinda kayit sayisi + MAE/MAPE
// (sadece gercegi girilmis -- has_actual=true -- kayitlar uzerinden, bkz. domain.PredictionSummary).
func (s *Store) GetPredictionsSummary(ctx context.Context) (*domain.PredictionSummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+predictionCols+` FROM prediction p`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out domain.PredictionSummary
	acc := map[string]*accumulator{"production": {}, "cutting": {}}
	for rows.Next() {
		it, err := scanPrediction(rows)
		if err != nil {
			return nil, err
		}
		a, ok := acc[it.TargetType]
		if !ok {
			continue
		}
		a.count++
		if it.HasActual && it.ErrorAbs != nil && it.ErrorPct != nil {
			if abs, err := decimal.NewFromString(*it.ErrorAbs); err == nil {
				a.sumAbs = a.sumAbs.Add(abs.Abs())
				if pct, err := decimal.NewFromString(*it.ErrorPct); err == nil {
					a.sumPct = a.sumPct.Add(pct.Abs())
				}
				a.withActual++
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out.Production = acc["production"].entry()
	out.Cutting = acc["cutting"].entry()
	return &out, nil
}

type accumulator struct {
	count, withActual int
	sumAbs, sumPct    decimal.Decimal
}

func (a *accumulator) entry() domain.PredictionSummaryEntry {
	e := domain.PredictionSummaryEntry{Count: a.count, CountWithActual: a.withActual}
	if a.withActual > 0 {
		n := decimal.NewFromInt(int64(a.withActual))
		mae := domain.Round4(a.sumAbs.DivRound(n, 8)).StringFixed(4)
		mape := domain.Round2(a.sumPct.DivRound(n, 8)).StringFixed(2)
		e.MAE, e.MAPE = &mae, &mape
	}
	return e
}
