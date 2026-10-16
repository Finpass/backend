// Package domain implements the Matching Engine YAML as pure, deterministic code: no I/O, no clock, no randomness.
package domain

import (
	"context"
	"errors"
	"time"
)

var ErrNoProfile = errors.New("matching: no completed profile")

// Text carries both languages; the engine never translates at runtime.
type Text struct {
	ID string `json:"id"`
	EN string `json:"en"`
}

const (
	Conservative = "Conservative"
	Moderate     = "Moderate"
	Aggressive   = "Aggressive"
)

// Classes of the Asset Classifier plus the generic instrument classes the catalog does not contain.
const (
	ClassLargeCap = "equity_large_cap"
	ClassMidSmall = "equity_mid_small"
	ClassGlobal   = "equity_global"
	ClassETF      = "etf"
	ClassFund     = "mutual_fund"
	ClassBondGov  = "bond_government"
	ClassSukuk    = "sukuk"
	ClassDeposit  = "deposits"
	ClassGold     = "gold"
	ClassCrypto   = "crypto"
	ClassProperty = "property"
)

var ClassLabels = map[string]Text{
	ClassLargeCap: {ID: "Saham Large Cap", EN: "Large-cap equity"},
	ClassMidSmall: {ID: "Saham Mid/Small Cap", EN: "Mid/small-cap equity"},
	ClassGlobal:   {ID: "Saham Global", EN: "Global equity"},
	ClassETF:      {ID: "ETF", EN: "ETF"},
	ClassFund:     {ID: "Reksa Dana", EN: "Mutual fund"},
	ClassBondGov:  {ID: "Obligasi Pemerintah", EN: "Government bond"},
	ClassSukuk:    {ID: "Sukuk", EN: "Sukuk"},
	ClassDeposit:  {ID: "Deposito", EN: "Deposit"},
	ClassGold:     {ID: "Emas", EN: "Gold"},
	ClassCrypto:   {ID: "Kripto", EN: "Crypto"},
	ClassProperty: {ID: "Properti", EN: "Property"},
}

type Health struct {
	Security, Safety, Growth, Wisdom, Freedom, Total float64
	Category                                         string
}

// Profile is what the engine needs from the profile slice (Result + answers), adapted in app wiring.
type Profile struct {
	UserID, Name                                                    string
	Risk                                                            string
	Ladder                                                          int
	Health                                                          Health
	EmergencyMonths, EmergencyTargetMax                             float64
	Income, Expenses, Surplus, Installment, Investable, Savings     int64
	Dependents                                                      int
	Employment, Knowledge, Sharia, Goal, GoalHorizon, InvestHorizon string
	GoalAmount                                                      int64
	HeldKinds                                                       []string // q23
	Priorities                                                      []string // briefingPriorities
}

// HorizonYears resolves the planning horizon from q19 (goal), falling back to q9.
func (p Profile) HorizonYears() float64 {
	h := p.GoalHorizon
	if h == "" || h == "none" {
		h = p.InvestHorizon
	}
	switch h {
	case "lt_1":
		return 0.5
	case "1_3":
		return 2
	case "3_5":
		return 4
	case "5_10":
		return 7.5
	case "gt_10":
		return 15
	}
	return 5
}

// Candidate is a catalog asset with the facts the scoring layer needs.
type Candidate struct {
	Symbol, Name, Class, Sector, Sharia, Geography, Volatility, Liquidity, Currency string
	Return1Y                                                                        float64 // percent
	Vol                                                                             float64 // annualised percent
	PositiveMonths                                                                  int
	PER, PBV, ROE, DivYield, Beta, ExpenseRatio                                     *float64        // ROE as percent, DivYield as percent
	Rets                                                                            map[int]float64 // daily returns keyed by yyyymmdd
}

// Holding is a current position with the quote facts needed for alerts. Values are IDR.
type Holding struct {
	Symbol, Name, Class, Sector, Currency string
	Manual                                bool
	Units, ValueIDR                       float64
	Last, Prev, Hi52Prev, Lo52Prev        float64 // zero when unknown
	LastDate                              time.Time
	FX                                    float64 // IDR per unit of Currency
	AlertThresholdPct                     *float64
	ExDate                                *time.Time
	DividendPerShare                      float64
}

type Article struct {
	ID, Source, Category, Title, Summary, URL string
	PublishedAt                               time.Time
	Tickers                                   []string
	Sentiment                                 *float64
}

type MacroIndicator struct {
	Key       string // bi_rate|inflation|usd_idr|gdp
	Label     Text
	ValueText string   // empty when only a headline exists
	Change    *float64 // percent
	Headline  string
	AsOf      time.Time
	Source    string
}

// MacroSignals feed the macro_fit component of the scoring layer.
type MacroSignals struct{ BIRateUp, InflationUp, RupiahWeak bool }

// DayRecord is the selection history used by suppression and variety rules (newest first).
type DayRecord struct {
	Date      string
	AlertKey  string
	NewsTopic string
	NewsID    string
	NudgeKey  string
}

type HoldingsReader interface {
	Holdings(ctx context.Context, userID string) ([]Holding, error)
}
type ProfileReader interface {
	Profile(ctx context.Context, userID string) (Profile, error)
}
type Catalog interface {
	Candidates(ctx context.Context) ([]Candidate, error)
	RiskFree(ctx context.Context) (float64, error)
	Signals(ctx context.Context) (MacroSignals, error)
}
type NewsReader interface {
	Recent(ctx context.Context, since time.Time, tickers []string, limit int) ([]Article, error)
}
type MacroReader interface {
	Indicators(ctx context.Context, since time.Time) ([]MacroIndicator, error)
}
type HistoryReader interface {
	History(ctx context.Context, userID string, days int) ([]DayRecord, error)
}
