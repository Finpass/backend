package domain

import (
	"math"
	"testing"
	"time"
)

func base() Profile {
	return Profile{UserID: "u", Name: "Surya", Risk: Moderate, Ladder: 3, Goal: "wealth_growth", GoalHorizon: "5_10", Knowledge: "intermediate",
		Income: 15_000_000, Expenses: 8_000_000, Surplus: 5_000_000, Investable: 2_000_000, Employment: "permanent_employee",
		Health: Health{Safety: 85, Growth: 62, Wisdom: 52, Freedom: 65, Total: 70}, EmergencyMonths: 4, EmergencyTargetMax: 6}
}

func sum(m map[string]float64) float64 {
	t := 0.0
	for _, v := range m {
		t += v
	}
	return t
}

func TestTargetAllocation(t *testing.T) {
	tests := []struct {
		name   string
		mut    func(*Profile)
		has    []string
		absent []string
		check  func(*testing.T, map[string]float64)
	}{
		{"moderate base", func(p *Profile) {}, []string{ClassLargeCap, ClassBondGov, ClassGold, ClassCrypto}, []string{ClassSukuk}, nil},
		{"conservative has no crypto", func(p *Profile) { p.Risk = Conservative }, []string{ClassBondGov}, []string{ClassCrypto}, nil},
		{"sharia yes swaps bond for sukuk", func(p *Profile) { p.Sharia = "yes" }, []string{ClassSukuk}, []string{ClassBondGov}, nil},
		{"knowledge none drops crypto and small cap", func(p *Profile) { p.Knowledge = "none" }, nil, []string{ClassCrypto, ClassMidSmall}, nil},
		{"knowledge basic drops small cap", func(p *Profile) { p.Knowledge = "basic" }, nil, []string{ClassMidSmall, ClassCrypto}, nil},
		{"ladder 2 means no investment", func(p *Profile) { p.Ladder = 2 }, []string{ClassDeposit, ClassFund, ClassBondGov}, []string{ClassLargeCap, ClassGold}, nil},
		{"ladder 1 sharia uses sukuk", func(p *Profile) { p.Ladder = 1; p.Sharia = "yes" }, []string{ClassSukuk}, []string{ClassBondGov}, nil},
		{"short horizon lowers equity", func(p *Profile) { p.GoalHorizon = "1_3" }, nil, nil, func(t *testing.T, m map[string]float64) {
			eq := m[ClassLargeCap] + m[ClassMidSmall] + m[ClassGlobal] + m[ClassETF]
			if eq > 48 { // lower bound 45 normalised against a total slightly above 100 before scaling
				t.Errorf("equity %.1f too high", eq)
			}
		}},
		{"freelancer moves equity to cash", func(p *Profile) { p.Employment = "freelancer" }, nil, nil, func(t *testing.T, m map[string]float64) {
			if m[ClassDeposit]+m[ClassFund] < 17 {
				t.Errorf("cash %.1f", m[ClassDeposit]+m[ClassFund])
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := base()
			tc.mut(&p)
			tr := TargetAllocation(p)
			if math.Abs(sum(tr.Target)-100) > 0.05 {
				t.Fatalf("sum %.2f %v", sum(tr.Target), tr.Target)
			}
			for _, c := range tc.has {
				if tr.Target[c] <= 0 {
					t.Errorf("%s missing: %v", c, tr.Target)
				}
			}
			for _, c := range tc.absent {
				if tr.Target[c] != 0 {
					t.Errorf("%s should be absent: %v", c, tr.Target)
				}
			}
			if tc.check != nil {
				tc.check(t, tr.Target)
			}
			if tr.Investable != (p.Ladder > 2) {
				t.Error("investable flag")
			}
		})
	}
}

func TestDependentsShiftEquity(t *testing.T) {
	a, b := base(), base()
	b.Dependents = 3
	ea := TargetAllocation(a).Target
	eb := TargetAllocation(b).Target
	if eb[ClassLargeCap] >= ea[ClassLargeCap] || eb[ClassBondGov] <= ea[ClassBondGov] {
		t.Errorf("q5>=3 should shift equity to fixed income: %v vs %v", ea, eb)
	}
}

func TestGates(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*Profile)
		c    Candidate
		gate string
	}{
		{"sharia non", func(p *Profile) { p.Sharia = "yes" }, Candidate{Class: ClassLargeCap, Sharia: "non_sharia", Volatility: "medium"}, "gate_sharia"},
		{"sharia unknown excluded", func(p *Profile) { p.Sharia = "yes" }, Candidate{Class: ClassLargeCap, Sharia: "unknown", Volatility: "medium"}, "gate_sharia"},
		{"sharia ok", func(p *Profile) { p.Sharia = "yes" }, Candidate{Class: ClassLargeCap, Sharia: "sharia_compliant", Volatility: "medium"}, ""},
		{"sharia prefer keeps non_sharia", func(p *Profile) { p.Sharia = "prefer" }, Candidate{Class: ClassLargeCap, Sharia: "non_sharia", Volatility: "medium"}, ""},
		{"horizon 2y high vol", func(p *Profile) { p.GoalHorizon = "1_3" }, Candidate{Volatility: "high", Sharia: "x"}, "gate_horizon"},
		{"horizon <1y medium vol", func(p *Profile) { p.GoalHorizon = "lt_1" }, Candidate{Volatility: "medium"}, "gate_horizon"},
		{"horizon <1y low vol ok", func(p *Profile) { p.GoalHorizon = "lt_1" }, Candidate{Volatility: "low"}, ""},
		{"horizon 4y high ok for moderate", func(p *Profile) { p.GoalHorizon = "3_5" }, Candidate{Volatility: "high"}, ""},
		{"conservative high vol", func(p *Profile) { p.Risk = Conservative }, Candidate{Volatility: "high"}, "gate_volatility"},
		{"q16 none crypto", func(p *Profile) { p.Knowledge = "none" }, Candidate{Class: ClassCrypto, Volatility: "medium"}, "gate_knowledge"},
		{"q16 none small cap", func(p *Profile) { p.Knowledge = "none" }, Candidate{Class: ClassMidSmall, Volatility: "medium"}, "gate_knowledge"},
		{"q16 basic crypto allowed by gate", func(p *Profile) { p.Knowledge = "basic" }, Candidate{Class: ClassCrypto, Volatility: "medium"}, ""},
		{"q16 basic small cap", func(p *Profile) { p.Knowledge = "basic" }, Candidate{Class: ClassMidSmall, Volatility: "medium"}, "gate_knowledge"},
		{"ladder 2 equity", func(p *Profile) { p.Ladder = 2 }, Candidate{Class: ClassLargeCap, Volatility: "low"}, "gate_ladder"},
		{"ladder 2 deposit", func(p *Profile) { p.Ladder = 2 }, Candidate{Class: ClassDeposit, Volatility: "low"}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := base()
			tc.mut(&p)
			if g, _ := CheckGates(p, tc.c); g != tc.gate {
				t.Errorf("gate %q want %q", g, tc.gate)
			}
		})
	}
}

func TestScoreBuckets(t *testing.T) {
	for _, tc := range []struct{ sharpe, want float64 }{{2, 100}, {1.99, 85}, {1.5, 85}, {1.49, 70}, {1, 70}, {0.99, 50}, {0.5, 50}, {0.49, 30}, {0, 30}, {-0.01, 0}} {
		if got := sharpeScore(tc.sharpe); got != tc.want {
			t.Errorf("sharpe %.2f got %v want %v", tc.sharpe, got, tc.want)
		}
	}
	for _, tc := range []struct{ er, want float64 }{{0.5, 100}, {0.51, 85}, {1, 85}, {2, 70}, {3, 50}, {3.01, 30}} {
		if got := costScore(tc.er); got != tc.want {
			t.Errorf("cost %.2f got %v want %v", tc.er, got, tc.want)
		}
	}
	if liquidityScore("high") != 100 || liquidityScore("medium") != 70 || liquidityScore("low") != 40 {
		t.Error("liquidity")
	}
}

func fp(v float64) *float64 { return &v }

func TestScoreFormula(t *testing.T) {
	c := Candidate{Symbol: "X", Class: ClassLargeCap, Liquidity: "high", Return1Y: 15, Vol: 15, PositiveMonths: 9, PER: fp(5), PBV: fp(0.5), ROE: fp(30), Beta: fp(1.2), Geography: "indonesia"}
	p := base()
	s := Score(p, c, 5, MacroSignals{})
	// sharpe (15-5)/15 = 0.67 -> 50; return: norm(15,-20,50)=0.5 -> 50*0.6 + 75*0.4 = 60; cost 70; liq 100; fund 100; goal 100; macro 50
	want := 50*0.25 + 60*0.20 + 70*0.15 + 100*0.15 + 100*0.15 + 100*0.05 + 50*0.05
	if math.Abs(s.Total-want) > 0.11 {
		t.Errorf("total %.2f want %.2f (%v)", s.Total, want, s.Components)
	}
	if s2 := Score(p, Candidate{Class: ClassGold, Liquidity: "low", Geography: "global"}, 0, MacroSignals{InflationUp: true}); s2.Components["macro_fit"] != 90 {
		t.Errorf("inflation gold macro fit %v", s2.Components)
	}
	if s3 := Score(p, Candidate{Class: ClassGlobal, Geography: "global", Liquidity: "high"}, 0, MacroSignals{RupiahWeak: true}); s3.Components["macro_fit"] != 85 {
		t.Error("rupiah weak global")
	}
}

func TestCapWeights(t *testing.T) {
	mk := func(scores ...float64) []scored {
		var o []scored
		for _, s := range scores {
			o = append(o, scored{S: ScoreTrace{Total: s}})
		}
		return o
	}
	w, left := capWeights(30, mk(90, 80, 70))
	if left != 0 || math.Abs(w[0]+w[1]+w[2]-30) > 1e-6 {
		t.Errorf("30 over 3: %v %v", w, left)
	}
	for _, x := range w {
		if x > 10+1e-9 {
			t.Errorf("cap exceeded %v", w)
		}
	}
	w, left = capWeights(30, mk(90, 80))
	if math.Abs(left-10) > 1e-6 || w[0] != 10 || w[1] != 10 {
		t.Errorf("30 over 2: %v left %v", w, left)
	}
	w, left = capWeights(15, mk(99, 20))
	if left != 0 || w[0] != 10 || math.Abs(w[1]-5) > 1e-6 {
		t.Errorf("overflow to second: %v %v", w, left)
	}
}

func cand(sym, class, sector string, ret float64) Candidate {
	return Candidate{Symbol: sym, Name: sym, Class: class, Sector: sector, Sharia: "sharia_compliant", Geography: "indonesia", Volatility: "medium", Liquidity: "high", Return1Y: ret, Vol: 20, PositiveMonths: 8}
}

func TestMatchEndToEnd(t *testing.T) {
	p := base()
	var cs []Candidate
	for i, s := range []string{"A", "B", "C", "D", "E", "F"} {
		cs = append(cs, cand(s, ClassLargeCap, "Financials", float64(30-i)))
	}
	cs = append(cs, cand("G", ClassLargeCap, "Energy", 5), cand("H", ClassLargeCap, "Healthcare", 4), cand("GLD", ClassGold, "", 10))
	out := Match(p, cs, nil, 4, MacroSignals{})
	if !out.Investable || len(out.Portfolio) == 0 {
		t.Fatal("expected a portfolio")
	}
	finSector := 0.0
	for _, pc := range out.Portfolio {
		for _, a := range pc.Assets {
			if a.Kind == "asset" && a.Weight > 10.001 {
				t.Errorf("asset cap broken: %s %.1f", a.Ticker, a.Weight)
			}
			if a.Sector == "Financials" {
				finSector += a.Weight
			}
		}
	}
	tgt := out.Trace.TargetAllocation.Target
	limit := 0.4 * (tgt[ClassLargeCap] + tgt[ClassMidSmall] + tgt[ClassETF])
	if finSector > limit+0.2 {
		t.Errorf("sector cap: %.1f > %.1f", finSector, limit)
	}
	var bond *ProductClass
	for i := range out.Portfolio {
		if out.Portfolio[i].Class == ClassBondGov {
			bond = &out.Portfolio[i]
		}
	}
	if bond == nil || bond.Assets[0].Kind != "instrument" || bond.Assets[0].Score != nil {
		t.Error("bond class should be a score-less instrument")
	}
	if len(out.Trace.ScoresPerAsset) == 0 || len(out.Trace.Diversification) == 0 {
		t.Error("trace should carry scores and diversification actions")
	}
}

func TestMatchShariaAndLadder(t *testing.T) {
	p := base()
	p.Sharia = "yes"
	nonS := cand("N", ClassLargeCap, "Financials", 40)
	nonS.Sharia = "non_sharia"
	unk := cand("U", ClassLargeCap, "Energy", 40)
	unk.Sharia = "unknown"
	ok := cand("OK", ClassLargeCap, "Energy", 1)
	out := Match(p, []Candidate{nonS, unk, ok}, nil, 4, MacroSignals{})
	for _, pc := range out.Portfolio {
		for _, a := range pc.Assets {
			if a.Ticker == "N" || a.Ticker == "U" {
				t.Errorf("sharia gate leaked %s", a.Ticker)
			}
		}
	}
	if len(out.Trace.Exclusions) != 2 {
		t.Errorf("exclusions %v", out.Trace.Exclusions)
	}
	p2 := base()
	p2.Ladder = 1
	o2 := Match(p2, []Candidate{cand("A", ClassLargeCap, "x", 10)}, nil, 4, MacroSignals{})
	if o2.Investable || len(o2.Flags) == 0 {
		t.Error("ladder 1 not investable")
	}
	for _, pc := range o2.Portfolio {
		if pc.Class == ClassLargeCap {
			t.Error("equity at ladder 1")
		}
	}
}

func TestHoldingsPriorityOnTies(t *testing.T) {
	a, b := scored{C: Candidate{Symbol: "AAA"}, S: ScoreTrace{Total: 80}}, scored{C: Candidate{Symbol: "BBB"}, S: ScoreTrace{Total: 80}}
	l := []scored{a, b}
	rank(l, map[string]bool{"BBB": true})
	if l[0].C.Symbol != "BBB" {
		t.Error("held asset should win a tie")
	}
	l = []scored{a, b}
	rank(l, nil)
	if l[0].C.Symbol != "AAA" {
		t.Error("alphabetical when no holding")
	}
}

func TestCorrelationDrop(t *testing.T) {
	r1, r2, r3 := map[int]float64{}, map[int]float64{}, map[int]float64{}
	for i := 0; i < 60; i++ {
		v := math.Sin(float64(i))
		r1[i], r2[i], r3[i] = v, v*2+0.0001, math.Cos(float64(i)*3.1)
	}
	if c, ok := correlation(r1, r2); !ok || c < 0.99 {
		t.Errorf("corr %v", c)
	}
	if c, _ := correlation(r1, r3); c > 0.5 {
		t.Errorf("corr %v", c)
	}
	if _, ok := correlation(map[int]float64{1: 1}, map[int]float64{1: 1}); ok {
		t.Error("too few points")
	}
	p := base()
	a := cand("A", ClassLargeCap, "Financials", 30)
	a.Rets = r1
	b := cand("B", ClassLargeCap, "Energy", 29)
	b.Rets = r2
	c := cand("C", ClassLargeCap, "Healthcare", 28)
	c.Rets = r3
	out := Match(p, []Candidate{a, b, c}, nil, 4, MacroSignals{})
	for _, pc := range out.Portfolio {
		for _, x := range pc.Assets {
			if x.Ticker == "B" {
				t.Error("B is 0.99 correlated with A and must be dropped")
			}
		}
	}
}

func TestSortedWeightsSum100(t *testing.T) {
	for _, r := range []string{Conservative, Moderate, Aggressive} {
		p := base()
		p.Risk = r
		total := 0
		for _, w := range SortedWeights(TargetAllocation(p).Target) {
			total += w.Pct
		}
		if total != 100 {
			t.Errorf("%s sums %d", r, total)
		}
	}
}

func TestAlertThresholds(t *testing.T) {
	today := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	h := func(class string, last float64) Holding {
		return Holding{Symbol: "X", Name: "X", Class: class, Units: 100, Last: last, Prev: 100, FX: 1}
	}
	tests := []struct {
		name  string
		h     Holding
		fires bool
	}{
		{"large cap exactly 3% no alert", h(ClassLargeCap, 97), false},
		{"large cap -3.01% alerts", h(ClassLargeCap, 96.99), true},
		{"large cap +3.5%", h(ClassLargeCap, 103.5), true},
		{"mid small 4.9% no alert", h(ClassMidSmall, 104.9), false},
		{"mid small 5.01% alert", h(ClassMidSmall, 105.01), true},
		{"gold 2.01%", h(ClassGold, 102.01), true},
		{"gold 2.0%", h(ClassGold, 102), false},
		{"crypto 5%", h(ClassCrypto, 95), false},
		{"crypto 5.5%", h(ClassCrypto, 94.5), true},
		{"fund 2.5%", h(ClassFund, 102.5), true},
		{"deposit never", h(ClassDeposit, 150), false},
		{"unknown class no threshold", h("other", 150), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := MatchAlert([]Holding{tc.h}, today, nil)
			if (a != nil && a.Type == AlertPrice) != tc.fires {
				t.Errorf("fires=%v alert=%+v", a != nil, a)
			}
		})
	}
	// stored per-asset threshold overrides the class default
	hh := h(ClassLargeCap, 98.5)
	hh.AlertThresholdPct = fp(1)
	if a, _ := MatchAlert([]Holding{hh}, today, nil); a == nil {
		t.Error("custom threshold ignored")
	}
}

func TestAlertPositionPickRepeatAndOthers(t *testing.T) {
	today := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	a, _ := MatchAlert([]Holding{{Symbol: "BBCA.JK", Class: ClassLargeCap, Units: 1000, Last: 9680, Prev: 10000, FX: 1}}, today, nil)
	if a == nil || a.PositionIDR != -320000 || a.Direction != "down" || a.ChangePct != -3.2 {
		t.Fatalf("%+v", a)
	}
	// larger absolute move wins
	hs := []Holding{{Symbol: "A", Class: ClassLargeCap, Units: 1, Last: 94, Prev: 100, FX: 1}, {Symbol: "B", Class: ClassLargeCap, Units: 1, Last: 90, Prev: 100, FX: 1}}
	if a, _ := MatchAlert(hs, today, nil); a.Asset != "B" {
		t.Errorf("picked %s", a.Asset)
	}
	hist := []DayRecord{{AlertKey: a.Key}, {AlertKey: a.Key}}
	b, _ := MatchAlert([]Holding{{Symbol: "BBCA.JK", Class: ClassLargeCap, Units: 1000, Last: 9680, Prev: 10000, FX: 1}}, today, hist)
	if !b.Repeated {
		t.Error("third day in a row should be Repeated")
	}
	b, _ = MatchAlert([]Holding{{Symbol: "BBCA.JK", Class: ClassLargeCap, Units: 1000, Last: 9680, Prev: 10000, FX: 1}}, today, hist[:1])
	if b.Repeated {
		t.Error("second day is not repeated yet")
	}
	// corporate action and 52 week
	ex := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	c, _ := MatchAlert([]Holding{{Symbol: "TLKM.JK", Class: ClassLargeCap, Units: 1, Last: 100, Prev: 100, FX: 1, ExDate: &ex, DividendPerShare: 50}}, today, nil)
	if c == nil || c.Type != AlertCorp {
		t.Errorf("corp %+v", c)
	}
	old := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if c, _ := MatchAlert([]Holding{{Symbol: "T", Class: ClassLargeCap, Units: 1, Last: 100, Prev: 100, FX: 1, ExDate: &old}}, today, nil); c != nil {
		t.Error("old ex-date")
	}
	w, _ := MatchAlert([]Holding{{Symbol: "T", Class: ClassLargeCap, Units: 1, Last: 101, Prev: 100, FX: 1, Hi52Prev: 100.5, Lo52Prev: 60}}, today, nil)
	if w == nil || w.Type != Alert52w || w.Detail != "high" {
		t.Errorf("52w %+v", w)
	}
	l, _ := MatchAlert([]Holding{{Symbol: "T", Class: ClassLargeCap, Units: 1, Last: 99, Prev: 100, FX: 1, Hi52Prev: 150, Lo52Prev: 99.5}}, today, nil)
	if l == nil || l.Detail != "low" {
		t.Errorf("52w low %+v", l)
	}
	if m, _ := MatchAlert([]Holding{{Symbol: "", Manual: true, Class: ClassFund, Units: 1, Last: 100}}, today, nil); m != nil {
		t.Error("manual holdings have no quotes")
	}
}

func TestQuietMarket(t *testing.T) {
	five := make([]DayRecord, 5)
	if !QuietMarket(five, false) {
		t.Error("5 empty days is quiet")
	}
	if QuietMarket(five, true) || QuietMarket(five[:4], false) {
		t.Error("alert today or <5 days is not quiet")
	}
	five[4].AlertKey = "x"
	if QuietMarket(five, false) {
		t.Error("an alert within 5 days")
	}
}

func art(id, cat, title string, tickers ...string) Article {
	return Article{ID: id, Source: "CNBC Indonesia", Category: cat, Title: title, Summary: "Ringkas satu. Dua.", Tickers: tickers, PublishedAt: time.Date(2026, 10, 16, 6, 0, 0, 0, time.UTC)}
}

func TestMatchNews(t *testing.T) {
	now := time.Date(2026, 10, 17, 7, 0, 0, 0, time.UTC)
	p := base()
	hs := []Holding{{Symbol: "BBCA.JK", Name: "Bank Central Asia", Class: ClassLargeCap}}
	held := art("1", "capital_markets", "BBCA bagi dividen", "BBCA")
	other := art("2", "macro", "Rupiah menguat tipis")
	pick, _, tr := MatchNews(p, hs, []Article{other, held}, nil, now)
	if pick == nil || pick.ID != "1" || pick.Score < 70 || pick.Summary != "Ringkas satu." {
		t.Fatalf("pick %+v", pick)
	}
	if len(tr) != 2 || tr[0].Decision != "chosen" {
		t.Errorf("trace %+v", tr)
	}
	// below threshold: no holdings, generic news
	if pick, _, _ := MatchNews(p, nil, []Article{other}, nil, now); pick != nil {
		t.Errorf("generic news must not pass 70: %+v", pick)
	}
	// conservative filters high volatility
	cons := base()
	cons.Risk = Conservative
	vol := art("3", "capital_markets", "BBCA anjlok tajam", "BBCA")
	if pick, _, tr := MatchNews(cons, hs, []Article{vol}, nil, now); pick != nil || tr[0].Decision == "candidate" {
		t.Errorf("conservative should filter: %+v %+v", pick, tr)
	}
	// holder of bonds is matched by class keyword
	bond := []Holding{{Name: "ORI", Class: ClassBondGov, Manual: true}}
	b := art("4", "macro", "BI Rate ditahan di 5,75%")
	b.Source = "Bank Indonesia"
	if pick, _, _ := MatchNews(p, bond, []Article{b}, nil, now); pick == nil || pick.NumbersSafe != true {
		t.Errorf("bi rate %+v", pick)
	}
	// already sent article is skipped
	if pick, _, _ := MatchNews(p, hs, []Article{held}, []DayRecord{{NewsID: "1"}}, now); pick != nil {
		t.Error("duplicate send")
	}
	// same topic 3 days in a row picks a different topic
	h2 := art("5", "capital_markets", "BMRI bagi dividen", "BMRI")
	hs2 := append(hs, Holding{Symbol: "BMRI.JK", Name: "Mandiri", Class: ClassLargeCap})
	hist := []DayRecord{{NewsTopic: "BBCA"}, {NewsTopic: "BBCA"}}
	if pick, _, _ := MatchNews(p, hs2, []Article{held, h2}, hist, now); pick == nil || pick.Topic != "BMRI" {
		t.Errorf("variety %+v", pick)
	}
	// only the repeated topic available: keep it, mark followup
	if pick, _, _ := MatchNews(p, hs, []Article{held}, hist, now); pick == nil || !pick.Followup {
		t.Errorf("followup %+v", pick)
	}
	// tier 3 market update: numbers not safe
	t3 := art("6", "capital_markets", "BBCA naik 5%", "BBCA")
	t3.Source = "Finnhub/Benzinga"
	if pick, _, _ := MatchNews(p, hs, []Article{t3}, nil, now); pick == nil || pick.NumbersSafe {
		t.Errorf("tier 3 %+v", pick)
	}
	// MORE list
	_, more, _ := MatchNews(p, hs, []Article{held, art("7", "capital_markets", "BBCA ekspansi", "BBCA"), art("8", "capital_markets", "BBCA laba", "BBCA")}, nil, now)
	if len(more) != 2 {
		t.Errorf("more %d", len(more))
	}
}

func TestMatchMacro(t *testing.T) {
	today := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	asof := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	chg := func(v float64) *float64 { return &v }
	us := []Holding{{Symbol: "AAPL", Class: ClassGlobal, Currency: "USD"}}
	tests := []struct {
		name string
		hs   []Holding
		ind  MacroIndicator
		want bool
	}{
		{"usd_idr 2.1% foreign holder", us, MacroIndicator{Key: "usd_idr", Change: chg(2.1), AsOf: asof}, true},
		{"usd_idr exactly 2% no", us, MacroIndicator{Key: "usd_idr", Change: chg(2.0), AsOf: asof}, false},
		{"usd_idr no foreign assets", nil, MacroIndicator{Key: "usd_idr", Change: chg(5), AsOf: asof}, false},
		{"stale release", us, MacroIndicator{Key: "usd_idr", Change: chg(5), AsOf: asof.AddDate(0, 0, -2)}, false},
		{"bi rate for bond holder", []Holding{{Class: ClassBondGov}}, MacroIndicator{Key: "bi_rate", Headline: "BI Rate", AsOf: asof}, true},
		{"bi rate no fixed income", us, MacroIndicator{Key: "bi_rate", Headline: "BI Rate", AsOf: asof}, false},
		{"gdp equity holder", []Holding{{Class: ClassLargeCap}}, MacroIndicator{Key: "gdp", Headline: "PDB", AsOf: asof}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := MatchMacro(base(), tc.hs, []MacroIndicator{tc.ind}, today)
			if (m != nil) != tc.want {
				t.Errorf("got %+v", m)
			}
		})
	}
	p := base()
	p.Goal = "emergency_fund"
	if m, _ := MatchMacro(p, nil, []MacroIndicator{{Key: "inflation", Headline: "Inflasi turun", AsOf: asof}}, today); m == nil {
		t.Error("inflation for emergency fund goal")
	}
}

func TestNudge(t *testing.T) {
	p := base()
	p.Ladder, p.Health.Safety, p.EmergencyMonths, p.EmergencyTargetMax, p.Surplus = 1, 26, 0.5, 9, 2_000_000
	n, _ := MatchNudge(p, nil)
	if n.Key != "emergency_fund" || n.Amount == nil || *n.Amount != 500_000 {
		t.Errorf("%+v", n)
	}
	// same nudge twice before: change
	n2, _ := MatchNudge(p, []DayRecord{{NudgeKey: "emergency_fund"}, {NudgeKey: "emergency_fund"}})
	if n2.Key == "emergency_fund" {
		t.Errorf("variety: %+v", n2)
	}
	// wisdom below 40 and healthy safety
	q := base()
	q.Health.Wisdom = 30
	if n, _ := MatchNudge(q, nil); n.Key != "learning" {
		t.Errorf("learning %+v", n)
	}
	// investment: 13.3% of income
	r := base()
	r.Health.Growth, r.Income, r.Investable, r.Surplus = 30, 15_000_000, 2_000_000, 5_000_000
	r.Health.Wisdom = 80
	n, _ = MatchNudge(r, nil)
	if n.Key != "investment" || n.Amount == nil || *n.Amount != 1_000_000 {
		t.Errorf("investment %+v", n)
	}
	// at target: falls through to the ladder default
	r.Investable = 3_000_000
	if n, _ := MatchNudge(r, nil); n.Key == "investment" {
		t.Errorf("already at 20%%: %+v", n)
	}
	// debt at ladder 2
	d := base()
	d.Ladder, d.Installment, d.Surplus = 2, 1_000_000, 4_000_000
	d.Health = Health{Safety: 90, Growth: 90, Wisdom: 90, Freedom: 90}
	if n, _ := MatchNudge(d, nil); n.Key != "debt" || *n.Amount != 1_000_000 {
		t.Errorf("debt %+v", n)
	}
	e := base()
	e.Ladder, e.Health = 5, Health{Safety: 90, Growth: 90, Wisdom: 90, Freedom: 90}
	if n, _ := MatchNudge(e, nil); n.Key != "estate" {
		t.Errorf("ladder 5 %+v", n)
	}
}

func TestRp(t *testing.T) {
	for in, want := range map[int64]string{0: "Rp 0", 999: "Rp 999", 1000: "Rp 1.000", 1_250_000: "Rp 1.250.000", -480000: "-Rp 480.000"} {
		if got := Rp(in); got != want {
			t.Errorf("%d: %s", in, got)
		}
	}
}

func TestMatchDailyAndHorizon(t *testing.T) {
	out := MatchDaily(DailyInput{Profile: base(), Now: time.Date(2026, 10, 17, 7, 0, 0, 0, time.UTC)})
	if out.Nudge.Key == "" || out.Alert != nil || out.News != nil || out.MoreNews == nil {
		t.Errorf("%+v", out)
	}
	for in, want := range map[string]float64{"lt_1": 0.5, "1_3": 2, "gt_10": 15} {
		p := Profile{GoalHorizon: in}
		if p.HorizonYears() != want {
			t.Error(in)
		}
	}
	if (Profile{GoalHorizon: "", InvestHorizon: "3_5"}).HorizonYears() != 4 {
		t.Error("fallback to q9")
	}
}

func TestMentionsTextWithoutTickerTags(t *testing.T) {
	now := time.Date(2026, 10, 17, 7, 0, 0, 0, time.UTC)
	hs := []Holding{{Symbol: "BBCA.JK", Name: "PT Bank Central Asia Tbk", Class: ClassLargeCap}}
	for _, title := range []string{"Saham BBCA anjlok 14% hari ini", "Laba Bank Central Asia naik"} {
		a := art("1", "capital_markets", title) // IDX news has no tickers
		pick, _, tr := MatchNews(base(), hs, []Article{a}, nil, now)
		if tr[0].Parts["holding_match"] != 100 {
			t.Errorf("%q: holding match %v", title, tr[0].Parts)
		}
		_ = pick
	}
	if mentions("Saham BBCAX naik", "BBCA", "") || mentions("bank lain", "BBCA", "PT Bank Central Asia Tbk") {
		t.Error("no partial word matches")
	}
}
