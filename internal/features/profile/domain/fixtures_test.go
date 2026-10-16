package domain

import (
	"math/rand/v2"
)

// dindaAnswers is the realistic reference user: freelancer, 8M income, 5M expenses, 7.5M savings.
func dindaAnswers() Answers {
	return Answers{
		"q1": "25_30", "q5": "0", "q10": "freelancer", "q11": "somewhat_stable",
		"q2": 8_000_000.0, "q3": 5_000_000.0, "q4": 7_500_000.0, "q6": []any{"paylater"}, "q6_installment": 500_000.0,
		"q7": 3_000_000.0, "q8": 1_000_000.0, "q9": "1_3", "q23": []any{"mutual_funds", "gold"},
		"q12": "hold", "q13": "balance", "q14": "cautious", "q15": "chance_5m", "q16": "basic", "q16b": "a",
		"q17": "home_purchase", "q18": 300_000_000.0, "q19": "5_10",
		"q20": "rent", "q21": "yes_not_active", "q22": "prefer",
	}
}

func dinda() Input { return InputFrom(dindaAnswers(), nil) }

// with returns the base input after applying f.
func with(f func(*Input)) Input {
	in := dinda()
	f(&in)
	return in
}

var (
	ages        = []string{"18_24", "25_30", "31_40", "41_50", "51_60", "60_plus"}
	deps        = []string{"0", "1", "2", "3", "4_plus"}
	employments = []string{"permanent_employee", "contract_employee", "freelancer", "business_owner", "student", "housewife", "retired"}
	stabilities = []string{"no_fixed_income", "unstable", "somewhat_stable", "very_stable"} // worst to best
	invHorizons = []string{"lt_1", "none", "1_3", "3_5", "5_10", "gt_10"}                   // by score
	goalHoriz   = []string{"lt_1", "1_3", "3_5", "5_10", "gt_10"}
	homes       = []string{"rent", "living_with_family", "own_mortgage", "own_outright"} // worst to best
	priors      = []string{"never", "yes_not_active", "yes_active"}
	goals       = []string{"emergency_fund", "home_purchase", "retirement", "children_education", "hajj_umrah", "marriage", "wealth_growth"}
	shariaOpts  = []string{"yes", "prefer", "no"}
	debtTypes   = []string{"credit_card", "paylater", "pinjol", "kta", "mortgage", "vehicle"}
	holdingOpts = []string{"stocks", "mutual_funds", "bonds_sbn", "gold", "deposits", "crypto", "property"}
)

func pick[T any](r *rand.Rand, s []T) T { return s[r.IntN(len(s))] }

func randomInput(r *rand.Rand) Input {
	in := Input{
		Age: pick(r, ages), Dependents: pick(r, deps), Employment: pick(r, employments), Stability: pick(r, stabilities),
		IncomeKnown:   r.IntN(10) != 0,
		InvestHorizon: pick(r, invHorizons), GoalHorizon: pick(r, goalHoriz),
		Q12: r.IntN(4), Q13: r.IntN(4), Q14: r.IntN(4), Q15: r.IntN(4), Knowledge: r.IntN(4), Q16bCorrect: r.IntN(2) == 0,
		Goal: pick(r, goals), Home: pick(r, homes), PriorExp: pick(r, priors), Sharia: pick(r, shariaOpts),
	}
	income := int64(1_000_000 + r.IntN(49_000_000))
	if in.IncomeKnown {
		in.Income = income
	}
	in.Expenses = int64(float64(income) * (0.2 + 1.1*r.Float64()))
	if r.IntN(10) != 0 {
		in.Savings = int64(float64(in.Expenses) * 15 * r.Float64())
	}
	if r.IntN(10) < 4 {
		in.Debts = []string{pick(r, debtTypes)}
		in.Installment = int64(float64(income) * 0.6 * r.Float64())
	}
	if r.IntN(10) < 7 {
		in.Investments = int64(float64(income) * 12 * 8 * r.Float64())
	}
	if s := income - in.Expenses - in.Installment; s > 0 {
		in.Investable = int64(float64(s) * r.Float64())
	}
	in.Holdings = []string{}
	for _, h := range holdingOpts {
		if r.IntN(4) == 0 {
			in.Holdings = append(in.Holdings, h)
		}
	}
	in.GoalAmount = int64(r.IntN(2_000_000_000))
	return in
}
