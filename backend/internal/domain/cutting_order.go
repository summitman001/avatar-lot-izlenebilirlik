package domain

import (
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// Gorev 2 (parcalama) servis katmani: recete yonetimi + parcalama emri yasam dongusu
// (draft -> closed). Maliyet dagitim hesabi AllocateCutting ile yapilir (bkz. cutting.go); bu dosya
// yalnizca varliklari, istek govdelerini ve durum gecisi kurallarini tasir. SQL/transaction mantigi
// repo/cutting.go'dadir.

const (
	CuttingDraft  = "draft"
	CuttingClosed = "closed"
)

var CuttingOrderStatuses = set(CuttingDraft, CuttingClosed)

// RatioSumTolerance: recete satirlarinin expected_ratio toplami 1.0000'den bu kadar sapabilir.
var RatioSumTolerance = decimal.RequireFromString("0.0005")

// ---------------------------------------------------------------------------
// Recete (cutting_recipe + cutting_recipe_line)
// ---------------------------------------------------------------------------

// CuttingRecipeLine hem istek (POST/PUT govdesi) hem yanit govdesidir; PartItemCode/Name yalniz
// yanitta doludur.
type CuttingRecipeLine struct {
	PartItemID         string `json:"part_item_id"`
	PartItemCode       string `json:"part_item_code,omitempty"`
	PartItemName       string `json:"part_item_name,omitempty"`
	ExpectedRatio      Amount `json:"expected_ratio"`
	ReferenceSalePrice Amount `json:"reference_sale_price"`
	IsByProduct        bool   `json:"is_by_product"`
	IsWaste            bool   `json:"is_waste"`
}

// CuttingRecipe GET/POST/PUT /cutting-recipes govdesidir (Item gibi: tek tip hem istek hem yanit).
type CuttingRecipe struct {
	ID         string              `json:"id"`
	Code       string              `json:"code"`
	Name       string              `json:"name"`
	AnimalType string              `json:"animal_type"`
	Lines      []CuttingRecipeLine `json:"lines"`
	CreatedAt  time.Time           `json:"created_at"`
}

func (r *CuttingRecipe) Validate() error {
	r.Code, r.Name, r.AnimalType = strings.TrimSpace(r.Code), strings.TrimSpace(r.Name), strings.TrimSpace(r.AnimalType)
	if err := required("code", r.Code, 40); err != nil {
		return err
	}
	if err := required("name", r.Name, 160); err != nil {
		return err
	}
	if err := required("animal_type", r.AnimalType, 40); err != nil {
		return err
	}
	if len(r.Lines) == 0 {
		return Invalid("lines bos olamaz")
	}
	seen := map[string]bool{}
	ratioSum := decimal.Zero
	for i, l := range r.Lines {
		if _, err := ParseID(l.PartItemID); err != nil {
			return Invalid("lines[%d].part_item_id: %s", i, err)
		}
		if seen[l.PartItemID] {
			return Invalid("ayni parca (%s) birden fazla satirda olamaz", l.PartItemID)
		}
		seen[l.PartItemID] = true
		if l.IsWaste && l.IsByProduct {
			return Invalid("lines[%d]: parca hem fire hem yan urun olamaz", i)
		}
		if !l.ExpectedRatio.IsPositive() {
			return Invalid("lines[%d].expected_ratio sifirdan buyuk olmali", i)
		}
		if l.ReferenceSalePrice.IsNegative() {
			return Invalid("lines[%d].reference_sale_price negatif olamaz", i)
		}
		ratioSum = ratioSum.Add(l.ExpectedRatio.Decimal)
	}
	if ratioSum.Sub(decimal.NewFromInt(1)).Abs().GreaterThan(RatioSumTolerance) {
		return Invalid("expected_ratio toplami 1.0000 (±%s) olmali, gelen: %s", RatioSumTolerance, ratioSum.StringFixed(4))
	}
	return nil
}

// ---------------------------------------------------------------------------
// Parcalama emri (cutting_order + cutting_output)
// ---------------------------------------------------------------------------

// CreateCuttingOrderRequest POST /cutting-orders govdesi. cost_basis/resolution_note SIPARIS
// OLUSTURULURKEN sabitlenir: giris lotunun alisinda PURCHASE_TOTAL_MISMATCH varsa ikisi de ZORUNLUDUR
// (verilmezse siparis yine olusur ama closable=false ve MISMATCH_RESOLUTION_MISSING doner).
type CreateCuttingOrderRequest struct {
	ID               *string   `json:"id"`
	OrderNo          *string   `json:"order_no"` // bos ise CUT-YYYY-NNN atomik uretilir
	RecipeID         string    `json:"recipe_id"`
	InputLotID       string    `json:"input_lot_id"`
	InputWeightKg    Qty       `json:"input_weight_kg"`
	AllocationMethod string    `json:"allocation_method"` // quantity|value
	CutAt            time.Time `json:"cut_at"`
	CostBasis        *string   `json:"cost_basis"` // computed|declared
	ResolutionNote   *string   `json:"resolution_note"`
}

func (r *CreateCuttingOrderRequest) Validate() error {
	for field, id := range map[string]string{"recipe_id": r.RecipeID, "input_lot_id": r.InputLotID} {
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
	if !r.InputWeightKg.IsPositive() {
		return Invalid("input_weight_kg sifirdan buyuk olmali")
	}
	if r.AllocationMethod != MethodQuantity && r.AllocationMethod != MethodValue {
		return Invalid("allocation_method gecersiz: %q", r.AllocationMethod)
	}
	if r.CutAt.IsZero() {
		return Invalid("cut_at zorunlu")
	}
	if r.CostBasis != nil && *r.CostBasis != BasisComputed && *r.CostBasis != BasisDeclared {
		return Invalid("cost_basis gecersiz: %q", *r.CostBasis)
	}
	trimPtr(&r.ResolutionNote)
	return nil
}

// UpdateCuttingOutputsLine bir parcanin fiili agirligini tasir (0 kabul edilir: ornegin bir yan urun
// bu parcalamada hic cikmadiysa).
type UpdateCuttingOutputsLine struct {
	PartItemID     string `json:"part_item_id"`
	ActualWeightKg Qty    `json:"actual_weight_kg"`
}

// UpdateCuttingOutputsRequest PUT /cutting-orders/{id}/outputs govdesi. draft iken birden cok kez
// cagrilabilir (fiili agirliklar uzerine yazilir, UNIQUE (cutting_order_id, part_item_id)).
type UpdateCuttingOutputsRequest struct {
	Outputs []UpdateCuttingOutputsLine `json:"outputs"`
}

func (r *UpdateCuttingOutputsRequest) Validate() error {
	if len(r.Outputs) == 0 {
		return Invalid("outputs bos olamaz")
	}
	seen := map[string]bool{}
	for i, o := range r.Outputs {
		if _, err := ParseID(o.PartItemID); err != nil {
			return Invalid("outputs[%d].part_item_id: %s", i, err)
		}
		if seen[o.PartItemID] {
			return Invalid("ayni parca (%s) birden fazla satirda olamaz", o.PartItemID)
		}
		seen[o.PartItemID] = true
		if o.ActualWeightKg.IsNegative() {
			return Invalid("outputs[%d].actual_weight_kg negatif olamaz", i)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Varlik (API JSON)
// ---------------------------------------------------------------------------

// CuttingOutputLine recete satirindaki bir parcanin GUNCEL durumudur: beklenen (recete), ML tahmini
// (varsa), fiili (girilmisse) agirlik + (kapandiktan sonra) olusan lot ve dagitilan maliyet.
type CuttingOutputLine struct {
	PartItemID         string  `json:"part_item_id"`
	PartItemCode       string  `json:"part_item_code"`
	PartItemName       string  `json:"part_item_name"`
	IsWaste            bool    `json:"is_waste"`
	IsByProduct        bool    `json:"is_by_product"`
	ExpectedRatio      Amount  `json:"expected_ratio"`
	ExpectedWeightKg   Qty     `json:"expected_weight_kg"`
	ReferenceSalePrice Amount  `json:"reference_sale_price"`
	PredictedWeightKg  *string `json:"predicted_weight_kg"`
	ActualWeightKg     *Qty    `json:"actual_weight_kg"`
	OutputLotID        *string `json:"output_lot_id"`
	OutputLotNo        *string `json:"output_lot_no"`
	AllocatedCost      *Amount `json:"allocated_cost"`
	UnitCost           *Amount `json:"unit_cost"`
}

// CuttingAllocationPreview: her iki dagitim yontemi (quantity/value) AYNI ANDA hesaplanip yan yana
// doner; kullanici her ikisini de gorebilir, kapanista secilen (AllocationMethod) kullanilir.
type CuttingAllocationPreview struct {
	Quantity *CuttingResult `json:"quantity"`
	Value    *CuttingResult `json:"value"`
}

// CuttingPredictionView /predict/cutting'in ML tahmini (en iyi gayret; ML kapaliysa nil kalir).
type CuttingPredictionView struct {
	ModelName         string            `json:"model_name"`
	ModelVersion      string            `json:"model_version"`
	Baseline          bool              `json:"baseline"`
	BreedUsed         string            `json:"breed_used"`
	ExpectedFireKg    string            `json:"expected_fire_kg"`
	ExpectedFireRatio string            `json:"expected_fire_ratio"`
	PartsKg           map[string]string `json:"parts_kg"` // parca adi (ML'in kullandigi) -> kg
}

// CuttingOrder GET /cutting-orders(/{id}) yanitidir. Outputs/Allocation/MLPrediction yalniz detay
// (GET /{id}) ve PUT .../outputs yanitlarinda doludur (liste gorunumunde omitempty ile dusurulur).
type CuttingOrder struct {
	ID                string                    `json:"id"`
	OrderNo           string                    `json:"order_no"`
	RecipeID          string                    `json:"recipe_id"`
	RecipeCode        string                    `json:"recipe_code"`
	RecipeName        string                    `json:"recipe_name"`
	InputLotID        string                    `json:"input_lot_id"`
	InputLotNo        string                    `json:"input_lot_no"`
	InputItemCode     string                    `json:"input_item_code"`
	InputItemName     string                    `json:"input_item_name"`
	InputWeightKg     Qty                       `json:"input_weight_kg"`
	UnitCostPerKg     Amount                    `json:"unit_cost_per_kg"`
	ComputedTotalCost Amount                    `json:"computed_total_cost"`
	DeclaredTotalCost *Amount                   `json:"declared_total_cost"`
	TotalInputCost    Amount                    `json:"total_input_cost"`
	AllocationMethod  string                    `json:"allocation_method"`
	CostBasis         *string                   `json:"cost_basis"`
	CostBasisUsed     string                    `json:"cost_basis_used"`
	ResolutionNote    *string                   `json:"resolution_note"`
	Status            string                    `json:"status"`
	CutAt             time.Time                 `json:"cut_at"`
	BalanceDiffKg     *Qty                      `json:"balance_diff_kg"`
	Closable          bool                      `json:"closable"`
	Warnings          []string                  `json:"warnings"`
	Outputs           []CuttingOutputLine       `json:"outputs,omitempty"`
	Allocation        *CuttingAllocationPreview `json:"allocation,omitempty"`
	MLPrediction      *CuttingPredictionView    `json:"ml_prediction,omitempty"`
	ClosedAt          *time.Time                `json:"closed_at"`
	CreatedAt         time.Time                 `json:"created_at"`
}

// ---------------------------------------------------------------------------
// Durum gecisi kontrolleri
// ---------------------------------------------------------------------------

func CanUpdateCuttingOutputs(status string) error {
	if status != CuttingDraft {
		return Conflict("gecersiz durum: %s (yalniz draft iken fiili agirlik girilebilir)", status)
	}
	return nil
}

func CanCloseCutting(status string) error {
	if status != CuttingDraft {
		return Conflict("gecersiz durum gecisi: %s -> closed", status)
	}
	return nil
}
