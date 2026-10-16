package infrastructure

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"shenzen/backend/internal/features/market/domain"
)

func TestCursorRoundTrip(t *testing.T) {
	ts := time.Date(2026, 10, 16, 7, 16, 31, 123456000, time.UTC)
	got, id, err := decodeCursor(encodeCursor(ts, "abc|def"))
	if err != nil || !got.Equal(ts) || id != "abc|def" {
		t.Fatalf("%v %v %v", got, id, err)
	}
	for _, bad := range []string{"!!!", "bm9waXBl", ""} {
		if _, _, err := decodeCursor(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}

// Integration tests run against the local database loaded by `make import-market classify-assets`;
// they skip when it is unreachable or empty.
func testRepo(t *testing.T) (Repo, *pgxpool.Pool) {
	t.Helper()
	_ = godotenv.Load("../../../../.env")
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Skip(err)
	}
	t.Cleanup(pool.Close)
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM assets`).Scan(&n); err != nil || n == 0 {
		t.Skip("market data not loaded")
	}
	return NewRepo(pool), pool
}

func TestRepoAssetsAndPrices(t *testing.T) {
	r, _ := testRepo(t)
	ctx := context.Background()
	a, err := r.GetAsset(ctx, "BBCA.JK")
	if err != nil || a.Exchange != "JKT" || a.Currency != "IDR" {
		t.Fatalf("%+v %v", a, err)
	}
	if _, err := r.GetAsset(ctx, "NOPE.XX"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	hits, err := r.SearchAssets(ctx, "bank central asia", "", 5)
	if err != nil || len(hits) == 0 || hits[0].Symbol != "BBCA.JK" || hits[0].LastPrice == nil {
		t.Fatalf("search by name: %+v %v", hits, err)
	}
	if hits, _ = r.SearchAssets(ctx, "bbca", "equity_large_cap", 5); len(hits) == 0 || hits[0].Symbol != "BBCA.JK" {
		t.Fatalf("search by symbol+classified class: %+v", hits)
	}
	if hits, _ = r.SearchAssets(ctx, "100%_", "", 5); len(hits) > 5 {
		t.Fatalf("limit")
	}
	if hits, _ = r.SearchAssets(ctx, "bitcoin", "crypto", 5); len(hits) == 0 || hits[0].Symbol != "bitcoin" {
		t.Fatalf("crypto search: %+v", hits)
	}
	p, err := r.LatestPrice(ctx, "BBCA.JK")
	if err != nil || p.Close <= 0 || p.Date.IsZero() {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err := r.LatestPrice(ctx, "NOPE.XX"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("want ErrNotFound")
	}
	m, err := r.LatestPrices(ctx, []string{"BBCA.JK", "AAPL", "NOPE.XX"})
	if err != nil || len(m) != 2 {
		t.Fatalf("%v %v", m, err)
	}
	from, to := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)
	s, err := r.PriceSeries(ctx, "BBCA.JK", from, to)
	if err != nil || len(s) < 40 {
		t.Fatalf("series %d %v", len(s), err)
	}
	for i := range s {
		if s[i].Date.Before(from) || s[i].Date.After(to) || (i > 0 && !s[i].Date.After(s[i-1].Date)) {
			t.Fatalf("series not ascending within range at %d", i)
		}
	}
	if all, _ := r.PriceSeries(ctx, "BBCA.JK", time.Time{}, time.Time{}); len(all) < len(s) {
		t.Fatalf("open-ended series")
	}
}

func TestRepoFXClassificationAux(t *testing.T) {
	r, _ := testRepo(t)
	ctx := context.Background()
	usdidr, err := r.FXRate(ctx, "USD", "IDR", time.Time{})
	if err != nil || usdidr < 10000 {
		t.Fatalf("usd/idr %v %v", usdidr, err)
	}
	inv, _ := r.FXRate(ctx, "idr", "usd", time.Time{})
	if d := inv*usdidr - 1; d > 1e-9 || d < -1e-9 {
		t.Fatalf("inverse %v", inv)
	}
	if one, _ := r.FXRate(ctx, "EUR", "EUR", time.Time{}); one != 1 {
		t.Fatalf("identity")
	}
	if _, err := r.FXRate(ctx, "EUR", "IDR", time.Time{}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("EUR has no rate in the data: %v", err)
	}
	old, err := r.FXRate(ctx, "USD", "IDR", time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC))
	if err != nil || old == usdidr {
		t.Fatalf("dated rate %v %v", old, err)
	}
	c, err := r.Classification(ctx, "BBCA.JK")
	if err != nil || c.AssetClass != "equity_large_cap" || len(c.Trace) != 6 || c.Version == "" {
		t.Fatalf("%+v %v", c, err)
	}
	if _, err := r.Classification(ctx, "NOPE.XX"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("want ErrNotFound")
	}
	if y, err := r.LatestYields(ctx); err != nil || len(y) != 14 {
		t.Fatalf("yields %d %v", len(y), err)
	}
	if f, err := r.Fundamentals(ctx, "BBCA.JK"); err != nil || f.MarketCap == nil || len(f.Info) == 0 {
		t.Fatalf("fundamentals %+v %v", f, err)
	}
	if d, err := r.Dividends(ctx, "BBCA.JK", time.Time{}); err != nil || len(d) == 0 {
		t.Fatalf("dividends %v %v", d, err)
	}
}

func TestRepoNews(t *testing.T) {
	r, _ := testRepo(t)
	ctx := context.Background()
	p1, err := r.ListNews(ctx, domain.NewsFilter{Limit: 10}, "")
	if err != nil || len(p1.Items) != 10 || p1.NextCursor == "" {
		t.Fatalf("%d %q %v", len(p1.Items), p1.NextCursor, err)
	}
	p2, err := r.ListNews(ctx, domain.NewsFilter{Limit: 10}, p1.NextCursor)
	if err != nil || len(p2.Items) != 10 {
		t.Fatalf("page 2: %v", err)
	}
	if !p2.Items[0].PublishedAt.Before(p1.Items[9].PublishedAt) && !(p2.Items[0].PublishedAt.Equal(p1.Items[9].PublishedAt) && p2.Items[0].ID < p1.Items[9].ID) {
		t.Fatal("page 2 must continue after page 1")
	}
	for _, c := range []string{domain.CatCapitalMarkets, domain.CatMacro, domain.CatGlobal, domain.CatRealSector} {
		pg, err := r.ListNews(ctx, domain.NewsFilter{Category: c, Limit: 5}, "")
		if err != nil || len(pg.Items) == 0 {
			t.Fatalf("%s: %d %v", c, len(pg.Items), err)
		}
		for _, a := range pg.Items {
			if a.Category != c {
				t.Fatalf("category leak %s in %s", a.Category, c)
			}
		}
	}
	pg, err := r.ListNews(ctx, domain.NewsFilter{Tickers: []string{"NVDA"}, Limit: 5}, "")
	if err != nil || len(pg.Items) == 0 {
		t.Fatalf("tickers: %v", err)
	}
	for _, a := range pg.Items {
		if !contains(a.Tickers, "NVDA") {
			t.Fatalf("ticker filter leak %v", a.Tickers)
		}
	}
	pg, err = r.ListNews(ctx, domain.NewsFilter{Q: "IHSG", Limit: 5}, "")
	if err != nil || len(pg.Items) == 0 {
		t.Fatalf("fulltext: %v", err)
	}
	if _, err := r.ListNews(ctx, domain.NewsFilter{Category: "bogus"}, ""); err == nil {
		t.Fatal("bad category must error")
	}
	if _, err := r.ListNews(ctx, domain.NewsFilter{}, "garbage"); err == nil {
		t.Fatal("bad cursor must error")
	}
	if ts, err := r.NewsLastUpdated(ctx); err != nil || ts.IsZero() {
		t.Fatalf("last updated %v %v", ts, err)
	}
	if a, err := r.GetArticle(ctx, p1.Items[0].ID); err != nil || a.URL != p1.Items[0].URL {
		t.Fatalf("get article %v", err)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// The classifier runner is deterministic: running it twice yields the same labels.
func TestClassifyAllIdempotent(t *testing.T) {
	_, pool := testRepo(t)
	ctx := context.Background()
	read := func() string {
		var s string
		if err := pool.QueryRow(ctx, `SELECT md5(string_agg(symbol||asset_class||sector||sharia_status||geography||volatility_class||liquidity_class, ',' ORDER BY symbol)) FROM asset_classifications`).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	before := read()
	if n, err := ClassifyAll(ctx, pool, nil); err != nil || n == 0 {
		t.Fatalf("%d %v", n, err)
	}
	if after := read(); after != before {
		t.Fatal("re-run changed labels")
	}
	var missing int
	pool.QueryRow(ctx, `SELECT count(*) FROM assets a WHERE NOT EXISTS (SELECT 1 FROM asset_classifications c WHERE c.symbol=a.symbol)`).Scan(&missing)
	if missing != 0 {
		t.Fatalf("%d assets unclassified", missing)
	}
}
