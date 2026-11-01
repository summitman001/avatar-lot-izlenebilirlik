package domain

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestReviewStatus(t *testing.T) {
	q := func(s string) Qty { return Qty{decimal.RequireFromString(s)} }
	clean := SalesLine{Qty: q("5"), ReturnedQty: q("0")}
	negOpen := SalesLine{Qty: q("10"), ReturnedQty: q("0"), NegativeStockViolation: true, NegativeStockOpen: true}
	negAcked := SalesLine{Qty: q("10"), ReturnedQty: q("4"), NegativeStockViolation: true}
	negReturned := SalesLine{Qty: q("10"), ReturnedQty: q("10"), NegativeStockViolation: true, NegativeStockOpen: true}
	recall := SalesLine{Qty: q("3"), ReturnedQty: q("0"), RecallViolation: true, NegativeStockViolation: true}
	recallReturned := SalesLine{Qty: q("3"), ReturnedQty: q("3"), RecallViolation: true}

	cases := []struct {
		name  string
		lines []SalesLine
		want  string
	}{
		{"ihlal yok", []SalesLine{clean}, ""},
		{"uyari acik", []SalesLine{clean, negOpen}, ReviewOpen},
		{"uyari okundu, kismi iade", []SalesLine{negAcked}, ReviewReviewed},
		{"tamami iade, uyari acik olsa da", []SalesLine{clean, negReturned}, ReviewReturned},
		{"geri cagirma ihlali uyari okunsa da iadesiz kapanmaz", []SalesLine{recall}, ReviewOpen},
		{"biri iade biri okundu", []SalesLine{negReturned, negAcked}, ReviewReviewed},
		{"geri cagirma tamami iade", []SalesLine{recallReturned, negReturned}, ReviewReturned},
	}
	for _, c := range cases {
		if got := ReviewStatus(c.lines); got != c.want {
			t.Errorf("%s: %q beklenirdi, %q geldi", c.name, c.want, got)
		}
	}
}
