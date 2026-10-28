package repo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/mlclient"
)

// Package repo (anomaly.go): anomaly_alert okuma/yazma + ML /detect/anomaly ve iki kural tabanli
// kontrolun (KUTLE_DENGESI, NEGATIVE_STOCK_OFFLINE) tetiklenme noktalari.
//
// ILKE: anomali uyarilari SADECE BILGILENDIRMEDIR. Bu dosyadaki hicbir fonksiyon bir lotu bloke
// etmez, bir islemi reddetmez veya baska bir transaction'i geri aldirmaz (bkz. docs/assumptions.md).
// ML cagrilari en iyi gayrettir: ML_URL erisilemezse/hata verirse sessizce loglanir, atlanir.

// refNoExpr: anomaly_alert.ref_type'a gore ilgili emrin insan-okunur numarasini (production/cutting
// siparis no, alis belgesi no, satis siparis no) cozen SQL ifadesi. Alias "a" anomaly_alert'tir.
const refNoExpr = `(CASE a.ref_type
	WHEN 'production_order' THEN (SELECT po.order_no FROM production_order po WHERE po.id = a.ref_id)
	WHEN 'cutting_order' THEN (SELECT co.order_no FROM cutting_order co WHERE co.id = a.ref_id)
	WHEN 'purchase_receipt' THEN (SELECT pr.doc_no FROM purchase_receipt pr WHERE pr.id = a.ref_id)
	WHEN 'sales_line' THEN (SELECT so.order_no FROM sales_line sl JOIN sales_order so ON so.id = sl.sales_order_id WHERE sl.id = a.ref_id)
	ELSE NULL
END)`

const anomalyAlertCols = `a.id, a.alert_type, a.severity, a.ref_type, a.ref_id, ` + refNoExpr + `,
       a.message, a.evidence_json, a.acknowledged, a.note, a.created_at, a.updated_at`

func scanAnomalyAlert(row interface{ Scan(...any) error }) (*domain.AnomalyAlert, error) {
	var al domain.AnomalyAlert
	var evidenceJSON []byte
	if err := row.Scan(&al.ID, &al.AlertType, &al.Severity, &al.RefType, &al.RefID, &al.RefNo,
		&al.Message, &evidenceJSON, &al.Acknowledged, &al.Note, &al.CreatedAt, &al.UpdatedAt); err != nil {
		return nil, mapErr(err)
	}
	if len(evidenceJSON) > 0 {
		if err := json.Unmarshal(evidenceJSON, &al.Evidence); err != nil {
			return nil, err
		}
	}
	return &al, nil
}

// upsertAnomalyAlert: ayni ref_type+ref_id+alert_type icin ACIK (acknowledged=false) bir uyari zaten
// varsa GUNCELLER (severity/message/evidence/updated_at); yoksa yeni kayit ekler. Boylece aynı emre
// tekrar tetiklenen ayni kural mukerrer satir yigmaz (bkz. docs/anomaly-integration.md, "Tetikleme
// noktalari"). q hem *sql.DB hem *sql.Tx olabilir: deterministik kural kontrolleri (KUTLE_DENGESI)
// cagiran islemle AYNI transaction'da, ML best-effort cagrilari committen SONRA s.db ile yazar.
func upsertAnomalyAlert(ctx context.Context, q DBTX, alertType, severity, refType, refID, message string, evidence map[string]string) error {
	var evidenceJSON any
	if len(evidence) > 0 {
		b, err := json.Marshal(evidence)
		if err != nil {
			return err
		}
		evidenceJSON = string(b)
	}
	var existingID string
	err := q.QueryRowContext(ctx,
		`SELECT id FROM anomaly_alert WHERE ref_type = ? AND ref_id = ? AND alert_type = ? AND acknowledged = FALSE LIMIT 1`,
		refType, refID, alertType).Scan(&existingID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = q.ExecContext(ctx,
			`INSERT INTO anomaly_alert (id, alert_type, severity, ref_type, ref_id, message, evidence_json)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			domain.NewID(), alertType, severity, refType, refID, message, evidenceJSON)
		return mapErr(err)
	case err != nil:
		return err
	default:
		_, err = q.ExecContext(ctx,
			`UPDATE anomaly_alert SET severity = ?, message = ?, evidence_json = ?, updated_at = CURRENT_TIMESTAMP(3) WHERE id = ?`,
			severity, message, evidenceJSON, existingID)
		return mapErr(err)
	}
}

// loadAnomalyAlerts ilgili emrin detay yanitina (GET production-orders/{id}, GET cutting-orders/{id})
// gomulecek uyari listesini doner (en yeniden eskiye).
func loadAnomalyAlerts(ctx context.Context, q DBTX, refType, refID string) ([]domain.AnomalyAlert, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT `+anomalyAlertCols+` FROM anomaly_alert a WHERE a.ref_type = ? AND a.ref_id = ? ORDER BY a.created_at DESC`,
		refType, refID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.AnomalyAlert{}
	for rows.Next() {
		al, err := scanAnomalyAlert(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *al)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// GET /anomaly-alerts, PATCH /anomaly-alerts/{id}, GET /anomaly-alerts/summary
// ---------------------------------------------------------------------------

type AnomalyAlertFilter struct {
	Severity     string
	Acknowledged *bool
	RefType      string
	From, To     string // created_at tarih araligi (YYYY-MM-DD)
	Page         Page
}

func (s *Store) ListAnomalyAlerts(ctx context.Context, f AnomalyAlertFilter) ([]domain.AnomalyAlert, error) {
	var b query
	if f.Severity != "" {
		b.add(`a.severity = ?`, f.Severity)
	}
	if f.Acknowledged != nil {
		b.add(`a.acknowledged = ?`, *f.Acknowledged)
	}
	if f.RefType != "" {
		b.add(`a.ref_type = ?`, f.RefType)
	}
	if f.From != "" {
		b.add(`a.created_at >= ?`, f.From)
	}
	if f.To != "" {
		b.add(`a.created_at < DATE_ADD(?, INTERVAL 1 DAY)`, f.To)
	}
	limit, offset := f.Page.normalized()
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+anomalyAlertCols+` FROM anomaly_alert a`+b.sql()+
			` ORDER BY a.acknowledged ASC, a.created_at DESC LIMIT ? OFFSET ?`,
		append(b.args, limit, offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.AnomalyAlert{}
	for rows.Next() {
		al, err := scanAnomalyAlert(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *al)
	}
	return out, rows.Err()
}

func (s *Store) GetAnomalyAlert(ctx context.Context, id string) (*domain.AnomalyAlert, error) {
	return scanAnomalyAlert(s.db.QueryRowContext(ctx, `SELECT `+anomalyAlertCols+` FROM anomaly_alert a WHERE a.id = ?`, id))
}

// PatchAnomalyAlert: acknowledged/note gunceller (ikisi de opsiyonel, en az biri zorunlu - bkz.
// domain.PatchAnomalyAlertRequest.Validate). Bu islem SADECE uyarinin okundu durumunu degistirir;
// hicbir lotu/siparisi etkilemez.
func (s *Store) PatchAnomalyAlert(ctx context.Context, id string, req domain.PatchAnomalyAlertRequest) (*domain.AnomalyAlert, error) {
	cur, err := s.GetAnomalyAlert(ctx, id)
	if err != nil {
		return nil, err
	}
	ack := cur.Acknowledged
	if req.Acknowledged != nil {
		ack = *req.Acknowledged
	}
	note := cur.Note
	if req.Note != nil {
		note = req.Note
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE anomaly_alert SET acknowledged = ?, note = ? WHERE id = ?`, ack, note, id); err != nil {
		return nil, mapErr(err)
	}
	return s.GetAnomalyAlert(ctx, id)
}

// GetAnomalyAlertSummary GET /anomaly-alerts/summary: severity bazinda ACIK (acknowledged=false)
// uyari sayisi (dashboard ozet karti icin).
func (s *Store) GetAnomalyAlertSummary(ctx context.Context) (*domain.AnomalyAlertSummary, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT severity, COUNT(*) FROM anomaly_alert WHERE acknowledged = FALSE GROUP BY severity`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out domain.AnomalyAlertSummary
	for rows.Next() {
		var severity string
		var n int
		if err := rows.Scan(&severity, &n); err != nil {
			return nil, err
		}
		switch severity {
		case "critical":
			out.Critical = n
		case "warning":
			out.Warning = n
		case "info":
			out.Info = n
		}
		out.Total += n
	}
	return &out, rows.Err()
}

// ---------------------------------------------------------------------------
// ML tetikleme noktalari (en iyi gayret, kendi islemi committen SONRA cagrilir)
// ---------------------------------------------------------------------------

// applyAnomalyFindings ML'den donen her bulguyu upsertAnomalyAlert ile yazar; tek bir bulgu
// kaydedilemese de digerleri denenir (en iyi gayret).
func (s *Store) applyAnomalyFindings(ctx context.Context, refType, refID string, findings []mlclient.AnomalyFinding) {
	for _, f := range findings {
		if err := upsertAnomalyAlert(ctx, s.db, f.Code, f.Severity, refType, refID, f.Message, f.Evidence); err != nil {
			slog.Warn("anomali uyarisi kaydedilemedi", "alert_type", f.Code, "ref_type", refType, "ref_id", refID, "err", err)
		}
	}
}

// detectProductionAnomaly: ReceiveProduction basariyla committen SONRA (en iyi gayret) cagrilir.
// VERIM_DUSUK (actual_yield_ratio = output_kg/et_kg, et_kg = tuketilen et miktari) + EKP_SAPMA
// (en guncel tahminin predicted_ekp_unit'i varsa). ML kapaliysa/hata verirse sessizce atlanir.
func (s *Store) detectProductionAnomaly(ctx context.Context, orderID string) {
	if s.ml == nil {
		return
	}
	var orderNo, producerSupplierID string
	var receivedQty *domain.Qty
	var ekpUnit *domain.Amount
	var unitWeightKg *decimal.Decimal
	err := s.db.QueryRowContext(ctx,
		`SELECT po.order_no, po.producer_supplier_id, po.received_qty, po.ekp_unit, i.unit_weight_kg
		 FROM production_order po JOIN item i ON i.id = po.product_item_id WHERE po.id = ?`, orderID).
		Scan(&orderNo, &producerSupplierID, &receivedQty, &ekpUnit, &unitWeightKg)
	if err != nil {
		slog.Warn("anomali tespiti icin siparis okunamadi", "order_id", orderID, "err", err)
		return
	}
	if receivedQty == nil {
		return // henuz teslim alinmamis (beklenmez, receive sonrasi cagrilir)
	}

	inputs, err := loadProductionInputs(ctx, s.db, orderID)
	if err != nil {
		slog.Warn("anomali tespiti icin girdiler okunamadi", "order_id", orderID, "err", err)
		return
	}
	meatKg := decimal.Zero
	for _, in := range inputs {
		if in.MaterialCategory != nil && *in.MaterialCategory == "meat" && in.QtyConsumed != nil {
			meatKg = meatKg.Add(in.QtyConsumed.Decimal)
		}
	}

	in := mlclient.DetectAnomalyInput{ReferenceID: orderNo, ProducerID: &producerSupplierID}
	if supplierID, ok := singleSupplierOf(ctx, s.db, inputs); ok {
		in.SupplierID = &supplierID
	}
	if meatKg.IsPositive() && unitWeightKg != nil {
		outputKg := receivedQty.Decimal.Mul(*unitWeightKg)
		yieldRatio := outputKg.DivRound(meatKg, 4).StringFixed(4)
		etKg := meatKg.StringFixed(domain.QtyScale)
		in.ActualYieldRatio, in.EtKg = &yieldRatio, &etKg
	}
	if predictedEkp, ok := latestPredictedEkpUnit(ctx, s.db, "production", orderID); ok && ekpUnit != nil {
		actual := ekpUnit.StringFixed(domain.AmountScale)
		in.PredictedEkpUnit, in.ActualEkpUnit = &predictedEkp, &actual
	}
	if in.ActualYieldRatio == nil && in.PredictedEkpUnit == nil {
		return // gonderilecek girdi yok
	}

	out, err := s.ml.DetectAnomaly(ctx, in)
	if err != nil {
		slog.Warn("anomali tespiti alinamadi (production)", "order_no", orderNo, "err", err)
		return
	}
	s.applyAnomalyFindings(ctx, "production_order", orderID, out.Findings)
}

// singleSupplierOf: sevk edilen hammaddelerin TEK bir ortak tedarikcisi varsa onu doner (farkli
// tedarikcilerden sevkiyat varsa ya da hic tedarikci yoksa (false, "")) - VERIM_DUSUK'un opsiyonel
// supplier_id alani icin (bkz. docs/anomaly-integration.md, "1. Tetikleme noktalari").
func singleSupplierOf(ctx context.Context, q DBTX, inputs []domain.ProductionInputLine) (string, bool) {
	lotIDs := make([]string, 0, len(inputs))
	for _, in := range inputs {
		lotIDs = append(lotIDs, in.LotID)
	}
	if len(lotIDs) == 0 {
		return "", false
	}
	placeholders := make([]string, len(lotIDs))
	args := make([]any, len(lotIDs))
	for i, id := range lotIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	rows, err := q.QueryContext(ctx,
		`SELECT DISTINCT supplier_id FROM lot WHERE id IN (`+strings.Join(placeholders, ",")+`) AND supplier_id IS NOT NULL`, args...)
	if err != nil {
		return "", false
	}
	defer rows.Close()
	var supplierID string
	count := 0
	for rows.Next() {
		if err := rows.Scan(&supplierID); err != nil {
			return "", false
		}
		count++
	}
	if count != 1 {
		return "", false
	}
	return supplierID, true
}

// storedPrediction (production.go) ve cutting_prediction (cutting.go) ikisinin de predicted_json'i
// en azindan {"predicted_ekp_unit": "..."} veya hic bu alani icermeyebilir; bu yuzden genel bir
// kucuk struct ile okunur (hem production hem cutting prediction satirlari icin ortak).
type predictedEkpOnly struct {
	PredictedEkpUnit *string `json:"predicted_ekp_unit"`
}

// latestPredictedEkpUnit: ilgili hedefin (production/cutting) EN GUNCEL tahmin satirindaki
// predicted_ekp_unit'i okur (yoksa ok=false). Production icin bu her zaman materials_sent
// asamasindandir (draft'ta null, bkz. docs/assumptions.md).
func latestPredictedEkpUnit(ctx context.Context, q DBTX, targetType, refID string) (string, bool) {
	var predictedJSON []byte
	err := q.QueryRowContext(ctx,
		`SELECT predicted_json FROM prediction WHERE target_type = ? AND ref_id = ? ORDER BY created_at DESC LIMIT 1`,
		targetType, refID).Scan(&predictedJSON)
	if err != nil {
		return "", false
	}
	var p predictedEkpOnly
	if err := json.Unmarshal(predictedJSON, &p); err != nil || p.PredictedEkpUnit == nil {
		return "", false
	}
	return *p.PredictedEkpUnit, true
}

// detectCuttingAnomaly: CloseCuttingOrder basariyla committen SONRA (en iyi gayret) cagrilir.
// FIRE_YUKSEK (actual_fire_kg = recetedeki fire parcasinin fiili agirligi) + KUTLE_DENGESI (ML'in
// kendi sigma/esik mantigi; Go'nun HER ZAMAN calisan deterministik KUTLE_DENGESI kontrolu
// UpdateCuttingOutputs'tadir, bkz. asagisi). ML kapaliysa/hata verirse sessizce atlanir.
func (s *Store) detectCuttingAnomaly(ctx context.Context, orderID string) {
	if s.ml == nil {
		return
	}
	o, err := s.GetCuttingOrder(ctx, orderID)
	if err != nil {
		slog.Warn("anomali tespiti icin parcalama emri okunamadi", "order_id", orderID, "err", err)
		return
	}
	var breed *string
	if o.RecipeID != "" {
		var animalType string
		if err := s.db.QueryRowContext(ctx, `SELECT animal_type FROM cutting_recipe WHERE id = ?`, o.RecipeID).Scan(&animalType); err == nil && animalType != "" {
			breed = &animalType
		}
	}

	fireKg, outputWeights := decimal.Zero, make([]string, 0, len(o.Outputs))
	for _, line := range o.Outputs {
		if line.ActualWeightKg == nil {
			continue
		}
		outputWeights = append(outputWeights, line.ActualWeightKg.StringFixed(domain.QtyScale))
		if line.IsWaste {
			fireKg = fireKg.Add(line.ActualWeightKg.Decimal)
		}
	}
	carcassKg := o.InputWeightKg.StringFixed(domain.QtyScale)
	actualFireKg := fireKg.StringFixed(domain.QtyScale)
	inputWeightKg := o.InputWeightKg.StringFixed(domain.QtyScale)

	in := mlclient.DetectAnomalyInput{
		ReferenceID: o.OrderNo, CarcassKg: &carcassKg, ActualFireKg: &actualFireKg, Breed: breed,
		InputWeightKg: &inputWeightKg, OutputWeightsKg: outputWeights,
	}
	out, err := s.ml.DetectAnomaly(ctx, in)
	if err != nil {
		slog.Warn("anomali tespiti alinamadi (cutting)", "order_no", o.OrderNo, "err", err)
		return
	}
	s.applyAnomalyFindings(ctx, "cutting_order", orderID, out.Findings)
}

// detectPurchaseCostAnomaly: CreatePurchaseLot basariyla committen SONRA (en iyi gayret) cagrilir.
// MALIYET_ARTISI: yeni birim fiyat, ayni urunun (bu satir haric) son 5 alisinin birim fiyatlarina
// gore. ML kapaliysa/hata verirse sessizce atlanir.
func (s *Store) detectPurchaseCostAnomaly(ctx context.Context, receiptID, itemID, excludeLotID string, currentUnitCost decimal.Decimal) {
	if s.ml == nil {
		return
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT unit_price FROM purchase_receipt_line WHERE item_id = ? AND lot_id != ? ORDER BY created_at DESC LIMIT 5`,
		itemID, excludeLotID)
	if err != nil {
		slog.Warn("anomali tespiti icin gecmis fiyatlar okunamadi", "item_id", itemID, "err", err)
		return
	}
	defer rows.Close()
	recent := []string{}
	for rows.Next() {
		var p domain.Amount
		if err := rows.Scan(&p); err != nil {
			slog.Warn("anomali tespiti icin gecmis fiyat okunamadi", "item_id", itemID, "err", err)
			return
		}
		recent = append(recent, p.StringFixed(domain.AmountScale))
	}
	if err := rows.Err(); err != nil || len(recent) == 0 {
		return
	}

	current := currentUnitCost.StringFixed(domain.AmountScale)
	out, err := s.ml.DetectAnomaly(ctx, mlclient.DetectAnomalyInput{
		ReferenceID: receiptID, CurrentUnitCost: &current, RecentUnitCosts: recent,
	})
	if err != nil {
		slog.Warn("anomali tespiti alinamadi (purchase)", "receipt_id", receiptID, "err", err)
		return
	}
	s.applyAnomalyFindings(ctx, "purchase_receipt", receiptID, out.Findings)
}

// ---------------------------------------------------------------------------
// Kural tabanli kontrol (ML'den BAGIMSIZ, her zaman calisir)
// ---------------------------------------------------------------------------

// checkMassBalanceRule: KUTLE_DENGESI - parcalama ciktilari (fire dahil) girisi asiyorsa (fiziksel
// olarak imkansiz) critical uyari yazar. UpdateCuttingOutputs icinde, fiili agirlik her
// guncellendiginde, AYNI transaction'da (tx) cagrilir - ML'den bagimsiz, deterministik bir kontroldur
// (bkz. docs/anomaly-integration.md, "2. Kural tabanli ek kontrol"). balanceDiffKg negatifse (cikti >
// girdi) tetiklenir; balanceDiffKg nil ise (henuz hic agirlik girilmemis) atlanir.
func checkMassBalanceRule(ctx context.Context, tx *sql.Tx, orderID, orderNo string, balanceDiffKg *domain.Qty) error {
	if balanceDiffKg == nil || !balanceDiffKg.Decimal.IsNegative() {
		return nil
	}
	over := balanceDiffKg.Decimal.Neg()
	message := fmt.Sprintf("Parcalama emri %s: ciktilar (fire dahil) girisi %s kg asiyor (fiziksel olarak imkansiz).",
		orderNo, over.StringFixed(domain.QtyScale))
	evidence := map[string]string{"balance_diff_kg": balanceDiffKg.StringFixed(domain.QtyScale), "over_kg": over.StringFixed(domain.QtyScale)}
	return upsertAnomalyAlert(ctx, tx, "KUTLE_DENGESI", "critical", "cutting_order", orderID, message, evidence)
}
