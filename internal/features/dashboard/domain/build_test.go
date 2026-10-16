package domain

import (
	"math"
	"math/rand"
	"testing"
	"time"
)

func d(s string) time.Time { t, _ := time.Parse("2006-01-02", s); return t }

var today = d("2026-10-17")

func TestAllocateSumsToHundred(t *testing.T) {
	if got := Allocate(nil, 0, 0); len(got) != 0 {
		t.Fatalf("empty: %v", got)
	}
	classes := []string{"equity", "etf", "gold", "crypto", "bond_sbn", "mutual_fund", "deposit", "other", "commodity"}
	rng := rand.New(rand.NewSource(3))
	for i := 0; i < 1000; i++ {
		var hs []Holding
		for j := rng.Intn(10); j > 0; j-- {
			hs = append(hs, Holding{AssetClass: classes[rng.Intn(len(classes))], Value: rng.Float64() * 1e8})
		}
		sl := Allocate(hs, int64(rng.Intn(1e8)), int64(rng.Intn(1e8)))
		var sum float64
		for _, s := range sl {
			sum += s.Pct
		}
		if len(sl) > 0 && math.Abs(sum-100) > 1e-9 {
			t.Fatalf("sum %v: %v", sum, sl)
		}
	}
}

func TestAllocateGroups(t *testing.T) {
	got := Allocate([]Holding{{AssetClass: "equity", Value: 450}, {AssetClass: "etf", Value: 50}, {AssetClass: "bond_sbn", Value: 200}}, 300, 999)
	want := map[string]float64{"Stocks": 50, "Cash & other": 30, "Bonds": 20}
	if len(got) != 3 {
		t.Fatalf("%v", got)
	}
	for _, s := range got {
		if want[s.Label] != s.Pct {
			t.Fatalf("%v", got)
		}
	}
	if got[0].Label != "Stocks" {
		t.Fatal("sorted by share")
	}
	// self-reported investments only count without holdings
	if s := Allocate(nil, 100, 100); len(s) != 2 || s[0].Pct != 50 {
		t.Fatalf("%v", s)
	}
}

func TestRulesRecommendation(t *testing.T) {
	for _, c := range []struct {
		p            string
		stocks, bond float64
	}{{"Conservative", 15, 50}, {"Moderate", 50, 35}, {"Aggressive", 70, 20}} {
		r := RulesRecommendation(Profile{RiskProfile: c.p}, today)
		var sum float64
		for _, a := range r.Allocation {
			sum += a.Pct
		}
		if sum != 100 || r.Allocation[0].Pct != c.stocks || r.Allocation[1].Pct != c.bond || r.Source != "rules" || r.TextID == "" || r.TextEn == "" {
			t.Fatalf("%s: %+v", c.p, r)
		}
	}
	if r := RulesRecommendation(Profile{RiskProfile: "Moderate", Sharia: "yes"}, today); r.Allocation[1].Label != "Sukuk" || r.Allocation[0].Label != "Saham syariah" {
		t.Fatalf("sharia: %+v", r.Allocation)
	}
	if r := RulesRecommendation(Profile{}, today); len(r.Allocation) != 0 || r.TextID != "" || r.Allocation == nil {
		t.Fatalf("no profile: %+v", r)
	}
}

func TestBuildHomeNoProfileNoHoldings(t *testing.T) {
	h := BuildHome(Profile{Status: "none"}, Snapshot{}, "6M", today, Recommendation{})
	if h.ProfileStatus != "none" || h.Kpis.TotalInvestmentAssets.Value != 0 || h.AssetTrend.Total != 0 || len(h.AssetTrend.Allocation) != 0 ||
		h.Recommendation.Allocation == nil || h.Account.Plan != "Platinum Member" || len(h.Kpis.MonthlyIncome.Sparkline) != 12 {
		t.Fatalf("%+v", h)
	}
	if len(h.AssetTrend.Points) == 0 {
		t.Fatal("flat points expected so the chart has an x axis")
	}
}

func TestBuildHomeWithHoldings(t *testing.T) {
	hist := []DayPoint{{d("2026-04-30"), 100, 90}, {d("2026-09-17"), 200, 150}, {d("2026-09-30"), 220, 150}, {d("2026-10-16"), 300, 150}}
	snap := Snapshot{Holdings: []Holding{{AssetClass: "equity", Value: 300, Cost: 150}}, History: hist}
	h := BuildHome(Profile{Status: "completed", FirstName: "Dinda", Savings: 100, Income: 8, Expenses: 5, RiskProfile: "Moderate"}, snap, "6M", today, Recommendation{})
	if h.AssetTrend.Total != 400 || h.Kpis.TotalInvestmentAssets.Value != 300 || h.Kpis.ReadySavings.Value != 100 {
		t.Fatalf("totals %+v", h.AssetTrend)
	}
	// one month earlier (2026-09-17) investments were 200: +50%; total 300 -> 400: +33.33%
	if h.Kpis.TotalInvestmentAssets.ChangePct != 50 || h.AssetTrend.ChangePct != 33.33 {
		t.Fatalf("change %v %v", h.Kpis.TotalInvestmentAssets.ChangePct, h.AssetTrend.ChangePct)
	}
	p := h.AssetTrend.Points
	if len(p) != 3 || p[0].TotalAssets != 200 || p[0].InvestmentValue != 100 || p[1].TotalAssets != 320 || p[2].TotalAssets != 400 {
		t.Fatalf("points %+v", p)
	}
	if h.Preferences.RiskLevel != 2 {
		t.Fatal("risk level")
	}
}

func TestBuildPortfolioEmptyAndYtd(t *testing.T) {
	e := BuildPortfolio("Dinda", Snapshot{}, "6M", today)
	if e.Holdings == nil || e.Performance.Points == nil || len(e.Summary) != 4 || e.Footer.Count != 0 || e.Kpis.TotalAssets.Value != 0 || len(e.Kpis.TotalAssets.Sparkline) != 12 {
		t.Fatalf("%+v", e)
	}
	// held since before the year: value 100 -> 150 on constant cost 100 => ytd +50%
	hist := []DayPoint{{d("2025-12-31"), 100, 100}, {d("2026-09-16"), 140, 100}, {d("2026-10-16"), 150, 100}}
	snap := Snapshot{Holdings: []Holding{{Value: 150, Cost: 100, Divs: 7}}, History: hist}
	pg := BuildPortfolio("x", snap, "1M", today)
	if pg.Kpis.ReturnYtd.Value != 50 || pg.Kpis.UnrealizedPnl.Value != 50 || pg.Kpis.TotalAssets.Value != 150 || pg.Footer.DividendsAccumulated != 7 {
		t.Fatalf("%+v", pg.Kpis)
	}
	// holding bought in March at cost 100, worth 120 now: ytd equals its total return
	hist = []DayPoint{{d("2026-01-01"), 0, 0}, {d("2026-03-02"), 100, 100}, {d("2026-10-16"), 120, 100}}
	pg = BuildPortfolio("x", Snapshot{Holdings: []Holding{{Value: 120, Cost: 100}}, History: hist}, "ALL", today)
	if pg.Kpis.ReturnYtd.Value != 20 {
		t.Fatalf("ytd %v", pg.Kpis.ReturnYtd.Value)
	}
	if len(pg.Performance.Points) != 2 {
		t.Fatalf("ALL trims the days before the first purchase: %+v", pg.Performance.Points)
	}
}

func TestRanges(t *testing.T) {
	for _, r := range []string{"6M", "1Y", "ALL"} {
		if _, ok := HomeStart(r, today); !ok {
			t.Errorf("home %s", r)
		}
	}
	for _, r := range []string{"1M", "3M", "6M", "1Y", "ALL"} {
		if _, ok := PortfolioStart(r, today); !ok {
			t.Errorf("portfolio %s", r)
		}
	}
	if _, ok := HomeStart("1M", today); ok {
		t.Error("1M is not a home range")
	}
	if got := HistoryFrom(today.AddDate(0, -1, 0), today, true); got != d("2026-01-01") {
		t.Errorf("ytd base %v", got)
	}
}
