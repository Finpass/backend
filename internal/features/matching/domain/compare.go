package domain

import (
	"fmt"
	"math"
	"sort"
)

// Product comparison bounds: fewer than two is not a comparison, more than four does not fit a screen or a prompt.
const (
	MinCompare = 2
	MaxCompare = 4
)

type CompareMetrics struct {
	Return1Y       float64  `json:"return1y"`
	Volatility     float64  `json:"volatility"`
	Sharpe         *float64 `json:"sharpe"`
	PositiveMonths int      `json:"positiveMonths"`
	PER            *float64 `json:"per"`
	PBV            *float64 `json:"pbv"`
	ROE            *float64 `json:"roe"`
	DivYield       *float64 `json:"divYield"`
	Beta           *float64 `json:"beta"`
	ExpenseRatio   *float64 `json:"expenseRatio"`
}

// GateResult is layer 2 for one asset: the first failing gate, or eligible.
type GateResult struct {
	Eligible bool   `json:"eligible"`
	Gate     string `json:"gate,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

type CompareAsset struct {
	Symbol     string         `json:"symbol"`
	Name       string         `json:"name"`
	Class      string         `json:"class"`
	ClassLabel Text           `json:"classLabel"`
	Sector     string         `json:"sector"`
	Currency   string         `json:"currency"`
	Sharia     string         `json:"sharia"`
	Geography  string         `json:"geography"`
	Volatility string         `json:"volatilityClass"`
	Liquidity  string         `json:"liquidity"`
	Metrics    CompareMetrics `json:"metrics"`
	Gate       GateResult     `json:"gate"`
	Score      ScoreTrace     `json:"score"`
	Series     []SeriesPoint  `json:"series"`
	Rank       int            `json:"rank"` // 1 = best fit among eligible assets; 0 when excluded by a gate
}

// Highlight names the asset that leads one measurable dimension; text is a fixed template, never generated.
type Highlight struct {
	Key    string `json:"key"`
	Symbol string `json:"symbol"`
	Text   Text   `json:"text"`
}

type CompareOutput struct {
	Assets     []CompareAsset `json:"assets"`
	Best       string         `json:"best"`
	Highlights []Highlight    `json:"highlights"`
}

// r2 rounds an optional figure to two decimals so the screen and the advisor quote the same short number.
func r2(p *float64) *float64 {
	if p == nil {
		return nil
	}
	v := math.Round(*p*100) / 100
	return &v
}

func sharpe(c Candidate, riskFree float64) *float64 {
	if c.Vol <= 0 {
		return nil
	}
	v := math.Round((c.Return1Y-riskFree)/c.Vol*100) / 100
	return &v
}

// Compare applies the same gates and score the portfolio uses, so a comparison never disagrees with a recommendation.
// Assets keep the caller's order; the profile decides eligibility and fit, the comparison only lines them up.
func Compare(p Profile, cands []Candidate, riskFree float64, m MacroSignals) CompareOutput {
	out := CompareOutput{Assets: make([]CompareAsset, 0, len(cands)), Highlights: []Highlight{}}
	for _, c := range cands {
		a := CompareAsset{
			Symbol: c.Symbol, Name: c.Name, Class: c.Class, ClassLabel: ClassLabels[c.Class], Sector: c.Sector, Currency: c.Currency,
			Sharia: c.Sharia, Geography: c.Geography, Volatility: c.Volatility, Liquidity: c.Liquidity,
			Metrics: CompareMetrics{round1(c.Return1Y), round1(c.Vol), sharpe(c, riskFree), c.PositiveMonths, r2(c.PER), r2(c.PBV), r2(c.ROE), r2(c.DivYield), r2(c.Beta), r2(c.ExpenseRatio)},
			Score:   Score(p, c, riskFree, m), Series: c.Series,
		}
		g, why := CheckGates(p, c)
		a.Gate = GateResult{Eligible: g == "", Gate: g, Reason: why}
		out.Assets = append(out.Assets, a)
	}
	idx := make([]int, 0, len(out.Assets))
	for i, a := range out.Assets {
		if a.Gate.Eligible {
			idx = append(idx, i)
		}
	}
	sort.SliceStable(idx, func(x, y int) bool {
		a, b := out.Assets[idx[x]], out.Assets[idx[y]]
		if a.Score.Total != b.Score.Total {
			return a.Score.Total > b.Score.Total
		}
		return a.Symbol < b.Symbol
	})
	for r, i := range idx {
		out.Assets[i].Rank = r + 1
	}
	if len(idx) > 0 {
		out.Best = out.Assets[idx[0]].Symbol
	}
	out.Highlights = highlights(out.Assets)
	return out
}

// highlights lists a leader only where at least two assets have the figure and they differ.
func highlights(as []CompareAsset) []Highlight {
	type dim struct {
		key  string
		val  func(CompareAsset) *float64
		high bool
		id   string
		en   string
		unit string
		dec  int
	}
	f := func(v float64) *float64 { return &v }
	dims := []dim{
		{"return", func(a CompareAsset) *float64 { return f(a.Metrics.Return1Y) }, true, "Return 1 tahun tertinggi", "Highest 1-year return", "%", 1},
		{"volatility", func(a CompareAsset) *float64 { return f(a.Metrics.Volatility) }, false, "Fluktuasi paling rendah", "Lowest volatility", "%", 1},
		{"sharpe", func(a CompareAsset) *float64 { return a.Metrics.Sharpe }, true, "Imbal hasil per risiko terbaik (Sharpe)", "Best return per unit of risk (Sharpe)", "", 2},
		{"yield", func(a CompareAsset) *float64 { return a.Metrics.DivYield }, true, "Dividen tertinggi", "Highest dividend yield", "%", 1},
		{"cost", func(a CompareAsset) *float64 { return a.Metrics.ExpenseRatio }, false, "Biaya paling rendah", "Lowest expense ratio", "%", 1},
	}
	var out []Highlight
	for _, d := range dims {
		best, n, distinct := -1, 0, false
		for i, a := range as {
			v := d.val(a)
			if v == nil {
				continue
			}
			n++
			if best < 0 {
				best = i
				continue
			}
			b := *d.val(as[best])
			if *v != b {
				distinct = true
			}
			if (d.high && *v > b) || (!d.high && *v < b) {
				best = i
			}
		}
		if n < 2 || !distinct {
			continue
		}
		v := *d.val(as[best])
		out = append(out, Highlight{d.key, as[best].Symbol, Text{
			fmt.Sprintf("%s: %s (%.*f%s)", d.id, as[best].Symbol, d.dec, v, d.unit), fmt.Sprintf("%s: %s (%.*f%s)", d.en, as[best].Symbol, d.dec, v, d.unit)}})
	}
	return out
}
