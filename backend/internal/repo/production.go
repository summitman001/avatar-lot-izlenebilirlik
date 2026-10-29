package repo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/mlclient"
)

// Gorev 1 uctan uca: dis hizmetle uretim ve gercek maliyet (EKP).
//
// Durum makinesi (domain.Can* fonksiyonlari transaction icinde kontrol eder):
//
//	draft -[send-materials]-> materials_sent -[receive]-> received -[close]-> closed
//
// Her yazma TEK transaction'dadir; bakiye gerektiren adimlarda lot satiri FOR UPDATE kilitlenir
// (ReverseMovement ile ayni desen). production_order sync'e acik DEGILDIR (change_log'a yazilmaz);
// yalniz item/customer/warehouse/lot senkronlanir (bkz. docs/sync.md).
//
// ML entegrasyonu EN IYI GAYRET'tir: servis erisilemezse islem bozulmaz, sadece loglanir ve
// prediction tahminsiz kalir (bkz. docs/assumptions.md, "ML entegrasyonu").

// assumedYieldRatio: ML'e ilk tahmin cagrisinda (siparis olusturulurken hammadde kompozisyonu henuz
// bilinmedigi icin) gereken et miktarini kabaca tahmin etmek icin kullanilan varsayilan verim.
// Golden UR-001..UR-004 senaryolarindaki yield_meat_ratio (0.8500) ile aynidir; gercek deger sevk
// edilen hammaddeler bilindiginde (receive asamasinda) actual_json ile karsilastirilir.
var assumedYieldRatio = decimal.RequireFromString("0.85")

func ptrStr(s string) *string { return &s }

// nextSequenceNo "<prefix>-<yil>-NNN" biciminde bir sonraki numarayi ATOMIK uretir (number_sequence
// tablosu, bkz. migration 000005). INSERT ... ON DUPLICATE KEY UPDATE tek basina satiri kilitler
// (satir yoksa olusturup 1 ile baslatir, varsa +1 artirir); es zamanli iki cagri aym satirda
// SERILESTIRILIR (ikincisi ilki commit/rollback olana kadar bu INSERT satirinda bekler), bu yuzden
// hic kimse ayni numarayi alamaz. `tx` disinda cagrilmaz: kilit, cagiran transaction commit olana
// kadar tutulur.
func nextSequenceNo(ctx context.Context, tx *sql.Tx, prefix string) (string, error) {
	year := time.Now().UTC().Year()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO number_sequence (prefix, year, seq_value) VALUES (?, ?, 1)
		 ON DUPLICATE KEY UPDATE seq_value = seq_value + 1`,
		prefix, year); err != nil {
		return "", err
	}
	var val int
	if err := tx.QueryRowContext(ctx,
		`SELECT seq_value FROM number_sequence WHERE prefix = ? AND year = ?`, prefix, year).Scan(&val); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%d-%03d", prefix, year, val), nil
}

// ---------------------------------------------------------------------------
// Okuma
// ---------------------------------------------------------------------------

const productionOrderCols = `po.id, po.order_no, po.product_item_id, item.code, item.name, item.unit_weight_kg,
       po.producer_supplier_id, sup.name, po.external_warehouse_id, wh.code,
       po.planned_qty, po.received_qty, po.service_unit_price, po.service_invoice_no, po.service_invoiced_qty,
       po.status, po.output_lot_id, out_lot.lot_no, po.material_cost, po.service_cost, po.total_cost, po.ekp_unit,
       po.sale_price, po.closed_at, po.created_at`

const productionOrderFrom = ` FROM production_order po
JOIN item item ON item.id = po.product_item_id
JOIN supplier sup ON sup.id = po.producer_supplier_id
JOIN warehouse wh ON wh.id = po.external_warehouse_id
LEFT JOIN lot out_lot ON out_lot.id = po.output_lot_id`

func scanProductionOrder(row interface{ Scan(...any) error }) (*domain.ProductionOrder, error) {
	var o domain.ProductionOrder
	var outputLotNo sql.NullString
	err := row.Scan(&o.ID, &o.OrderNo, &o.ProductItemID, &o.ProductItemCode, &o.ProductItemName, &o.ProductUnitWeightKg,
		&o.ProducerSupplierID, &o.ProducerSupplierName, &o.ExternalWarehouseID, &o.ExternalWarehouseCode,
		&o.PlannedQty, &o.ReceivedQty, &o.ServiceUnitPrice, &o.ServiceInvoiceNo, &o.ServiceInvoicedQty,
		&o.Status, &o.OutputLotID, &outputLotNo, &o.MaterialCost, &o.ServiceCost, &o.TotalCost, &o.EkpUnit,
		&o.SalePrice, &o.ClosedAt, &o.CreatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	if outputLotNo.Valid {
		o.OutputLotNo = &outputLotNo.String
	}
	o.Warnings = domain.ComputeProductionWarnings(&o)
	return &o, nil
}

func getProductionOrderRow(ctx context.Context, q DBTX, id string) (*domain.ProductionOrder, error) {
	return scanProductionOrder(q.QueryRowContext(ctx, `SELECT `+productionOrderCols+productionOrderFrom+` WHERE po.id = ?`, id))
}

// loadProductionInputs sevk edilen hammadde satirlarini (lot/urun bilgisiyle) sira ile doner.
// qty_consumed henuz yazilmamissa (receive oncesi) QtyConsumed/QtyReturned nil kalir.
func loadProductionInputs(ctx context.Context, q DBTX, orderID string) ([]domain.ProductionInputLine, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT pin.id, pin.lot_id, l.lot_no, l.item_id, it.code, it.name, it.material_category,
		        pin.qty_sent, pin.qty_consumed, pin.unit_cost_at_send
		 FROM production_input pin
		 JOIN lot l ON l.id = pin.lot_id
		 JOIN item it ON it.id = l.item_id
		 WHERE pin.production_order_id = ?
		 ORDER BY pin.created_at`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ProductionInputLine{}
	for rows.Next() {
		var l domain.ProductionInputLine
		var qtyConsumed *domain.Qty
		if err := rows.Scan(&l.ID, &l.LotID, &l.LotNo, &l.ItemID, &l.ItemCode, &l.ItemName, &l.MaterialCategory,
			&l.QtySent, &qtyConsumed, &l.UnitCostAtSend); err != nil {
			return nil, err
		}
		l.QtyConsumed = qtyConsumed
		if qtyConsumed != nil {
			ret := domain.NewQty(l.QtySent.Decimal.Sub(qtyConsumed.Decimal))
			l.QtyReturned = &ret
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// predictionStage: ML tahmininin hangi asamada alindigi (asama basina ayri satir, uzerine yazilmaz).
const (
	predictionStageDraft         = "draft"
	predictionStageMaterialsSent = "materials_sent"
)

// storedPrediction tahmin tablosunun predicted_json'una yazilan govdedir: ML'in HAM yaniti
// (mlclient.ProductionPrediction) + bizim HESAPLADIGIMIZ hibrit EKP tahmini (ML'in bilmedigi).
// PredictedEkpUnit yalnizca materials_sent asamasinda doludur (draft'ta malzeme maliyeti bilinmez).
type storedPrediction struct {
	mlclient.ProductionPrediction
	PredictedEkpUnit *string `json:"predicted_ekp_unit"`
}

// loadProductionPrediction en son ML tahminini (bu siparis icin, hangi asamada olursa olsun en
// guncel satir) API gorunumune cevirir; tahmin hic alinamadiysa (ML her iki asamada da kapaliydi)
// nil, nil doner.
func loadProductionPrediction(ctx context.Context, q DBTX, orderID string) (*domain.ProductionPredictionView, error) {
	var modelName, modelVersion, stage string
	var predictedJSON, actualJSON, errorJSON []byte
	err := q.QueryRowContext(ctx,
		`SELECT model_name, model_version, stage, predicted_json, actual_json, error_json
		 FROM prediction WHERE target_type = 'production' AND ref_id = ? ORDER BY created_at DESC LIMIT 1`,
		orderID).Scan(&modelName, &modelVersion, &stage, &predictedJSON, &actualJSON, &errorJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var pred storedPrediction
	if err := json.Unmarshal(predictedJSON, &pred); err != nil {
		return nil, err
	}
	view := &domain.ProductionPredictionView{
		ModelName: modelName, ModelVersion: modelVersion, Baseline: pred.Baseline, Stage: stage,
		PredictedYieldRatio: pred.PredictedYieldRatio, PredictedOutputKg: pred.PredictedOutputKg,
		PredictedDeliveredQty: pred.PredictedDeliveredQty, PredictedEkpUnit: pred.PredictedEkpUnit,
	}
	if len(actualJSON) > 0 {
		var actual struct {
			DeliveredQty string  `json:"delivered_qty"`
			EkpUnit      *string `json:"ekp_unit"`
		}
		if err := json.Unmarshal(actualJSON, &actual); err == nil {
			view.ActualDeliveredQty = &actual.DeliveredQty
			view.ActualEkpUnit = actual.EkpUnit
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
			view.DeliveredQtyErrorAbs = &e.DeliveredQtyErrorAbs
			view.DeliveredQtyErrorPct = &e.DeliveredQtyErrorPct
			view.EkpErrorAbs = e.EkpErrorAbs
			view.EkpErrorPct = e.EkpErrorPct
		}
	}
	return view, nil
}

// statusHistory ayri bir tablo yerine mevcut zaman damgalarindan turetilir (bkz. docs/assumptions.md):
// draft = siparisin created_at'i, materials_sent = bu siparise ait ilk send_to_producer hareketi,
// received = cikti lotunun created_at'i, closed = closed_at.
func loadStatusHistory(ctx context.Context, q DBTX, o *domain.ProductionOrder) ([]domain.ProductionStatusEvent, error) {
	hist := []domain.ProductionStatusEvent{{Status: domain.ProductionDraft, At: o.CreatedAt}}
	var sentAt sql.NullTime
	err := q.QueryRowContext(ctx,
		`SELECT MIN(created_at) FROM stock_movement WHERE ref_type = 'production_order' AND ref_id = ? AND movement_type = 'send_to_producer'`,
		o.ID).Scan(&sentAt)
	if err != nil {
		return nil, err
	}
	if sentAt.Valid {
		hist = append(hist, domain.ProductionStatusEvent{Status: domain.ProductionMaterialsSent, At: sentAt.Time})
	}
	if o.OutputLotID != nil {
		var receivedAt time.Time
		if err := q.QueryRowContext(ctx, `SELECT created_at FROM lot WHERE id = ?`, *o.OutputLotID).Scan(&receivedAt); err != nil {
			return nil, err
		}
		hist = append(hist, domain.ProductionStatusEvent{Status: domain.ProductionReceived, At: receivedAt})
	}
	if o.ClosedAt != nil {
		hist = append(hist, domain.ProductionStatusEvent{Status: domain.ProductionClosed, At: *o.ClosedAt})
	}
	return hist, nil
}

// getProductionOrderDetail tum detaylari (girdiler, durum gecmisi, ML tahmini) doldurur.
func getProductionOrderDetail(ctx context.Context, q DBTX, id string) (*domain.ProductionOrder, error) {
	o, err := getProductionOrderRow(ctx, q, id)
	if err != nil {
		return nil, err
	}
	if o.Inputs, err = loadProductionInputs(ctx, q, id); err != nil {
		return nil, err
	}
	if o.StatusHistory, err = loadStatusHistory(ctx, q, o); err != nil {
		return nil, err
	}
	if o.Prediction, err = loadProductionPrediction(ctx, q, id); err != nil {
		return nil, err
	}
	if o.AnomalyAlerts, err = loadAnomalyAlerts(ctx, q, "production_order", id); err != nil {
		return nil, err
	}
	return o, nil
}

// GetProductionOrder GET /production-orders/{id} icin tam detayi doner.
func (s *Store) GetProductionOrder(ctx context.Context, id string) (*domain.ProductionOrder, error) {
	return getProductionOrderDetail(ctx, s.db, id)
}

type ProductionOrderFilter struct {
	Status string
	Q      string
	Page   Page
}

// ListProductionOrders GET /production-orders listesi; girdiler/tahmin icermez (performans).
func (s *Store) ListProductionOrders(ctx context.Context, f ProductionOrderFilter) ([]domain.ProductionOrder, error) {
	var b query
	if f.Status != "" {
		b.add(`po.status = ?`, f.Status)
	}
	if f.Q != "" {
		b.add(`(po.order_no LIKE ? OR item.code LIKE ? OR item.name LIKE ?)`, like(f.Q), like(f.Q), like(f.Q))
	}
	limit, offset := f.Page.normalized()
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+productionOrderCols+productionOrderFrom+b.sql()+` ORDER BY po.created_at DESC LIMIT ? OFFSET ?`,
		append(b.args, limit, offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ProductionOrder{}
	for rows.Next() {
		o, err := scanProductionOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *o)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// ML tahmini (en iyi gayret)
// ---------------------------------------------------------------------------

// insertPrediction tahmin satirini ekler (asama basina ayri satir; oncekinin UZERINE YAZILMAZ).
func (s *Store) insertPrediction(ctx context.Context, orderID, stage string, pred *mlclient.ProductionPrediction, predictedEkpUnit *string, input map[string]string) error {
	inputJSON, _ := json.Marshal(input)
	predictedJSON, _ := json.Marshal(storedPrediction{ProductionPrediction: *pred, PredictedEkpUnit: predictedEkpUnit})
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO prediction (id, model_name, model_version, target_type, stage, ref_id, input_json, predicted_json)
		 VALUES (?, ?, ?, 'production', ?, ?, ?, ?)`,
		domain.NewID(), pred.ModelName, pred.ModelVersion, stage, orderID, string(inputJSON), string(predictedJSON))
	return err
}

// predictOnCreate siparis olustuktan SONRA (ayni transaction disinda) cagrilir: ML cagrisi basarisiz
// olursa islem geri alinmaz, yalniz loglanir. Hammadde kompozisyonu henuz bilinmedigi icin et_kg
// kaba bir varsayimdir (assumedYieldRatio); malzeme maliyeti de bilinmedigi icin bu asamada
// predicted_ekp_unit HESAPLANMAZ (bkz. docs/assumptions.md, "Hibrit EKP tahmini"). Kesin tahmin
// predictOnMaterialsSent'tedir.
func (s *Store) predictOnCreate(ctx context.Context, o *domain.ProductionOrder) {
	if s.ml == nil {
		return
	}
	unitWeight := decimal.NewFromInt(1)
	if o.ProductUnitWeightKg != nil {
		unitWeight = o.ProductUnitWeightKg.Decimal
	}
	estimatedEtKg := o.PlannedQty.Decimal.Mul(unitWeight).DivRound(assumedYieldRatio, 3)
	producerID := o.ProducerSupplierID

	pred, err := s.ml.PredictProduction(ctx, mlclient.ProductionPredictionInput{
		EtKg:         estimatedEtKg.StringFixed(domain.QtyScale),
		ProducerID:   &producerID,
		UnitWeightKg: unitWeight.StringFixed(domain.QtyScale),
	})
	if err != nil {
		slog.Warn("ml tahmini alinamadi, siparis tahminsiz olusturuldu", "order_no", o.OrderNo, "err", err)
		return
	}
	input := map[string]string{
		"et_kg_estimate":      estimatedEtKg.StringFixed(domain.QtyScale),
		"unit_weight_kg":      unitWeight.StringFixed(domain.QtyScale),
		"assumed_yield_ratio": assumedYieldRatio.String(),
	}
	if err := s.insertPrediction(ctx, o.ID, predictionStageDraft, pred, nil, input); err != nil {
		slog.Warn("ml tahmini kaydedilemedi", "order_no", o.OrderNo, "err", err)
	}
}

// predictOnMaterialsSent SendMaterials basariyla committen SONRA (en iyi gayret) cagrilir. Bu noktada
// GERCEK sevk edilen et miktari ve KESIN malzeme maliyeti bilinir; ML'e gercek et_kg ile sorulur ve
// sonuc hibrit formulle EKP tahminine cevrilir:
//
//	predicted_ekp_unit = (material_cost + round2(predicted_qty * service_unit_price)) / predicted_qty
//
// (hizmet bedelinde faturalanan adetin tahmini adetle ayni oldugu varsayilir; 4 ondalik, DivRound).
// Onceki (draft asamasindaki) tahmin satirini DEGISTIRMEZ, yeni bir satir ekler (stage=materials_sent);
// en guncel satir GET/cost-report'ta gosterilir (loadProductionPrediction, ORDER BY created_at DESC).
func (s *Store) predictOnMaterialsSent(ctx context.Context, orderID string) {
	if s.ml == nil {
		return
	}
	var orderNo, productItemID, producerSupplierID string
	var serviceUnitPrice domain.Amount
	if err := s.db.QueryRowContext(ctx,
		`SELECT order_no, product_item_id, producer_supplier_id, service_unit_price FROM production_order WHERE id = ?`, orderID).
		Scan(&orderNo, &productItemID, &producerSupplierID, &serviceUnitPrice); err != nil {
		slog.Warn("ml tahmini icin siparis okunamadi", "order_id", orderID, "err", err)
		return
	}
	product, err := getItem(ctx, s.db, productItemID)
	if err != nil {
		slog.Warn("ml tahmini icin urun okunamadi", "order_id", orderID, "err", err)
		return
	}
	inputs, err := loadProductionInputs(ctx, s.db, orderID)
	if err != nil {
		slog.Warn("ml tahmini icin girdiler okunamadi", "order_id", orderID, "err", err)
		return
	}
	materialCost, meatKg := decimal.Zero, decimal.Zero
	for _, in := range inputs {
		materialCost = materialCost.Add(domain.LineTotal(in.QtySent.Decimal, in.UnitCostAtSend.Decimal))
		if in.MaterialCategory != nil && *in.MaterialCategory == "meat" {
			meatKg = meatKg.Add(in.QtySent.Decimal)
		}
	}
	if !meatKg.IsPositive() {
		slog.Warn("ml tahmini atlandi: sevk edilen hammaddeler arasinda et (meat) yok", "order_no", orderNo)
		return
	}
	unitWeight := decimal.NewFromInt(1)
	if product.UnitWeightKg != nil {
		unitWeight = product.UnitWeightKg.Decimal
	}

	pred, err := s.ml.PredictProduction(ctx, mlclient.ProductionPredictionInput{
		EtKg: meatKg.StringFixed(domain.QtyScale), ProducerID: &producerSupplierID,
		UnitWeightKg: unitWeight.StringFixed(domain.QtyScale),
	})
	if err != nil {
		slog.Warn("ml tahmini alinamadi (materials_sent), siparis tahminsiz devam ediyor", "order_no", orderNo, "err", err)
		return
	}
	predictedQty, err := decimal.NewFromString(pred.PredictedDeliveredQty)
	if err != nil || !predictedQty.IsPositive() {
		slog.Warn("ml gecersiz predicted_delivered_qty dondurdu", "order_no", orderNo, "value", pred.PredictedDeliveredQty)
		return
	}
	predictedServiceCost := domain.LineTotal(predictedQty, serviceUnitPrice.Decimal)
	predictedEkp := materialCost.Add(predictedServiceCost).DivRound(predictedQty, domain.AmountScale)
	predictedEkpStr := predictedEkp.StringFixed(domain.AmountScale)

	input := map[string]string{
		"et_kg": meatKg.StringFixed(domain.QtyScale), "unit_weight_kg": unitWeight.StringFixed(domain.QtyScale),
		"material_cost":      materialCost.StringFixed(domain.AmountScale),
		"service_unit_price": serviceUnitPrice.StringFixed(domain.AmountScale),
	}
	if err := s.insertPrediction(ctx, orderID, predictionStageMaterialsSent, pred, &predictedEkpStr, input); err != nil {
		slog.Warn("ml tahmini kaydedilemedi (materials_sent)", "order_no", orderNo, "err", err)
	}
}

// recordPredictionActual receive sirasinda gerceklesen degeri EN GUNCEL tahminle (draft veya
// materials_sent, hangisi varsa) karsilastirip o satiri gunceller. predicted_ekp_unit yalnizca
// materials_sent asamasinda doluysa EKP sapmasi da yazilir; draft'tan hic materials_sent tahmini
// alinamadiysa (ML o sirada kapaliydi) yalniz adet sapmasi yazilir, EKP alanlari bos kalir.
// Tahmin hic alinmamissa (ML hicbir asamada erisilemedi) sessizce atlanir.
func (s *Store) recordPredictionActual(ctx context.Context, tx *sql.Tx, orderID string, receivedQty, ekpUnit decimal.Decimal) error {
	var id string
	var predictedJSON []byte
	err := tx.QueryRowContext(ctx,
		`SELECT id, predicted_json FROM prediction WHERE target_type = 'production' AND ref_id = ? ORDER BY created_at DESC LIMIT 1`,
		orderID).Scan(&id, &predictedJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var pred storedPrediction
	if err := json.Unmarshal(predictedJSON, &pred); err != nil {
		return err
	}
	predictedQty, err := decimal.NewFromString(pred.PredictedDeliveredQty)
	if err != nil {
		return err
	}

	actualFields := map[string]string{"delivered_qty": receivedQty.StringFixed(domain.QtyScale)}
	errAbs := receivedQty.Sub(predictedQty)
	errPct := decimal.Zero
	if predictedQty.IsPositive() {
		errPct = domain.Round2(errAbs.Abs().Mul(decimal.NewFromInt(100)).Div(predictedQty))
	}
	errFields := map[string]string{
		"delivered_qty_error_abs": errAbs.StringFixed(domain.QtyScale),
		"delivered_qty_error_pct": errPct.StringFixed(2),
	}

	if pred.PredictedEkpUnit != nil {
		predictedEkp, perr := decimal.NewFromString(*pred.PredictedEkpUnit)
		if perr == nil && predictedEkp.IsPositive() {
			actualFields["ekp_unit"] = ekpUnit.StringFixed(domain.AmountScale)
			ekpErrAbs := ekpUnit.Sub(predictedEkp)
			errFields["ekp_error_abs"] = ekpErrAbs.StringFixed(domain.AmountScale)
			errFields["ekp_error_pct"] = domain.Round2(ekpErrAbs.Abs().Mul(decimal.NewFromInt(100)).Div(predictedEkp)).StringFixed(2)
		}
	}

	actual, _ := json.Marshal(actualFields)
	errJSON, _ := json.Marshal(errFields)
	_, err = tx.ExecContext(ctx, `UPDATE prediction SET actual_json = ?, error_json = ? WHERE id = ?`,
		string(actual), string(errJSON), id)
	return err
}

// ---------------------------------------------------------------------------
// Yazma: create / send-materials / receive / close
// ---------------------------------------------------------------------------

func (s *Store) CreateProductionOrder(ctx context.Context, req domain.CreateProductionOrderRequest) (*domain.ProductionOrder, error) {
	id, err := resolveIDPtr(req.ID)
	if err != nil {
		return nil, err
	}
	var out *domain.ProductionOrder
	err = s.InTx(ctx, func(tx *sql.Tx) error {
		item, err := getItem(ctx, tx, req.ProductItemID)
		if errors.Is(err, domain.ErrNotFound) {
			return domain.Invalid("product_item_id bulunamadi")
		} else if err != nil {
			return err
		}
		if !item.IsActive {
			return domain.Invalid("urun pasif: %s", item.Code)
		}
		for table, fid := range map[string]string{"supplier": req.ProducerSupplierID, "warehouse": req.ExternalWarehouseID} {
			ok, err := exists(ctx, tx, table, fid)
			if err != nil {
				return err
			}
			if !ok {
				return domain.Invalid("%s_id bulunamadi", table)
			}
		}

		orderNo := ""
		if req.OrderNo != nil {
			orderNo = *req.OrderNo
		} else if orderNo, err = nextSequenceNo(ctx, tx, "URT"); err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx,
			`INSERT INTO production_order (id, order_no, product_item_id, producer_supplier_id, external_warehouse_id,
			                                planned_qty, service_unit_price, sale_price, status)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'draft')`,
			id, orderNo, req.ProductItemID, req.ProducerSupplierID, req.ExternalWarehouseID,
			req.PlannedQty, req.ServiceUnitPrice, req.SalePrice); err != nil {
			return mapErr(err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if out, err = s.GetProductionOrder(ctx, id); err != nil {
		return nil, err
	}
	s.predictOnCreate(ctx, out) // en iyi gayret; basarisizlik islem sonucunu etkilemez
	if out, err = s.GetProductionOrder(ctx, id); err != nil {
		return nil, err
	}
	return out, nil
}

// lotSourceWarehouse lotun yeterli bakiyeye sahip OLDUGU depoyu bulur (birden fazla depoda yeterli
// varsa en yuksek bakiyeliyi secer). Lot satiri FOR UPDATE kilitlenir; es zamanli sevkler sira bekler.
// Coklu depoya bolunmus sevkiyat desteklenmez (bkz. docs/assumptions.md).
func (s *Store) lotSourceWarehouse(ctx context.Context, tx *sql.Tx, lotID string, needed decimal.Decimal) (string, error) {
	var tmp string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM lot WHERE id = ? FOR UPDATE`, lotID).Scan(&tmp); err != nil {
		return "", mapErr(err)
	}
	rows, err := tx.QueryContext(ctx, `SELECT warehouse_id, qty_on_hand FROM v_lot_balance WHERE lot_id = ? AND qty_on_hand > 0`, lotID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	best, bestQty, any := "", decimal.Zero, false
	for rows.Next() {
		var wh string
		var q domain.Qty
		if err := rows.Scan(&wh, &q); err != nil {
			return "", err
		}
		any = true
		if q.GreaterThanOrEqual(needed) && (best == "" || q.GreaterThan(bestQty)) {
			best, bestQty = wh, q.Decimal
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if best == "" {
		if !any {
			return "", domain.Conflict("lot stokta yok")
		}
		return "", domain.Conflict("lot stogu yetersiz veya birden fazla depoya bolunmus (tek depoda yeterli miktar yok)")
	}
	return best, nil
}

// sendSourceWarehouse bir girdinin hangi (ana) depodan sevk edildigini defterden okur (schema
// degisikligi gerektirmez: production_input+lot UNIQUE kisiti sayesinde her lot icin tam bir
// send_to_producer hareketi vardir).
func sendSourceWarehouse(ctx context.Context, tx *sql.Tx, orderID, lotID string) (string, error) {
	var wh string
	err := tx.QueryRowContext(ctx,
		`SELECT warehouse_id FROM stock_movement
		 WHERE ref_type = 'production_order' AND ref_id = ? AND lot_id = ? AND movement_type = 'send_to_producer'`,
		orderID, lotID).Scan(&wh)
	if errors.Is(err, sql.ErrNoRows) {
		return "", domain.Invalid("lot bu siparise sevk edilmemis")
	}
	return wh, mapErr(err)
}

// SendMaterials hammaddeleri dis uretici deposuna sevk eder: her satir icin send_to_producer (-) ve
// transfer_in (+) hareket cifti + production_input satiri, tek transaction'da. draft veya
// materials_sent durumunda birden fazla cagriya (kismi sevkiyat) izin verilir.
func (s *Store) SendMaterials(ctx context.Context, orderID string, req domain.SendMaterialsRequest) (*domain.ProductionOrder, error) {
	err := s.InTx(ctx, func(tx *sql.Tx) error {
		var status, externalWhID string
		if err := tx.QueryRowContext(ctx,
			`SELECT status, external_warehouse_id FROM production_order WHERE id = ? FOR UPDATE`, orderID).
			Scan(&status, &externalWhID); err != nil {
			return mapErr(err)
		}
		if err := domain.CanSendMaterials(status); err != nil {
			return err
		}

		for i, line := range req.Lines {
			lot, err := getLot(ctx, tx, line.LotID, false)
			if errors.Is(err, domain.ErrNotFound) {
				return domain.Invalid("lines[%d].lot_id bulunamadi", i)
			} else if err != nil {
				return err
			}
			if lot.Status != "active" {
				return domain.Invalid("lot %s aktif degil (durum: %s), sevk edilemez", lot.LotNo, lot.Status)
			}
			// receive kategorisiz satiri reddeder; burada yakalanmazsa siparis materials_sent'te kilitli kalir.
			lotItem, err := getItem(ctx, tx, lot.ItemID)
			if err != nil {
				return err
			}
			if lotItem.MaterialCategory == nil {
				return domain.Invalid("lot %s: urun %s icin hammadde kategorisi tanimli degil (item.material_category), sevk edilemez",
					lot.LotNo, lotItem.Code)
			}
			srcWh, err := s.lotSourceWarehouse(ctx, tx, line.LotID, line.Qty.Decimal)
			if err != nil {
				return err
			}
			if _, err := insertMovement(ctx, tx, domain.StockMovement{
				LotID: line.LotID, WarehouseID: srcWh, MovementType: "send_to_producer",
				Qty: domain.NewQty(line.Qty.Decimal.Neg()), UnitCost: lot.UnitCost,
				RefType: "production_order", RefID: orderID,
			}); err != nil {
				return err
			}
			if _, err := insertMovement(ctx, tx, domain.StockMovement{
				LotID: line.LotID, WarehouseID: externalWhID, MovementType: "transfer_in",
				Qty: line.Qty, UnitCost: lot.UnitCost, RefType: "production_order", RefID: orderID,
			}); err != nil {
				return err
			}
			if err := appendLotBalance(ctx, tx, line.LotID); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO production_input (id, production_order_id, lot_id, qty_sent, unit_cost_at_send)
				 VALUES (?, ?, ?, ?, ?)`,
				domain.NewID(), orderID, line.LotID, line.Qty, lot.UnitCost); err != nil {
				return mapErr(err)
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE production_order SET status = 'materials_sent' WHERE id = ?`, orderID); err != nil {
			return mapErr(err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Bu noktada gercek malzeme maliyeti kesindir; ML'e gercek et_kg ile sorup hibrit EKP tahmini
	// uretir (en iyi gayret, ayri bir prediction satiri olarak -- oncekinin uzerine yazmaz).
	s.predictOnMaterialsSent(ctx, orderID)
	return s.GetProductionOrder(ctx, orderID)
}

// ReceiveProduction mamulu teslim alir ve EKP'yi domain.CalculateProductionCost ile hesaplar:
//   - iade edilen miktarlar icin transfer_out/transfer_in (dis depo -> ana depo);
//   - iade edilmeyen (fire dahil) miktar icin production_consume (dis depo, -);
//   - cikti lotu (origin_type=production, unit_cost=ekp_unit) + production_in (ana depo, +);
//   - her girdi icin lot_link (parent=hammadde lotu, child=cikti lotu, qty_from_parent=tuketilen).
//
// Tum girdiler AYNI depodan sevk edilmis olmali (coklu depo desteklenmez, bkz. docs/assumptions.md);
// cikti da o depoya girer.
func (s *Store) ReceiveProduction(ctx context.Context, orderID string, req domain.ReceiveProductionRequest) (*domain.ProductionOrder, error) {
	var out *domain.ProductionOrder
	err := s.InTx(ctx, func(tx *sql.Tx) error {
		var status, productItemID, externalWhID, producerSupplierID string
		var serviceUnitPrice domain.Amount
		var salePrice *domain.Amount
		var plannedQty domain.Qty
		if err := tx.QueryRowContext(ctx,
			`SELECT status, product_item_id, external_warehouse_id, producer_supplier_id, service_unit_price, sale_price, planned_qty
			 FROM production_order WHERE id = ? FOR UPDATE`, orderID).
			Scan(&status, &productItemID, &externalWhID, &producerSupplierID, &serviceUnitPrice, &salePrice, &plannedQty); err != nil {
			return mapErr(err)
		}
		if err := domain.CanReceive(status); err != nil {
			return err
		}

		product, err := getItem(ctx, tx, productItemID)
		if err != nil {
			return err
		}

		inputs, err := loadProductionInputs(ctx, tx, orderID)
		if err != nil {
			return err
		}
		if len(inputs) == 0 {
			return domain.Invalid("hammadde sevk edilmeden teslim alinamaz")
		}

		bySentLot := make(map[string]domain.ProductionInputLine, len(inputs))
		for _, in := range inputs {
			bySentLot[in.LotID] = in
		}
		returned := map[string]decimal.Decimal{}
		for i, rl := range req.Returns {
			in, ok := bySentLot[rl.LotID]
			if !ok {
				return domain.Invalid("returns[%d].lot_id bu siparise sevk edilmemis", i)
			}
			returned[rl.LotID] = returned[rl.LotID].Add(rl.Qty.Decimal)
			if returned[rl.LotID].GreaterThan(in.QtySent.Decimal) {
				return domain.Invalid("returns[%d]: iade miktari sevk edilenden fazla (lot %s)", i, in.LotNo)
			}
		}

		// Tum girdiler ayni (ana) depodan sevk edilmis olmali; cikti da o depoya girer.
		mainWarehouseID := ""
		for _, in := range inputs {
			wh, err := sendSourceWarehouse(ctx, tx, orderID, in.LotID)
			if err != nil {
				return err
			}
			if mainWarehouseID == "" {
				mainWarehouseID = wh
			} else if mainWarehouseID != wh {
				return domain.Invalid("girdi hammaddeler farkli depolardan sevk edilmis; tek depo destekleniyor")
			}
		}

		materialLots := make([]domain.MaterialLot, 0, len(inputs))
		for _, in := range inputs {
			if in.MaterialCategory == nil {
				return domain.Invalid("urun %s icin hammadde kategorisi tanimli degil (item.material_category)", in.ItemCode)
			}
			ret := returned[in.LotID]
			consumed := in.QtySent.Decimal.Sub(ret)

			if ret.IsPositive() {
				if _, err := insertMovement(ctx, tx, domain.StockMovement{
					LotID: in.LotID, WarehouseID: externalWhID, MovementType: "transfer_out",
					Qty: domain.NewQty(ret.Neg()), UnitCost: in.UnitCostAtSend, RefType: "production_order", RefID: orderID,
				}); err != nil {
					return err
				}
				if _, err := insertMovement(ctx, tx, domain.StockMovement{
					LotID: in.LotID, WarehouseID: mainWarehouseID, MovementType: "transfer_in",
					Qty: domain.NewQty(ret), UnitCost: in.UnitCostAtSend, RefType: "production_order", RefID: orderID,
				}); err != nil {
					return err
				}
			}
			if consumed.IsPositive() {
				if _, err := insertMovement(ctx, tx, domain.StockMovement{
					LotID: in.LotID, WarehouseID: externalWhID, MovementType: "production_consume",
					Qty: domain.NewQty(consumed.Neg()), UnitCost: in.UnitCostAtSend, RefType: "production_order", RefID: orderID,
				}); err != nil {
					return err
				}
			}
			if _, err := tx.ExecContext(ctx, `UPDATE production_input SET qty_consumed = ? WHERE id = ?`,
				domain.NewQty(consumed), in.ID); err != nil {
				return mapErr(err)
			}
			if err := appendLotBalance(ctx, tx, in.LotID); err != nil {
				return err
			}

			materialLots = append(materialLots, domain.MaterialLot{
				Name: in.ItemName, Category: *in.MaterialCategory, Quantity: consumed, UnitCost: in.UnitCostAtSend.Decimal,
			})
		}

		invoicedQty := req.ReceivedQty
		if req.ServiceInvoicedQty != nil {
			invoicedQty = *req.ServiceInvoicedQty
		}
		var unitWeight *decimal.Decimal
		if product.UnitWeightKg != nil {
			unitWeight = &product.UnitWeightKg.Decimal
		}
		var sp *decimal.Decimal
		if salePrice != nil {
			sp = &salePrice.Decimal
		}
		result, err := domain.CalculateProductionCost(domain.ProductionInput{
			Lots: materialLots, DeliveredQty: req.ReceivedQty.Decimal, ServiceUnitPrice: serviceUnitPrice.Decimal,
			InvoicedQty: &invoicedQty.Decimal, SalePrice: sp, UnitWeightKg: unitWeight,
		})
		if err != nil {
			return err
		}

		// Cikti lotu
		outputLotID := domain.NewID()
		lotNo := ""
		if req.OutputLotNo != nil {
			lotNo = *req.OutputLotNo
		} else if lotNo, err = nextSequenceNo(ctx, tx, "SUC"); err != nil {
			return err
		}
		ekp := decimal.RequireFromString(result.EkpUnit)
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO lot (id, lot_no, item_id, supplier_id, origin_type, origin_id, received_or_produced_at,
			                  expiry_date, initial_qty, uom, unit_cost, status)
			 VALUES (?, ?, ?, ?, 'production', ?, ?, ?, ?, ?, ?, 'active')`,
			outputLotID, lotNo, productItemID, producerSupplierID, orderID,
			domain.Today(), domain.ExpiryFrom(req.ExpiryDate, domain.Today(), product.ShelfLifeDays),
			req.ReceivedQty, product.BaseUOM, domain.NewAmount(ekp)); err != nil {
			return mapErr(err)
		}
		if _, err := insertMovement(ctx, tx, domain.StockMovement{
			LotID: outputLotID, WarehouseID: mainWarehouseID, MovementType: "production_in",
			Qty: req.ReceivedQty, UnitCost: domain.NewAmount(ekp), RefType: "production_order", RefID: orderID,
		}); err != nil {
			return err
		}
		// hareketten SONRA ve bakiyeli: cihaz yeni lotun stogunu pull'dan ogrenir (docs/sync.md)
		outLot, err := getLot(ctx, tx, outputLotID, true)
		if err != nil {
			return err
		}
		if err := appendChange(ctx, tx, "lot", outputLotID, "upsert", outLot); err != nil {
			return err
		}
		for _, in := range inputs {
			consumed := in.QtySent.Decimal.Sub(returned[in.LotID])
			if !consumed.IsPositive() {
				continue
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO lot_link (id, parent_lot_id, child_lot_id, qty_from_parent, uom, process_type, process_id)
				 VALUES (?, ?, ?, ?, 'kg', 'production', ?)`,
				domain.NewID(), in.LotID, outputLotID, domain.NewQty(consumed), orderID); err != nil {
				return mapErr(err)
			}
		}

		if _, err := tx.ExecContext(ctx,
			`UPDATE production_order
			 SET status = 'received', received_qty = ?, output_lot_id = ?, service_invoice_no = ?,
			     service_invoiced_qty = ?, material_cost = ?, service_cost = ?, total_cost = ?, ekp_unit = ?
			 WHERE id = ?`,
			req.ReceivedQty, outputLotID, req.ServiceInvoiceNo, domain.NewQty(invoicedQty.Decimal),
			domain.NewAmount(decimal.RequireFromString(result.MaterialCost)),
			domain.NewAmount(decimal.RequireFromString(result.ServiceCost)),
			domain.NewAmount(decimal.RequireFromString(result.TotalCost)),
			domain.NewAmount(ekp), orderID); err != nil {
			return mapErr(err)
		}

		if err := s.recordPredictionActual(ctx, tx, orderID, req.ReceivedQty.Decimal, ekp); err != nil {
			slog.Warn("tahmin/gercek karsilastirmasi yazilamadi", "order_id", orderID, "err", err)
		}

		out, err = getProductionOrderDetail(ctx, tx, orderID)
		return err
	})
	if err == nil {
		s.detectProductionAnomaly(ctx, orderID) // en iyi gayret, commit sonrasi (bkz. anomaly.go)
	}
	return out, err
}

// CloseProductionOrder siparisi kapatir; servis fatura numarasi (receive'de veya burada) verilmemisse
// 422. Fatura genelde mamulden sonra gelir; bu yuzden receive'de opsiyoneldir ve burada
// (req.ServiceInvoiceNo) tamamlanabilir.
func (s *Store) CloseProductionOrder(ctx context.Context, orderID string, req domain.CloseProductionOrderRequest) (*domain.ProductionOrder, error) {
	var out *domain.ProductionOrder
	err := s.InTx(ctx, func(tx *sql.Tx) error {
		var status string
		var invoiceNo *string
		if err := tx.QueryRowContext(ctx, `SELECT status, service_invoice_no FROM production_order WHERE id = ? FOR UPDATE`, orderID).
			Scan(&status, &invoiceNo); err != nil {
			return mapErr(err)
		}
		if err := domain.CanClose(status); err != nil {
			return err
		}
		if req.ServiceInvoiceNo != nil {
			invoiceNo = req.ServiceInvoiceNo
		}
		if invoiceNo == nil || strings.TrimSpace(*invoiceNo) == "" {
			return domain.Invalid("servis fatura numarasi olmadan siparis kapatilamaz")
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE production_order SET status = 'closed', service_invoice_no = ?, closed_at = CURRENT_TIMESTAMP(3) WHERE id = ?`,
			invoiceNo, orderID); err != nil {
			return mapErr(err)
		}
		var err error
		out, err = getProductionOrderDetail(ctx, tx, orderID)
		return err
	})
	return out, err
}

// GetProductionCostReport dokumandaki bicimde maliyet raporu uretir. material_cost/service_cost/
// total_cost/ekp_unit production_order'da sakli degerlerdir; verim/kar/marj/ekp_display her istekte
// CalculateProductionCost ile taze hesaplanir (receive'deki ile ayni girdilerle, ayni sonucu verir).
func (s *Store) GetProductionCostReport(ctx context.Context, orderID string) (*domain.ProductionCostReport, error) {
	o, err := s.GetProductionOrder(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if o.ReceivedQty == nil || o.MaterialCost == nil {
		return nil, domain.Invalid("maliyet raporu icin siparis once teslim alinmis olmali (durum: %s)", o.Status)
	}

	materialLots := make([]domain.MaterialLot, 0, len(o.Inputs))
	lines := make([]domain.CostReportLine, 0, len(o.Inputs)+1)
	for _, in := range o.Inputs {
		if in.QtyConsumed == nil || in.MaterialCategory == nil {
			continue
		}
		materialLots = append(materialLots, domain.MaterialLot{
			Name: in.ItemName, Category: *in.MaterialCategory, Quantity: in.QtyConsumed.Decimal, UnitCost: in.UnitCostAtSend.Decimal,
		})
		lines = append(lines, domain.CostReportLine{
			Label: in.ItemName, Qty: ptrStr(in.QtyConsumed.StringFixed(domain.QtyScale)),
			UnitPrice: ptrStr(in.UnitCostAtSend.StringFixed(domain.AmountScale)),
			Amount:    domain.LineTotal(in.QtyConsumed.Decimal, in.UnitCostAtSend.Decimal).StringFixed(2),
		})
	}

	var unitWeight *decimal.Decimal
	if o.ProductUnitWeightKg != nil {
		unitWeight = &o.ProductUnitWeightKg.Decimal
	}
	var sp *decimal.Decimal
	if o.SalePrice != nil {
		sp = &o.SalePrice.Decimal
	}
	invoiced := o.ReceivedQty.Decimal
	if o.ServiceInvoicedQty != nil {
		invoiced = o.ServiceInvoicedQty.Decimal
	}
	result, err := domain.CalculateProductionCost(domain.ProductionInput{
		Lots: materialLots, DeliveredQty: o.ReceivedQty.Decimal, ServiceUnitPrice: o.ServiceUnitPrice.Decimal,
		InvoicedQty: &invoiced, SalePrice: sp, UnitWeightKg: unitWeight,
	})
	if err != nil {
		return nil, err
	}
	lines = append(lines, domain.CostReportLine{
		Label: "Hizmet bedeli", Qty: ptrStr(invoiced.StringFixed(domain.QtyScale)),
		UnitPrice: ptrStr(o.ServiceUnitPrice.StringFixed(domain.AmountScale)), Amount: result.ServiceCost,
	})

	warnings := append([]string{}, result.Warnings...)
	if domain.QtyVariancePct(o.ReceivedQty.Decimal, o.PlannedQty.Decimal).GreaterThan(domain.QtyVarianceThresholdPct) {
		warnings = append(warnings, domain.WarnQtyVariance)
	}

	return &domain.ProductionCostReport{
		OrderNo: o.OrderNo, Status: o.Status, Lines: lines,
		MaterialCost: result.MaterialCost, ServiceCost: result.ServiceCost, TotalCost: result.TotalCost,
		PlannedQty: o.PlannedQty.StringFixed(domain.QtyScale), ReceivedQty: o.ReceivedQty.StringFixed(domain.QtyScale),
		InvoicedQty:    invoiced.StringFixed(domain.QtyScale),
		QtyVariancePct: domain.QtyVariancePct(o.ReceivedQty.Decimal, o.PlannedQty.Decimal).StringFixed(2),
		EkpUnit:        result.EkpUnit, EkpDisplay: result.EkpDisplay, EkpPerKg: result.EkpPerKg,
		UnitWeightKg: result.UnitWeightKg, OutputKg: result.OutputKg, InputKgTotal: result.InputKgTotal,
		YieldInputPct: result.YieldInputPct, YieldMeatRatio: result.YieldMeatRatio,
		SalePrice: result.SalePrice, Revenue: result.Revenue, GrossProfitUnit: result.GrossProfitUnit,
		GrossProfitTotal: result.GrossProfitTotal, MarginPct: result.MarginPct,
		Warnings: warnings, Prediction: o.Prediction,
	}, nil
}
