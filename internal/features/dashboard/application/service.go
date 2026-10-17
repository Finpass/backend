// Package application loads the profile and the valued portfolio and builds the dashboard pages.
package application

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"shenzen/backend/internal/features/dashboard/domain"
	"shenzen/backend/internal/shared/apperr"
)

type Service struct {
	portfolio domain.Portfolio
	profiles  domain.ProfileReader
	rec       domain.Recommender
	now       func() time.Time
}

func NewService(p domain.Portfolio, pr domain.ProfileReader, rec domain.Recommender, now func() time.Time) *Service {
	return &Service{portfolio: p, profiles: pr, rec: rec, now: now}
}

// RulesRecommender is the default Recommender: the fixed allocation table of the final profile.
type RulesRecommender struct{}

func (RulesRecommender) Recommend(_ context.Context, _ string, p domain.Profile, now time.Time) (domain.Recommendation, error) {
	return domain.RulesRecommendation(p, now), nil
}

func badRange() error {
	return apperr.Validation(apperr.Detail{Field: "range", Message: "invalid range"})
}

func (s *Service) Home(ctx context.Context, userID, rng string) (domain.Home, error) {
	if rng == "" {
		rng = "6M"
	}
	rng = strings.ToUpper(rng)
	today := domain.Today(s.now())
	start, ok := domain.HomeStart(rng, today)
	if !ok {
		return domain.Home{}, badRange()
	}
	prof, err := s.profiles.Read(ctx, userID)
	if err != nil {
		return domain.Home{}, err
	}
	snap, err := s.portfolio.Snapshot(ctx, userID, start)
	if err != nil {
		return domain.Home{}, err
	}
	rec, err := s.rec.Recommend(ctx, userID, prof, s.now())
	if err != nil {
		slog.WarnContext(ctx, "recommender failed, using rules fallback", "err", err)
		rec = domain.RulesRecommendation(prof, s.now())
	}
	return domain.BuildHome(prof, snap, rng, today, rec), nil
}

func (s *Service) Portfolio(ctx context.Context, userID, rng string) (domain.PortfolioPage, error) {
	if rng == "" {
		rng = "6M"
	}
	rng = strings.ToUpper(rng)
	today := domain.Today(s.now())
	start, ok := domain.PortfolioStart(rng, today)
	if !ok {
		return domain.PortfolioPage{}, badRange()
	}
	prof, err := s.profiles.Read(ctx, userID)
	if err != nil {
		return domain.PortfolioPage{}, err
	}
	snap, err := s.portfolio.Snapshot(ctx, userID, domain.HistoryFrom(start, today, true))
	if err != nil {
		return domain.PortfolioPage{}, err
	}
	var cash int64
	if prof.Status == "completed" {
		cash = prof.Savings
	}
	return domain.BuildPortfolio(prof.FirstName, snap, rng, today, cash), nil
}
