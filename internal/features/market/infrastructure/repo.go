// Package infrastructure holds the Postgres read side of the market slice plus the classifier runner.
package infrastructure

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"shenzen/backend/internal/features/market/domain"
)

type Repo struct{ pool *pgxpool.Pool }

func NewRepo(p *pgxpool.Pool) Repo { return Repo{p} }

func mapErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return err
}

func (r Repo) GetAsset(ctx context.Context, symbol string) (domain.Asset, error) {
	var a domain.Asset
	err := r.pool.QueryRow(ctx, `SELECT symbol, name, asset_class, coalesce(currency,''), coalesce(exchange,''), coalesce(country,''), has_metadata
		FROM assets WHERE symbol = $1`, symbol).Scan(&a.Symbol, &a.Name, &a.AssetClass, &a.Currency, &a.Exchange, &a.Country, &a.HasMetadata)
	return a, mapErr(err)
}

func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// SearchAssets finds assets by symbol or name (case-insensitive substring, trigram indexed).
// class filters on the source class (equity, etf, gold, commodity, crypto) or the classified class
// (equity_large_cap, ...); "" or "all" means any. Exact symbol, symbol prefix and name prefix rank first.
// Assets with a price history rank above price-less ones.
func (r Repo) SearchAssets(ctx context.Context, q, class string, limit int) ([]domain.AssetHit, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if class == "all" {
		class = ""
	}
	q = strings.ToLower(strings.TrimSpace(q))
	esc := likeEscape(q)
	rows, err := r.pool.Query(ctx, `
		WITH m AS (
		  SELECT a.symbol, a.name, a.asset_class, coalesce(a.currency,'') currency, coalesce(a.exchange,'') exchange, coalesce(a.country,'') country,
		         a.has_metadata, coalesce(c.asset_class,'') classified,
		         CASE WHEN lower(a.symbol) = $1 THEN 0 WHEN lower(a.symbol) LIKE $2 || '%' THEN 1 WHEN lower(a.name) LIKE $2 || '%' THEN 2 ELSE 3 END rank
		  FROM assets a LEFT JOIN asset_classifications c ON c.symbol = a.symbol
		  WHERE ($1 = '' OR lower(a.symbol) LIKE '%' || $2 || '%' OR lower(a.name) LIKE '%' || $2 || '%')
		    AND ($3 = '' OR a.asset_class = $3 OR c.asset_class = $3)
		  ORDER BY rank, a.has_metadata DESC, a.symbol
		  LIMIT $4)
		SELECT m.symbol, m.name, m.asset_class, m.currency, m.exchange, m.country, m.has_metadata, m.classified, p.close, p.trade_date
		FROM m LEFT JOIN LATERAL (SELECT close, trade_date FROM prices_daily WHERE symbol = m.symbol ORDER BY trade_date DESC LIMIT 1) p ON TRUE
		ORDER BY (p.close IS NULL), m.rank, m.has_metadata DESC, m.symbol`, q, esc, class, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.AssetHit{}
	for rows.Next() {
		var h domain.AssetHit
		if err := rows.Scan(&h.Symbol, &h.Name, &h.AssetClass, &h.Currency, &h.Exchange, &h.Country, &h.HasMetadata, &h.Classified, &h.LastPrice, &h.LastTradeDate); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

const priceCols = `symbol, trade_date, open, high, low, close, volume`

func scanPrice(row pgx.Row) (domain.Price, error) {
	var p domain.Price
	err := row.Scan(&p.Symbol, &p.Date, &p.Open, &p.High, &p.Low, &p.Close, &p.Volume)
	return p, err
}

func (r Repo) LatestPrice(ctx context.Context, symbol string) (domain.Price, error) {
	p, err := scanPrice(r.pool.QueryRow(ctx, `SELECT `+priceCols+` FROM prices_daily WHERE symbol = $1 ORDER BY trade_date DESC LIMIT 1`, symbol))
	return p, mapErr(err)
}

// LatestPrices returns the newest row per symbol; symbols without prices are absent from the map.
func (r Repo) LatestPrices(ctx context.Context, symbols []string) (map[string]domain.Price, error) {
	rows, err := r.pool.Query(ctx, `SELECT p.symbol, p.trade_date, p.open, p.high, p.low, p.close, p.volume
		FROM unnest($1::text[]) s(symbol)
		CROSS JOIN LATERAL (SELECT * FROM prices_daily WHERE symbol = s.symbol ORDER BY trade_date DESC LIMIT 1) p`, symbols)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]domain.Price, len(symbols))
	for rows.Next() {
		p, err := scanPrice(rows)
		if err != nil {
			return nil, err
		}
		out[p.Symbol] = p
	}
	return out, rows.Err()
}

// PriceSeries returns rows ascending by date between from and to inclusive; a zero time is open-ended.
func (r Repo) PriceSeries(ctx context.Context, symbol string, from, to time.Time) ([]domain.Price, error) {
	var f, t any
	if !from.IsZero() {
		f = from
	}
	if !to.IsZero() {
		t = to
	}
	rows, err := r.pool.Query(ctx, `SELECT `+priceCols+` FROM prices_daily
		WHERE symbol = $1 AND ($2::date IS NULL OR trade_date >= $2) AND ($3::date IS NULL OR trade_date <= $3) ORDER BY trade_date`, symbol, f, t)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Price{}
	for rows.Next() {
		p, err := scanPrice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// FXRate returns how many units of `to` one unit of `from` buys, using the last close on or before
// date (zero = latest). Rates come from Yahoo "<CCY>=X" symbols (units of CCY per USD); the data
// currently has only IDR=X, so pairs involving other currencies return ErrNotFound.
func (r Repo) FXRate(ctx context.Context, from, to string, date time.Time) (float64, error) {
	from, to = strings.ToUpper(from), strings.ToUpper(to)
	if from == to {
		return 1, nil
	}
	perUSD := func(ccy string) (float64, error) {
		if ccy == "USD" {
			return 1, nil
		}
		var d any
		if !date.IsZero() {
			d = date
		}
		var v float64
		err := r.pool.QueryRow(ctx, `SELECT close FROM prices_daily WHERE symbol = $1 AND ($2::date IS NULL OR trade_date <= $2) ORDER BY trade_date DESC LIMIT 1`, ccy+"=X", d).Scan(&v)
		return v, mapErr(err)
	}
	f, err := perUSD(from)
	if err != nil {
		return 0, err
	}
	t, err := perUSD(to)
	if err != nil {
		return 0, err
	}
	return t / f, nil
}

func (r Repo) Classification(ctx context.Context, symbol string) (domain.Classification, error) {
	var c domain.Classification
	var trace []byte
	err := r.pool.QueryRow(ctx, `SELECT symbol, asset_class, sector, sharia_status, geography, volatility_class, liquidity_class, annualized_vol,
		avg_daily_value_idr, market_cap_idr, alert_threshold_pct, trace, classifier_version, classified_at FROM asset_classifications WHERE symbol = $1`, symbol).
		Scan(&c.Symbol, &c.AssetClass, &c.Sector, &c.ShariaStatus, &c.Geography, &c.Volatility, &c.Liquidity, &c.AnnualizedVol,
			&c.AvgDailyValueIDR, &c.MarketCapIDR, &c.AlertThresholdPc, &trace, &c.Version, &c.ClassifiedAt)
	if err != nil {
		return c, mapErr(err)
	}
	return c, json.Unmarshal(trace, &c.Trace)
}

func (r Repo) Dividends(ctx context.Context, symbol string, since time.Time) ([]domain.Dividend, error) {
	rows, err := r.pool.Query(ctx, `SELECT symbol, ex_date, amount, coalesce(currency,'') FROM dividends WHERE symbol = $1 AND ex_date >= $2 ORDER BY ex_date`, symbol, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Dividend{}
	for rows.Next() {
		var d domain.Dividend
		if err := rows.Scan(&d.Symbol, &d.ExDate, &d.Amount, &d.Currency); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// LatestYields returns the newest curve (one point per tenor, ascending tenor).
func (r Repo) LatestYields(ctx context.Context) ([]domain.YieldPoint, error) {
	rows, err := r.pool.Query(ctx, `SELECT trade_date, tenor, tenor_months::float8, yield_pct::float8 FROM treasury_yields
		WHERE trade_date = (SELECT max(trade_date) FROM treasury_yields) ORDER BY tenor_months`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.YieldPoint{}
	for rows.Next() {
		var y domain.YieldPoint
		if err := rows.Scan(&y.TradeDate, &y.Tenor, &y.TenorMonths, &y.YieldPct); err != nil {
			return nil, err
		}
		out = append(out, y)
	}
	return out, rows.Err()
}

func (r Repo) Fundamentals(ctx context.Context, symbol string) (domain.Fundamentals, error) {
	var f domain.Fundamentals
	err := r.pool.QueryRow(ctx, `SELECT symbol, coalesce(sector,''), coalesce(industry,''), market_cap, info, statements FROM fundamentals WHERE symbol = $1`, symbol).
		Scan(&f.Symbol, &f.Sector, &f.Industry, &f.MarketCap, &f.Info, &f.Statements)
	return f, mapErr(err)
}

// ListNews returns newest-first articles. cursor is the opaque NextCursor of the previous page.
func (r Repo) ListNews(ctx context.Context, f domain.NewsFilter, cursor string) (domain.NewsPage, error) {
	limit := f.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var cat any
	if f.Category != "" && f.Category != "all" {
		if !domain.ValidNewsCategory(f.Category) {
			return domain.NewsPage{}, fmt.Errorf("invalid category %q", f.Category)
		}
		cat = f.Category
	}
	var q any
	if s := strings.TrimSpace(f.Q); s != "" {
		q = s
	}
	var tickers any
	if len(f.Tickers) > 0 {
		tickers = f.Tickers
	}
	var curTS, curID any
	if cursor != "" {
		ts, id, err := decodeCursor(cursor)
		if err != nil {
			return domain.NewsPage{}, fmt.Errorf("invalid cursor")
		}
		curTS, curID = ts, id
	}
	rows, err := r.pool.Query(ctx, `SELECT id, source, channel, category, title, summary, url, published_at, coalesce(language,''), coalesce(region,''), tickers, sentiment
		FROM news_articles
		WHERE ($1::text IS NULL OR category = $1)
		  AND ($2::text IS NULL OR search @@ websearch_to_tsquery('simple', $2))
		  AND ($3::text[] IS NULL OR tickers && $3)
		  AND ($4::timestamptz IS NULL OR (published_at, id) < ($4, $5))
		ORDER BY published_at DESC, id DESC LIMIT $6`, cat, q, tickers, curTS, curID, limit+1)
	if err != nil {
		return domain.NewsPage{}, err
	}
	defer rows.Close()
	page := domain.NewsPage{Items: []domain.Article{}}
	for rows.Next() {
		var a domain.Article
		if err := rows.Scan(&a.ID, &a.Source, &a.Channel, &a.Category, &a.Title, &a.Summary, &a.URL, &a.PublishedAt, &a.Language, &a.Region, &a.Tickers, &a.Sentiment); err != nil {
			return page, err
		}
		a.PublishedAt = a.PublishedAt.UTC()
		page.Items = append(page.Items, a)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		last := page.Items[limit-1]
		page.NextCursor = encodeCursor(last.PublishedAt, last.ID)
	}
	return page, nil
}

// NewsLastUpdated is the newest published_at (the "last updated" time of the feed).
func (r Repo) NewsLastUpdated(ctx context.Context) (time.Time, error) {
	var t *time.Time
	if err := r.pool.QueryRow(ctx, `SELECT max(published_at) FROM news_articles`).Scan(&t); err != nil || t == nil {
		return time.Time{}, domain.ErrNotFound
	}
	return t.UTC(), nil
}

// GetArticle returns one article by id.
func (r Repo) GetArticle(ctx context.Context, id string) (domain.Article, error) {
	var a domain.Article
	err := r.pool.QueryRow(ctx, `SELECT id, source, channel, category, title, summary, url, published_at, coalesce(language,''), coalesce(region,''), tickers, sentiment
		FROM news_articles WHERE id = $1`, id).Scan(&a.ID, &a.Source, &a.Channel, &a.Category, &a.Title, &a.Summary, &a.URL, &a.PublishedAt, &a.Language, &a.Region, &a.Tickers, &a.Sentiment)
	a.PublishedAt = a.PublishedAt.UTC()
	return a, mapErr(err)
}

func encodeCursor(t time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(t.UnixMicro(), 10) + "|" + id))
}

func decodeCursor(c string) (time.Time, string, error) {
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return time.Time{}, "", err
	}
	ts, id, ok := strings.Cut(string(b), "|")
	us, err := strconv.ParseInt(ts, 10, 64)
	if !ok || err != nil || id == "" {
		return time.Time{}, "", errors.New("bad cursor")
	}
	return time.UnixMicro(us).UTC(), id, nil
}
