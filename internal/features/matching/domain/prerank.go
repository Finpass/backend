package domain

import "sort"

// ShortlistSize is how many gate-passing assets per class get the full layer 3 score.
const ShortlistSize = 20

type ShortlistTrace struct {
	Class    string `json:"class"`
	Eligible int    `json:"eligible"`
	Kept     int    `json:"kept"`
}

// Prerank is a cheap 0-100 key built only from facts already on the candidate (no fundamentals, no macro):
// sharpe 50%, 1-year return 30%, liquidity 20%. It only decides who enters the full scoring.
func Prerank(c Candidate, riskFree float64) float64 {
	sharpe := 0.0
	if c.Vol > 0 {
		sharpe = (c.Return1Y - riskFree) / c.Vol
	}
	return sharpeScore(sharpe)*0.5 + normalize(c.Return1Y, -20, 50)*100*0.3 + liquidityScore(c.Liquidity)*0.2
}

// shortlist keeps the top ShortlistSize of one class by Prerank (ties by symbol, so the result is deterministic).
func shortlist(in []Candidate, riskFree float64) []Candidate {
	if len(in) <= ShortlistSize {
		return in
	}
	keys := make(map[string]float64, len(in))
	for _, c := range in {
		keys[c.Symbol] = Prerank(c, riskFree)
	}
	sort.SliceStable(in, func(i, j int) bool {
		if keys[in[i].Symbol] != keys[in[j].Symbol] {
			return keys[in[i].Symbol] > keys[in[j].Symbol]
		}
		return in[i].Symbol < in[j].Symbol
	})
	return in[:ShortlistSize]
}
