package domain

import (
	"strings"

	"github.com/shopspring/decimal"
)

// Gorev 2: kuzu parcalama ve maliyet dagitimi. ml/oracle/cutting_allocation.py'nin Go karsiligi;
// dogruluk referansi docs/golden/cutting_cases.json (cutting_test.go okur). Kurallar docs/assumptions.md'de.
//
// Ozet:
//   - agirlik dengesi: balance_diff = giris - toplam(tum parcalar, fire ve yan urun dahil);
//     |fark| > 0.05 kg => WEIGHT_IMBALANCE ve closable=false
//   - hesaplanan toplam != beyan edilen toplam => PURCHASE_TOTAL_MISMATCH; cost_basis + resolution_note zorunlu,
//     yoksa MISMATCH_RESOLUTION_MISSING ve closable=false (gecici olarak hesaplanan uzerinden gosterilir)
//   - fire maliyet tasimaz; yan urun kendi satis degeri kadar tasir, kalan havuz ana parcalara dagitilir
//   - quantity: kg oraninda, value: kg x referans fiyat oraninda; toplam her zaman kaynaga esit (Distribute)

const (
	WarnWeightImbalance           = "WEIGHT_IMBALANCE"
	WarnPurchaseTotalMismatch     = "PURCHASE_TOTAL_MISMATCH"
	WarnMismatchResolutionMissing = "MISMATCH_RESOLUTION_MISSING"

	MethodQuantity = "quantity"
	MethodValue    = "value"
	BasisComputed  = "computed"
	BasisDeclared  = "declared"
)

// WeightToleranceKg: bu degere kadar (dahil) agirlik farki kabul edilir.
var WeightToleranceKg = decimal.RequireFromString("0.05")

type Part struct {
	Name           string
	Kg             decimal.Decimal
	ReferencePrice decimal.Decimal // EUR/kg
	IsWaste        bool
	IsByProduct    bool
}

func (p Part) role() string {
	switch {
	case p.IsWaste:
		return "waste"
	case p.IsByProduct:
		return "by_product"
	}
	return "main"
}

type CuttingInput struct {
	InputWeightKg  decimal.Decimal
	Parts          []Part
	Method         string           // quantity|value
	UnitCostPerKg  *decimal.Decimal // hesaplanan toplam icin
	DeclaredTotal  *decimal.Decimal // belgede beyan edilen toplam
	CostBasis      *string          // computed|declared; uyusmazlikta zorunlu
	ResolutionNote *string          // uyusmazlikta zorunlu
}

type CuttingPartResult struct {
	Name          string `json:"name"`
	Role          string `json:"role"`
	Kg            string `json:"kg"`
	YieldPct      string `json:"yield_pct"`
	AllocatedCost string `json:"allocated_cost"`
	UnitCost      string `json:"unit_cost"`
}

type AltPartResult struct {
	Name          string `json:"name"`
	AllocatedCost string `json:"allocated_cost"`
}

type AlternativeBasis struct {
	CostBasis      string          `json:"cost_basis"`
	TotalInputCost string          `json:"total_input_cost"`
	Parts          []AltPartResult `json:"parts"`
	TotalAllocated string          `json:"total_allocated"`
}

type CuttingResult struct {
	Method            string              `json:"method"`
	CostBasis         *string             `json:"cost_basis"`
	CostBasisUsed     string              `json:"cost_basis_used"`
	ResolutionNote    *string             `json:"resolution_note"`
	InputWeightKg     string              `json:"input_weight_kg"`
	ComputedTotalCost *string             `json:"computed_total_cost"`
	DeclaredTotalCost *string             `json:"declared_total_cost"`
	TotalInputCost    string              `json:"total_input_cost"`
	PartsWeightKg     string              `json:"parts_weight_kg"`
	BalanceDiffKg     string              `json:"balance_diff_kg"`
	Closable          bool                `json:"closable"`
	SellableKg        string              `json:"sellable_kg"`
	ByProductValue    string              `json:"by_product_value"`
	AllocationPool    string              `json:"allocation_pool"`
	Parts             []CuttingPartResult `json:"parts"`
	TotalAllocated    string              `json:"total_allocated"`
	AlternativeBasis  *AlternativeBasis   `json:"alternative_basis"`
	Warnings          []string            `json:"warnings"`
}

// distributeCosts: (parca maliyetleri, ana parca havuzu, yan urun degeri toplami)
func distributeCosts(total decimal.Decimal, parts []Part, method string) ([]decimal.Decimal, decimal.Decimal, decimal.Decimal, error) {
	byProduct := map[int]decimal.Decimal{}
	byProductTotal := decimal.Zero
	var mainIdx []int
	for i, p := range parts {
		switch p.role() {
		case "by_product":
			v := LineTotal(p.Kg, p.ReferencePrice)
			byProduct[i] = v
			byProductTotal = byProductTotal.Add(v)
		case "main":
			mainIdx = append(mainIdx, i)
		}
	}
	pool := total.Sub(byProductTotal)
	if pool.IsNegative() {
		return nil, decimal.Zero, decimal.Zero, Invalid("yan urun degeri toplam maliyeti asiyor")
	}
	if len(mainIdx) == 0 {
		return nil, decimal.Zero, decimal.Zero, Invalid("en az bir ana parca gerekli")
	}
	weights := make([]decimal.Decimal, len(mainIdx))
	for k, i := range mainIdx {
		if method == MethodQuantity {
			weights[k] = parts[i].Kg
		} else {
			weights[k] = parts[i].Kg.Mul(parts[i].ReferencePrice)
		}
	}
	alloc, err := Distribute(pool, weights, 2)
	if err != nil {
		return nil, decimal.Zero, decimal.Zero, err
	}
	costs := make([]decimal.Decimal, len(parts))
	for i := range costs {
		costs[i] = decimal.Zero.Round(2) // fire: 0.00
	}
	for k, i := range mainIdx {
		costs[i] = alloc[k]
	}
	for i, v := range byProduct {
		costs[i] = v
	}
	return costs, pool, byProductTotal, nil
}

func sum(ds []decimal.Decimal) decimal.Decimal {
	s := decimal.Zero
	for _, d := range ds {
		s = s.Add(d)
	}
	return s
}

// CostBasisResolution: hesaplanan ve beyan edilen toplamdan PARCALAR BILINMEDEN de turetilebilen
// karar (hangi esas kullanilacak, uyusmazlik var mi, cozumlenmis mi). AllocateCutting bunu dagitim
// icin kullanir; repo/cutting.go da parcalama siparisi OLUSTURULURKEN (henuz fiili agirlik girilmeden)
// toplam maliyeti ve PURCHASE_TOTAL_MISMATCH/MISMATCH_RESOLUTION_MISSING uyarisini belirlemek icin
// AYNI fonksiyonu dogrudan cagirir.
type CostBasisResolution struct {
	Computed, Declared *decimal.Decimal
	TotalCost          decimal.Decimal
	BasisUsed          string
	Mismatch           bool
	Unresolved         bool
	Note               *string
	Warnings           []string
}

func ResolveCostBasis(unitCostPerKg, declaredTotal *decimal.Decimal, inputWeightKg decimal.Decimal, costBasis, resolutionNote *string) (CostBasisResolution, error) {
	var zero CostBasisResolution
	if costBasis != nil && *costBasis != BasisComputed && *costBasis != BasisDeclared {
		return zero, Invalid("gecersiz maliyet esasi: %s", *costBasis)
	}
	var computed, declared *decimal.Decimal
	if unitCostPerKg != nil {
		c := LineTotal(inputWeightKg, *unitCostPerKg)
		computed = &c
	}
	if declaredTotal != nil {
		d := Round2(*declaredTotal)
		declared = &d
	}
	if computed == nil && declared == nil {
		return zero, Invalid("unit_cost_per_kg veya declared_total_cost gerekli")
	}
	totals := map[string]*decimal.Decimal{BasisComputed: computed, BasisDeclared: declared}

	mismatch := computed != nil && declared != nil && !computed.Equal(*declared)
	// Esas verilmemisse gecici olarak hesaplanan tutar (yoksa beyan) kullanilir.
	basisUsed := BasisDeclared
	if costBasis != nil {
		basisUsed = *costBasis
	} else if computed != nil {
		basisUsed = BasisComputed
	}
	if totals[basisUsed] == nil {
		return zero, Invalid("%s esasi icin gerekli girdi eksik", basisUsed)
	}

	var note *string
	if resolutionNote != nil {
		if n := strings.TrimSpace(*resolutionNote); n != "" {
			note = &n
		}
	}
	unresolved := mismatch && (costBasis == nil || note == nil)

	warnings := []string{}
	if mismatch {
		warnings = append(warnings, WarnPurchaseTotalMismatch)
	}
	if unresolved {
		warnings = append(warnings, WarnMismatchResolutionMissing)
	}

	return CostBasisResolution{
		Computed: computed, Declared: declared, TotalCost: *totals[basisUsed], BasisUsed: basisUsed,
		Mismatch: mismatch, Unresolved: unresolved, Note: note, Warnings: warnings,
	}, nil
}

func AllocateCutting(in CuttingInput) (CuttingResult, error) {
	var zero CuttingResult
	if in.Method != MethodQuantity && in.Method != MethodValue {
		return zero, Invalid("gecersiz yontem: %s", in.Method)
	}
	if !in.InputWeightKg.IsPositive() {
		return zero, Invalid("giris agirligi sifirdan buyuk olmali")
	}
	if len(in.Parts) == 0 {
		return zero, Invalid("en az bir parca gerekli")
	}
	for _, p := range in.Parts {
		if p.IsWaste && p.IsByProduct {
			return zero, Invalid("parca hem fire hem yan urun olamaz: %s", p.Name)
		}
		if p.Kg.IsNegative() || p.ReferencePrice.IsNegative() {
			return zero, Invalid("negatif deger: %s", p.Name)
		}
	}

	basis, err := ResolveCostBasis(in.UnitCostPerKg, in.DeclaredTotal, in.InputWeightKg, in.CostBasis, in.ResolutionNote)
	if err != nil {
		return zero, err
	}
	totals := map[string]*decimal.Decimal{BasisComputed: basis.Computed, BasisDeclared: basis.Declared}
	computed, declared := basis.Computed, basis.Declared
	mismatch, basisUsed, totalCost, note, unresolved, warnings := basis.Mismatch, basis.BasisUsed, basis.TotalCost, basis.Note, basis.Unresolved, basis.Warnings

	partsWeight := decimal.Zero
	sellable := decimal.Zero
	for _, p := range in.Parts {
		partsWeight = partsWeight.Add(p.Kg)
		if p.role() == "main" {
			sellable = sellable.Add(p.Kg)
		}
	}
	balanceDiff := in.InputWeightKg.Sub(partsWeight)
	balanced := balanceDiff.Abs().LessThanOrEqual(WeightToleranceKg)
	if !balanced {
		warnings = append(warnings, WarnWeightImbalance)
	}

	costs, pool, byProductTotal, err := distributeCosts(totalCost, in.Parts, in.Method)
	if err != nil {
		return zero, err
	}
	totalAllocated := sum(costs)
	if !totalAllocated.Equal(totalCost) { // Distribute bunu garanti eder; savunma amacli
		return zero, Invalid("dagitim toplami maliyete esit olmali: %s != %s", totalAllocated, totalCost)
	}

	hundred := decimal.NewFromInt(100)
	parts := make([]CuttingPartResult, len(in.Parts))
	for i, p := range in.Parts {
		unit := decimal.Zero
		if p.Kg.IsPositive() {
			unit = costs[i].DivRound(p.Kg, 4)
		}
		parts[i] = CuttingPartResult{
			Name:          p.Name,
			Role:          p.role(),
			Kg:            fix(p.Kg, 3),
			YieldPct:      fix(p.Kg.Mul(hundred).DivRound(in.InputWeightKg, 2), 2),
			AllocatedCost: fix(costs[i], 2),
			UnitCost:      fix(unit, 4),
		}
	}

	var alt *AlternativeBasis
	if mismatch {
		other := BasisComputed
		if basisUsed == BasisComputed {
			other = BasisDeclared
		}
		altCosts, _, _, err := distributeCosts(*totals[other], in.Parts, in.Method)
		if err != nil {
			return zero, err
		}
		alt = &AlternativeBasis{
			CostBasis:      other,
			TotalInputCost: fix(*totals[other], 2),
			TotalAllocated: fix(sum(altCosts), 2),
			Parts:          make([]AltPartResult, len(in.Parts)),
		}
		for i, p := range in.Parts {
			alt.Parts[i] = AltPartResult{Name: p.Name, AllocatedCost: fix(altCosts[i], 2)}
		}
	}

	res := CuttingResult{
		Method:           in.Method,
		CostBasis:        in.CostBasis,
		CostBasisUsed:    basisUsed,
		ResolutionNote:   note,
		InputWeightKg:    fix(in.InputWeightKg, 3),
		TotalInputCost:   fix(totalCost, 2),
		PartsWeightKg:    fix(partsWeight, 3),
		BalanceDiffKg:    fix(balanceDiff, 3),
		Closable:         balanced && !unresolved,
		SellableKg:       fix(sellable, 3),
		ByProductValue:   fix(byProductTotal, 2),
		AllocationPool:   fix(pool, 2),
		Parts:            parts,
		TotalAllocated:   fix(totalAllocated, 2),
		AlternativeBasis: alt,
		Warnings:         warnings,
	}
	if computed != nil {
		res.ComputedTotalCost = fixP(*computed, 2)
	}
	if declared != nil {
		res.DeclaredTotalCost = fixP(*declared, 2)
	}
	return res, nil
}
