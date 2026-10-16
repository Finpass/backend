package domain

import "testing"

type scenario struct {
	name    string
	in      Input
	profile string
	ladder  int
	flags   []string // codes that must be present
}

func scenarios() []scenario {
	m := func(f func(*Input)) Input { return with(f) }
	conservativeAnswers := func(in *Input) { in.Q12, in.Q13, in.Q14, in.Q15 = 0, 0, 0, 0 }
	boldAnswers := func(in *Input) { in.Q12, in.Q13, in.Q14, in.Q15 = 3, 3, 3, 3 }
	noDebt := func(in *Input) { in.Debts, in.Installment = nil, 0 }
	funded := func(in *Input) { in.Savings = 12 * in.Expenses }
	return []scenario{
		{"dinda reference", dinda(), Moderate, 1, []string{"high_interest_debt"}},
		{"wealthy aggressive investor", strongInput(func(in *Input) {
			boldAnswers(in)
			in.Investments, in.Holdings = 30*12*in.Expenses, []string{"stocks", "bonds_sbn", "property"}
		}), Aggressive, 5, nil},
		{"wealthy but only basic knowledge", strongInput(func(in *Input) {
			boldAnswers(in)
			in.Knowledge = 1
			in.Investments, in.Holdings = 30*12*in.Expenses, []string{"stocks", "bonds_sbn"}
		}), Moderate, 5, nil},
		{"wealthy intermediate fails literacy question", strongInput(func(in *Input) {
			boldAnswers(in)
			in.Knowledge, in.Q16bCorrect = 2, false
		}), Moderate, 3, nil},
		{"fresh graduate no savings", m(func(in *Input) {
			in.Age, in.Employment, in.Income, in.Expenses, in.Savings, in.Investments, in.Investable = "18_24", "contract_employee", 5_000_000, 3_500_000, 0, 0, 0
			noDebt(in)
			in.InvestHorizon, in.Holdings = "none", []string{}
			conservativeAnswers(in)
		}), Conservative, 1, nil},
		{"conservative saver but strong capacity", m(func(in *Input) { conservativeAnswers(in); funded(in); noDebt(in); in.Employment = "permanent_employee" }), Moderate, 3, nil},
		{"low capacity high willingness is capped", m(func(in *Input) { boldAnswers(in); in.Savings = 0; in.Dependents = "4_plus" }), Moderate, 1, nil},
		{"drowning in pinjol", m(func(in *Input) { in.Debts, in.Installment = []string{"pinjol", "paylater"}, 5_000_000; in.Savings = 0 }), Conservative, 1, []string{"high_interest_debt"}},
		{"funded but credit card debt", m(func(in *Input) {
			funded(in)
			in.Employment, in.Debts, in.Installment = "permanent_employee", []string{"credit_card"}, 400_000
		}), Moderate, 2, []string{"high_interest_debt"}},
		{"funded with mortgage only", m(func(in *Input) {
			funded(in)
			in.Employment, in.Debts, in.Installment = "permanent_employee", []string{"mortgage"}, 1_500_000
		}), Moderate, 3, nil},
		{"zero surplus caps ladder at 2", m(func(in *Input) {
			funded(in)
			noDebt(in)
			in.Employment, in.Expenses, in.Investable = "permanent_employee", in.Income, 0
			in.Savings = 12 * in.Expenses
		}), Moderate, 2, nil},
		{"negative surplus caps ladder at 2", m(func(in *Input) {
			noDebt(in)
			in.Employment, in.Expenses, in.Investable = "permanent_employee", in.Income+1_000_000, 0
			in.Savings = 12 * in.Expenses
		}), Moderate, 2, nil},
		{"retired needs 12 months", m(func(in *Input) {
			in.Employment, in.Age, in.Stability = "retired", "60_plus", "very_stable"
			noDebt(in)
			in.Savings = 11 * in.Expenses
		}), Moderate, 1, nil},
		{"retired with 12 months", m(func(in *Input) {
			in.Employment, in.Age, in.Stability = "retired", "60_plus", "very_stable"
			noDebt(in)
			in.Savings = 12 * in.Expenses
		}), Moderate, 3, nil},
		{"freelancer 9 months", m(func(in *Input) { noDebt(in); in.Savings = 9 * in.Expenses }), Moderate, 3, nil},
		{"freelancer 8 months", m(func(in *Input) { noDebt(in); in.Savings = 8 * in.Expenses }), Moderate, 1, nil},
		{"business owner between 9 and 12", m(func(in *Input) { in.Employment = "business_owner"; noDebt(in); in.Savings = 10 * in.Expenses }), Moderate, 3, nil},
		{"housewife with household income", m(func(in *Input) { in.Employment = "housewife"; noDebt(in); in.Savings = 6 * in.Expenses }), Moderate, 3, nil},
		{"student at the 3 month target", m(func(in *Input) { in.Employment = "student"; noDebt(in); in.Savings = 3 * in.Expenses }), Moderate, 3, nil},
		{"income opted out", m(func(in *Input) { in.IncomeKnown, in.Income = false, 0; funded(in); noDebt(in) }), Moderate, 2, []string{"provisional"}},
		{"income opted out cannot be aggressive", strongInput(func(in *Input) { boldAnswers(in); in.IncomeKnown, in.Income = false, 0 }), Moderate, 2, []string{"provisional"}},
		{"inconsistent risk answers", m(func(in *Input) { in.Q12, in.Q13 = 0, 3 }), Moderate, 1, []string{"consistency"}},
		{"permanent employee with no fixed income", m(func(in *Input) { in.Employment, in.Stability = "permanent_employee", "no_fixed_income" }), Moderate, 1, []string{"impossible_combination"}},
		{"student earning 150M", m(func(in *Input) { in.Employment, in.Income = "student", 150_000_000 }), Moderate, 1, []string{"impossible_combination"}},
		{"investing more than surplus", m(func(in *Input) { in.Investable = 9_000_000 }), Moderate, 1, []string{"impossible_combination"}},
		{"savings above a year of income", m(func(in *Input) { in.Savings = 200_000_000 }), Moderate, 2, []string{"impossible_combination"}},
		{"sharia only holder of gold", m(func(in *Input) { in.Sharia, in.Holdings = "yes", []string{"gold"} }), Moderate, 1, nil},
		{"holds nothing", m(func(in *Input) { in.Holdings = []string{}; in.Investments = 0 }), Moderate, 1, nil},
		{"single holding blocks level 4", m(func(in *Input) {
			funded(in)
			noDebt(in)
			in.Employment, in.Investments, in.Holdings = "permanent_employee", 20*in.Expenses, []string{"gold"}
		}), Moderate, 3, nil},
		{"diversified reaches level 4", m(func(in *Input) {
			funded(in)
			noDebt(in)
			in.Employment, in.Investments, in.Holdings = "permanent_employee", 20*in.Expenses, []string{"gold", "stocks"}
		}), Moderate, 4, nil},
		{"unanswered holdings hold level 3", m(func(in *Input) {
			funded(in)
			noDebt(in)
			in.Employment, in.Investments, in.Holdings = "permanent_employee", 30*12*in.Expenses, nil
		}), Moderate, 3, nil},
		{"high capacity low willingness", strongInput(func(in *Input) { conservativeAnswers(in) }), Moderate, 3, nil},
		{"low capacity low willingness", m(func(in *Input) {
			conservativeAnswers(in)
			in.Savings, in.Dependents, in.Stability = 0, "4_plus", "no_fixed_income"
		}), Conservative, 1, nil},
		{"medium capacity high willingness", m(func(in *Input) { boldAnswers(in) }), Moderate, 1, nil},
		{"older worker with dependents", m(func(in *Input) {
			in.Age, in.Dependents, in.Employment = "51_60", "3", "permanent_employee"
			funded(in)
			noDebt(in)
		}), Moderate, 3, nil},
		{"zero expenses reported", m(func(in *Input) { in.Expenses, in.Savings = 0, 1_000_000; noDebt(in) }), Moderate, 3, nil},
		{"zero income and zero everything", m(func(in *Input) {
			in.Income, in.Expenses, in.Savings, in.Investable, in.Investments = 0, 0, 0, 0, 0
			noDebt(in)
		}), Moderate, 1, nil},
	}
}

func TestScenarios(t *testing.T) {
	all := scenarios()
	if len(all) < 30 {
		t.Fatalf("need at least 30 scenarios, have %d", len(all))
	}
	for _, s := range all {
		t.Run(s.name, func(t *testing.T) {
			out, _ := Evaluate(s.in, nil)
			if s.profile != "?" && out.FinalProfile != s.profile {
				t.Errorf("profile %s, want %s (capacity %v, willingness %v)", out.FinalProfile, s.profile, out.CapacityScore, out.WillingnessScore)
			}
			if out.LadderLevel != s.ladder {
				t.Errorf("ladder %d, want %d (months %v)", out.LadderLevel, s.ladder, out.EmergencyFundMonths)
			}
			for _, f := range s.flags {
				if !hasFlag(out, f) {
					t.Errorf("missing flag %s", f)
				}
			}
		})
	}
}
