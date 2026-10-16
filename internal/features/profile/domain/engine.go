package domain

import (
	"fmt"
	"math"
	"slices"
)

const (
	Conservative = "Conservative"
	Moderate     = "Moderate"
	Aggressive   = "Aggressive"

	studentIncomeLimit = 100_000_000 // IDR per month
)

// Previous is the last assessment's outcome, used for hysteresis.
type Previous struct{ Profile string }

type tracer []TraceEntry

func (t *tracer) add(step, rule string, v float64, format string, args ...any) {
	*t = append(*t, TraceEntry{Step: step, Rule: rule, Value: round1(v), Detail: fmt.Sprintf(format, args...)})
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
func round4(v float64) float64 { return math.Round(v*10000) / 10000 }
func clamp(v float64) float64  { return math.Min(100, math.Max(0, v)) }
func norm(score int) float64   { return float64(score) / 3 * 100 }

func band(score float64) string {
	switch {
	case score < 40:
		return "low"
	case score <= 70:
		return "medium"
	}
	return "high"
}

func (in Input) hasDebt() bool { return len(in.Debts) > 0 }

func (in Input) highInterestDebt() bool {
	// The questionnaire has no outstanding balance, so any installment on a high-interest product counts as material.
	return in.Installment > 0 && slices.ContainsFunc(in.Debts, func(d string) bool {
		return d == "credit_card" || d == "paylater" || d == "pinjol"
	})
}

func (in Input) months() float64 {
	if in.Expenses == 0 {
		if in.Savings > 0 {
			return 120
		}
		return 0
	}
	return float64(in.Savings) / float64(in.Expenses)
}

func (in Input) surplus() int64 { return in.Income - in.Expenses - in.Installment }

func (in Input) savingsRate() (float64, bool) {
	if !in.IncomeKnown {
		return 0, false
	}
	if in.Income == 0 {
		if in.surplus() < 0 {
			return -1, true
		}
		return 0, true
	}
	return float64(in.surplus()) / float64(in.Income), true
}

func (in Input) dti() (float64, bool) {
	switch {
	case !in.hasDebt():
		return 0, true
	case !in.IncomeKnown:
		return 0, false
	case in.Income == 0:
		return 10, true
	}
	return float64(in.Installment) / float64(in.Income), true
}

func (in Input) investableRatio() (float64, bool) {
	if !in.IncomeKnown {
		return 0, false
	}
	if in.Income == 0 {
		return 0, true
	}
	return float64(in.Investable) / float64(in.Income), true
}

// Component scores shared by capacity (Step 1) and the health dimensions (Step 7).

func savingsRateScore(in Input) float64 {
	r, ok := in.savingsRate()
	if !ok {
		return 40
	}
	return lookup(r, savingsRateTiers, 0)
}

func debtScore(in Input) float64 {
	if !in.hasDebt() {
		return 100
	}
	d, ok := in.dti()
	switch {
	case !ok:
		return 50
	case d < 0.10:
		return 85
	case d < 0.25:
		return 65
	case d < 0.40:
		return 40
	case d <= 0.50:
		return 20
	}
	return 0
}

func investableScore(in Input) float64 {
	r, ok := in.investableRatio()
	if !ok {
		return 25
	}
	if r > 0 {
		return lookup(r, investableTiers, 25)
	}
	return 0
}

func efBaseScore(m float64) float64 {
	if s := lookup(m, efBaseTiers, 0); s > 0 {
		return s
	}
	if m > 0 {
		return 15
	}
	return 0
}

func efTargetScore(m float64, t MonthsRange) float64 {
	switch {
	case m >= t.Max:
		return 100
	case m >= t.Min:
		return 75
	case m >= t.Min/2:
		return 40
	}
	return 0
}

// Evaluate runs Steps 1-7. It is pure: the same input and previous outcome always give the same output and trace.
func Evaluate(in Input, prev *Previous) (Output, []TraceEntry) {
	var tr tracer
	out := Output{Flags: []Flag{}, BriefingPriorities: []string{}}

	months := in.months()
	target := capacityTarget[in.Employment]

	// Step 1: capacity.
	efBase, efTgt := efBaseScore(months), efTargetScore(months, target)
	ef := (efBase + efTgt) / 2
	tr.add("1", "emergency_fund", ef, "months=%.2f base=%.0f target=%.0f-%.0f vs_target=%.0f", months, efBase, target.Min, target.Max, efTgt)
	debt := debtScore(in)
	tr.add("1", "debt", debt, "has_debt=%t", in.hasDebt())
	stab := clamp(employmentBase[in.Employment] + stabilityMod[in.Stability])
	tr.add("1", "employment_stability", stab, "employment=%s income_stability=%s", in.Employment, in.Stability)
	sav := savingsRateScore(in)
	tr.add("1", "savings_rate", sav, "income_known=%t", in.IncomeKnown)
	hor := math.Min(horizonScore[in.InvestHorizon], horizonScore[in.GoalHorizon])
	tr.add("1", "horizon", hor, "q9=%s q19=%s (shorter wins)", in.InvestHorizon, in.GoalHorizon)
	age, dep := ageCapacity[in.Age], depCapacity[in.Dependents]
	tr.add("1", "age", age, "q1=%s", in.Age)
	tr.add("1", "dependents", dep, "q5=%s", in.Dependents)
	inv := investableScore(in)
	tr.add("1", "investable", inv, "income_known=%t", in.IncomeKnown)
	capacity := round1(clamp(ef*0.25 + debt*0.20 + stab*0.15 + sav*0.15 + hor*0.10 + age*0.05 + dep*0.05 + inv*0.05))
	tr.add("1", "capacity_score", capacity, "weighted sum, clamped 0-100")

	// Step 2: willingness.
	willingness := round1(clamp(norm(in.Q12)*0.40 + norm(in.Q13)*0.30 + (norm(in.Q14)+norm(in.Q15))/2*0.30))
	tr.add("2", "willingness_score", willingness, "q12=%d q13=%d q14=%d q15=%d", in.Q12, in.Q13, in.Q14, in.Q15)

	// Step 3: PJM 3x3, hysteresis, caps.
	out.CapacityScore, out.WillingnessScore = capacity, willingness
	out.CapacityBand, out.WillingnessBand = band(capacity), band(willingness)
	profile := pjm[bandIndex(out.CapacityBand)][bandIndex(out.WillingnessBand)]
	tr.add("3", "pjm_matrix", float64(profile), "capacity=%s willingness=%s -> %s", out.CapacityBand, out.WillingnessBand, profileName[profile])
	if prev != nil {
		if p, ok := profileIndex(prev.Profile); ok && p != profile && (nearThreshold(capacity) || nearThreshold(willingness)) {
			tr.add("3", "hysteresis", float64(p), "score within 3 of a band threshold, keep previous profile %s", prev.Profile)
			profile = p
		}
	}
	provisional := !in.IncomeKnown
	knowledgeCap := in.Knowledge <= 1 || (in.Knowledge == 2 && !in.Q16bCorrect)
	if profile == 2 && knowledgeCap {
		profile, out.KnowledgeCapApplied = 1, true
		tr.add("3", "knowledge_cap", 1, "q16=%d q16b_correct=%t capped at Moderate", in.Knowledge, in.Q16bCorrect)
	}
	if profile == 2 && provisional {
		profile = 1
		tr.add("3", "provisional_cap", 1, "income not shared, capped at Moderate")
	}
	out.FinalProfile = profileName[profile]
	out.ProfileLabel = "final"
	if provisional {
		out.ProfileLabel = "provisional"
	}
	out.ConsistencyFlag = abs(in.Q12-in.Q13) >= 2
	tr.add("3", "consistency_check", b2f(out.ConsistencyFlag), "|q12-q13|=%d", abs(in.Q12-in.Q13))
	effective := willingness
	switch profile {
	case 0:
		effective = math.Min(effective, 40)
	case 1:
		effective = math.Min(effective, 70)
	}
	out.RiskScoreEquivalent = round1(0.5 + effective/100*9.5)
	tr.add("3", "risk_score_equivalent", out.RiskScoreEquivalent, "effective_willingness=%.1f", effective)

	// Step 4: ladder.
	hi := in.highInterestDebt()
	surplusPositive := in.IncomeKnown && in.surplus() > 0
	annual := float64(in.Expenses) * 12
	level := 1
	switch {
	case months < target.Min:
	case hi || !surplusPositive:
		level = 2
	default:
		level = 3
		if in.Holdings != nil && in.Expenses > 0 { // without a spending baseline the 12x/25x tests mean nothing
			if float64(in.Investments) > 12*float64(in.Expenses) && diversified(in.Holdings) {
				level = 4
			}
			if float64(in.Investments) > 25*annual {
				level = 5
			}
		}
	}
	out.LadderLevel = level
	switch {
	case annual > 0:
		out.LadderProgress = round1(math.Min(100, float64(in.Investments)/(25*annual)*100))
	}
	tr.add("4", "ladder_level", float64(level), "months=%.2f target_min=%.0f high_interest_debt=%t surplus_positive=%t holdings_answered=%t", months, target.Min, hi, surplusPositive, in.Holdings != nil)
	out.EmergencyFundMonths, out.EmergencyTargetMonths = round1(months), target

	// Step 5: briefing priorities.
	if len(in.Holdings) > 0 {
		out.BriefingPriorities = append(out.BriefingPriorities, "asset_alerts")
	}
	if level <= 2 {
		out.BriefingPriorities = append(out.BriefingPriorities, "emergency_fund", "debt_management")
	} else {
		out.BriefingPriorities = append(out.BriefingPriorities, "market_news", "macro_indicators")
	}

	// Step 6: flags.
	out.Flags = flagsFor(in, out, hi, provisional)
	tr.add("6", "flags", float64(len(out.Flags)), "count")

	// Step 7: financial health.
	out.FinancialHealth = health(in, months, &tr)
	out.Metrics = metrics(in)
	out.Gates = Gates{Sharia: in.Sharia, Holdings: slices.Clone(in.Holdings)}
	if out.Gates.Holdings == nil {
		out.Gates.Holdings = []string{}
	}
	return out, tr
}

func health(in Input, months float64, tr *tracer) Health {
	// Security.
	debtHealth := debtScore(in)
	if in.highInterestDebt() {
		debtHealth -= 15
	}
	security := clamp(savingsRateScore(in)*0.55 + clamp(debtHealth)*0.45)
	tr.add("7", "financial_security", security, "savings=%.0f debt=%.0f", savingsRateScore(in), clamp(debtHealth))

	// Safety.
	adj := 0.6
	switch t := safetyTarget[in.Employment]; {
	case months >= t:
		adj = 1
	case months >= t*0.5:
		adj = 0.8
	}
	safety := clamp(efBaseScore(months)*adj + ageSafetyMod[in.Age])
	tr.add("7", "financial_safety", safety, "base=%.0f adj=%.1f age_mod=%.0f", efBaseScore(months), adj, ageSafetyMod[in.Age])

	// Growth.
	var asset float64
	switch {
	case !in.IncomeKnown:
		if in.Investments > 0 {
			asset = 25
		}
	case in.Income == 0:
		if in.Investments > 0 {
			asset = 100
		}
	case in.Investments > 0:
		r := float64(in.Investments) / (float64(in.Income) * 12)
		asset = lookup(r, assetTiers, 25)
		if r > 5 {
			asset = 100
		}
	}
	growth := clamp(asset*0.30 + investableScore(in)*0.35 + horizonScore[in.InvestHorizon]*0.35 + ageGrowthMod[in.Age])
	tr.add("7", "financial_growth", growth, "asset=%.0f investable=%.0f horizon=%.0f age_mod=%.0f", asset, investableScore(in), horizonScore[in.InvestHorizon], ageGrowthMod[in.Age])

	// Wisdom.
	attitude := (norm(in.Q12)*0.40 + norm(in.Q13)*0.30 + norm(in.Q14)*0.15 + norm(in.Q15)*0.15)
	knowledge := knowledgeScore[in.Knowledge]*0.50 + b2f(in.Q16bCorrect)*100*0.30 + priorExpScore[in.PriorExp]*0.20
	wisdom := clamp(attitude*0.45 + knowledge*0.55)
	tr.add("7", "financial_wisdom", wisdom, "attitude=%.1f knowledge=%.1f", attitude, knowledge)

	// Freedom.
	clarity := 100.0
	if in.Goal == "wealth_growth" {
		clarity = 50
	}
	var feasibility float64
	if in.GoalAmount > 0 {
		need := float64(in.GoalAmount) / timelineMonths[in.GoalHorizon]
		feasibility = lookup(float64(in.Investable)/need, feasibilityTiers, 0)
	}
	freedom := clamp(clarity*0.25 + feasibility*0.35 + timelineQuality[in.GoalHorizon]*0.20 + homeStability[in.Home]*0.20 + depFreedom[in.Dependents])
	tr.add("7", "financial_freedom", freedom, "clarity=%.0f feasibility=%.0f timeline=%.0f home=%.0f dependents_mod=%.0f", clarity, feasibility, timelineQuality[in.GoalHorizon], homeStability[in.Home], depFreedom[in.Dependents])

	total := (security + safety + growth + wisdom + freedom) / 5
	h := Health{
		FinancialSecurity: round1(security), FinancialSafety: round1(safety), FinancialGrowth: round1(growth),
		FinancialWisdom: round1(wisdom), FinancialFreedom: round1(freedom), TotalScore: round1(total),
	}
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
	tr.add("7", "total_score", h.TotalScore, "category=%s", h.Category)
	return h
}

func metrics(in Input) Metrics {
	m := Metrics{MonthlyExpenses: in.Expenses, MonthlyInstallment: in.Installment}
	if !in.IncomeKnown {
		return m
	}
	m.MonthlyIncome, m.MonthlySurplus = in.Income, in.surplus()
	r, _ := in.savingsRate()
	m.SavingsRate = round4(r)
	d, _ := in.dti()
	m.DTI = round4(d)
	ir, _ := in.investableRatio()
	m.InvestableRatio = round4(ir)
	return m
}

func flagsFor(in Input, out Output, hi, provisional bool) []Flag {
	flags := []Flag{}
	add := func(code, en, id string) { flags = append(flags, Flag{Code: code, MessageEn: en, MessageID: id}) }
	if in.Employment == "permanent_employee" && in.Stability == "no_fixed_income" {
		add("impossible_combination", "A permanent employee with no fixed income looks inconsistent. Please review your answers.", "Karyawan tetap tanpa pendapatan tetap tampak tidak konsisten. Mohon periksa kembali jawaban Anda.")
	}
	if in.IncomeKnown {
		if in.Employment == "student" && in.Income > studentIncomeLimit {
			add("impossible_combination", "A student income above Rp 100,000,000 per month looks unusual. Please review your answers.", "Pendapatan mahasiswa di atas Rp 100.000.000 per bulan tampak tidak biasa. Mohon periksa kembali jawaban Anda.")
		}
		if in.Investable > max(0, in.surplus()) {
			add("impossible_combination", "Your monthly investment is higher than income minus expenses and installments.", "Investasi bulanan Anda lebih besar dari pendapatan dikurangi pengeluaran dan cicilan.")
		}
		if in.Savings > in.Income*12 {
			add("impossible_combination", "Your savings exceed twelve months of income. Please confirm the amount.", "Tabungan Anda melebihi pendapatan 12 bulan. Mohon pastikan jumlahnya.")
		}
	}
	if out.ConsistencyFlag {
		add("consistency", "Your reaction to a loss and your stated investment preference point in different directions.", "Reaksi Anda terhadap kerugian dan preferensi investasi Anda menunjukkan arah yang berbeda.")
	}
	if provisional {
		add("provisional", "Income was not shared, so conservative assumptions were used and the profile is provisional.", "Pendapatan tidak dibagikan, sehingga asumsi konservatif dipakai dan profil bersifat sementara.")
	}
	if hi {
		add("high_interest_debt", "You carry high-interest debt (credit card, paylater or pinjol). Paying it down comes before investing.", "Anda memiliki utang berbunga tinggi (kartu kredit, paylater atau pinjol). Melunasinya didahulukan sebelum berinvestasi.")
	}
	return flags
}

var (
	pjm         = [3][3]int{{0, 0, 1}, {0, 1, 1}, {1, 1, 2}}
	profileName = [3]string{Conservative, Moderate, Aggressive}
)

func bandIndex(b string) int {
	switch b {
	case "low":
		return 0
	case "medium":
		return 1
	}
	return 2
}

func profileIndex(name string) (int, bool) {
	i := slices.Index(profileName[:], name)
	return i, i >= 0
}

// nearThreshold: within 3 points of the 40 or 70 band edges.
func nearThreshold(score float64) bool {
	return math.Abs(score-40) <= 3 || math.Abs(score-70) <= 3
}

func diversified(h []string) bool { return len(h) >= 2 }

func abs(i int) int {
	if i < 0 {
		return -i
	}
	return i
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
