package repo

import (
	"context"
	"math"
	"sort"
	"strings"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/domain"
)

// Package repo (fuzzy.go): fatura/irsaliye OCR onerilerindeki tedarikci/urun adlarini mevcut
// kayitlarla eslestirmek icin basit (model egitimi YOK) normalize + Levenshtein benzerligi. SADECE
// ONERI uretir, OTOMATIK SECIM YAPMAZ -- kullanici adaylardan birini secer ya da yeni kayit
// olusturur (bkz. docs/invoice-ocr.md, repo/invoice_import.go).

// minMatchScore altindaki adaylar hic listelenmez ("eslesme bulunamazsa bos birakilir").
const (
	minMatchScore   = 0.30
	maxMatchResults = 3
)

// normalizeForMatch Turkce karakterleri sadelestirir, harf/rakam disindaki her seyi tek boslukla
// degistirir, kucuk harfe cevirir. Amac: "Yayla Hayvancılık" ile "yayla hayvancilik ltd." gibi
// yazim farklarini es gormek.
func normalizeForMatch(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.NewReplacer("ç", "c", "ğ", "g", "ı", "i", "ö", "o", "ş", "s", "ü", "u").Replace(s)
	var b strings.Builder
	prevSpace := true
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevSpace = false
		default:
			if !prevSpace {
				b.WriteRune(' ')
			}
			prevSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

// levenshtein iki rune dizisi arasindaki duzenleme mesafesi (tek satirlik DP, O(n) bellek).
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}

// similarity 0 (alakasiz) ile 1 (birebir, normalize edilmis) arasinda bir benzerlik skoru.
func similarity(a, b string) float64 {
	na, nb := normalizeForMatch(a), normalizeForMatch(b)
	if na == "" || nb == "" {
		return 0
	}
	maxLen := len([]rune(na))
	if l := len([]rune(nb)); l > maxLen {
		maxLen = l
	}
	if maxLen == 0 {
		return 1
	}
	return 1 - float64(levenshtein(na, nb))/float64(maxLen)
}

func round4(f float64) float64 { return math.Round(f*10000) / 10000 }

// topMatches query'ye en cok benzeyen en fazla maxMatchResults adayi (skor azalan) doner; hic
// aday minMatchScore'u gecmezse bos dilim.
func topMatches(query string, n int, get func(i int) (id, code, name string)) []domain.MatchCandidate {
	if strings.TrimSpace(query) == "" {
		return []domain.MatchCandidate{}
	}
	type scored struct {
		c     domain.MatchCandidate
		score float64
	}
	all := make([]scored, 0, n)
	for i := 0; i < n; i++ {
		id, code, name := get(i)
		sc := similarity(query, name)
		if sc < minMatchScore {
			continue
		}
		all = append(all, scored{domain.MatchCandidate{ID: id, Code: code, Name: name, Score: round4(sc)}, sc})
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].score > all[j].score })
	if len(all) > maxMatchResults {
		all = all[:maxMatchResults]
	}
	out := make([]domain.MatchCandidate, len(all))
	for i, s := range all {
		out[i] = s.c
	}
	return out
}

// matchSupplierCandidates OCR'dan okunan tedarikci adini TUM tedarikcilerle karsilastirir (demo
// olcegi: sayfalamaya gerek yok, bkz. docs/invoice-ocr.md).
func (s *Store) matchSupplierCandidates(ctx context.Context, name string) ([]domain.MatchCandidate, error) {
	if strings.TrimSpace(name) == "" {
		return []domain.MatchCandidate{}, nil
	}
	suppliers, err := s.ListSuppliers(ctx, SupplierFilter{Page: Page{Limit: MaxLimit}})
	if err != nil {
		return nil, err
	}
	return topMatches(name, len(suppliers), func(i int) (string, string, string) {
		return suppliers[i].ID, suppliers[i].Code, suppliers[i].Name
	}), nil
}

// matchItemCandidates OCR'dan okunan urun adini AKTIF urunlerle karsilastirir (pasif urune lot
// acilamaz, bkz. repo/lots.go createPurchaseLotInTx).
func (s *Store) matchItemCandidates(ctx context.Context, name string) ([]domain.MatchCandidate, error) {
	if strings.TrimSpace(name) == "" {
		return []domain.MatchCandidate{}, nil
	}
	active := true
	items, err := s.ListItems(ctx, ItemFilter{Active: &active, Page: Page{Limit: MaxLimit}})
	if err != nil {
		return nil, err
	}
	return topMatches(name, len(items), func(i int) (string, string, string) {
		return items[i].ID, items[i].Code, items[i].Name
	}), nil
}
