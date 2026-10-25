package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// Gorev 1 uctan uca: dis hizmetle uretim siparisi. Durum makinesi:
//
//	draft -> materials_sent -> received -> closed
//
// Gecersiz gecisler 409 (Conflict) doner. Maliyet hesabi receive adiminda CalculateProductionCost ile
// yapilir (bkz. production_cost.go); bu dosya yalnizca siparis varligini, istek govdelerini ve
// durum gecisi kurallarini tasir. SQL/transaction mantigi repo/production.go'dadir.

const (
	ProductionDraft         = "draft"
	ProductionMaterialsSent = "materials_sent"
	ProductionReceived      = "received"
	ProductionClosed        = "closed"

	// WarnQtyVariance: |teslim alinan - planlanan| / planlanan > QtyVarianceThresholdPct.
	WarnQtyVariance = "QTY_VARIANCE"
)

// ProductionOrderStatuses: gecerli durum degerleri (query filtresi dogrulamasinda kullanilir).
var ProductionOrderStatuses = set(ProductionDraft, ProductionMaterialsSent, ProductionReceived, ProductionClosed)

// QtyVarianceThresholdPct: bu esigin USTUNDE (katı, esitlik degil) fark QTY_VARIANCE uyarisi uretir.
var QtyVarianceThresholdPct = decimal.RequireFromString("2")

// ---------------------------------------------------------------------------
// Varlik
// ---------------------------------------------------------------------------

// ProductionOrder API JSON'udur (GET/POST yanitlarinda doner). Inputs yalnizca detay (GET /{id})
// yanitinda doludur.
type ProductionOrder struct {
	ID                    string                    `json:"id"`
	OrderNo               string                    `json:"order_no"`
	ProductItemID         string                    `json:"product_item_id"`
	ProductItemCode       string                    `json:"product_item_code"`
	ProductItemName       string                    `json:"product_item_name"`
	ProductUnitWeightKg   *Qty                      `json:"product_unit_weight_kg"`
	ProducerSupplierID    string                    `json:"producer_supplier_id"`
	ProducerSupplierName  string                    `json:"producer_supplier_name"`
	ExternalWarehouseID   string                    `json:"external_warehouse_id"`
	ExternalWarehouseCode string                    `json:"external_warehouse_code"`
	PlannedQty            Qty                       `json:"planned_qty"`
	ReceivedQty           *Qty                      `json:"received_qty"`
	ServiceUnitPrice      Amount                    `json:"service_unit_price"`
	ServiceInvoiceNo      *string                   `json:"service_invoice_no"`
	ServiceInvoicedQty    *Qty                      `json:"service_invoiced_qty"`
	Status                string                    `json:"status"`
	OutputLotID           *string                   `json:"output_lot_id"`
	OutputLotNo           *string                   `json:"output_lot_no"`
	MaterialCost          *Amount                   `json:"material_cost"`
	ServiceCost           *Amount                   `json:"service_cost"`
	TotalCost             *Amount                   `json:"total_cost"`
	EkpUnit               *Amount                   `json:"ekp_unit"`
	SalePrice             *Amount                   `json:"sale_price"`
	ClosedAt              *time.Time                `json:"closed_at"`
	CreatedAt             time.Time                 `json:"created_at"`
	Warnings              []string                  `json:"warnings"`
	Inputs                []ProductionInputLine     `json:"inputs,omitempty"`
	StatusHistory         []ProductionStatusEvent   `json:"status_history,omitempty"`
	Prediction            *ProductionPredictionView `json:"prediction,omitempty"`
}

// ProductionInputLine bir sevk edilen hammadde satiri (production_input + lot/item bilgisi).
type ProductionInputLine struct {
	ID               string  `json:"id"`
	LotID            string  `json:"lot_id"`
	LotNo            string  `json:"lot_no"`
	ItemID           string  `json:"item_id"`
	ItemCode         string  `json:"item_code"`
	ItemName         string  `json:"item_name"`
	MaterialCategory *string `json:"material_category"`
	QtySent          Qty     `json:"qty_sent"`
	QtyConsumed      *Qty    `json:"qty_consumed"`
	QtyReturned      *Qty    `json:"qty_returned"`
	UnitCostAtSend   Amount  `json:"unit_cost_at_send"`
}

// ProductionPredictionView prediction tablosunun API gorunumudur (model tahmini vs gerceklesen).
// EN GUNCEL kayit donulur (stage='materials_sent' varsa o, yoksa 'draft'). EKP tahmini HIBRIT bir
// yaklasimdir: ML yalnizca fiziksel verimi (adet/kg) tahmin eder, maliyeti bilmez; predicted_ekp_unit
// bu tahmini KESIN malzeme maliyetiyle birlestirerek sunucu tarafinda hesaplanir (bkz.
// docs/assumptions.md, "Hibrit EKP tahmini"). Bu yuzden 'draft' asamasinda (malzeme maliyeti henuz
// bilinmezken) predicted_ekp_unit alani null'dur; yalnizca 'materials_sent' asamasinda doludur.
type ProductionPredictionView struct {
	ModelName             string  `json:"model_name"`
	ModelVersion          string  `json:"model_version"`
	Baseline              bool    `json:"baseline"`
	Stage                 string  `json:"stage"` // draft | materials_sent
	PredictedYieldRatio   string  `json:"predicted_yield_ratio"`
	PredictedOutputKg     string  `json:"predicted_output_kg"`
	PredictedDeliveredQty string  `json:"predicted_delivered_qty"`
	PredictedEkpUnit      *string `json:"predicted_ekp_unit"`
	ActualDeliveredQty    *string `json:"actual_delivered_qty"`
	ActualEkpUnit         *string `json:"actual_ekp_unit"`
	DeliveredQtyErrorAbs  *string `json:"delivered_qty_error_abs"`
	DeliveredQtyErrorPct  *string `json:"delivered_qty_error_pct"`
	EkpErrorAbs           *string `json:"ekp_error_abs"`
	EkpErrorPct           *string `json:"ekp_error_pct"`
}

// ---------------------------------------------------------------------------
// Istek govdeleri
// ---------------------------------------------------------------------------

// CreateProductionOrderRequest POST /production-orders govdesi.
type CreateProductionOrderRequest struct {
	ID                  *string `json:"id"`
	OrderNo             *string `json:"order_no"` // bos ise URT-YYYY-NNN uretilir
	ProductItemID       string  `json:"product_item_id"`
	ProducerSupplierID  string  `json:"producer_supplier_id"`
	ExternalWarehouseID string  `json:"external_warehouse_id"`
	PlannedQty          Qty     `json:"planned_qty"`
	ServiceUnitPrice    Amount  `json:"service_unit_price"`
	SalePrice           *Amount `json:"sale_price"`
}

func (r *CreateProductionOrderRequest) Validate() error {
	for field, id := range map[string]string{
		"product_item_id": r.ProductItemID, "producer_supplier_id": r.ProducerSupplierID,
		"external_warehouse_id": r.ExternalWarehouseID,
	} {
		if _, err := ParseID(id); err != nil {
			return Invalid("%s: %s", field, err)
		}
	}
	if r.ID != nil {
		if _, err := ParseID(*r.ID); err != nil {
			return Invalid("id: %s", err)
		}
	}
	trimPtr(&r.OrderNo)
	if r.OrderNo != nil && len(*r.OrderNo) > 40 {
		return Invalid("order_no en fazla 40 karakter olabilir")
	}
	if !r.PlannedQty.IsPositive() {
		return Invalid("planned_qty sifirdan buyuk olmali")
	}
	if r.ServiceUnitPrice.IsNegative() {
		return Invalid("service_unit_price negatif olamaz")
	}
	if r.SalePrice != nil && r.SalePrice.IsNegative() {
		return Invalid("sale_price negatif olamaz")
	}
	return nil
}

// SendMaterialsLine bir hammadde sevk satiri.
type SendMaterialsLine struct {
	LotID string `json:"lot_id"`
	Qty   Qty    `json:"qty"`
}

// SendMaterialsRequest POST /production-orders/{id}/send-materials govdesi.
type SendMaterialsRequest struct {
	Lines []SendMaterialsLine `json:"lines"`
}

func (r *SendMaterialsRequest) Validate() error {
	if len(r.Lines) == 0 {
		return Invalid("lines bos olamaz")
	}
	seen := map[string]bool{}
	for i, l := range r.Lines {
		if _, err := ParseID(l.LotID); err != nil {
			return Invalid("lines[%d].lot_id: %s", i, err)
		}
		if !l.Qty.IsPositive() {
			return Invalid("lines[%d].qty sifirdan buyuk olmali", i)
		}
		if seen[l.LotID] {
			return Invalid("ayni lot (%s) birden fazla satirda olamaz", l.LotID)
		}
		seen[l.LotID] = true
	}
	return nil
}

// ReceiveReturnLine dis uretici deposundan iade edilen miktar (orijinal sevk lotlarindan biri).
type ReceiveReturnLine struct {
	LotID string `json:"lot_id"`
	Qty   Qty    `json:"qty"`
}

// ReceiveProductionRequest POST /production-orders/{id}/receive govdesi.
//
// service_invoice_no gercekte opsiyoneldir: hizmet faturasi teslimden sonra gelebilir; kapatma
// (close) bu alanin dolu olmasini zorunlu kilar (422, doldurulmamissa).
type ReceiveProductionRequest struct {
	ReceivedQty        Qty                 `json:"received_qty"`
	OutputLotNo        *string             `json:"output_lot_no"` // bos ise SUC-YYYY-NNN uretilir
	ExpiryDate         *Date               `json:"expiry_date"`
	ServiceInvoiceNo   *string             `json:"service_invoice_no"`
	ServiceInvoicedQty *Qty                `json:"service_invoiced_qty"` // bos ise received_qty
	Returns            []ReceiveReturnLine `json:"returns"`
}

func (r *ReceiveProductionRequest) Validate() error {
	if !r.ReceivedQty.IsPositive() {
		return Invalid("received_qty sifirdan buyuk olmali")
	}
	trimPtr(&r.OutputLotNo)
	if r.OutputLotNo != nil && len(*r.OutputLotNo) > 40 {
		return Invalid("output_lot_no en fazla 40 karakter olabilir")
	}
	trimPtr(&r.ServiceInvoiceNo)
	if r.ServiceInvoiceNo != nil && len(*r.ServiceInvoiceNo) > 60 {
		return Invalid("service_invoice_no en fazla 60 karakter olabilir")
	}
	if r.ServiceInvoicedQty != nil && !r.ServiceInvoicedQty.IsPositive() {
		return Invalid("service_invoiced_qty sifirdan buyuk olmali")
	}
	seen := map[string]bool{}
	for i, l := range r.Returns {
		if _, err := ParseID(l.LotID); err != nil {
			return Invalid("returns[%d].lot_id: %s", i, err)
		}
		if !l.Qty.IsPositive() {
			return Invalid("returns[%d].qty sifirdan buyuk olmali", i)
		}
		if seen[l.LotID] {
			return Invalid("ayni lot (%s) birden fazla iade satirinda olamaz", l.LotID)
		}
		seen[l.LotID] = true
	}
	return nil
}

// CloseProductionOrderRequest POST /production-orders/{id}/close govdesi (opsiyonel).
//
// service_invoice_no receive'de verilmemisse (fatura teslimden sonra geldiyse) burada saglanabilir;
// receive'de zaten verilmisse ve burada da gonderilirse buradaki deger gecerli olur (duzeltme).
type CloseProductionOrderRequest struct {
	ServiceInvoiceNo *string `json:"service_invoice_no"`
}

func (r *CloseProductionOrderRequest) Validate() error {
	trimPtr(&r.ServiceInvoiceNo)
	if r.ServiceInvoiceNo != nil && len(*r.ServiceInvoiceNo) > 60 {
		return Invalid("service_invoice_no en fazla 60 karakter olabilir")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Durum gecisi kontrolleri (repo bu fonksiyonlari transaction icinde cagirir)
// ---------------------------------------------------------------------------

// CanSendMaterials: draft veya materials_sent durumunda birden fazla sevk kabul edilir (kismi sevkiyat).
func CanSendMaterials(status string) error {
	if status != ProductionDraft && status != ProductionMaterialsSent {
		return Conflict("gecersiz durum gecisi: %s -> sevk", status)
	}
	return nil
}

func CanReceive(status string) error {
	if status != ProductionMaterialsSent {
		return Conflict("gecersiz durum gecisi: %s -> received (once hammadde sevk edilmeli)", status)
	}
	return nil
}

func CanClose(status string) error {
	if status != ProductionReceived {
		return Conflict("gecersiz durum gecisi: %s -> closed (once mamul teslim alinmali)", status)
	}
	return nil
}

// QtyVariancePct: |received - planned| / planned * 100, 2 ondalik. planned<=0 beklenmez (zaten Validate reddeder).
func QtyVariancePct(received, planned decimal.Decimal) decimal.Decimal {
	return Round2(received.Sub(planned).Abs().Mul(decimal.NewFromInt(100)).Div(planned))
}

// ComputeProductionWarnings siparisin saklanan alanlarindan (hesaplama yapmadan) uyari listesi cikarir.
// GET /production-orders (liste + detay) icin kullanilir; maliyet raporu kendi uyarilarini
// CalculateProductionCost'tan tazeden alir (bkz. repo/production.go).
func ComputeProductionWarnings(o *ProductionOrder) []string {
	warnings := []string{}
	if o.ReceivedQty == nil {
		return warnings
	}
	invoiced := o.ReceivedQty.Decimal
	if o.ServiceInvoicedQty != nil {
		invoiced = o.ServiceInvoicedQty.Decimal
	}
	if !invoiced.Equal(o.ReceivedQty.Decimal) {
		warnings = append(warnings, WarnInvoicedQtyMismatch)
	}
	if QtyVariancePct(o.ReceivedQty.Decimal, o.PlannedQty.Decimal).GreaterThan(QtyVarianceThresholdPct) {
		warnings = append(warnings, WarnQtyVariance)
	}
	return warnings
}

// ---------------------------------------------------------------------------
// Durum gecmisi (ayri bir tablo yerine mevcut zaman damgalarindan turetilir)
// ---------------------------------------------------------------------------

type ProductionStatusEvent struct {
	Status string    `json:"status"`
	At     time.Time `json:"at"`
}

// ---------------------------------------------------------------------------
// Maliyet raporu (GET /production-orders/{id}/cost-report)
// ---------------------------------------------------------------------------

// CostReportLine dokumandaki tabloya karsilik gelir: "Maliyet unsuru | miktar | birim fiyat | tutar".
type CostReportLine struct {
	Label     string  `json:"label"`
	Qty       *string `json:"qty"`
	UnitPrice *string `json:"unit_price"`
	Amount    string  `json:"amount"`
}

// ProductionCostReport siparis kapandiktan/teslim alindiktan sonra GET ile uretilir; kalici alanlar
// (material_cost/service_cost/total_cost/ekp_unit) production_order'da saklanir, geri kalani
// (verim, kar, marj, ekp_display) her istekte CalculateProductionCost ile taze hesaplanir.
type ProductionCostReport struct {
	OrderNo          string                    `json:"order_no"`
	Status           string                    `json:"status"`
	Lines            []CostReportLine          `json:"lines"`
	MaterialCost     string                    `json:"material_cost"`
	ServiceCost      string                    `json:"service_cost"`
	TotalCost        string                    `json:"total_cost"`
	PlannedQty       string                    `json:"planned_qty"`
	ReceivedQty      string                    `json:"received_qty"`
	InvoicedQty      string                    `json:"invoiced_qty"`
	QtyVariancePct   string                    `json:"qty_variance_pct"`
	EkpUnit          string                    `json:"ekp_unit"`
	EkpDisplay       string                    `json:"ekp_display"`
	EkpPerKg         *string                   `json:"ekp_per_kg"`
	UnitWeightKg     *string                   `json:"unit_weight_kg"`
	OutputKg         *string                   `json:"output_kg"`
	InputKgTotal     string                    `json:"input_kg_total"`
	YieldInputPct    *string                   `json:"yield_input_pct"`
	YieldMeatRatio   *string                   `json:"yield_meat_ratio"`
	SalePrice        *string                   `json:"sale_price"`
	Revenue          *string                   `json:"revenue"`
	GrossProfitUnit  *string                   `json:"gross_profit_unit"`
	GrossProfitTotal *string                   `json:"gross_profit_total"`
	MarginPct        *string                   `json:"margin_pct"`
	Warnings         []string                  `json:"warnings"`
	Prediction       *ProductionPredictionView `json:"prediction"`
}
