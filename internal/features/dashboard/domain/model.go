// Package domain builds the dashboard read models (home and portfolio pages) from a profile and a valued portfolio.
package domain

import (
	"context"
	"time"
)

// Holding mirrors the portfolio slice's valued holding (same JSON); the composition root converts between them.
type Holding struct {
	ID                 string    `json:"id"`
	Symbol             *string   `json:"symbol"`
	Name               string    `json:"name"`
	Subtitle           string    `json:"subtitle"`
	AssetClass         string    `json:"assetClass"`
	AssetClassLabelID  string    `json:"assetClassLabelId"`
	AssetClassLabelEn  string    `json:"assetClassLabelEn"`
	Currency           string    `json:"currency"`
	Units              float64   `json:"units"`
	AvgCost            float64   `json:"avgCost"`
	LastPrice          float64   `json:"lastPrice"`
	LastPriceDate      string    `json:"lastPriceDate"`
	MarketValue        int64     `json:"marketValue"`
	CostBasis          int64     `json:"costBasis"`
	Change1dPct        float64   `json:"change1dPct"`
	TotalReturnPct     float64   `json:"totalReturnPct"`
	TotalReturnIDR     int64     `json:"totalReturnIDR"`
	WeightPct          float64   `json:"weightPct"`
	Trend1y            []float64 `json:"trend1y"`
	IsManual           bool      `json:"isManual"`
	AcquiredAt         string    `json:"acquiredAt"`
	DividendsIDR       int64     `json:"dividendsIDR"`
	ValuationAvailable bool      `json:"valuationAvailable"`

	Value float64 `json:"-"`
	Cost  float64 `json:"-"`
	Divs  float64 `json:"-"`
}

type DayPoint struct {
	Date  time.Time
	Value float64
	Cost  float64
}

type Snapshot struct {
	Holdings []Holding
	History  []DayPoint // ascending daily value and cost since the requested date
}

// Portfolio is implemented by an adapter over the portfolio slice.
type Portfolio interface {
	Snapshot(ctx context.Context, userID string, from time.Time) (Snapshot, error)
}

// Profile is the subset of the user's profile the dashboard needs. Amounts are whole IDR.
type Profile struct {
	FirstName   string
	Status      string // none | draft | completed
	Income      int64  // q2
	Expenses    int64  // q3
	Savings     int64  // q4
	Investments int64  // q7 (self-reported total investment assets)
	Dependents  int    // q5
	Goal        string // q17
	Horizon     string // q19
	Sharia      string // q22 yes|prefer|no
	RiskProfile string // Result.finalProfile, "" without an assessment
}

type ProfileReader interface {
	Read(ctx context.Context, userID string) (Profile, error)
}

type Recommendation struct {
	TextID      string       `json:"textId"`
	TextEn      string       `json:"textEn"`
	Allocation  []Allocation `json:"allocation"`
	Source      string       `json:"source"` // rules | llm
	GeneratedAt string       `json:"generatedAt"`
}

type Allocation struct {
	Label string  `json:"label"`
	Pct   float64 `json:"pct"`
}

// Recommender produces the "recommendation of the day". The matching service replaces the rules fallback.
type Recommender interface {
	Recommend(ctx context.Context, userID string, p Profile, now time.Time) (Recommendation, error)
}

type K struct {
	Value     float64   `json:"value"`
	ChangePct float64   `json:"changePct"`
	Sparkline []float64 `json:"sparkline"`
}

type TrendPoint struct {
	Date            string `json:"date"`
	TotalAssets     int64  `json:"totalAssets"`
	InvestmentValue int64  `json:"investmentValue"`
}

type Slice struct {
	Label   string  `json:"label"`
	LabelID string  `json:"labelId"`
	Pct     float64 `json:"pct"`
}

type Home struct {
	Name string `json:"name"`
	Kpis struct {
		TotalInvestmentAssets K `json:"totalInvestmentAssets"`
		ReadySavings          K `json:"readySavings"`
		MonthlyExpenses       K `json:"monthlyExpenses"`
		MonthlyIncome         K `json:"monthlyIncome"`
	} `json:"kpis"`
	AssetTrend struct {
		Range      string       `json:"range"`
		Points     []TrendPoint `json:"points"`
		Total      int64        `json:"total"`
		ChangePct  float64      `json:"changePct"`
		Allocation []Slice      `json:"allocation"`
	} `json:"assetTrend"`
	FinancialSummary struct {
		MonthlyIncome         int64 `json:"monthlyIncome"`
		MonthlyExpenses       int64 `json:"monthlyExpenses"`
		LiquidSavings         int64 `json:"liquidSavings"`
		TotalInvestmentAssets int64 `json:"totalInvestmentAssets"`
		PrincipalDebt         int64 `json:"principalDebt"`
		Dependents            int   `json:"dependents"`
	} `json:"financialSummary"`
	Preferences struct {
		RiskProfile string `json:"riskProfile"`
		RiskLevel   int    `json:"riskLevel"`
		PrimaryGoal string `json:"primaryGoal"`
		GoalHorizon string `json:"goalHorizon"`
		Sharia      string `json:"sharia"`
	} `json:"preferences"`
	Recommendation Recommendation `json:"recommendation"`
	Account        struct {
		Plan string `json:"plan"`
	} `json:"account"`
	ProfileStatus string `json:"profileStatus"`
}

type PerfPoint struct {
	Date  string `json:"date"`
	Value int64  `json:"value"`
	Cost  int64  `json:"cost"`
}

type SummaryRow struct {
	Key       string  `json:"key"`
	Label     string  `json:"label"`
	Value     float64 `json:"value"`
	Kind      string  `json:"kind"`
	ChangePct float64 `json:"changePct"`
}

type PortfolioPage struct {
	Name string `json:"name"`
	Kpis struct {
		TotalAssets   K `json:"totalAssets"`
		TotalCost     K `json:"totalCost"`
		UnrealizedPnl K `json:"unrealizedPnl"`
		ReturnYtd     K `json:"returnYtd"`
	} `json:"kpis"`
	Performance struct {
		Points []PerfPoint `json:"points"`
	} `json:"performance"`
	Summary  []SummaryRow `json:"summary"`
	Holdings []Holding    `json:"holdings"`
	Footer   struct {
		Count                int   `json:"count"`
		Unrealized           int64 `json:"unrealized"`
		DividendsAccumulated int64 `json:"dividendsAccumulated"`
	} `json:"footer"`
}
