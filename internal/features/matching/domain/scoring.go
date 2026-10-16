package domain

import (
	"math"
	"sort"
)

// Weights of the layer 3 formula.
const (
	wRisk, wReturn, wCost, wLiq, wFund, wGoal, wMacro = 0.25, 0.20, 0.15, 0.15, 0.15, 0.05, 0.05
)

type ScoreTrace struct {
	Total      float64            `json:"total"`
	Components map[string]float64 `json:"components"`
	Notes      []string           `json:"notes,omitempty"`
}

func normalize(v, lo, hi float64) float64 { return math.Min(1, math.Max(0, (v-lo)/(hi-lo))) }

func sharpeScore(s float64) float64 {
	switch {
	case s >= 2:
		return 100
	case s >= 1.5:
		return 85
	case s >= 1:
		return 70
	case s >= 0.5:
		return 50
	case s >= 0:
		return 30
	}
	return 0
}

func costScore(er float64) float64 {
	switch {
	case er <= 0.5:
		return 100
	case er <= 1:
		return 85
	case er <= 2:
		return 70
	case er <= 3:
		return 50
	}
	return 30
}

func liquidityScore(l string) float64 {
	switch l {
	case "high":
		return 100
	case "medium":
		return 70
	}
	return 40
}

// Score implements layer 3. riskFree is a percent (annual).
func Score(p Profile, c Candidate, riskFree float64, m MacroSignals) ScoreTrace {
	t := ScoreTrace{Components: map[string]float64{}}
	note := func(s string) { t.Notes = append(t.Notes, s) }

	risk := 50.0
	if c.Vol > 0 {
		sh := (c.Return1Y - riskFree) / c.Vol
		risk = sharpeScore(sh)
		note("sharpe=" + f1(sh))
	} else {
		note("risk_fit neutral 50: volatility unavailable")
	}
	ret := (normalize(c.Return1Y, -20, 50)*100)*0.6 + (float64(c.PositiveMonths)/12*100)*0.4

	cost := 70.0
	if c.ExpenseRatio != nil {
		cost = costScore(*c.ExpenseRatio)
	} else {
		note("cost_efficiency assumed 70: no expense ratio in data")
	}

	fund, n := 0.0, 0
	if c.PER != nil && *c.PER > 0 {
		fund += 100 - normalize(*c.PER, 5, 30)*100
		n++
	}
	if c.PBV != nil && *c.PBV > 0 {
		fund += 100 - normalize(*c.PBV, 0.5, 5)*100
		n++
	}
	if c.ROE != nil {
		fund += normalize(*c.ROE, 0, 30) * 100
		n++
	}
	if n == 0 {
		fund = 50
		note("fundamentals neutral 50: no financial statements")
	} else {
		fund /= float64(n)
	}

	goal := 50.0
	switch {
	case p.Goal == "wealth_growth" && c.Beta != nil && *c.Beta > 1:
		goal = 100
	case p.Goal == "retirement" && c.DivYield != nil && *c.DivYield > 3:
		goal = 100
	}

	macro := 50.0
	switch {
	case m.BIRateUp && (c.Class == ClassBondGov || c.Class == ClassSukuk):
		macro = 80
	case m.InflationUp && c.Class == ClassGold:
		macro = 90
	case m.RupiahWeak && c.Geography == "global":
		macro = 85
	}

	t.Components = map[string]float64{
		"risk_fit": risk, "return_quality": round1(ret), "cost_efficiency": cost, "liquidity": liquidityScore(c.Liquidity),
		"fundamentals": round1(fund), "goal_alignment": goal, "macro_fit": macro,
	}
	t.Total = round1(risk*wRisk + ret*wReturn + cost*wCost + liquidityScore(c.Liquidity)*wLiq + fund*wFund + goal*wGoal + macro*wMacro)
	return t
}

func f1(v float64) string { return trimFloat(round1(v)) }

func trimFloat(v float64) string {
	return fmtFloat(v)
}

type scored struct {
	C Candidate
	S ScoreTrace
}

func rank(in []scored, held map[string]bool) {
	sort.SliceStable(in, func(i, j int) bool {
		if in[i].S.Total != in[j].S.Total {
			return in[i].S.Total > in[j].S.Total
		}
		if held[in[i].C.Symbol] != held[in[j].C.Symbol] { // holdings priority on ties
			return held[in[i].C.Symbol]
		}
		return in[i].C.Symbol < in[j].C.Symbol
	})
}
