package app

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"

	advapp "shenzen/backend/internal/features/advisory/application"
	advdomain "shenzen/backend/internal/features/advisory/domain"
	advinfra "shenzen/backend/internal/features/advisory/infrastructure"
	advhttp "shenzen/backend/internal/features/advisory/transport"
	authhttp "shenzen/backend/internal/features/auth/transport"
	marketinfra "shenzen/backend/internal/features/market/infrastructure"
	matchapp "shenzen/backend/internal/features/matching/application"
	matchdomain "shenzen/backend/internal/features/matching/domain"
	newsapp "shenzen/backend/internal/features/news/application"
	newsdomain "shenzen/backend/internal/features/news/domain"
	newsinfra "shenzen/backend/internal/features/news/infrastructure"
	pfapp "shenzen/backend/internal/features/portfolio/application"
	pfdomain "shenzen/backend/internal/features/portfolio/domain"
	pfinfra "shenzen/backend/internal/features/portfolio/infrastructure"
	profileinfra "shenzen/backend/internal/features/profile/infrastructure"
	"shenzen/backend/internal/platform/aiclient"
)

// aiChatTimeout covers the AI service's cold model load (up to ~40 s).
const aiChatTimeout = 60 * time.Second

func mountAdvisory(r chi.Router, d Deps, authH *authhttp.Handler) {
	market := marketBridge{pool: d.Pool, repo: marketinfra.NewRepo(d.Pool)}
	pf := pfapp.NewService(pfinfra.NewRepo(d.Pool), market, newID, time.Now)
	svc := advapp.NewService(advapp.Deps{
		Store:    advinfra.NewStore(d.Pool),
		Contexts: contextBuilder{profiles: profileBridge{profileinfra.NewRepo(d.Pool)}, matching: matching(d), portfolio: pf},
		News:     newsBridge{svc: newsapp.NewService(newsFeed{market.repo}, newsinfra.NewRepo(d.Pool), pf)},
		AI:       advinfra.NewAI(aiclient.New(d.Config.AIServiceURL, d.Config.AIInternalKey, aiChatTimeout)),
		NewID:    newUUID, Now: time.Now,
	})
	advhttp.New(svc, authH.RequireAuth, authhttp.UserID).Routes(r)
}

// contextBuilder assembles the AI user context from the profile, the portfolio and the matching engine.
type contextBuilder struct {
	profiles  matchdomain.ProfileReader
	matching  *matchapp.Service
	portfolio *pfapp.Service
}

func (b contextBuilder) Build(ctx context.Context, userID string, compare []string) (advdomain.UserContext, error) {
	p, err := b.profiles.Profile(ctx, userID)
	if errors.Is(err, matchdomain.ErrNoProfile) {
		return advdomain.UserContext{}, advdomain.ErrNoProfile
	}
	if err != nil {
		return advdomain.UserContext{}, err
	}
	prod, err := b.matching.Products(ctx, userID)
	if errors.Is(err, matchdomain.ErrNoProfile) {
		return advdomain.UserContext{}, advdomain.ErrNoProfile
	}
	if err != nil {
		return advdomain.UserContext{}, err
	}
	var hs []pfdomain.HoldingView
	if snap, err := b.portfolio.Snapshot(ctx, userID, time.Time{}); err == nil { // holdings are context, not a precondition
		hs = snap.Holdings
	}
	uc := buildUserContext(p, prod, hs)
	if len(compare) > 0 {
		if cmp, err := b.matching.Compare(ctx, userID, compare); err == nil {
			uc.Comparison = compareFacts(cmp)
		} else {
			slog.WarnContext(ctx, "advisory: comparison unavailable", "err", err) // the question is still answered, without it
		}
	}
	return uc, nil
}

func compareFacts(c matchapp.CompareResult) []advdomain.CompareFact {
	out := make([]advdomain.CompareFact, 0, len(c.Assets))
	for _, a := range c.Assets {
		out = append(out, advdomain.CompareFact{
			Symbol: a.Symbol, Name: a.Name, Class: a.Class, Eligible: a.Gate.Eligible, Gate: a.Gate.Gate, Score: a.Score.Total, Rank: a.Rank,
			Return1Y: a.Metrics.Return1Y, Volatility: a.Metrics.Volatility, Sharpe: a.Metrics.Sharpe, DivYield: a.Metrics.DivYield, PER: a.Metrics.PER, PBV: a.Metrics.PBV,
		})
	}
	return out
}

// buildUserContext maps facts only: it never reads Profile.Name, and no email or phone is available to it.
func buildUserContext(p matchdomain.Profile, prod matchapp.ProductResult, hs []pfdomain.HoldingView) advdomain.UserContext {
	uc := advdomain.UserContext{
		Profile: advdomain.ProfileFacts{
			RiskProfile: p.Risk, LadderLevel: p.Ladder, Sharia: p.Sharia, HorizonYears: p.HorizonYears(), GoalAmount: p.GoalAmount,
			Income: p.Income, Expenses: p.Expenses, Surplus: p.Surplus, Installment: p.Installment, Savings: p.Savings, Investable: p.Investable,
			Dependents: p.Dependents, Employment: p.Employment, Knowledge: p.Knowledge,
			HeldKinds: orEmpty(p.HeldKinds), Priorities: orEmpty(p.Priorities),
			EmergencyMonths: p.EmergencyMonths, EmergencyTargetMonths: p.EmergencyTargetMax, HealthCategory: p.Health.Category, HealthScore: p.Health.Total,
		},
		Holdings: []advdomain.HoldingFact{}, Recommended: []advdomain.ClassFact{},
	}
	if p.Goal != "" && p.Goal != "none" {
		g := p.Goal
		uc.Profile.Goal = &g
	}
	var total int64
	for _, h := range hs {
		total += h.MarketValue
	}
	for _, h := range hs {
		f := advdomain.HoldingFact{Name: h.Name, AssetClass: h.AssetClass, ValueIDR: h.MarketValue}
		if h.Symbol != nil {
			f.Symbol = *h.Symbol
		}
		if total > 0 {
			f.WeightPct = math.Round(float64(h.MarketValue)/float64(total)*1000) / 10
		}
		uc.Holdings = append(uc.Holdings, f)
	}
	for _, c := range prod.Portfolio {
		cf := advdomain.ClassFact{Class: c.Class, AllocationPct: c.Allocation, Assets: []advdomain.AssetFact{}}
		for _, a := range c.Assets {
			cf.Assets = append(cf.Assets, advdomain.AssetFact{Ticker: a.Ticker, WeightPct: a.Weight})
		}
		uc.Recommended = append(uc.Recommended, cf)
	}
	return uc
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// newsBridge offers the advisor the latest articles, those touching the user's holdings first.
type newsBridge struct{ svc *newsapp.Service }

func (b newsBridge) Latest(ctx context.Context, userID string, limit int) ([]advdomain.NewsFact, error) {
	feed, err := b.svc.List(ctx, userID, newsdomain.Filter{Category: "all", Limit: 30})
	if err != nil {
		return nil, err
	}
	items := feed.Items
	sort.SliceStable(items, func(i, j int) bool { return items[i].RelevantToHoldings && !items[j].RelevantToHoldings })
	out := make([]advdomain.NewsFact, 0, limit)
	for _, a := range items {
		if len(out) == limit {
			break
		}
		out = append(out, advdomain.NewsFact{Title: a.Title, Source: a.Source, URL: a.URL, PublishedAt: a.PublishedAt, Summary: a.Summary})
	}
	return out, nil
}

// newUUID returns a random v4 UUID (chat_sessions.id is a uuid column).
func newUUID() string {
	b := []byte(newID()) // 32 hex chars of crypto/rand
	b[12], b[16] = '4', '8'
	return string(b[0:8]) + "-" + string(b[8:12]) + "-" + string(b[12:16]) + "-" + string(b[16:20]) + "-" + string(b[20:32])
}
