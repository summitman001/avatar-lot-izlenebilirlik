package domain

import (
	"encoding/json"
	"math/rand"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
)

type decDoc struct {
	UnitCost Amount `json:"unit_cost"`
	Qty      Qty    `json:"qty"`
	Day      Date   `json:"day"`
}

// Decimal JSON round-trip: "18.1588" string olarak gidip gelir, float'a donmez.
func TestDecimalJSONRoundTrip(t *testing.T) {
	in := `{"unit_cost":"18.1588","qty":"85.000","day":"2026-03-01"}`
	var doc decDoc
	if err := json.Unmarshal([]byte(in), &doc); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != in {
		t.Fatalf("round-trip bozuldu:\n got  %s\n want %s", out, in)
	}
}

func TestDecimalJSONFixedScale(t *testing.T) {
	// Sondaki sifirlar korunur: "10" -> "10.0000" / "10.000"
	var doc decDoc
	if err := json.Unmarshal([]byte(`{"unit_cost":"10","qty":"1.5","day":"2026-03-01"}`), &doc); err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(doc)
	if !strings.Contains(string(out), `"unit_cost":"10.0000"`) || !strings.Contains(string(out), `"qty":"1.500"`) {
		t.Fatalf("sabit olcek beklenirdi: %s", out)
	}
}

func TestDecimalJSONRejectsNumbersAndExcessScale(t *testing.T) {
	cases := map[string]string{
		"json number":     `{"unit_cost":18.1588,"qty":"1.000","day":"2026-03-01"}`,
		"too many places": `{"unit_cost":"18.15881","qty":"1.000","day":"2026-03-01"}`,
		"qty places":      `{"unit_cost":"1.0000","qty":"1.0001","day":"2026-03-01"}`,
		"not a decimal":   `{"unit_cost":"abc","qty":"1.000","day":"2026-03-01"}`,
		"bad date":        `{"unit_cost":"1.0000","qty":"1.000","day":"01.03.2026"}`,
	}
	for name, body := range cases {
		var doc decDoc
		if err := json.Unmarshal([]byte(body), &doc); err == nil {
			t.Errorf("%s: hata beklenirdi", name)
		}
	}
}

func TestLineTotalAndMismatch(t *testing.T) {
	// KZ-2026-001: 25 kg x 11.50 = 287.50, belgede 351.00 yaziyor
	computed := LineTotal(decimal.RequireFromString("25.000"), decimal.RequireFromString("11.5000"))
	if computed.StringFixed(2) != "287.50" {
		t.Fatalf("computed = %s", computed)
	}
	declared := decimal.RequireFromString("351.00")
	if !HasMismatch(computed, &declared) {
		t.Fatal("287.50 != 351.00 icin has_mismatch beklenirdi")
	}
	same := decimal.RequireFromString("287.50")
	if HasMismatch(computed, &same) || HasMismatch(computed, nil) {
		t.Fatal("esit veya beyansiz tutarda has_mismatch olmamali")
	}
}

// Dagitim: toplam kaynaga her zaman kurusu kurusuna esit (rastgele girdilerle).
func TestDistributeSumsExactly(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for n := 0; n < 2000; n++ {
		total := decimal.New(rng.Int63n(10_000_000), -2)
		weights := make([]decimal.Decimal, 1+rng.Intn(8))
		for i := range weights {
			weights[i] = decimal.New(1+rng.Int63n(100_000), -3)
		}
		out, err := Distribute(total, weights, 2)
		if err != nil {
			t.Fatal(err)
		}
		sum := decimal.Zero
		for _, o := range out {
			sum = sum.Add(o)
		}
		if !sum.Equal(total) {
			t.Fatalf("toplam %s != %s (agirliklar %v)", sum, total, weights)
		}
	}
}

func TestParseID(t *testing.T) {
	if _, err := ParseID(NewID()); err != nil {
		t.Fatalf("uretilen ID gecerli olmali: %v", err)
	}
	for _, bad := range []string{"", "abc", "6ba7b810-9dad-11d1-80b4-00c04fd430c8" /* v1 */} {
		if _, err := ParseID(bad); err == nil {
			t.Errorf("%q reddedilmeliydi", bad)
		}
	}
}
