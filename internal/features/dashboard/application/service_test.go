package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"shenzen/backend/internal/features/dashboard/domain"
	"shenzen/backend/internal/shared/apperr"
)

type fakePortfolio struct{ from time.Time }

func (f *fakePortfolio) Snapshot(_ context.Context, _ string, from time.Time) (domain.Snapshot, error) {
	f.from = from
	return domain.Snapshot{}, nil
}

type fakeProfiles struct{}

func (fakeProfiles) Read(context.Context, string) (domain.Profile, error) {
	return domain.Profile{Status: "completed", FirstName: "Dinda", RiskProfile: "Aggressive", Savings: 1000}, nil
}

type failingRec struct{}

func (failingRec) Recommend(context.Context, string, domain.Profile, time.Time) (domain.Recommendation, error) {
	return domain.Recommendation{}, errors.New("matching down")
}

func TestHomeFallsBackToRulesAndValidatesRange(t *testing.T) {
	pf := &fakePortfolio{}
	s := NewService(pf, fakeProfiles{}, failingRec{}, func() time.Time { return time.Date(2026, 10, 17, 3, 0, 0, 0, time.UTC) })
	h, err := s.Home(context.Background(), "u", "")
	if err != nil || h.Recommendation.Source != "rules" || h.Recommendation.Allocation[0].Pct != 70 || h.AssetTrend.Range != "6M" || h.Name != "Dinda" {
		t.Fatalf("%+v %v", h, err)
	}
	if !pf.from.Equal(time.Date(2026, 4, 17, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("history start %v", pf.from)
	}
	for _, bad := range []string{"1M", "2Y", "x"} {
		if _, err := s.Home(context.Background(), "u", bad); err == nil {
			t.Errorf("home range %q accepted", bad)
		} else if ae, _ := apperr.As(err); ae.Code != apperr.ValidationFailed {
			t.Errorf("code %v", ae.Code)
		}
	}
	if _, err := s.Portfolio(context.Background(), "u", "1m"); err != nil {
		t.Fatalf("ranges are case-insensitive: %v", err)
	}
	if _, err := s.Portfolio(context.Background(), "u", "5Y"); err == nil {
		t.Fatal("bad portfolio range")
	}
}
