package domain

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"
)

func rng() *rand.Rand { return rand.New(rand.NewPCG(42, 7)) }

func dims(o Output) map[string]float64 {
	h := o.FinancialHealth
	return map[string]float64{
		"capacity": o.CapacityScore, "willingness": o.WillingnessScore, "security": h.FinancialSecurity, "safety": h.FinancialSafety,
		"growth": h.FinancialGrowth, "wisdom": h.FinancialWisdom, "freedom": h.FinancialFreedom, "total": h.TotalScore,
	}
}

func TestScoresAlwaysInRange(t *testing.T) {
	r := rng()
	for range 20_000 {
		out, trace := Evaluate(randomInput(r), nil)
		for name, v := range dims(out) {
			if math.IsNaN(v) || v < 0 || v > 100 {
				t.Fatalf("%s = %v out of [0,100]", name, v)
			}
		}
		if out.RiskScoreEquivalent < 0.5 || out.RiskScoreEquivalent > 10 || out.LadderLevel < 1 || out.LadderLevel > 5 || out.LadderProgress < 0 || out.LadderProgress > 100 {
			t.Fatalf("out of range: %+v", out)
		}
		if len(trace) == 0 {
			t.Fatal("empty trace")
		}
	}
}

// step returns the next better element after cur in an ordered worst-to-best list.
func step(order []string, cur string) (string, bool) {
	i := slices.Index(order, cur)
	if i < 0 || i == len(order)-1 {
		return "", false
	}
	return order[i+1], true
}

type move struct {
	name    string
	apply   func(*Input) bool
	affects []string
}

func moves() []move {
	return []move{
		{"more savings", func(in *Input) bool { in.Savings += 1 + in.Expenses/3; return true }, []string{"capacity", "safety"}},
		{"lower expenses", func(in *Input) bool { in.Expenses -= in.Expenses / 4; return in.Expenses > 0 }, []string{"capacity", "safety", "security"}},
		{"lower installment", func(in *Input) bool { in.Installment /= 2; return in.hasDebt() }, []string{"capacity", "security"}},
		{"higher income", func(in *Input) bool { in.Income += in.Income / 4; return in.IncomeKnown && in.Income > 0 }, []string{"security"}},
		{"invest more monthly", func(in *Input) bool { in.Investable += 100_000; return true }, []string{"capacity", "growth", "freedom"}},
		{"more investments", func(in *Input) bool { in.Investments += 1_000_000; return true }, []string{"growth"}},
		{"longer investing horizon", func(in *Input) bool { h, ok := step(invHorizons, in.InvestHorizon); in.InvestHorizon = h; return ok }, []string{"capacity", "growth"}},
		{"longer goal horizon", func(in *Input) bool { h, ok := step(goalHoriz, in.GoalHorizon); in.GoalHorizon = h; return ok }, []string{"capacity", "freedom"}},
		{"younger", func(in *Input) bool {
			i := slices.Index(ages, in.Age)
			if i == 0 {
				return false
			}
			in.Age = ages[i-1]
			return true
		}, []string{"capacity"}},
		{"fewer dependents", func(in *Input) bool {
			i := slices.Index(deps, in.Dependents)
			if i == 0 {
				return false
			}
			in.Dependents = deps[i-1]
			return true
		}, []string{"capacity", "freedom"}},
		{"steadier income", func(in *Input) bool { s, ok := step(stabilities, in.Stability); in.Stability = s; return ok }, []string{"capacity"}},
		{"calmer in a drop", func(in *Input) bool { in.Q12++; return in.Q12 <= 3 }, []string{"willingness", "wisdom"}},
		{"growth preference", func(in *Input) bool { in.Q13++; return in.Q13 <= 3 }, []string{"willingness", "wisdom"}},
		{"risk taker self-image", func(in *Input) bool { in.Q14++; return in.Q14 <= 3 }, []string{"willingness", "wisdom"}},
		{"takes the gamble", func(in *Input) bool { in.Q15++; return in.Q15 <= 3 }, []string{"willingness", "wisdom"}},
		{"better knowledge", func(in *Input) bool { in.Knowledge++; return in.Knowledge <= 3 }, []string{"wisdom"}},
		{"answers literacy question", func(in *Input) bool { was := in.Q16bCorrect; in.Q16bCorrect = true; return !was }, []string{"wisdom"}},
		{"more experience", func(in *Input) bool { p, ok := step(priors, in.PriorExp); in.PriorExp = p; return ok }, []string{"wisdom"}},
		{"better home", func(in *Input) bool { h, ok := step(homes, in.Home); in.Home = h; return ok }, []string{"freedom"}},
		{"specific goal", func(in *Input) bool { was := in.Goal; in.Goal = "retirement"; return was == "wealth_growth" }, []string{"freedom"}},
	}
}

func TestImprovingOneInputNeverLowersAScore(t *testing.T) {
	r := rng()
	for _, m := range moves() {
		t.Run(m.name, func(t *testing.T) {
			applied := 0
			for range 3_000 {
				before := randomInput(r)
				after := before
				after.Debts, after.Holdings = slices.Clone(before.Debts), slices.Clone(before.Holdings)
				if !m.apply(&after) {
					continue
				}
				applied++
				b, _ := Evaluate(before, nil)
				a, _ := Evaluate(after, nil)
				bd, ad := dims(b), dims(a)
				for _, name := range m.affects {
					if ad[name] < bd[name]-1e-9 {
						t.Fatalf("%s dropped %v -> %v\nbefore=%+v\nafter=%+v", name, bd[name], ad[name], before, after)
					}
				}
			}
			if applied < 100 {
				t.Fatalf("move only applied %d times, the property is not being exercised", applied)
			}
		})
	}
}

// The 10,000 answer simulation from the YAML test_harness: the five dimensions must discriminate between users.
func TestSimulationDimensionsNotDegenerate(t *testing.T) {
	r := rng()
	const n = 10_000
	series := map[string][]float64{}
	for range n {
		out, _ := Evaluate(randomInput(r), nil)
		h := out.FinancialHealth
		for name, v := range map[string]float64{"security": h.FinancialSecurity, "safety": h.FinancialSafety, "growth": h.FinancialGrowth, "wisdom": h.FinancialWisdom, "freedom": h.FinancialFreedom, "total": h.TotalScore} {
			series[name] = append(series[name], v)
		}
	}
	for name, vs := range series {
		lo, hi := slices.Min(vs), slices.Max(vs)
		mean, sd := stats(vs)
		distinct := map[float64]int{}
		mode := 0
		for _, v := range vs {
			distinct[v]++
			mode = max(mode, distinct[v])
		}
		t.Logf("%-8s min=%5.1f max=%5.1f mean=%5.1f sd=%5.1f distinct=%d modeShare=%.2f", name, lo, hi, mean, sd, len(distinct), float64(mode)/n)
		switch {
		case hi-lo < 50:
			t.Errorf("%s spans only %v..%v", name, lo, hi)
		case sd < 8:
			t.Errorf("%s standard deviation %v is too small", name, sd)
		case mean < 15 || mean > 85:
			t.Errorf("%s mean %v is pinned to an end", name, mean)
		case len(distinct) < 15:
			t.Errorf("%s has only %d distinct values", name, len(distinct))
		case float64(mode)/n > 0.4:
			t.Errorf("%s: one value holds %.0f%% of users", name, float64(mode)/n*100)
		}
	}
}

func stats(vs []float64) (mean, sd float64) {
	for _, v := range vs {
		mean += v
	}
	mean /= float64(len(vs))
	for _, v := range vs {
		sd += (v - mean) * (v - mean)
	}
	return mean, math.Sqrt(sd / float64(len(vs)))
}

func TestSimulationCoversEveryProfileAndLadderLevel(t *testing.T) {
	r := rng()
	profiles, levels := map[string]int{}, map[int]int{}
	for range 10_000 {
		out, _ := Evaluate(randomInput(r), nil)
		profiles[out.FinalProfile]++
		levels[out.LadderLevel]++
	}
	for _, p := range []string{Conservative, Moderate, Aggressive} {
		if profiles[p] == 0 {
			t.Errorf("profile %s never produced: %v", p, profiles)
		}
	}
	for l := 1; l <= 4; l++ {
		if levels[l] == 0 {
			t.Errorf("ladder level %d never produced: %v", l, levels)
		}
	}
}
