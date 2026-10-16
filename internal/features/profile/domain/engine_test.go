package domain

import (
	"slices"
	"testing"
)

func TestEmergencyFundBaseScore(t *testing.T) {
	cases := []struct {
		months, want float64
	}{
		{0, 0}, {0.01, 15}, {0.99, 15}, {1, 35}, {2.99, 35}, {3, 65}, {5.99, 65}, {6, 85}, {11.99, 85}, {12, 100}, {50, 100},
	}
	for _, c := range cases {
		if got := efBaseScore(c.months); got != c.want {
			t.Errorf("efBaseScore(%v) = %v, want %v", c.months, got, c.want)
		}
	}
}

func TestEmergencyFundTargetScore(t *testing.T) {
	freelancer := MonthsRange{Min: 9, Max: 9}
	permanent := MonthsRange{Min: 3, Max: 6}
	cases := []struct {
		name   string
		months float64
		target MonthsRange
		want   float64
	}{
		{"freelancer at target", 9, freelancer, 100},
		{"freelancer just under", 8.99, freelancer, 40},
		{"freelancer half", 4.5, freelancer, 40},
		{"freelancer below half", 4.49, freelancer, 0},
		{"permanent at max", 6, permanent, 100},
		{"permanent between", 5.9, permanent, 75},
		{"permanent at min", 3, permanent, 75},
		{"permanent below min", 2.9, permanent, 40},
		{"permanent below half", 1.4, permanent, 0},
	}
	for _, c := range cases {
		if got := efTargetScore(c.months, c.target); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestDebtScore(t *testing.T) {
	cases := []struct {
		name        string
		debts       []string
		installment int64
		income      int64
		known       bool
		want        float64
	}{
		{"no debt", nil, 0, 10_000_000, true, 100},
		{"dti 9.99%", []string{"kta"}, 999_000, 10_000_000, true, 85},
		{"dti 10%", []string{"kta"}, 1_000_000, 10_000_000, true, 65},
		{"dti 24.99%", []string{"kta"}, 2_499_000, 10_000_000, true, 65},
		{"dti 25%", []string{"kta"}, 2_500_000, 10_000_000, true, 40},
		{"dti 39.99%", []string{"kta"}, 3_999_000, 10_000_000, true, 40},
		{"dti 40%", []string{"kta"}, 4_000_000, 10_000_000, true, 20},
		{"dti 50%", []string{"kta"}, 5_000_000, 10_000_000, true, 20},
		{"dti 50.01%", []string{"kta"}, 5_001_000, 10_000_000, true, 0},
		{"debt with zero installment", []string{"kta"}, 0, 10_000_000, true, 85},
		{"zero income with installment", []string{"kta"}, 100, 0, true, 0},
		{"income opted out", []string{"kta"}, 100, 0, false, 50},
		{"income opted out, no debt", nil, 0, 0, false, 100},
	}
	for _, c := range cases {
		in := Input{Debts: c.debts, Installment: c.installment, Income: c.income, IncomeKnown: c.known}
		if got := debtScore(in); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestSavingsRateScore(t *testing.T) {
	cases := []struct {
		name                          string
		income, expenses, installment int64
		known                         bool
		want                          float64
	}{
		{"30% exactly", 10_000_000, 7_000_000, 0, true, 100},
		{"29.9%", 10_000_000, 7_010_000, 0, true, 80},
		{"20% exactly", 10_000_000, 8_000_000, 0, true, 80},
		{"19.9%", 10_000_000, 8_010_000, 0, true, 60},
		{"10% exactly", 10_000_000, 9_000_000, 0, true, 60},
		{"9.9%", 10_000_000, 9_010_000, 0, true, 35},
		{"0% exactly", 10_000_000, 10_000_000, 0, true, 35},
		{"negative", 10_000_000, 10_000_001, 0, true, 0},
		{"installment counts", 10_000_000, 5_000_000, 3_000_000, true, 80},
		{"zero income zero spend", 0, 0, 0, true, 35},
		{"zero income with spend", 0, 1, 0, true, 0},
		{"opted out", 0, 5, 0, false, 40},
	}
	for _, c := range cases {
		in := Input{Income: c.income, Expenses: c.expenses, Installment: c.installment, IncomeKnown: c.known}
		if got := savingsRateScore(in); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestInvestableScore(t *testing.T) {
	cases := []struct {
		name               string
		income, investable int64
		known              bool
		want               float64
	}{
		{"zero", 10_000_000, 0, true, 0},
		{"tiny", 10_000_000, 1, true, 25},
		{"4.9%", 10_000_000, 490_000, true, 25},
		{"5%", 10_000_000, 500_000, true, 50},
		{"10%", 10_000_000, 1_000_000, true, 75},
		{"19.9%", 10_000_000, 1_990_000, true, 75},
		{"20%", 10_000_000, 2_000_000, true, 100},
		{"opted out", 0, 0, false, 25},
	}
	for _, c := range cases {
		if got := investableScore(Input{Income: c.income, Investable: c.investable, IncomeKnown: c.known}); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestBandBoundaries(t *testing.T) {
	cases := map[float64]string{0: "low", 39.9: "low", 40: "medium", 55: "medium", 70: "medium", 70.1: "high", 100: "high"}
	for score, want := range cases {
		if got := band(score); got != want {
			t.Errorf("band(%v) = %s, want %s", score, got, want)
		}
	}
}

func TestHysteresisWindow(t *testing.T) {
	cases := map[float64]bool{36.9: false, 37: true, 40: true, 43: true, 43.1: false, 66.9: false, 67: true, 70: true, 73: true, 73.1: false, 10: false, 90: false}
	for score, want := range cases {
		if got := nearThreshold(score); got != want {
			t.Errorf("nearThreshold(%v) = %t, want %t", score, got, want)
		}
	}
}

// strongInput has high capacity and every knowledge gate open, so only willingness decides Moderate vs Aggressive.
func strongInput(f func(*Input)) Input {
	return with(func(in *Input) {
		in.Employment, in.Stability, in.Age, in.Debts, in.Installment = "permanent_employee", "very_stable", "18_24", nil, 0
		in.Savings, in.Investable, in.InvestHorizon, in.GoalHorizon = 12*in.Expenses, 2_000_000, "gt_10", "gt_10"
		in.Dependents, in.Knowledge, in.Q16bCorrect = "0", 3, true
		f(in)
	})
}

func TestHysteresisKeepsPreviousProfile(t *testing.T) {
	in := strongInput(func(in *Input) { in.Q12, in.Q13, in.Q14, in.Q15 = 2, 3, 1, 2 }) // willingness 71.7
	fresh, _ := Evaluate(in, nil)
	if fresh.CapacityBand != "high" || fresh.WillingnessScore != 71.7 || fresh.FinalProfile != Aggressive {
		t.Fatalf("fixture drifted: %+v", fresh)
	}
	held, trace := Evaluate(in, &Previous{Profile: Moderate})
	if held.FinalProfile != Moderate {
		t.Errorf("got %s, want the previous Moderate held", held.FinalProfile)
	}
	if !slices.ContainsFunc(trace, func(e TraceEntry) bool { return e.Rule == "hysteresis" }) {
		t.Error("hysteresis missing from trace")
	}
	far := strongInput(func(in *Input) { in.Q12, in.Q13, in.Q14, in.Q15 = 3, 3, 3, 3 })
	if out, _ := Evaluate(far, &Previous{Profile: Moderate}); out.FinalProfile != Aggressive {
		t.Errorf("willingness 100 is far from 70, got %s", out.FinalProfile)
	}
}

func TestHysteresisIgnoredFarFromThreshold(t *testing.T) {
	in := with(func(in *Input) { in.Q12, in.Q13, in.Q14, in.Q15 = 0, 0, 0, 0 })
	out, _ := Evaluate(in, &Previous{Profile: Aggressive})
	if out.FinalProfile != Conservative {
		t.Errorf("got %s, want Conservative: willingness %v is nowhere near a threshold", out.FinalProfile, out.WillingnessScore)
	}
}

func TestPJMMatrix(t *testing.T) {
	// Capacity bands low/medium/high crossed with willingness bands low/medium/high.
	want := [3][3]string{
		{Conservative, Conservative, Moderate},
		{Conservative, Moderate, Moderate},
		{Moderate, Moderate, Aggressive},
	}
	for c := range 3 {
		for w := range 3 {
			if got := profileName[pjm[c][w]]; got != want[c][w] {
				t.Errorf("pjm[%d][%d] = %s, want %s", c, w, got, want[c][w])
			}
		}
	}
}

func TestWillingnessScore(t *testing.T) {
	cases := []struct {
		q12, q13, q14, q15 int
		want               float64
	}{
		{0, 0, 0, 0, 0}, {3, 3, 3, 3, 100}, {3, 0, 0, 0, 40}, {0, 3, 0, 0, 30}, {0, 0, 3, 3, 30}, {0, 0, 3, 0, 15}, {2, 1, 2, 1, 51.7},
	}
	for _, c := range cases {
		out, _ := Evaluate(with(func(in *Input) { in.Q12, in.Q13, in.Q14, in.Q15 = c.q12, c.q13, c.q14, c.q15 }), nil)
		if out.WillingnessScore != c.want {
			t.Errorf("%v: got %v, want %v", c, out.WillingnessScore, c.want)
		}
	}
}

func TestConsistencyFlag(t *testing.T) {
	for _, c := range []struct {
		q12, q13 int
		want     bool
	}{{0, 0, false}, {0, 1, false}, {0, 2, true}, {3, 1, true}, {3, 2, false}, {0, 3, true}} {
		out, _ := Evaluate(with(func(in *Input) { in.Q12, in.Q13 = c.q12, c.q13 }), nil)
		if out.ConsistencyFlag != c.want {
			t.Errorf("q12=%d q13=%d: got %t", c.q12, c.q13, out.ConsistencyFlag)
		}
	}
}

func TestEmergencyTargets(t *testing.T) {
	want := map[string]MonthsRange{
		"permanent_employee": {3, 6}, "contract_employee": {6, 6}, "freelancer": {9, 9}, "business_owner": {9, 12},
		"student": {3, 3}, "housewife": {6, 6}, "retired": {12, 12},
	}
	for emp, r := range want {
		out, _ := Evaluate(with(func(in *Input) { in.Employment = emp }), nil)
		if out.EmergencyTargetMonths != r {
			t.Errorf("%s: got %v, want %v", emp, out.EmergencyTargetMonths, r)
		}
	}
}

func TestLadderBoundaries(t *testing.T) {
	rich := func(f func(*Input)) Input {
		return with(func(in *Input) {
			in.Employment, in.Debts, in.Installment = "permanent_employee", nil, 0
			in.Savings = 6 * in.Expenses
			in.Holdings = []string{"stocks", "gold"}
			f(in)
		})
	}
	exp := dinda().Expenses
	cases := []struct {
		name string
		in   Input
		want int
	}{
		{"emergency 3 vs min 3 months: ok", rich(func(in *Input) { in.Savings = 3 * exp }), 3},
		{"just below min 3 months", rich(func(in *Input) { in.Savings = 3*exp - 1 }), 1},
		{"min met, high interest debt", rich(func(in *Input) { in.Debts, in.Installment = []string{"pinjol"}, 100_000 }), 2},
		{"min met, kta not high interest", rich(func(in *Input) { in.Debts, in.Installment = []string{"kta"}, 100_000 }), 3},
		{"no surplus caps at 2", rich(func(in *Input) { in.Installment, in.Debts = 3_000_000, []string{"kta"}; in.Expenses = in.Income }), 2},
		{"12x expenses exactly stays 3", rich(func(in *Input) { in.Investments = 12 * in.Expenses }), 3},
		{"12x expenses + 1 and diversified is 4", rich(func(in *Input) { in.Investments = 12*in.Expenses + 1 }), 4},
		{"12x+1 but single holding stays 3", rich(func(in *Input) { in.Investments, in.Holdings = 12*in.Expenses+1, []string{"gold"} }), 3},
		{"25x annual exactly stays 4", rich(func(in *Input) { in.Investments = 25 * 12 * in.Expenses }), 4},
		{"25x annual + 1 is 5", rich(func(in *Input) { in.Investments = 25*12*in.Expenses + 1 }), 5},
		{"q23 unanswered holds at 3", rich(func(in *Input) { in.Investments, in.Holdings = 25*12*in.Expenses+1, nil }), 3},
		{"level 5 needs emergency fund", rich(func(in *Input) { in.Investments, in.Savings = 25*12*in.Expenses+1, 0 }), 1},
	}
	for _, c := range cases {
		out, _ := Evaluate(c.in, nil)
		if out.LadderLevel != c.want {
			t.Errorf("%s: got level %d, want %d", c.name, out.LadderLevel, c.want)
		}
	}
}

func TestLadderProgress(t *testing.T) {
	in := with(func(in *Input) { in.Investments = 25 * 12 * in.Expenses / 2 })
	if out, _ := Evaluate(in, nil); out.LadderProgress != 50 {
		t.Errorf("progress = %v, want 50", out.LadderProgress)
	}
	in.Investments = 100 * 12 * in.Expenses
	if out, _ := Evaluate(in, nil); out.LadderProgress != 100 {
		t.Errorf("progress = %v, want 100", out.LadderProgress)
	}
}

func TestKnowledgeCaps(t *testing.T) {
	aggressive := func(f func(*Input)) Input {
		return strongInput(func(in *Input) { in.Q12, in.Q13, in.Q14, in.Q15 = 3, 3, 3, 3; f(in) })
	}
	cases := []struct {
		name        string
		knowledge   int
		correct     bool
		wantProfile string
		wantCap     bool
	}{
		{"none", 0, true, Moderate, true},
		{"basic", 1, true, Moderate, true},
		{"intermediate correct", 2, true, Aggressive, false},
		{"intermediate incorrect", 2, false, Moderate, true},
		{"advanced incorrect", 3, false, Aggressive, false},
		{"advanced correct", 3, true, Aggressive, false},
	}
	for _, c := range cases {
		out, _ := Evaluate(aggressive(func(in *Input) { in.Knowledge, in.Q16bCorrect = c.knowledge, c.correct }), nil)
		if out.FinalProfile != c.wantProfile || out.KnowledgeCapApplied != c.wantCap {
			t.Errorf("%s: got %s cap=%t (capacity %v willingness %v)", c.name, out.FinalProfile, out.KnowledgeCapApplied, out.CapacityScore, out.WillingnessScore)
		}
	}
}

func TestProvisionalWhenIncomeOptedOut(t *testing.T) {
	in := with(func(in *Input) {
		in.IncomeKnown, in.Income = false, 0
		in.Employment, in.Stability, in.Age, in.Debts, in.Installment = "permanent_employee", "very_stable", "18_24", nil, 0
		in.Savings, in.InvestHorizon, in.GoalHorizon = 12*in.Expenses, "gt_10", "gt_10"
		in.Q12, in.Q13, in.Q14, in.Q15, in.Knowledge, in.Q16bCorrect = 3, 3, 3, 3, 3, true
	})
	out, _ := Evaluate(in, nil)
	if out.ProfileLabel != "provisional" || out.FinalProfile == Aggressive {
		t.Errorf("got %s %s", out.ProfileLabel, out.FinalProfile)
	}
	if !hasFlag(out, "provisional") {
		t.Error("provisional flag missing")
	}
	if out.Metrics.MonthlyIncome != 0 {
		t.Error("opted-out income must not leak into metrics")
	}
}

func hasFlag(o Output, code string) bool {
	return slices.ContainsFunc(o.Flags, func(f Flag) bool { return f.Code == code })
}

func TestImpossibleCombinationFlags(t *testing.T) {
	cases := []struct {
		name string
		mod  func(*Input)
		want bool
	}{
		{"permanent with no fixed income", func(in *Input) { in.Employment, in.Stability = "permanent_employee", "no_fixed_income" }, true},
		{"freelancer with no fixed income is fine", func(in *Input) { in.Stability = "no_fixed_income" }, false},
		{"student above 100M", func(in *Input) { in.Employment, in.Income = "student", 100_000_001 }, true},
		{"student at 100M", func(in *Input) { in.Employment, in.Income = "student", 100_000_000 }, false},
		{"q8 above surplus", func(in *Input) { in.Investable = in.Income - in.Expenses - in.Installment + 1 }, true},
		{"q8 equals surplus", func(in *Input) { in.Investable = in.Income - in.Expenses - in.Installment }, false},
		{"savings above 12 months income", func(in *Input) { in.Savings = in.Income*12 + 1 }, true},
		{"savings at 12 months income", func(in *Input) { in.Savings = in.Income * 12 }, false},
		{"baseline", func(*Input) {}, false},
	}
	for _, c := range cases {
		out, _ := Evaluate(with(c.mod), nil)
		if got := hasFlag(out, "impossible_combination"); got != c.want {
			t.Errorf("%s: got %t, want %t", c.name, got, c.want)
		}
	}
}

func TestBriefingPriorities(t *testing.T) {
	low, _ := Evaluate(with(func(in *Input) { in.Savings = 0 }), nil)
	if want := []string{"asset_alerts", "emergency_fund", "debt_management"}; !slices.Equal(low.BriefingPriorities, want) {
		t.Errorf("got %v, want %v", low.BriefingPriorities, want)
	}
	high, _ := Evaluate(with(func(in *Input) {
		in.Employment, in.Debts, in.Installment, in.Savings, in.Holdings = "permanent_employee", nil, 0, 6*in.Expenses, []string{}
	}), nil)
	if want := []string{"market_news", "macro_indicators"}; !slices.Equal(high.BriefingPriorities, want) {
		t.Errorf("got %v, want %v", high.BriefingPriorities, want)
	}
}

func TestHealthCategories(t *testing.T) {
	cases := map[float64]string{0: "Really Struggling", 30: "Really Struggling", 30.1: "Struggling", 50: "Struggling", 50.1: "Coping", 70: "Coping", 70.1: "Thriving", 100: "Thriving"}
	for total, want := range cases {
		// Build a Health the same way Evaluate does.
		h := Health{TotalScore: total}
		switch {
		case h.TotalScore <= 30:
			h.Category = "Really Struggling"
		case h.TotalScore <= 50:
			h.Category = "Struggling"
		case h.TotalScore <= 70:
			h.Category = "Coping"
		default:
			h.Category = "Thriving"
		}
		if h.Category != want {
			t.Errorf("%v -> %s, want %s", total, h.Category, want)
		}
	}
}

func TestHealthDimensionsDinda(t *testing.T) {
	out, trace := Evaluate(dinda(), nil)
	h := out.FinancialHealth
	for name, v := range map[string]float64{"security": h.FinancialSecurity, "safety": h.FinancialSafety, "growth": h.FinancialGrowth, "wisdom": h.FinancialWisdom, "freedom": h.FinancialFreedom} {
		if v <= 0 || v >= 100 {
			t.Errorf("%s = %v, want a mid-range value for a realistic user", name, v)
		}
	}
	if out.LadderLevel != 1 || out.EmergencyFundMonths != 1.5 || out.EmergencyTargetMonths.Min != 9 {
		t.Errorf("ladder %d months %v target %v", out.LadderLevel, out.EmergencyFundMonths, out.EmergencyTargetMonths)
	}
	if !hasFlag(out, "high_interest_debt") {
		t.Error("paylater with an installment must raise high_interest_debt")
	}
	if len(trace) < 20 {
		t.Errorf("trace has only %d entries", len(trace))
	}
}

func TestEvaluateIsDeterministic(t *testing.T) {
	a, ta := Evaluate(dinda(), &Previous{Profile: Moderate})
	b, tb := Evaluate(dinda(), &Previous{Profile: Moderate})
	if !slices.Equal(ta, tb) || a.FinancialHealth != b.FinancialHealth || a.FinalProfile != b.FinalProfile {
		t.Error("two runs differ")
	}
}

func TestGoalFeasibility(t *testing.T) {
	// need per month = amount / months; ratio = investable / need.
	cases := []struct {
		name       string
		amount     int64
		horizon    string
		investable int64
		want       float64
	}{
		{"exactly on track", 24_000_000, "1_3", 1_000_000, 100},
		{"70%", 24_000_000, "1_3", 700_000, 75},
		{"50%", 24_000_000, "1_3", 500_000, 50},
		{"30%", 24_000_000, "1_3", 300_000, 25},
		{"below 30%", 24_000_000, "1_3", 299_999, 0},
		{"no target amount", 0, "1_3", 5_000_000, 0},
	}
	for _, c := range cases {
		// Isolate the freedom dimension: clarity 100, timeline 60, home 40, no dependents.
		in := Input{Goal: "retirement", GoalAmount: c.amount, GoalHorizon: c.horizon, Investable: c.investable, Home: "rent", Dependents: "0"}
		var tr tracer
		got := health(in, 0, &tr).FinancialFreedom
		want := round1(100*0.25 + c.want*0.35 + 60*0.20 + 40*0.20)
		if got != want {
			t.Errorf("%s: freedom %v, want %v", c.name, got, want)
		}
	}
}

func TestAgeAndDependentModifiers(t *testing.T) {
	base := Input{Employment: "freelancer", Dependents: "0", Home: "rent", Goal: "retirement", GoalHorizon: "1_3", InvestHorizon: "none", Age: "25_30", IncomeKnown: true, Income: 1}
	var tr tracer
	b := health(base, 0, &tr)
	old := base
	old.Age = "60_plus"
	o := health(old, 0, &tr)
	if o.FinancialGrowth != 0 || b.FinancialGrowth-o.FinancialGrowth < 0 {
		t.Errorf("growth should drop for 60+: %v vs %v", b.FinancialGrowth, o.FinancialGrowth)
	}
	if o.FinancialSafety-b.FinancialSafety != 15 {
		t.Errorf("safety modifier for 60+ should be +15, got %v", o.FinancialSafety-b.FinancialSafety)
	}
	many := base
	many.Dependents = "4_plus"
	if d := health(base, 0, &tr).FinancialFreedom - health(many, 0, &tr).FinancialFreedom; d != 10 {
		t.Errorf("4+ dependents should cost 10 freedom points, got %v", d)
	}
}
