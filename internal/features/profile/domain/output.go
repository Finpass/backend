package domain

import "time"

type TraceEntry struct {
	Step   string  `json:"step"`
	Rule   string  `json:"rule"`
	Value  float64 `json:"value"`
	Detail string  `json:"detail,omitempty"`
}

type Flag struct {
	Code      string `json:"code"`
	MessageEn string `json:"messageEn"`
	MessageID string `json:"messageId"`
}

type MonthsRange struct {
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

type Health struct {
	FinancialSecurity float64 `json:"financialSecurity"`
	FinancialSafety   float64 `json:"financialSafety"`
	FinancialGrowth   float64 `json:"financialGrowth"`
	FinancialWisdom   float64 `json:"financialWisdom"`
	FinancialFreedom  float64 `json:"financialFreedom"`
	TotalScore        float64 `json:"totalScore"`
	Category          string  `json:"category"`
}

type Metrics struct {
	MonthlyIncome      int64   `json:"monthlyIncome"`
	MonthlyExpenses    int64   `json:"monthlyExpenses"`
	MonthlyInstallment int64   `json:"monthlyInstallment"`
	MonthlySurplus     int64   `json:"monthlySurplus"`
	SavingsRate        float64 `json:"savingsRate"`
	DTI                float64 `json:"dti"`
	InvestableRatio    float64 `json:"investableRatio"`
}

type Gates struct {
	Sharia   string   `json:"sharia"`
	Holdings []string `json:"holdings"`
}

// Output is everything the rule engine decides; it is stored as-is in the audit trail.
type Output struct {
	CapacityScore         float64     `json:"capacityScore"`
	WillingnessScore      float64     `json:"willingnessScore"`
	CapacityBand          string      `json:"capacityBand"`
	WillingnessBand       string      `json:"willingnessBand"`
	FinalProfile          string      `json:"finalProfile"`
	ProfileLabel          string      `json:"profileLabel"`
	RiskScoreEquivalent   float64     `json:"riskScoreEquivalent"`
	ConsistencyFlag       bool        `json:"consistencyFlag"`
	KnowledgeCapApplied   bool        `json:"knowledgeCapApplied"`
	LadderLevel           int         `json:"ladderLevel"`
	LadderProgress        float64     `json:"ladderProgress"`
	EmergencyFundMonths   float64     `json:"emergencyFundMonths"`
	EmergencyTargetMonths MonthsRange `json:"emergencyTargetMonths"`
	BriefingPriorities    []string    `json:"briefingPriorities"`
	Flags                 []Flag      `json:"flags"`
	FinancialHealth       Health      `json:"financialHealth"`
	Metrics               Metrics     `json:"metrics"`
	Gates                 Gates       `json:"gates"`
}

type Explanation struct {
	Locale     string  `json:"locale"`
	Source     string  `json:"source"`
	Model      *string `json:"model"`
	Summary    string  `json:"summary"`
	Risk       string  `json:"risk"`
	Ladder     string  `json:"ladder"`
	Health     string  `json:"health"`
	NextStep   string  `json:"nextStep"`
	Disclaimer string  `json:"disclaimer"`
}

// Assessment is the immutable audit record: versions + raw input + rule trace + output.
type Assessment struct {
	ID                   string
	UserID               string
	QuestionnaireVersion string
	RuleVersion          string
	Input                StoredInput
	Trace                []TraceEntry
	Output               Output
	Explanation          *Explanation
	CreatedAt            time.Time
}

type StoredInput struct {
	Answers Answers  `json:"answers"`
	OptOut  []string `json:"optOut"`
}

// Result is the API view of an Assessment.
type Result struct {
	ID                   string `json:"id"`
	CreatedAt            string `json:"createdAt"`
	QuestionnaireVersion string `json:"questionnaireVersion"`
	RuleVersion          string `json:"ruleVersion"`
	Output
	Explanation *Explanation `json:"explanation"`
}

func (a Assessment) Result() Result {
	return Result{
		ID: a.ID, CreatedAt: a.CreatedAt.UTC().Format(time.RFC3339),
		QuestionnaireVersion: a.QuestionnaireVersion, RuleVersion: a.RuleVersion,
		Output: a.Output, Explanation: a.Explanation,
	}
}
