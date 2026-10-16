package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"shenzen/backend/internal/features/matching/domain"
)

type fp struct {
	p   domain.Profile
	err error
}

func (f fp) Profile(context.Context, string) (domain.Profile, error) { return f.p, f.err }

type fh struct {
	h   []domain.Holding
	err error
}

func (f fh) Holdings(context.Context, string) ([]domain.Holding, error) { return f.h, f.err }

type fc struct{ c []domain.Candidate }

func (f fc) Candidates(context.Context) ([]domain.Candidate, error) { return f.c, nil }
func (fc) RiskFree(context.Context) (float64, error)                { return 4, nil }
func (fc) Signals(context.Context) (domain.MacroSignals, error)     { return domain.MacroSignals{}, nil }

type fn struct {
	a   []domain.Article
	err error
}

func (f fn) Recent(context.Context, time.Time, []string, int) ([]domain.Article, error) {
	return f.a, f.err
}

type fm struct{}

func (fm) Indicators(context.Context, time.Time) ([]domain.MacroIndicator, error) { return nil, nil }

type fhist struct{}

func (fhist) History(context.Context, string, int) ([]domain.DayRecord, error) { return nil, nil }

func profile() domain.Profile {
	return domain.Profile{UserID: "u", Name: "Surya", Risk: domain.Moderate, Ladder: 3, Goal: "wealth_growth", GoalHorizon: "5_10", Knowledge: "intermediate", Income: 15_000_000, Surplus: 5_000_000,
		Health: domain.Health{Safety: 80, Growth: 80, Wisdom: 80, Freedom: 80}}
}

func svc(p fp, h fh, news fn) *Service {
	return NewService(Deps{Profiles: p, Holdings: h, Catalog: fc{[]domain.Candidate{{Symbol: "A", Name: "A", Class: domain.ClassLargeCap, Sharia: "sharia_compliant", Volatility: "medium", Liquidity: "high", Return1Y: 12, Vol: 18, PositiveMonths: 7}}},
		News: news, Macro: fm{}, History: fhist{}, Now: func() time.Time { return time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC) }})
}

func TestRecommend(t *testing.T) {
	r, err := svc(fp{p: profile()}, fh{}, fn{}).Recommend(context.Background(), "u")
	if err != nil || r.Source != "rules" || r.TextID == "" || r.TextEn == "" {
		t.Fatalf("%+v %v", r, err)
	}
	sum := 0
	for _, a := range r.Allocation {
		sum += a.Pct
		if a.Label == "" || a.LabelID == "" {
			t.Error("labels")
		}
	}
	if sum != 100 {
		t.Errorf("allocation sums to %d", sum)
	}
	if !strings.Contains(r.TextEn, "Moderate") || !strings.Contains(r.TextEn, "not a guarantee") {
		t.Error(r.TextEn)
	}
	var _ Recommender = svc(fp{}, fh{}, fn{})
	if _, err := svc(fp{err: domain.ErrNoProfile}, fh{}, fn{}).Recommend(context.Background(), "u"); !errors.Is(err, domain.ErrNoProfile) {
		t.Errorf("no profile: %v", err)
	}
}

func TestProductsLadderAndHoldings(t *testing.T) {
	h := []domain.Holding{{Symbol: "A", Class: domain.ClassLargeCap, ValueIDR: 1000}}
	res, err := svc(fp{p: profile()}, fh{h: h}, fn{}).Products(context.Background(), "u")
	if err != nil || !res.Investable || res.Profile.Risk != domain.Moderate {
		t.Fatalf("%+v %v", res, err)
	}
	for _, pc := range res.Portfolio {
		if pc.Class == domain.ClassLargeCap && (pc.CurrentPct != 100 || !pc.Assets[0].Held) {
			t.Errorf("holdings: %+v", pc)
		}
	}
	p := profile()
	p.Ladder = 1
	res, _ = svc(fp{p: p}, fh{}, fn{}).Products(context.Background(), "u")
	if res.Investable || len(res.Trace.ScoresPerAsset) != 0 {
		t.Error("ladder 1 does not score assets")
	}
	if _, err := svc(fp{err: domain.ErrNoProfile}, fh{}, fn{}).Products(context.Background(), "u"); !errors.Is(err, domain.ErrNoProfile) {
		t.Error("no profile")
	}
}

func TestDailyDegradesInsteadOfFailing(t *testing.T) {
	res, err := svc(fp{p: profile()}, fh{err: errors.New("db")}, fn{err: errors.New("news down")}).Daily(context.Background(), "u", time.Date(2026, 10, 17, 7, 0, 0, 0, time.UTC))
	if err != nil || len(res.Degraded) != 2 || res.Output.Nudge.Key == "" {
		t.Fatalf("%+v %v", res, err)
	}
}
