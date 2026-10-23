package domain

import "github.com/shopspring/decimal"

// Yuvarlama kurallari docs/assumptions.md ile ve ml/oracle ile birebir aynidir:
//   - satir tutari: 2 ondalik, ROUND_HALF_UP (shopspring Round: yariyi sifirdan uzaga yuvarlar, yani
//     Python'daki ROUND_HALF_UP ile ayni)
//   - birim maliyet: 4 ondalik
//   - bolmeler DivRound ile tam hassasiyette yapilip bir kez yuvarlanir (ara degerler yuvarlanmaz)

func Round2(d decimal.Decimal) decimal.Decimal { return d.Round(2) }
func Round3(d decimal.Decimal) decimal.Decimal { return d.Round(3) }
func Round4(d decimal.Decimal) decimal.Decimal { return d.Round(4) }

// LineTotal bir maliyet/alis satirinin tutarini hesaplar: round2(qty x birim fiyat).
func LineTotal(qty, unitPrice decimal.Decimal) decimal.Decimal {
	return Round2(qty.Mul(unitPrice))
}

// HasMismatch belgede beyan edilen tutar hesaplanandan farkliysa true doner (beyan yoksa false).
func HasMismatch(computed decimal.Decimal, declared *decimal.Decimal) bool {
	return declared != nil && !declared.Equal(computed)
}

// Distribute total'i weights oraninda places ondaliga dagitir.
//
//  1. Her satir tam hassasiyetle hesaplanir: total * w_i / sum(w).
//  2. Her satir places ondaliga yuvarlanir.
//  3. Kalan fark (total - yuvarlanmis toplam) en buyuk tutarli satira eklenir;
//     esitlikte girdi sirasindaki ilk satir secilir.
//
// Sonuc: sum(sonuc) == total.Round(places) her zaman (kurus kaybi/kazanci yok).
func Distribute(total decimal.Decimal, weights []decimal.Decimal, places int32) ([]decimal.Decimal, error) {
	if len(weights) == 0 {
		return nil, Invalid("dagitim icin en az bir satir gerekli")
	}
	denom := decimal.Zero
	for _, w := range weights {
		if w.IsNegative() {
			return nil, Invalid("agirlik negatif olamaz")
		}
		denom = denom.Add(w)
	}
	if !denom.IsPositive() {
		return nil, Invalid("agirliklarin toplami sifirdan buyuk olmali")
	}
	out := make([]decimal.Decimal, len(weights))
	sum := decimal.Zero
	best := 0
	for i, w := range weights {
		out[i] = total.Mul(w).DivRound(denom, places)
		sum = sum.Add(out[i])
		if out[i].GreaterThan(out[best]) {
			best = i
		}
	}
	out[best] = out[best].Add(total.Round(places).Sub(sum))
	return out, nil
}
