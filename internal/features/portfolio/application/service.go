// Package application holds the portfolio use cases: holdings CRUD scoped to one user, and IDR valuation.
package application

import (
	"context"
	"strings"
	"time"

	"shenzen/backend/internal/features/portfolio/domain"
	"shenzen/backend/internal/shared/apperr"
)

type Service struct {
	repo   domain.Repository
	market domain.Market
	newID  func() string
	now    func() time.Time
}

func NewService(repo domain.Repository, market domain.Market, newID func() string, now func() time.Time) *Service {
	return &Service{repo: repo, market: market, newID: newID, now: now}
}

func (s *Service) Search(ctx context.Context, q, class string, limit int) ([]domain.AssetHit, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return []domain.AssetHit{}, nil
	}
	if limit < 1 || limit > 50 {
		limit = 20
	}
	hits, err := s.market.Search(ctx, q, class, limit)
	if hits == nil {
		hits = []domain.AssetHit{}
	}
	return hits, err
}

type Listing struct {
	Items  []domain.HoldingView `json:"items"`
	Totals domain.Totals        `json:"totals"`
}

// Snapshot is the valued portfolio plus its daily value/cost history since `from` (zero: no history).
type Snapshot struct {
	Holdings []domain.HoldingView
	History  []domain.DayPoint
}

func (s *Service) List(ctx context.Context, userID string) (Listing, error) {
	snap, err := s.Snapshot(ctx, userID, time.Time{})
	if err != nil {
		return Listing{}, err
	}
	return Listing{Items: snap.Holdings, Totals: domain.SumTotals(snap.Holdings)}, nil
}

// Symbols are the catalog symbols the user holds (for news relevance).
func (s *Service) Symbols(ctx context.Context, userID string) ([]string, error) {
	hs, err := s.repo.List(ctx, userID)
	out := []string{}
	for _, h := range hs {
		if h.Symbol != "" {
			out = append(out, h.Symbol)
		}
	}
	return out, err
}

// Snapshot loads and prices every holding of the user with a fixed number of queries.
func (s *Service) Snapshot(ctx context.Context, userID string, from time.Time) (Snapshot, error) {
	hs, err := s.repo.List(ctx, userID)
	if err != nil {
		return Snapshot{}, err
	}
	snap := Snapshot{Holdings: []domain.HoldingView{}}
	if len(hs) == 0 {
		return snap, nil
	}
	today := domain.Today(s.now())
	fetchFrom := today.AddDate(-1, 0, -1) // the 1Y trend always needs a year
	if !from.IsZero() && from.Before(fetchFrom) {
		fetchFrom = from
	}
	var symbols []string
	for _, h := range hs {
		if h.Symbol != "" {
			symbols = append(symbols, h.Symbol)
		}
	}
	var assets map[string]domain.AssetInfo
	var series map[string][]domain.Point
	var divs map[string][]domain.Dividend
	var fx domain.FX
	if len(symbols) > 0 {
		if assets, err = s.market.Assets(ctx, symbols); err != nil {
			return snap, err
		}
		if series, err = s.market.Series(ctx, symbols, fetchFrom); err != nil {
			return snap, err
		}
		if divs, err = s.market.Dividends(ctx, symbols); err != nil {
			return snap, err
		}
		if fx.USDIDR, err = s.market.USDIDR(ctx, fetchFrom); err != nil {
			return snap, err
		}
	}
	held := make([]domain.Held, len(hs))
	for i, h := range hs {
		held[i] = domain.Held{H: h, In: instrument(h, assets[h.Symbol], series[h.Symbol])}
		snap.Holdings = append(snap.Holdings, domain.Value(h, held[i].In, fx, divs[h.Symbol]))
	}
	domain.ApplyWeights(snap.Holdings)
	if !from.IsZero() {
		snap.History = domain.History(held, fx, from, today)
	}
	return snap, nil
}

func instrument(h domain.Holding, a domain.AssetInfo, series []domain.Point) domain.Instrument {
	if m := h.Manual; m != nil {
		id, _ := domain.ClassLabels(m.Class)
		return domain.Instrument{Name: m.Name, Subtitle: id, Class: m.Class, Currency: m.Currency, ManualPrice: m.Price, ManualDate: domain.Today(m.PriceUpdatedAt)}
	}
	return domain.Instrument{Name: a.Name, Subtitle: a.Exchange, Class: a.AssetClass, Currency: a.Currency, Series: series}
}

func (s *Service) Create(ctx context.Context, userID string, in domain.CreateInput) (domain.HoldingView, error) {
	h, err := domain.NewHolding(s.newID(), userID, in, s.now())
	if err != nil {
		return domain.HoldingView{}, err
	}
	if h.Symbol != "" {
		if err := s.checkSymbol(ctx, h.Symbol); err != nil {
			return domain.HoldingView{}, err
		}
	}
	if err := s.repo.Insert(ctx, h, domain.MaxHoldingsPerUser); err != nil {
		return domain.HoldingView{}, err
	}
	return s.view(ctx, userID, h.ID)
}

func (s *Service) checkSymbol(ctx context.Context, symbol string) error {
	a, err := s.market.Assets(ctx, []string{symbol})
	if err != nil {
		return err
	}
	if _, ok := a[symbol]; !ok {
		return apperr.Validation(apperr.Detail{Field: "symbol", Message: "unknown symbol"})
	}
	ser, err := s.market.Series(ctx, []string{symbol}, domain.Today(s.now()).AddDate(-2, 0, 0))
	if err != nil {
		return err
	}
	if len(ser[symbol]) == 0 {
		return apperr.Validation(apperr.Detail{Field: "symbol", Message: "no price data for this symbol"})
	}
	return nil
}

// Update is scoped by user: another user's id is indistinguishable from a missing one (404).
func (s *Service) Update(ctx context.Context, userID, id string, in domain.UpdateInput) (domain.HoldingView, error) {
	h, err := s.repo.Get(ctx, userID, id)
	if err != nil {
		return domain.HoldingView{}, err
	}
	h, err = h.Apply(in, s.now())
	if err != nil {
		return domain.HoldingView{}, err
	}
	if err := s.repo.Update(ctx, h); err != nil {
		return domain.HoldingView{}, err
	}
	return s.view(ctx, userID, id)
}

func (s *Service) Delete(ctx context.Context, userID, id string) error {
	return s.repo.Delete(ctx, userID, id)
}

func (s *Service) view(ctx context.Context, userID, id string) (domain.HoldingView, error) {
	snap, err := s.Snapshot(ctx, userID, time.Time{})
	if err != nil {
		return domain.HoldingView{}, err
	}
	for _, v := range snap.Holdings {
		if v.ID == id {
			return v, nil
		}
	}
	return domain.HoldingView{}, domain.ErrNotFound
}
