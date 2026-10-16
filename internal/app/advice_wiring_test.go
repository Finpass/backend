package app

import (
	"testing"

	profiledomain "shenzen/backend/internal/features/profile/domain"
)

func TestToMatchProfile(t *testing.T) {
	a := profiledomain.Assessment{}
	a.Output.FinalProfile, a.Output.LadderLevel = "Aggressive", 4
	a.Output.Gates.Sharia = "yes"
	a.Output.Metrics.MonthlyIncome = 15_000_000
	a.Output.EmergencyTargetMonths.Max = 9
	a.Output.FinancialHealth.FinancialSafety = 26
	a.Output.BriefingPriorities = []string{"debt_management"}
	a.Input.Answers = profiledomain.Answers{"q5": "4_plus", "q8": float64(2_000_000), "q10": "freelancer", "q16": "basic", "q17": "retirement", "q19": "3_5", "q23": []any{"stocks", "gold"}, "q18": float64(500_000_000)}
	p := toMatchProfile("u1", "Dewi", a)
	if p.Risk != "Aggressive" || p.Ladder != 4 || p.Sharia != "yes" || p.Dependents != 4 || p.Investable != 2_000_000 || p.Employment != "freelancer" ||
		p.Goal != "retirement" || p.GoalHorizon != "3_5" || len(p.HeldKinds) != 2 || p.GoalAmount != 500_000_000 || p.Health.Safety != 26 || p.EmergencyTargetMax != 9 || p.Name != "Dewi" || p.Priorities[0] != "debt_management" {
		t.Errorf("%+v", p)
	}
	if firstWord("  Dinda Ayu ") != "Dinda" || firstWord("") != "" {
		t.Error("firstWord")
	}
}
