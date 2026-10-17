package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"shenzen/backend/internal/features/discover/domain"
	"shenzen/backend/internal/shared/apperr"
)

func f(v float64) *float64 { return &v }

type catalog struct {
	rows []domain.Metrics
	got  domain.Query
}

func (c *catalog) List(_ context.Context, q domain.Query) (domain.Page, error) {
	c.got = q
	n := q.Limit
	if n > len(c.rows) {
		n = len(c.rows)
	}
	return domain.Page{Rows: c.rows[:n], Total: len(c.rows), More: len(c.rows) > n}, nil
}
func (c *catalog) Facets(context.Context) (domain.Facets, error) {
	return domain.Facets{Classes: []domain.Facet{{Value: "equity", Count: 3}, {Value: "commodity", Count: 1}},
		Exchanges: []domain.Facet{{Value: "JKT", Count: 2}}, Industries: []domain.Facet{{Value: "Equity", Count: 3}}}, nil
}
func (c *catalog) AsOf(context.Context) (time.Time, error) {
	return time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC), nil
}

type stars map[string]map[string]bool

func (s stars) Add(_ context.Context, u, sym string) error {
	if sym == "NOPE" {
		return domain.ErrAssetNotFound
	}
	if s[u] == nil {
		s[u] = map[string]bool{}
	}
	s[u][sym] = true
	return nil
}
func (s stars) Remove(_ context.Context, u, sym string) error { delete(s[u], sym); return nil }
func (s stars) Marked(_ context.Context, u string, syms []string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, x := range syms {
		out[x] = s[u][x]
	}
	return out, nil
}

func newSvc() (*Service, *catalog, stars) {
	c := &catalog{rows: []domain.Metrics{
		{Symbol: "BBCA.JK", Name: "BCA", AssetClass: "equity", Currency: "IDR", Exchange: "JKT", LastPrice: f(5225), LastTradeDate: "2026-10-16",
			MarketCap: f(6.4e14), Valuation: &domain.Valuation{Metric: "PE", Value: 11.07}, Industry: "Banks - Regional"},
		{Symbol: "GC=F", Name: "Gold Futures", AssetClass: "gold", Currency: "USD", Industry: "Gold"},
		{Symbol: "bitcoin", Name: "Bitcoin", AssetClass: "crypto", Industry: "Cryptocurrency"},
	}}
	s := stars{}
	return NewService(c, s), c, s
}

func TestListShapeLabelsNullsAndAsOf(t *testing.T) {
	svc, _, _ := newSvc()
	out, err := svc.List(context.Background(), "u1", Params{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 2 || out.Total != 3 || out.NextCursor == nil || out.AsOf == nil || *out.AsOf != "2026-10-16" {
		t.Fatalf("%+v", out)
	}
	b, g := out.Items[0], out.Items[1]
	if b.AssetClassLabelID != "Saham" || b.AssetClassLabelEN != "Stocks" || b.AnalystTarget != nil || b.Valuation == nil || b.Ret7D != nil {
		t.Fatalf("%+v", b)
	}
	if g.AssetClassLabelID != "Emas" || g.Exchange != nil || g.LastPrice != nil || g.LastTradeDate != nil {
		t.Fatalf("%+v", g)
	}
	c, ok := domain.DecodeCursor(*out.NextCursor)
	if !ok || c.Symbol != "GC=F" || c.Sort != "marketCap" || c.Dir != "desc" || c.Value != nil {
		t.Fatalf("%+v", c)
	}
	last, _ := svc.List(context.Background(), "u1", Params{Limit: 3})
	if last.NextCursor != nil {
		t.Fatal("no cursor expected on the last page")
	}
}

func TestDefaultsAndValidation(t *testing.T) {
	svc, cat, _ := newSvc()
	if _, err := svc.List(context.Background(), "u", Params{Class: "ALL", Exchange: " jkt "}); err != nil {
		t.Fatal(err)
	}
	if cat.got.Sort != "marketCap" || cat.got.Dir != "desc" || cat.got.Limit != 50 || cat.got.Class != "" || cat.got.Exchange != "JKT" {
		t.Fatalf("%+v", cat.got)
	}
	if _, err := svc.List(context.Background(), "u", Params{Sort: "name"}); err != nil || cat.got.Dir != "asc" {
		t.Fatalf("name defaults to asc: %+v %v", cat.got, err)
	}
	cur := domain.Cursor{Sort: "marketCap", Dir: "desc", Symbol: "X"}.Encode()
	for name, p := range map[string]Params{
		"class":       {Class: "bond"},
		"sort":        {Sort: "volume"},
		"dir":         {Dir: "up"},
		"limit high":  {Limit: 101},
		"limit neg":   {Limit: -1},
		"cursor junk": {Cursor: "zzz"},
		"cursor sort": {Sort: "ret7d", Cursor: cur},
		"cursor dir":  {Dir: "asc", Cursor: cur},
		"q too long":  {Q: strings.Repeat("a", 101)},
	} {
		_, err := svc.List(context.Background(), "u", p)
		if e, ok := apperr.As(err); !ok || e.Code != apperr.ValidationFailed {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := svc.List(context.Background(), "u", Params{Cursor: cur}); err != nil || cat.got.Cursor == nil {
		t.Fatalf("valid cursor rejected: %v", err)
	}
}

func TestWatchlistIsolationBetweenUsers(t *testing.T) {
	svc, _, _ := newSvc()
	ctx := context.Background()
	if err := svc.Star(ctx, "alice", "BBCA.JK"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Star(ctx, "alice", "BBCA.JK"); err != nil {
		t.Fatal("star must be idempotent")
	}
	a, _ := svc.List(ctx, "alice", Params{})
	b, _ := svc.List(ctx, "bob", Params{})
	if !a.Items[0].Starred || b.Items[0].Starred {
		t.Fatalf("alice=%v bob=%v", a.Items[0].Starred, b.Items[0].Starred)
	}
	if err := svc.Unstar(ctx, "bob", "BBCA.JK"); err != nil {
		t.Fatal(err)
	}
	if a, _ = svc.List(ctx, "alice", Params{}); !a.Items[0].Starred {
		t.Fatal("bob's unstar must not touch alice")
	}
	if err := svc.Star(ctx, "alice", "NOPE"); err != domain.ErrAssetNotFound {
		t.Fatal(err)
	}
}

func TestWatchlistFilterPassesUser(t *testing.T) {
	svc, cat, _ := newSvc()
	_, _ = svc.List(context.Background(), "alice", Params{Watchlist: true})
	if !cat.got.WatchlistOnly || cat.got.UserID != "alice" {
		t.Fatalf("%+v", cat.got)
	}
}

func TestFacetsLabelsAndEmptyArrays(t *testing.T) {
	svc, _, _ := newSvc()
	f, err := svc.Facets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Classes) != 2 || f.Classes[1].LabelID != "Komoditas" || f.Classes[0].LabelEN != "Stocks" || f.Exchanges[0].Count != 2 {
		t.Fatalf("%+v", f)
	}
}
