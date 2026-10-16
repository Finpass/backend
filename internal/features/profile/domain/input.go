package domain

import "slices"

// Input is the typed view of validated answers that the rule engine consumes.
type Input struct {
	Age, Dependents, Employment, Stability string

	Income      int64
	IncomeKnown bool // false when the user opted out of q2
	Expenses    int64
	Savings     int64
	Debts       []string
	Installment int64

	Investments    int64
	Investable     int64
	InvestHorizon  string
	Holdings       []string // nil means q23 was not answered
	Q12, Q13, Q14  int
	Q15            int
	Knowledge      int // q16 score 0..3
	Q16bCorrect    bool
	Goal           string
	GoalAmount     int64
	GoalHorizon    string
	Home, PriorExp string
	Sharia         string
}

// InputFrom assumes the answers passed Missing/ValidateAnswers.
func InputFrom(a Answers, optOut []string) Input {
	in := Input{
		Age: a.str("q1"), Dependents: a.str("q5"), Employment: a.str("q10"), Stability: a.str("q11"),
		Expenses: n(a, "q3"), Savings: n(a, "q4"),
		InvestHorizon: a.str("q9"), Investable: n(a, "q8"),
		Q12: optionIndex("q12", a.str("q12")), Q13: optionIndex("q13", a.str("q13")),
		Q14: optionIndex("q14", a.str("q14")), Q15: optionIndex("q15", a.str("q15")),
		Knowledge: optionIndex("q16", a.str("q16")), Q16bCorrect: a.str("q16b") == "a",
		Goal: a.str("q17"), GoalAmount: n(a, "q18"), GoalHorizon: a.str("q19"),
		Home: a.str("q20"), PriorExp: a.str("q21"), Sharia: a.str("q22"),
	}
	if !slices.Contains(optOut, "q2") {
		in.Income, in.IncomeKnown = n(a, "q2"), true
	}
	if !slices.Contains(optOut, "q7") {
		in.Investments = n(a, "q7")
	}
	if hasDebt(a) {
		in.Debts, _ = a.list("q6")
		in.Installment = n(a, "q6_installment")
	}
	if h, ok := a.list("q23"); ok {
		in.Holdings = slices.DeleteFunc(h, func(s string) bool { return s == "none" })
		if in.Holdings == nil {
			in.Holdings = []string{}
		}
	}
	return in
}

func n(a Answers, k string) int64 { v, _ := a.num(k); return v }
