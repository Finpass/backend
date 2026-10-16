package domain

import "math"

// Window sizes from the Asset Classifier YAML: volatility uses 1 year (252 trading days).
const (
	VolWindowReturns = 252
	MinVolObs        = 30 // below this many returns the annualized figure is too noisy; classify unknown
	LiquidityWindow  = 30 // trading days averaged for volume x price
)

// AnnualizedVol implements: annualized_vol = std(daily_returns) x sqrt(252), daily_returns = diff(p)/p[:-1].
// closes are ascending by date; only the last 253 are used. Population std (numpy default).
// Returns the number of returns used. Pairs with a non-positive previous close are skipped.
func AnnualizedVol(closes []float64) (vol float64, n int) {
	if len(closes) > VolWindowReturns+1 {
		closes = closes[len(closes)-VolWindowReturns-1:]
	}
	rets := make([]float64, 0, len(closes))
	for i := 1; i < len(closes); i++ {
		if closes[i-1] > 0 && !math.IsNaN(closes[i]) && !math.IsNaN(closes[i-1]) {
			rets = append(rets, closes[i]/closes[i-1]-1)
		}
	}
	if len(rets) < 2 {
		return 0, len(rets)
	}
	var sum float64
	for _, r := range rets {
		sum += r
	}
	mean := sum / float64(len(rets))
	var ss float64
	for _, r := range rets {
		ss += (r - mean) * (r - mean)
	}
	return math.Sqrt(ss/float64(len(rets))) * math.Sqrt(252), len(rets)
}

// AvgDailyValue implements daily_value = avg_volume x avg_price over the last LiquidityWindow rows
// that carry a volume (NaN = missing). ok is false when no row has a volume.
func AvgDailyValue(closes, volumes []float64) (v float64, ok bool) {
	var sv, sp float64
	var n int
	for i := len(closes) - 1; i >= 0 && n < LiquidityWindow; i-- {
		if i >= len(volumes) || math.IsNaN(volumes[i]) || math.IsNaN(closes[i]) {
			continue
		}
		sv += volumes[i]
		sp += closes[i]
		n++
	}
	if n == 0 {
		return 0, false
	}
	return (sv / float64(n)) * (sp / float64(n)), true
}

// IDRFactor returns how many IDR one unit of the quoted price is worth. fx maps ISO currency
// to IDR per 1 unit (e.g. USD -> 16575). GBp/ZAc/ILA style minor units are 1/100 of the major.
func IDRFactor(currency string, fx map[string]float64) (float64, bool) {
	switch currency {
	case "IDR":
		return 1, true
	case "GBp", "GBX":
		r, ok := fx["GBP"]
		return r / 100, ok && r > 0
	}
	r, ok := fx[currency]
	return r, ok && r > 0
}
