package app

import (
	"context"
	"errors"
	"math"
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

func (b contextBuilder) Build(ctx context.Context, userID string) (advdomain.UserContext, error) {
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
	return buildUserContext(p, prod, hs), nil
}

// buildUserContext maps facts only: it never reads Profile.Name, and no email or phone is available to it.
func buildUserContext(p matchdomain.Profile, prod matchapp.ProductResult, hs []pfdomain.HoldingView) advdomain.UserContext {
	uc := advdomain.UserContext{
		Profile:  advdomain.ProfileFacts{RiskProfile: p.Risk, LadderLevel: p.Ladder, Sharia: p.Sharia, HorizonYears: p.HorizonYears()},
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
		f := advdomain.HoldingFact{Name: h.Name, AssetClass: h.AssetClass}
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

// newUUID returns a random v4 UUID (chat_sessions.id is a uuid column).
func newUUID() string {
	b := []byte(newID()) // 32 hex chars of crypto/rand
	b[12], b[16] = '4', '8'
	return string(b[0:8]) + "-" + string(b[8:12]) + "-" + string(b[12:16]) + "-" + string(b[16:20]) + "-" + string(b[20:32])
}
