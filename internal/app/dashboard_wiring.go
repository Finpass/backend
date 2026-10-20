package app

import (
	"context"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	authhttp "shenzen/backend/internal/features/auth/transport"
	dashapp "shenzen/backend/internal/features/dashboard/application"
	dashdomain "shenzen/backend/internal/features/dashboard/domain"
	dashinfra "shenzen/backend/internal/features/dashboard/infrastructure"
	dashhttp "shenzen/backend/internal/features/dashboard/transport"
	discoverapp "shenzen/backend/internal/features/discover/application"
	discoverinfra "shenzen/backend/internal/features/discover/infrastructure"
	discoverhttp "shenzen/backend/internal/features/discover/transport"
	marketdomain "shenzen/backend/internal/features/market/domain"
	marketinfra "shenzen/backend/internal/features/market/infrastructure"
	newsapp "shenzen/backend/internal/features/news/application"
	newsdomain "shenzen/backend/internal/features/news/domain"
	newsinfra "shenzen/backend/internal/features/news/infrastructure"
	newshttp "shenzen/backend/internal/features/news/transport"
	pfapp "shenzen/backend/internal/features/portfolio/application"
	pfdomain "shenzen/backend/internal/features/portfolio/domain"
	pfinfra "shenzen/backend/internal/features/portfolio/infrastructure"
	pfhttp "shenzen/backend/internal/features/portfolio/transport"
)

// mountDashboard wires portfolio, dashboard and news. Slices only know small interfaces; the adapters below
// connect them to each other and to the market data.
func mountDashboard(r chi.Router, d Deps, authH *authhttp.Handler) {
	market := marketBridge{pool: d.Pool, repo: marketinfra.NewRepo(d.Pool)}
	pf := pfapp.NewService(pfinfra.NewRepo(d.Pool), market, newID, time.Now)
	pfhttp.New(pf, authH.RequireAuth, authhttp.UserID).Routes(r)
	d.Export.add("portfolio", func(ctx context.Context, id string) (any, error) { return pf.List(ctx, id) })

	dash := dashapp.NewService(portfolioBridge{pf}, dashinfra.NewProfileRepo(d.Pool, d.Keys), newRecommender(d), time.Now)
	dashhttp.New(dash, authH.RequireAuth, authhttp.UserID).Routes(r)

	news := newsapp.NewService(newsFeed{market.repo}, newsinfra.NewRepo(d.Pool), pf)
	newshttp.New(news, authH.RequireAuth, authhttp.UserID).Routes(r)

	discover := discoverapp.NewService(discoverinfra.NewCatalog(d.Pool), discoverinfra.NewWatchlist(d.Pool))
	discoverhttp.New(discover, authH.RequireAuth, authhttp.UserID).Routes(r)
}

// newRecommender is the seam for the matching engine: replace the rules fallback by an adapter over its service.
func newRecommender(d Deps) dashdomain.Recommender { return recommenderBridge{matching(d)} } // advice_wiring.go

// marketBridge adapts the market tables to the portfolio Market port (batch reads, one query per kind).
type marketBridge struct {
	pool *pgxpool.Pool
	repo marketinfra.Repo
}

func (m marketBridge) Search(ctx context.Context, q, class string, limit int) ([]pfdomain.AssetHit, error) {
	hits, err := m.repo.SearchAssets(ctx, q, class, limit)
	if err != nil {
		return nil, err
	}
	out := make([]pfdomain.AssetHit, len(hits))
	for i, h := range hits {
		out[i] = pfdomain.AssetHit{Symbol: h.Symbol, Name: h.Name, AssetClass: h.AssetClass, Currency: h.Currency, Exchange: h.Exchange}
		if h.LastPrice != nil {
			out[i].LastPrice = *h.LastPrice
		}
		if h.LastTradeDate != nil {
			out[i].LastTradeDate = h.LastTradeDate.Format("2006-01-02")
		}
	}
	return out, nil
}

func (m marketBridge) Assets(ctx context.Context, symbols []string) (map[string]pfdomain.AssetInfo, error) {
	rows, err := m.pool.Query(ctx, `SELECT symbol, name, asset_class, COALESCE(currency, ''), COALESCE(exchange, '') FROM assets WHERE symbol = ANY($1)`, symbols)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]pfdomain.AssetInfo{}
	for rows.Next() {
		var a pfdomain.AssetInfo
		if err := rows.Scan(&a.Symbol, &a.Name, &a.AssetClass, &a.Currency, &a.Exchange); err != nil {
			return nil, err
		}
		out[a.Symbol] = a
	}
	return out, rows.Err()
}

func (m marketBridge) Series(ctx context.Context, symbols []string, from time.Time) (map[string][]pfdomain.Point, error) {
	rows, err := m.pool.Query(ctx, `SELECT symbol, trade_date, close FROM prices_daily WHERE symbol = ANY($1) AND trade_date >= $2 ORDER BY symbol, trade_date`, symbols, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]pfdomain.Point{}
	for rows.Next() {
		var s string
		var p pfdomain.Point
		if err := rows.Scan(&s, &p.Date, &p.Value); err != nil {
			return nil, err
		}
		p.Date = p.Date.UTC()
		out[s] = append(out[s], p)
	}
	return out, rows.Err()
}

func (m marketBridge) Dividends(ctx context.Context, symbols []string) (map[string][]pfdomain.Dividend, error) {
	rows, err := m.pool.Query(ctx, `SELECT d.symbol, d.ex_date, d.amount, COALESCE(d.currency, a.currency, '') FROM dividends d JOIN assets a USING (symbol) WHERE d.symbol = ANY($1)`, symbols)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]pfdomain.Dividend{}
	for rows.Next() {
		var s string
		var v pfdomain.Dividend
		if err := rows.Scan(&s, &v.ExDate, &v.Amount, &v.Currency); err != nil {
			return nil, err
		}
		v.ExDate = v.ExDate.UTC()
		out[s] = append(out[s], v)
	}
	return out, rows.Err()
}

func (m marketBridge) USDIDR(ctx context.Context, from time.Time) ([]pfdomain.Point, error) {
	s, err := m.Series(ctx, []string{"IDR=X"}, from)
	return s["IDR=X"], err
}

// portfolioBridge adapts the portfolio service to the dashboard Portfolio port (the types have identical fields).
type portfolioBridge struct{ svc *pfapp.Service }

func (b portfolioBridge) Snapshot(ctx context.Context, userID string, from time.Time) (dashdomain.Snapshot, error) {
	s, err := b.svc.Snapshot(ctx, userID, from)
	if err != nil {
		return dashdomain.Snapshot{}, err
	}
	out := dashdomain.Snapshot{Holdings: make([]dashdomain.Holding, len(s.Holdings)), History: make([]dashdomain.DayPoint, len(s.History))}
	for i, h := range s.Holdings {
		out.Holdings[i] = dashdomain.Holding(h)
	}
	for i, p := range s.History {
		out.History[i] = dashdomain.DayPoint(p)
	}
	return out, nil
}

// newsFeed adapts market.Repo.ListNews to the news Feed port.
type newsFeed struct{ repo marketinfra.Repo }

func (f newsFeed) List(ctx context.Context, fl newsdomain.Filter) (newsdomain.Page, error) {
	p, err := f.repo.ListNews(ctx, marketdomainFilter(fl), fl.Cursor)
	if err != nil {
		return newsdomain.Page{}, err
	}
	out := newsdomain.Page{NextCursor: p.NextCursor, Items: make([]newsdomain.Article, len(p.Items))}
	for i, a := range p.Items {
		t := a.Tickers
		if t == nil {
			t = []string{}
		}
		out.Items[i] = newsdomain.Article{ID: a.ID, Source: a.Source, Category: a.Category, Title: a.Title, Summary: a.Summary, URL: a.URL,
			PublishedAt: a.PublishedAt.UTC().Format(time.RFC3339), Tickers: t, Sentiment: newsdomain.SentimentLabel(a.Sentiment)}
	}
	return out, nil
}

func (f newsFeed) LastUpdated(ctx context.Context) (time.Time, error) {
	return f.repo.NewsLastUpdated(ctx)
}

func marketdomainFilter(f newsdomain.Filter) marketdomain.NewsFilter {
	return marketdomain.NewsFilter{Category: f.Category, Q: f.Q, Limit: f.Limit}
}
