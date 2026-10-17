package domain

import (
	"math"
	"sort"
	"time"
)

const PlanLabel = "Platinum Member"

// Today is the Jakarta calendar date as UTC midnight (the same convention as the portfolio slice).
func Today(now time.Time) time.Time {
	y, m, d := now.In(time.FixedZone("WIB", 7*3600)).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// HomeStart is the first day of a home range (6M, 1Y, ALL = the 2 year data window); ok is false for other values.
func HomeStart(rng string, today time.Time) (time.Time, bool) {
	switch rng {
	case "6M":
		return today.AddDate(0, -6, 0), true
	case "1Y":
		return today.AddDate(-1, 0, 0), true
	case "ALL":
		return today.AddDate(-2, 0, 0), true
	}
	return time.Time{}, false
}

// PortfolioStart is the first day of a portfolio range (1M, 3M, 6M, 1Y, ALL).
func PortfolioStart(rng string, today time.Time) (time.Time, bool) {
	switch rng {
	case "1M":
		return today.AddDate(0, -1, 0), true
	case "3M":
		return today.AddDate(0, -3, 0), true
	}
	return HomeStart(rng, today)
}

// HistoryFrom is how far back the portfolio history must reach: the range start, and for the portfolio page also
// January 1st of the year one month ago (YTD base for today and for the one-month comparison).
func HistoryFrom(start, today time.Time, ytd bool) time.Time {
	if !ytd {
		return start
	}
	base := today.AddDate(0, -1, 0)
	jan := time.Date(base.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	if jan.Before(start) {
		return jan
	}
	return start
}

func pct(num, den float64) float64 {
	if den == 0 {
		return 0
	}
	return math.Round(num/den*10000) / 100
}

func rnd(v float64) int64 { return int64(math.Round(v)) }

func flat(n int, v float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = math.Round(v)
	}
	return out
}

func downsample[T any](s []T, n int) []T {
	if len(s) <= n || n < 2 {
		return s
	}
	out := make([]T, n)
	for i := range out {
		out[i] = s[i*(len(s)-1)/(n-1)]
	}
	return out
}

// at returns the last point on or before d (the first point when d precedes the history).
func at(h []DayPoint, d time.Time) DayPoint {
	if len(h) == 0 {
		return DayPoint{}
	}
	i := sort.Search(len(h), func(i int) bool { return h[i].Date.After(d) })
	if i == 0 {
		return h[0]
	}
	return h[i-1]
}

func since(h []DayPoint, d time.Time) []DayPoint {
	i := sort.Search(len(h), func(i int) bool { return !h[i].Date.Before(d) })
	return h[i:]
}

// trimLeadingEmpty drops the days before the first holding was acquired.
func trimLeadingEmpty(h []DayPoint) []DayPoint {
	for i, p := range h {
		if p.Value != 0 || p.Cost != 0 {
			return h[i:]
		}
	}
	return nil
}

// monthly keeps the last point of every calendar month.
func monthly(h []DayPoint) []DayPoint {
	var out []DayPoint
	for i, p := range h {
		if i+1 == len(h) || h[i+1].Date.Month() != p.Date.Month() || h[i+1].Date.Year() != p.Date.Year() {
			out = append(out, p)
		}
	}
	return out
}

// flatHistory is a constant series with month-end dates, used when there are no holdings to value.
func flatHistory(start, today time.Time, v float64) []DayPoint {
	var out []DayPoint
	for d := time.Date(start.Year(), start.Month()+1, 0, 0, 0, 0, 0, time.UTC); d.Before(today); d = time.Date(d.Year(), d.Month()+2, 0, 0, 0, 0, 0, time.UTC) {
		if !d.Before(start) {
			out = append(out, DayPoint{Date: d, Value: v, Cost: v})
		}
	}
	return append(out, DayPoint{Date: today, Value: v, Cost: v})
}

func sum(hs []Holding) (value, cost, divs float64) {
	for _, h := range hs {
		value += h.Value
		cost += h.Cost
		divs += h.Divs
	}
	return
}

// withCash returns the holdings as page rows with weights over total assets (investments + cash), cash last.
func withCash(h []Holding, cash int64) []HoldingRow {
	out := make([]HoldingRow, 0, len(h)+1)
	total := float64(cash)
	for _, x := range h {
		total += x.Value
	}
	for _, x := range h {
		if total > 0 {
			x.WeightPct = math.Round(x.Value/total*1000) / 10
		}
		out = append(out, HoldingRow{Holding: x})
	}
	if cash > 0 {
		c := cashHolding(cash)
		c.WeightPct = math.Round(float64(cash)/total*1000) / 10
		out = append(out, HoldingRow{Holding: c, IsCash: true})
	}
	return out
}

func holdingsOrEmpty(h []Holding) []Holding {
	if h == nil {
		return []Holding{}
	}
	return h
}

// BuildHome assembles the main dashboard. Total assets = liquid savings + investment value, where the investment
// value is the valued holdings, or the self-reported q7 amount when the user has no holdings yet.
func BuildHome(p Profile, snap Snapshot, rng string, today time.Time, rec Recommendation) Home {
	var h Home
	start, _ := HomeStart(rng, today)
	hold := snap.Holdings
	hist := snap.History
	invNow, _, _ := sum(hold)
	if len(hold) == 0 {
		invNow = float64(p.Investments)
		hist = flatHistory(start, today, invNow)
	} else if rng == "ALL" {
		hist = trimLeadingEmpty(hist)
	}
	hist = since(hist, start)
	sav := float64(p.Savings)

	h.Name = p.FirstName
	inv1m := at(hist, today.AddDate(0, -1, 0)).Value
	if len(hold) == 0 {
		inv1m = invNow
	}
	spark := flat(12, invNow)
	if recent := since(hist, today.AddDate(0, 0, -30)); len(hold) > 0 && len(recent) >= 2 {
		recent = downsample(recent, 12)
		spark = make([]float64, len(recent))
		for i, r := range recent {
			spark[i] = math.Round(r.Value)
		}
	}
	h.Kpis.TotalInvestmentAssets = K{Value: math.Round(invNow), ChangePct: pct(invNow-inv1m, inv1m), Sparkline: spark}
	h.Kpis.ReadySavings = K{Value: sav, Sparkline: flat(12, sav)}
	h.Kpis.MonthlyExpenses = K{Value: float64(p.Expenses), Sparkline: flat(12, float64(p.Expenses))}
	h.Kpis.MonthlyIncome = K{Value: float64(p.Income), Sparkline: flat(12, float64(p.Income))}

	h.AssetTrend.Range = rng
	h.AssetTrend.Points = []TrendPoint{}
	for _, m := range monthly(hist) {
		h.AssetTrend.Points = append(h.AssetTrend.Points, TrendPoint{Date: m.Date.Format("2006-01-02"), TotalAssets: rnd(m.Value + sav), InvestmentValue: rnd(m.Value)})
	}
	total := invNow + sav
	h.AssetTrend.Total = rnd(total)
	h.AssetTrend.ChangePct = pct(total-(inv1m+sav), inv1m+sav)
	h.AssetTrend.Allocation = Allocate(hold, p.Savings, p.Investments)

	fs := &h.FinancialSummary
	fs.MonthlyIncome, fs.MonthlyExpenses, fs.LiquidSavings, fs.TotalInvestmentAssets, fs.Dependents = p.Income, p.Expenses, p.Savings, rnd(invNow), p.Dependents

	h.Preferences.RiskProfile, h.Preferences.RiskLevel = p.RiskProfile, RiskLevel(p.RiskProfile)
	h.Preferences.PrimaryGoal, h.Preferences.GoalHorizon, h.Preferences.Sharia = p.Goal, p.Horizon, p.Sharia
	h.Recommendation = rec
	if h.Recommendation.Allocation == nil {
		h.Recommendation.Allocation = []Allocation{}
	}
	h.Account.Plan = PlanLabel
	h.ProfileStatus = p.Status
	return h
}

// ytdPct is the year-to-date return at date d: profit made since January 1st over the capital at the start of the
// year plus the cost added since. Dividends are not included.
func ytdPct(h []DayPoint, d time.Time) float64 {
	jan := time.Date(d.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	cur := at(h, d)
	var v0, c0 float64
	if len(h) > 0 && h[0].Date.Before(jan) {
		b := at(h, jan.AddDate(0, 0, -1))
		v0, c0 = b.Value, b.Cost
	}
	gain := (cur.Value - cur.Cost) - (v0 - c0)
	return pct(gain, v0+(cur.Cost-c0))
}

// cashHolding is the profile's liquid savings as a portfolio row: no market price, so value equals cost and it never moves.
func cashHolding(amount int64) Holding {
	return Holding{
		ID: "cash", Name: "Kas", Subtitle: "Tabungan likuid", AssetClass: "cash", AssetClassLabelID: "Kas", AssetClassLabelEn: "Cash",
		Currency: "IDR", Units: 1, AvgCost: float64(amount), LastPrice: float64(amount), MarketValue: amount, CostBasis: amount,
		Trend1y: []float64{}, Value: float64(amount), Cost: float64(amount),
	}
}

// BuildPortfolio assembles the portfolio page. History must reach back to HistoryFrom(start, today, true).
// cash (liquid savings from the profile) counts toward total assets, weights and allocation only: cost, profit and
// return describe the investments, so idle cash does not dilute them.
func BuildPortfolio(name string, snap Snapshot, rng string, today time.Time, cash int64) PortfolioPage {
	var pg PortfolioPage
	start, _ := PortfolioStart(rng, today)
	pg.Name = name
	pg.Holdings = withCash(snap.Holdings, cash)
	hist := snap.History
	if rng == "ALL" {
		hist = trimLeadingEmpty(hist)
	}
	val, cost, divs := sum(snap.Holdings)
	pnl := val - cost
	month := today.AddDate(0, -1, 0)
	m := at(hist, month)
	shown := since(hist, start)

	sp := func(f func(DayPoint) float64) []float64 {
		d := downsample(shown, 12)
		if len(d) < 2 {
			return flat(12, 0)
		}
		out := make([]float64, len(d))
		for i, p := range d {
			out[i] = math.Round(f(p))
		}
		return out
	}
	ytdNow, ytdPrev := ytdPct(hist, today), ytdPct(hist, month)
	c := float64(cash)
	pg.Kpis.TotalAssets = K{Value: math.Round(val + c), ChangePct: pct(val-m.Value, m.Value+c), Sparkline: sp(func(p DayPoint) float64 { return p.Value + c })}
	pg.Kpis.TotalCost = K{Value: math.Round(cost), ChangePct: pct(cost-m.Cost, m.Cost), Sparkline: sp(func(p DayPoint) float64 { return p.Cost })}
	prevPnl := m.Value - m.Cost
	pg.Kpis.UnrealizedPnl = K{Value: math.Round(pnl), ChangePct: pct(pnl-prevPnl, math.Abs(prevPnl)), Sparkline: sp(func(p DayPoint) float64 { return p.Value - p.Cost })}
	pg.Kpis.ReturnYtd = K{Value: ytdNow, ChangePct: math.Round((ytdNow-ytdPrev)*100) / 100, Sparkline: sp(func(p DayPoint) float64 { return pct(p.Value-p.Cost, p.Cost) })}

	pg.Performance.Points = []PerfPoint{}
	for _, p := range downsample(shown, 120) {
		pg.Performance.Points = append(pg.Performance.Points, PerfPoint{Date: p.Date.Format("2006-01-02"), Value: rnd(p.Value), Cost: rnd(p.Cost)})
	}
	pg.Summary = []SummaryRow{
		{Key: "totalAssets", Label: "Total Aset", Value: math.Round(val + c), Kind: "idr", ChangePct: pg.Kpis.TotalAssets.ChangePct},
		{Key: "totalCost", Label: "Modal Investasi", Value: math.Round(cost), Kind: "idr", ChangePct: pg.Kpis.TotalCost.ChangePct},
		{Key: "unrealizedPnl", Label: "Keuntungan / Kerugian", Value: math.Round(pnl), Kind: "idr", ChangePct: pg.Kpis.UnrealizedPnl.ChangePct},
		{Key: "returnYtd", Label: "Return (YTD)", Value: ytdNow, Kind: "percent", ChangePct: pg.Kpis.ReturnYtd.ChangePct},
	}
	pg.Footer.Count, pg.Footer.Unrealized, pg.Footer.DividendsAccumulated = len(snap.Holdings), rnd(pnl), rnd(divs)
	return pg
}
