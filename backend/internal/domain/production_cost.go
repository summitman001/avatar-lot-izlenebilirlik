package domain

import (
	"github.com/shopspring/decimal"
)

// Gorev 1: dis hizmetle uretim maliyeti (EKP). ml/oracle/production_cost.py'nin Go karsiligi;
// dogruluk referansi docs/golden/production_cases.json (production_cost_test.go okur).
//
//	her maliyet satiri (hammadde ve hizmet) ayri ayri round2
//	material_cost = yuvarlanmis hammadde satirlari toplami
//	service_cost  = round2(faturalanan_adet x hizmet_birim_fiyati)
//	total_cost    = material_cost + service_cost
//	ekp_unit      = round4(total_cost / teslim_alinan_adet)
//	ekp_display   = round2(total_cost / teslim_alinan_adet)   (ekp_unit'ten ikinci yuvarlama YOK)
//
// Degerleme ekp_unit veya total_cost uzerinden yapilir; ekp_display x adet YANLISTIR.

const (
	WarnInvoicedQtyMismatch = "INVOICED_QTY_MISMATCH"
)

var materialCategories = map[string]bool{"meat": true, "spice": true, "casing": true, "other": true}

type MaterialLot struct {
	Name     string
	Category string // meat|spice|casing|other
	Quantity decimal.Decimal
	UnitCost decimal.Decimal
}

type ProductionInput struct {
	Lots             []MaterialLot
	DeliveredQty     decimal.Decimal
	ServiceUnitPrice decimal.Decimal
	InvoicedQty      *decimal.Decimal // nil: teslim alinan adede esit
	SalePrice        *decimal.Decimal
	UnitWeightKg     *decimal.Decimal // nil: kg bazli verim alanlari null doner
}

// ProductionResult JSON sozlesmesidir (golden "expected" ile ayni anahtarlar).
// Decimal'ler sabit noktali string'dir; olmayan deger null.
type ProductionResult struct {
	LotLines         []LotLineResult `json:"lot_lines"`
	MaterialCost     string          `json:"material_cost"`
	ServiceCost      string          `json:"service_cost"`
	TotalCost        string          `json:"total_cost"`
	DeliveredQty     string          `json:"delivered_qty"`
	InvoicedQty      string          `json:"invoiced_qty"`
	EkpUnit          string          `json:"ekp_unit"`
	EkpDisplay       string          `json:"ekp_display"`
	UnitWeightKg     *string         `json:"unit_weight_kg"`
	OutputKg         *string         `json:"output_kg"`
	EkpPerKg         *string         `json:"ekp_per_kg"`
	InputKgTotal     string          `json:"input_kg_total"`
	YieldInputPct    *string         `json:"yield_input_pct"`
	YieldMeatRatio   *string         `json:"yield_meat_ratio"`
	SalePrice        *string         `json:"sale_price"`
	Revenue          *string         `json:"revenue"`
	GrossProfitUnit  *string         `json:"gross_profit_unit"`
	GrossProfitTotal *string         `json:"gross_profit_total"`
	MarginPct        *string         `json:"margin_pct"`
	Warnings         []string        `json:"warnings"`
}

type LotLineResult struct {
	Name   string `json:"name"`
	Amount string `json:"amount"`
}

// Fixed, d'yi ondalik basamak sayisini koruyarak yazar ("85", "1.000"); Python format(x,'f') karsiligi.
func fixed(d decimal.Decimal) string {
	places := -d.Exponent()
	if places < 0 {
		places = 0
	}
	return d.StringFixed(places)
}

func fixedPtr(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	s := fixed(*d)
	return &s
}

func fix(d decimal.Decimal, places int32) string { return d.StringFixed(places) }

func fixP(d decimal.Decimal, places int32) *string {
	s := fix(d, places)
	return &s
}

func CalculateProductionCost(in ProductionInput) (ProductionResult, error) {
	var zero ProductionResult
	if !in.DeliveredQty.IsPositive() {
		return zero, Invalid("teslim alinan adet sifirdan buyuk olmali")
	}
	if in.ServiceUnitPrice.IsNegative() {
		return zero, Invalid("hizmet birim fiyati negatif olamaz")
	}
	if in.UnitWeightKg != nil && !in.UnitWeightKg.IsPositive() {
		return zero, Invalid("unit_weight_kg sifirdan buyuk olmali")
	}
	for _, l := range in.Lots {
		if !materialCategories[l.Category] {
			return zero, Invalid("gecersiz kategori: %s", l.Category)
		}
		if l.Quantity.IsNegative() || l.UnitCost.IsNegative() {
			return zero, Invalid("miktar ve birim maliyet negatif olamaz")
		}
	}
	invoiced := in.DeliveredQty
	if in.InvoicedQty != nil {
		invoiced = *in.InvoicedQty
	}
	if invoiced.IsNegative() {
		return zero, Invalid("faturalanan adet negatif olamaz")
	}

	// Her satir ayri yuvarlanir; toplamlar yuvarlanmis satirlarin toplamidir.
	lines := make([]LotLineResult, len(in.Lots))
	material := decimal.Zero
	inputKg, meatKg := decimal.Zero, decimal.Zero
	for i, l := range in.Lots {
		amount := LineTotal(l.Quantity, l.UnitCost)
		lines[i] = LotLineResult{Name: l.Name, Amount: fix(amount, 2)}
		material = material.Add(amount)
		inputKg = inputKg.Add(l.Quantity)
		if l.Category == "meat" {
			meatKg = meatKg.Add(l.Quantity)
		}
	}
	service := LineTotal(invoiced, in.ServiceUnitPrice)
	total := material.Add(service)

	res := ProductionResult{
		LotLines:     lines,
		MaterialCost: fix(material, 2),
		ServiceCost:  fix(service, 2),
		TotalCost:    fix(total, 2),
		DeliveredQty: fixed(in.DeliveredQty),
		InvoicedQty:  fixed(invoiced),
		EkpUnit:      fix(total.DivRound(in.DeliveredQty, 4), 4),
		EkpDisplay:   fix(total.DivRound(in.DeliveredQty, 2), 2),
		UnitWeightKg: fixedPtr(in.UnitWeightKg),
		InputKgTotal: fix(inputKg, 3),
		Warnings:     []string{},
	}
	if !invoiced.Equal(in.DeliveredQty) {
		res.Warnings = append(res.Warnings, WarnInvoicedQtyMismatch)
	}

	// Verim: adet, unit_weight_kg ile kg'a cevrilir.
	if in.UnitWeightKg != nil {
		outputKg := in.DeliveredQty.Mul(*in.UnitWeightKg)
		res.OutputKg = fixP(outputKg, 3)
		res.EkpPerKg = fixP(total.DivRound(outputKg, 4), 4)
		if inputKg.IsPositive() {
			res.YieldInputPct = fixP(outputKg.Mul(decimal.NewFromInt(100)).DivRound(inputKg, 2), 2)
		}
		if meatKg.IsPositive() {
			res.YieldMeatRatio = fixP(outputKg.DivRound(meatKg, 4), 4)
		}
	}

	// Satis (ciro, kar, marj) teslim alinan adet uzerinden.
	if in.SalePrice != nil {
		if in.SalePrice.IsNegative() {
			return zero, Invalid("satis fiyati negatif olamaz")
		}
		revenue := Round2(in.SalePrice.Mul(in.DeliveredQty))
		profitTotal := revenue.Sub(total)
		res.SalePrice = fixedPtr(in.SalePrice)
		res.Revenue = fixP(revenue, 2)
		// birim kar = satis - total/adet, tam hassasiyette tek yuvarlama
		res.GrossProfitUnit = fixP(in.SalePrice.Mul(in.DeliveredQty).Sub(total).DivRound(in.DeliveredQty, 4), 4)
		res.GrossProfitTotal = fixP(profitTotal, 2)
		if revenue.IsPositive() {
			res.MarginPct = fixP(profitTotal.Mul(decimal.NewFromInt(100)).DivRound(revenue, 2), 2)
		}
	}
	return res, nil
}
