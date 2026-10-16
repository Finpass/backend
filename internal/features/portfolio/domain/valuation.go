package domain

import (
	"math"
	"sort"
	"strings"
	"time"
)

type Point struct {
	Date  time.Time
	Value float64
}

// FX converts to IDR. Only IDR and USD (via IDR=X) are known; any other currency degrades (ok == false).
type FX struct{ USDIDR []Point }

// Rate is the IDR value of one unit of ccy on `at` (carry-forward, first value before the series; zero time = latest).
func (f FX) Rate(ccy string, at time.Time) (float64, bool) {
	switch ccy {
	case "IDR":
		return 1, true
	case "USD":
		if len(f.USDIDR) == 0 {
			return 0, false
		}
		if at.IsZero() {
			return f.USDIDR[len(f.USDIDR)-1].Value, true
		}
		return PriceAt(f.USDIDR, at), true
	}
	return 0, false
}

// PriceAt is the last value on or before d, or the first value when d precedes the series. Empty series give 0.
func PriceAt(s []Point, d time.Time) float64 {
	if len(s) == 0 {
		return 0
	}
	i := sort.Search(len(s), func(i int) bool { return s[i].Date.After(d) })
	if i == 0 {
		return s[0].Value
	}
	return s[i-1].Value
}

// Instrument is what a holding points at, resolved by the application layer.
type Instrument struct {
	Name     string
	Subtitle string
	Class    string
	Currency string
	Series   []Point // native closes ascending; empty for manual
	// manual
	ManualPrice float64
	ManualDate  time.Time
}

var classLabels = map[string][2]string{
	"equity": {"Saham", "Stocks"}, "etf": {"ETF", "ETF"}, "gold": {"Emas", "Gold"}, "commodity": {"Komoditas", "Commodities"},
	"crypto": {"Kripto", "Crypto"}, "fx": {"Valas", "Foreign exchange"}, "bond_sbn": {"Obligasi / SBN", "Bonds / SBN"},
	"mutual_fund": {"Reksa Dana", "Mutual funds"}, "deposit": {"Deposito", "Deposits"}, "other": {"Lainnya", "Other"},
}

// ClassLabels returns the Indonesian and English label of an asset class code.
func ClassLabels(code string) (id, en string) {
	if l, ok := classLabels[code]; ok {
		return l[0], l[1]
	}
	t := strings.ToUpper(code[:min(1, len(code))]) + strings.ReplaceAll(code[min(1, len(code)):], "_", " ")
	return t, t
}

// HoldingView is the API shape of a valued holding. Money is whole IDR; avgCost and lastPrice are IDR per unit.
type HoldingView struct {
	ID                 string    `json:"id"`
	Symbol             *string   `json:"symbol"`
	Name               string    `json:"name"`
	Subtitle           string    `json:"subtitle"`
	AssetClass         string    `json:"assetClass"`
	AssetClassLabelID  string    `json:"assetClassLabelId"`
	AssetClassLabelEn  string    `json:"assetClassLabelEn"`
	Currency           string    `json:"currency"`
	Units              float64   `json:"units"`
	AvgCost            float64   `json:"avgCost"`
	LastPrice          float64   `json:"lastPrice"`
	LastPriceDate      string    `json:"lastPriceDate"`
	MarketValue        int64     `json:"marketValue"`
	CostBasis          int64     `json:"costBasis"`
	Change1dPct        float64   `json:"change1dPct"`
	TotalReturnPct     float64   `json:"totalReturnPct"`
	TotalReturnIDR     int64     `json:"totalReturnIDR"`
	WeightPct          float64   `json:"weightPct"`
	Trend1y            []float64 `json:"trend1y"`
	IsManual           bool      `json:"isManual"`
	AcquiredAt         string    `json:"acquiredAt"`
	DividendsIDR       int64     `json:"dividendsIDR"`
	ValuationAvailable bool      `json:"valuationAvailable"` // false: no FX/price, valued at cost

	Value float64 `json:"-"` // unrounded market value, for weights and totals
	Cost  float64 `json:"-"`
	Divs  float64 `json:"-"`
}

func round(v float64, dec int) float64 {
	p := math.Pow10(dec)
	return math.Round(v*p) / p
}

func pct(num, den float64) float64 {
	if den == 0 {
		return 0
	}
	return round(num/den*100, 2)
}

// Downsample keeps n evenly spaced items including the first and the last.
func Downsample[T any](s []T, n int) []T {
	if n < 2 || len(s) <= n {
		return s
	}
	out := make([]T, n)
	for i := range out {
		out[i] = s[i*(len(s)-1)/(n-1)]
	}
	return out
}

// Value prices one holding in IDR. Weight is filled by ApplyWeights.
func Value(h Holding, in Instrument, fx FX, divs []Dividend) HoldingView {
	cost := h.Units * h.AvgCost
	idLabel, enLabel := ClassLabels(in.Class)
	v := HoldingView{
		ID: h.ID, Name: in.Name, Subtitle: in.Subtitle, AssetClass: in.Class, AssetClassLabelID: idLabel, AssetClassLabelEn: enLabel,
		Currency: in.Currency, Units: h.Units, AvgCost: h.AvgCost, IsManual: h.IsManual(), AcquiredAt: h.AcquiredAt.Format("2006-01-02"),
		Trend1y: []float64{}, Cost: cost,
	}
	if h.Symbol != "" {
		s := h.Symbol
		v.Symbol = &s
	}
	rate, rok := fx.Rate(in.Currency, time.Time{})
	var last float64
	var lastDate time.Time
	if h.IsManual() {
		last, lastDate = in.ManualPrice, in.ManualDate
	} else if n := len(in.Series); n > 0 {
		last, lastDate = in.Series[n-1].Value, in.Series[n-1].Date
		if n > 1 && in.Series[n-2].Value > 0 {
			v.Change1dPct = pct(last-in.Series[n-2].Value, in.Series[n-2].Value)
		}
	}
	v.ValuationAvailable = rok && !lastDate.IsZero()
	if v.ValuationAvailable {
		v.LastPrice = round(last*rate, 2)
		v.LastPriceDate = lastDate.Format("2006-01-02")
		v.Value = h.Units * last * rate
		v.Trend1y = trend(in, fx, lastDate)
	} else {
		v.LastPrice, v.Value = h.AvgCost, cost
	}
	v.MarketValue, v.CostBasis = int64(math.Round(v.Value)), int64(math.Round(cost))
	v.TotalReturnIDR = int64(math.Round(v.Value - cost))
	v.TotalReturnPct = pct(v.Value-cost, cost)
	for _, d := range divs {
		if d.ExDate.Before(h.AcquiredAt) {
			continue
		}
		amt := d.Amount
		if d.Currency == "USD" || d.Currency == "IDR" {
			if r, ok := fx.Rate(d.Currency, time.Time{}); ok {
				v.Divs += amt * r * h.Units
			}
		}
	}
	v.DividendsIDR = int64(math.Round(v.Divs))
	return v
}

// trend is the last year of closes in IDR per unit, about 30 points. Manual instruments have a flat line.
func trend(in Instrument, fx FX, lastDate time.Time) []float64 {
	if len(in.Series) == 0 {
		out := make([]float64, 30)
		for i := range out {
			out[i] = round(in.ManualPrice, 2)
		}
		return out
	}
	start := lastDate.AddDate(-1, 0, 0)
	i := sort.Search(len(in.Series), func(i int) bool { return !in.Series[i].Date.Before(start) })
	pts := Downsample(in.Series[i:], 30)
	out := make([]float64, len(pts))
	for k, p := range pts {
		r, _ := fx.Rate(in.Currency, p.Date)
		out[k] = round(p.Value*r, 2)
	}
	return out
}

// ApplyWeights sets WeightPct (share of total market value, 2 decimals). No value means zero weights.
func ApplyWeights(vs []HoldingView) {
	var total float64
	for _, v := range vs {
		total += v.Value
	}
	for i := range vs {
		vs[i].WeightPct = pct(vs[i].Value, total)
	}
}

type Totals struct {
	Count         int     `json:"count"`
	MarketValue   int64   `json:"marketValue"`
	CostBasis     int64   `json:"costBasis"`
	UnrealizedPnl int64   `json:"unrealizedPnl"`
	ReturnPct     float64 `json:"returnPct"`
	DividendsIDR  int64   `json:"dividendsIDR"`
	Change1dIDR   int64   `json:"change1dIDR"`
	UnvaluedCount int     `json:"unvaluedCount"`
}

func SumTotals(vs []HoldingView) Totals {
	var t Totals
	var val, cost, div, d1 float64
	for _, v := range vs {
		val += v.Value
		cost += v.Cost
		div += v.Divs
		if v.ValuationAvailable && v.Change1dPct > -100 {
			d1 += v.Value - v.Value/(1+v.Change1dPct/100)
		} else if !v.ValuationAvailable {
			t.UnvaluedCount++
		}
	}
	t.Count = len(vs)
	t.MarketValue, t.CostBasis, t.UnrealizedPnl = int64(math.Round(val)), int64(math.Round(cost)), int64(math.Round(val-cost))
	t.ReturnPct, t.DividendsIDR, t.Change1dIDR = pct(val-cost, cost), int64(math.Round(div)), int64(math.Round(d1))
	return t
}

// Held pairs a holding with its resolved instrument for history computation.
type Held struct {
	H  Holding
	In Instrument
}

type DayPoint struct {
	Date  time.Time
	Value float64
	Cost  float64
}

// History values the portfolio on each trading date in [from, to]: a holding counts from its acquisition date,
// is priced with the last close on or before the date (and the FX of that date), and without a price or FX it is
// carried at cost. With no catalog prices in range a weekly grid is used (manual-only portfolios).
func History(held []Held, fx FX, from, to time.Time) []DayPoint {
	if len(held) == 0 {
		return nil
	}
	seen := map[time.Time]struct{}{}
	for _, h := range held {
		for _, p := range h.In.Series {
			if !p.Date.Before(from) && !p.Date.After(to) {
				seen[p.Date] = struct{}{}
			}
		}
	}
	dates := make([]time.Time, 0, len(seen))
	for d := range seen {
		dates = append(dates, d)
	}
	if len(dates) == 0 {
		for d := to; !d.Before(from); d = d.AddDate(0, 0, -7) {
			dates = append(dates, d)
		}
	}
	sort.Slice(dates, func(i, j int) bool { return dates[i].Before(dates[j]) })
	out := make([]DayPoint, 0, len(dates))
	for _, d := range dates {
		var p DayPoint
		p.Date = d
		for _, h := range held {
			if h.H.AcquiredAt.After(d) {
				continue
			}
			cost := h.H.Units * h.H.AvgCost
			p.Cost += cost
			rate, rok := fx.Rate(h.In.Currency, d)
			switch {
			case h.H.IsManual():
				p.Value += h.H.Units * h.In.ManualPrice
			case rok && len(h.In.Series) > 0:
				p.Value += h.H.Units * PriceAt(h.In.Series, d) * rate
			default:
				p.Value += cost
			}
		}
		out = append(out, p)
	}
	return out
}
