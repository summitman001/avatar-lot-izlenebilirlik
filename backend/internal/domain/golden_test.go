package domain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/shopspring/decimal"
)

// Go hesabi, ml/oracle'in urettigi docs/golden/*.json dosyalariyla birebir ayni sonucu vermek zorundadir.
// Dosyalar `cd ml && python -m oracle.cli` ile uretilir; burada yalnizca okunur.

func goldenDir(t testing.TB) string {
	t.Helper()
	if d := os.Getenv("GOLDEN_DIR"); d != "" {
		return d
	}
	return filepath.Join("..", "..", "..", "docs", "golden") // backend/internal/domain -> repo koku
}

type goldenCase struct {
	CaseID      string          `json:"case_id"`
	Description string          `json:"description"`
	Input       json.RawMessage `json:"input"`
	Expected    json.RawMessage `json:"expected"`
}

func loadGolden(t testing.TB, name string) []goldenCase {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(goldenDir(t), name))
	if err != nil {
		t.Fatalf("golden dosya okunamadi: %v", err)
	}
	var cases []goldenCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if len(cases) == 0 {
		t.Fatalf("%s bos", name)
	}
	return cases
}

func dec(t testing.TB, s string) decimal.Decimal {
	t.Helper()
	d, err := decimal.NewFromString(s)
	if err != nil {
		t.Fatalf("decimal %q: %v", s, err)
	}
	return d
}

func decPtr(t testing.TB, s *string) *decimal.Decimal {
	if s == nil {
		return nil
	}
	d := dec(t, *s)
	return &d
}

// assertJSONEqual iki JSON degerini yapisal olarak karsilastirir (sayisal JSON degeri yoktur, hepsi string).
func assertJSONEqual(t *testing.T, id string, got any, wantRaw json.RawMessage) {
	t.Helper()
	gotRaw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var g, w any
	if err := json.Unmarshal(gotRaw, &g); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(wantRaw, &w); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g, w) {
		gp, _ := json.MarshalIndent(g, "", "  ")
		wp, _ := json.MarshalIndent(w, "", "  ")
		t.Errorf("%s: golden ile uyusmuyor\n--- beklenen\n%s\n--- Go sonucu\n%s", id, wp, gp)
	}
}

func TestGoldenProduction(t *testing.T) {
	type lot struct {
		Name     string `json:"name"`
		Category string `json:"category"`
		Quantity string `json:"quantity"`
		UnitCost string `json:"unit_cost"`
	}
	type input struct {
		UnitWeightKg     *string `json:"unit_weight_kg"`
		Lots             []lot   `json:"lots"`
		DeliveredQty     string  `json:"delivered_qty"`
		ServiceUnitPrice string  `json:"service_unit_price"`
		InvoicedQty      *string `json:"invoiced_qty"`
		SalePrice        *string `json:"sale_price"`
	}
	for _, c := range loadGolden(t, "production_cases.json") {
		t.Run(c.CaseID, func(t *testing.T) {
			var in input
			if err := json.Unmarshal(c.Input, &in); err != nil {
				t.Fatal(err)
			}
			pi := ProductionInput{
				DeliveredQty:     dec(t, in.DeliveredQty),
				ServiceUnitPrice: dec(t, in.ServiceUnitPrice),
				InvoicedQty:      decPtr(t, in.InvoicedQty),
				SalePrice:        decPtr(t, in.SalePrice),
				UnitWeightKg:     decPtr(t, in.UnitWeightKg),
			}
			for _, l := range in.Lots {
				pi.Lots = append(pi.Lots, MaterialLot{l.Name, l.Category, dec(t, l.Quantity), dec(t, l.UnitCost)})
			}
			got, err := CalculateProductionCost(pi)
			if err != nil {
				t.Fatal(err)
			}
			// rounding_proof yalnizca golden'da var (ekp_display x adet kanitlamasi); hesapla ve ekle.
			var want map[string]json.RawMessage
			_ = json.Unmarshal(c.Expected, &want)
			if proof, ok := want["rounding_proof"]; ok {
				naive := Round2(dec(t, got.EkpDisplay).Mul(pi.DeliveredQty))
				gotProof := map[string]string{
					"ekp_display_times_qty": naive.StringFixed(2),
					"total_cost":            got.TotalCost,
					"difference":            naive.Sub(dec(t, got.TotalCost)).StringFixed(2),
				}
				assertJSONEqual(t, c.CaseID+"/rounding_proof", gotProof, proof)
				delete(want, "rounding_proof")
			}
			wantBytes, _ := json.Marshal(want)
			assertJSONEqual(t, c.CaseID, got, wantBytes)
		})
	}
}

func TestGoldenCutting(t *testing.T) {
	type part struct {
		Name           string `json:"name"`
		Kg             string `json:"kg"`
		ReferencePrice string `json:"reference_price"`
		IsWaste        bool   `json:"is_waste"`
		IsByProduct    bool   `json:"is_by_product"`
	}
	type input struct {
		InputWeightKg  string  `json:"input_weight_kg"`
		UnitCostPerKg  *string `json:"unit_cost_per_kg"`
		DeclaredTotal  *string `json:"declared_total_cost"`
		CostBasis      *string `json:"cost_basis"`
		ResolutionNote *string `json:"resolution_note"`
		Method         string  `json:"method"`
		Parts          []part  `json:"parts"`
	}
	for _, c := range loadGolden(t, "cutting_cases.json") {
		t.Run(c.CaseID, func(t *testing.T) {
			var in input
			if err := json.Unmarshal(c.Input, &in); err != nil {
				t.Fatal(err)
			}
			ci := CuttingInput{
				InputWeightKg:  dec(t, in.InputWeightKg),
				UnitCostPerKg:  decPtr(t, in.UnitCostPerKg),
				DeclaredTotal:  decPtr(t, in.DeclaredTotal),
				CostBasis:      in.CostBasis,
				ResolutionNote: in.ResolutionNote,
				Method:         in.Method,
			}
			for _, p := range in.Parts {
				ci.Parts = append(ci.Parts, Part{p.Name, dec(t, p.Kg), dec(t, p.ReferencePrice), p.IsWaste, p.IsByProduct})
			}
			got, err := AllocateCutting(ci)
			if err != nil {
				t.Fatal(err)
			}
			assertJSONEqual(t, c.CaseID, got, c.Expected)
		})
	}
}
