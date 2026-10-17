// Package domain holds the Discover screener model: per-asset metrics derived from prices, fundamentals and
// dividends (pure functions, no I/O), the class/label catalog, sort rules and the keyset cursor.
package domain

import (
	"math"
	"sort"
)

// Valuation is the first usable multiple of the chain PE -> PB -> PS.
type Valuation struct {
	Metric string  `json:"metric"` // PE | PB | PS
	Value  float64 `json:"value"`
}

// Growth is the year-over-year change of annual net income (E) or, when that is not usable, of revenue (R), in percent.
type Growth struct {
	Metric string  `json:"metric"` // E | R
	Pct    float64 `json:"pct"`
}

// Period is one annual income statement column. Nil means the statement has no value for it.
type Period struct {
	Date      string // ISO date, sortable as text
	NetIncome *float64
	Revenue   *float64
}

// Fundamentals are the fields of fundamentals.info / statements the metrics need. Only ~100 IDX stocks have them.
type Fundamentals struct {
	MarketCap        *float64
	TrailingPE       *float64
	PriceToBook      *float64
	TotalRevenue     *float64
	DividendYieldPct *float64 // already a percent in the data (6.27 = 6.27%)
	Sector           string
	Industry         string
	Periods          []Period // any order
}

// Inputs are the raw facts for one asset, read from the database by the refresh command.
type Inputs struct {
	Symbol, Name, AssetClass, Currency, Exchange string
	ClassSector                                  string // asset_classifications.sector, "unknown" or "" when not derivable
	LastPrice                                    *float64
	LastTradeDate                                string   // ISO date or ""
	Base7D, Base1Y                               *float64 // close on or before last date - 7 days / - 1 year; nil without such a row
	TTMDividends                                 *float64 // sum of dividends of the last 365 days in the asset currency (currency checked in SQL)
	Fund                                         *Fundamentals
}

// Metrics is one stored screener row.
type Metrics struct {
	Symbol, Name, AssetClass, ClassGroup, Currency, Exchange string
	LastPrice                                                *float64
	LastTradeDate                                            string
	Ret7D, Ret1Y                                             *float64
	MarketCap                                                *float64
	Valuation                                                *Valuation
	Growth                                                   *Growth
	DivYieldPct                                              *float64
	Industry                                                 string
}

func pos(v *float64) bool { return v != nil && !math.IsNaN(*v) && !math.IsInf(*v, 0) && *v > 0 }

// Round4 keeps stored percentages tidy (4 decimals).
func Round4(v float64) float64 { return math.Round(v*1e4) / 1e4 }

func ptr(v float64) *float64 { v = Round4(v); return &v }

// ReturnPct is the percent change last/base - 1. Without a base close (history shorter than the window) the
// result is nil: a shortened period is never reported.
func ReturnPct(last, base *float64) *float64 {
	if !pos(last) || !pos(base) {
		return nil
	}
	return ptr((*last/(*base) - 1) * 100)
}

// ChooseValuation: PE when trailingPE > 0, else PB when priceToBook > 0, else PS = marketCap / totalRevenue when both > 0.
func ChooseValuation(f *Fundamentals) *Valuation {
	if f == nil {
		return nil
	}
	if pos(f.TrailingPE) {
		return &Valuation{"PE", Round4(*f.TrailingPE)}
	}
	if pos(f.PriceToBook) {
		return &Valuation{"PB", Round4(*f.PriceToBook)}
	}
	if pos(f.MarketCap) && pos(f.TotalRevenue) {
		return &Valuation{"PS", Round4(*f.MarketCap / *f.TotalRevenue)}
	}
	return nil
}

// ChooseGrowth compares the two most recent annual periods that carry the value. Earnings growth needs a positive
// previous net income (a percent change from a loss or zero is not meaningful); otherwise revenue growth is used.
func ChooseGrowth(f *Fundamentals) *Growth {
	if f == nil {
		return nil
	}
	ps := append([]Period(nil), f.Periods...)
	sort.Slice(ps, func(i, j int) bool { return ps[i].Date > ps[j].Date })
	if pct, ok := yoy(ps, func(p Period) *float64 { return p.NetIncome }); ok {
		return &Growth{"E", pct}
	}
	if pct, ok := yoy(ps, func(p Period) *float64 { return p.Revenue }); ok {
		return &Growth{"R", pct}
	}
	return nil
}

// yoy takes the newest two periods (newest first) that have a value; the previous one must be > 0.
func yoy(ps []Period, get func(Period) *float64) (float64, bool) {
	var vals []float64
	for _, p := range ps {
		if v := get(p); v != nil && !math.IsNaN(*v) && !math.IsInf(*v, 0) {
			vals = append(vals, *v)
			if len(vals) == 2 {
				break
			}
		}
	}
	if len(vals) < 2 || vals[1] <= 0 {
		return 0, false
	}
	return Round4((vals[0] - vals[1]) / vals[1] * 100), true
}

// DivYield: fundamentals dividendYield is already a percent (checked: BBCA 6.27 with 5,225 close; max 14.34), used
// when > 0. Otherwise trailing 12-month dividends / last price * 100 (same currency, checked by the caller).
// A zero yield is reported as nil (n/a): the screener does not distinguish "pays nothing" from "unknown".
//
// Yields above MaxYieldPct are treated as bad source data (58 assets, e.g. a 453.6 per-share dividend on a 1.36 price,
// unadjusted splits or return-of-capital) and reported as nil.
func DivYield(f *Fundamentals, last, ttm *float64) *float64 {
	var y *float64
	if f != nil && pos(f.DividendYieldPct) {
		y = ptr(*f.DividendYieldPct)
	} else if pos(last) && pos(ttm) {
		y = ptr(*ttm / *last * 100)
	}
	if y == nil || *y > MaxYieldPct {
		return nil
	}
	return y
}

// MaxYieldPct is the plausibility ceiling for a dividend yield, in percent.
const MaxYieldPct = 100

// Derive builds the stored row for one asset.
func Derive(in Inputs) Metrics {
	m := Metrics{Symbol: in.Symbol, Name: in.Name, AssetClass: in.AssetClass, ClassGroup: ClassGroup(in.AssetClass),
		Currency: in.Currency, Exchange: in.Exchange, LastPrice: in.LastPrice, LastTradeDate: in.LastTradeDate}
	m.Ret7D = ReturnPct(in.LastPrice, in.Base7D)
	m.Ret1Y = ReturnPct(in.LastPrice, in.Base1Y)
	if in.Fund != nil {
		if pos(in.Fund.MarketCap) {
			v := *in.Fund.MarketCap
			m.MarketCap = &v
		}
		m.Valuation = ChooseValuation(in.Fund)
		m.Growth = ChooseGrowth(in.Fund)
	}
	m.DivYieldPct = DivYield(in.Fund, in.LastPrice, in.TTMDividends)
	m.Industry = Industry(in.Fund, in.ClassSector, in.AssetClass)
	return m
}
