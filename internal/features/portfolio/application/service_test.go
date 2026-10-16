package application

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"shenzen/backend/internal/features/portfolio/domain"
	"shenzen/backend/internal/shared/apperr"
)

type fakeRepo struct{ rows []domain.Holding }

func (f *fakeRepo) Insert(_ context.Context, h domain.Holding, limit int) error {
	n := 0
	for _, r := range f.rows {
		if r.UserID == h.UserID {
			n++
		}
	}
	if n >= limit {
		return domain.ErrLimitReached
	}
	f.rows = append(f.rows, h)
	return nil
}
func (f *fakeRepo) List(_ context.Context, u string) ([]domain.Holding, error) {
	var out []domain.Holding
	for _, r := range f.rows {
		if r.UserID == u {
			out = append(out, r)
		}
	}
	return out, nil
}
func (f *fakeRepo) idx(u, id string) int {
	for i, r := range f.rows {
		if r.UserID == u && r.ID == id {
			return i
		}
	}
	return -1
}
func (f *fakeRepo) Get(_ context.Context, u, id string) (domain.Holding, error) {
	if i := f.idx(u, id); i >= 0 {
		return f.rows[i], nil
	}
	return domain.Holding{}, domain.ErrNotFound
}
func (f *fakeRepo) Update(_ context.Context, h domain.Holding) error {
	i := f.idx(h.UserID, h.ID)
	if i < 0 {
		return domain.ErrNotFound
	}
	f.rows[i] = h
	return nil
}
func (f *fakeRepo) Delete(_ context.Context, u, id string) error {
	i := f.idx(u, id)
	if i < 0 {
		return domain.ErrNotFound
	}
	f.rows = append(f.rows[:i], f.rows[i+1:]...)
	return nil
}

type fakeMarket struct{}

var day = time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)

func (fakeMarket) Search(context.Context, string, string, int) ([]domain.AssetHit, error) {
	return []domain.AssetHit{{Symbol: "BBCA.JK"}}, nil
}
func (fakeMarket) Assets(_ context.Context, s []string) (map[string]domain.AssetInfo, error) {
	out := map[string]domain.AssetInfo{}
	for _, x := range s {
		switch x {
		case "BBCA.JK":
			out[x] = domain.AssetInfo{Symbol: x, Name: "BCA", AssetClass: "equity", Currency: "IDR", Exchange: "JKT"}
		case "AAPL":
			out[x] = domain.AssetInfo{Symbol: x, Name: "Apple", AssetClass: "equity", Currency: "USD", Exchange: "US"}
		case "NOPRICE":
			out[x] = domain.AssetInfo{Symbol: x, Name: "No price", AssetClass: "equity", Currency: "IDR"}
		}
	}
	return out, nil
}
func (fakeMarket) Series(_ context.Context, s []string, _ time.Time) (map[string][]domain.Point, error) {
	out := map[string][]domain.Point{}
	for _, x := range s {
		if x == "BBCA.JK" || x == "AAPL" {
			out[x] = []domain.Point{{Date: day.AddDate(0, 0, -1), Value: 100}, {Date: day, Value: 110}}
		}
	}
	return out, nil
}
func (fakeMarket) Dividends(context.Context, []string) (map[string][]domain.Dividend, error) {
	return nil, nil
}
func (fakeMarket) USDIDR(context.Context, time.Time) ([]domain.Point, error) {
	return []domain.Point{{Date: day, Value: 16000}}, nil
}

func newSvc() (*Service, *fakeRepo) {
	r := &fakeRepo{}
	n := 0
	return NewService(r, fakeMarket{}, func() string { n++; return fmt.Sprint("h", n) }, func() time.Time { return day.Add(12 * time.Hour) }), r
}

func code(err error) apperr.Code {
	if ae, ok := apperr.As(err); ok {
		return ae.Code
	}
	return ""
}

func TestOwnershipIsolation(t *testing.T) {
	ctx := context.Background()
	s, _ := newSvc()
	a, err := s.Create(ctx, "alice", domain.CreateInput{Symbol: "BBCA.JK", Units: 10, AvgCost: 90, AcquiredAt: "2026-01-01"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, "bob", domain.CreateInput{Symbol: "AAPL", Units: 1, AvgCost: 1, AcquiredAt: "2026-01-01"}); err != nil {
		t.Fatal(err)
	}
	u := 5.0
	if _, err := s.Update(ctx, "bob", a.ID, domain.UpdateInput{Units: &u}); code(err) != apperr.NotFound {
		t.Fatalf("bob patching alice: %v", err)
	}
	if err := s.Delete(ctx, "bob", a.ID); code(err) != apperr.NotFound {
		t.Fatalf("bob deleting alice: %v", err)
	}
	la, _ := s.List(ctx, "alice")
	lb, _ := s.List(ctx, "bob")
	if len(la.Items) != 1 || la.Items[0].Units != 10 || len(lb.Items) != 1 || lb.Items[0].ID == a.ID {
		t.Fatalf("lists leaked: %+v %+v", la, lb)
	}
	if lb.Items[0].WeightPct != 100 {
		t.Fatal("weights are per user")
	}
	if err := s.Delete(ctx, "alice", a.ID); err != nil {
		t.Fatal(err)
	}
	if l, _ := s.List(ctx, "alice"); len(l.Items) != 0 || l.Totals.MarketValue != 0 {
		t.Fatal("empty after delete")
	}
}

func TestCreateValidation(t *testing.T) {
	ctx := context.Background()
	s, repo := newSvc()
	cases := map[string]domain.CreateInput{
		"unknown symbol": {Symbol: "NOPE", Units: 1, AvgCost: 1, AcquiredAt: "2026-01-01"},
		"no price data":  {Symbol: "NOPRICE", Units: 1, AvgCost: 1, AcquiredAt: "2026-01-01"},
		"zero units":     {Symbol: "AAPL", Units: 0, AvgCost: 1, AcquiredAt: "2026-01-01"},
		"future":         {Symbol: "AAPL", Units: 1, AvgCost: 1, AcquiredAt: "2026-10-18"},
	}
	for name, in := range cases {
		if _, err := s.Create(ctx, "u", in); code(err) != apperr.ValidationFailed {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(repo.rows) != 0 {
		t.Fatal("nothing may be stored on validation failure")
	}
}

func TestLimitAndValuation(t *testing.T) {
	ctx := context.Background()
	s, repo := newSvc()
	for i := 0; i < domain.MaxHoldingsPerUser; i++ {
		repo.rows = append(repo.rows, domain.Holding{ID: fmt.Sprint("x", i), UserID: "u", Symbol: "BBCA.JK", Units: 1, AvgCost: 1, AcquiredAt: day})
	}
	_, err := s.Create(ctx, "u", domain.CreateInput{Symbol: "AAPL", Units: 1, AvgCost: 1, AcquiredAt: "2026-01-01"})
	if code(err) != apperr.Conflict || !errors.Is(err, domain.ErrLimitReached) {
		t.Fatalf("limit: %v", err)
	}
	if _, err := s.Create(ctx, "other", domain.CreateInput{Symbol: "AAPL", Units: 2, AvgCost: 1_000_000, AcquiredAt: "2026-01-01"}); err != nil {
		t.Fatalf("limit is per user: %v", err)
	}
	l, _ := s.List(ctx, "other")
	if v := l.Items[0]; v.MarketValue != 2*110*16000 || v.Change1dPct != 10 || v.CostBasis != 2_000_000 {
		t.Fatalf("%+v", v)
	}
}

func TestSnapshotHistoryAndManualUpdate(t *testing.T) {
	ctx := context.Background()
	s, _ := newSvc()
	m, err := s.Create(ctx, "u", domain.CreateInput{Manual: &domain.ManualInput{Name: "RD", AssetClass: "mutual_fund", Currency: "IDR", CurrentPrice: 1000}, Units: 10, AvgCost: 900, AcquiredAt: "2026-10-01"})
	if err != nil {
		t.Fatal(err)
	}
	p := 1100.0
	if m, err = s.Update(ctx, "u", m.ID, domain.UpdateInput{CurrentPrice: &p}); err != nil || m.MarketValue != 11000 {
		t.Fatalf("%+v %v", m, err)
	}
	snap, err := s.Snapshot(ctx, "u", day.AddDate(0, 0, -14))
	if err != nil || len(snap.History) == 0 || snap.History[len(snap.History)-1].Value != 11000 {
		t.Fatalf("%+v %v", snap, err)
	}
	if snap, _ := s.Snapshot(ctx, "nobody", day); len(snap.Holdings) != 0 || snap.History != nil {
		t.Fatal("empty portfolio")
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	s, _ := newSvc()
	if h, err := s.Search(context.Background(), "  ", "", 5); err != nil || h == nil || len(h) != 0 {
		t.Fatalf("%v %v", h, err)
	}
}
