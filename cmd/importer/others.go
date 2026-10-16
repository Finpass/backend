package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"shenzen/backend/internal/features/market/domain"
)

func knownSymbols(ctx context.Context, pool *pgxpool.Pool) (map[string]bool, error) {
	rows, err := pool.Query(ctx, `SELECT symbol FROM assets`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]bool{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		m[s] = true
	}
	return m, rows.Err()
}

// replaceTable loads rows into a table inside one transaction: truncate then COPY (idempotent, atomic).
func replaceTable(ctx context.Context, pool *pgxpool.Pool, table string, cols []string, rows [][]any) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM `+table); err != nil {
		return err
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{table}, cols, pgx.CopyFromRows(rows)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func loadDividends(ctx context.Context, pool *pgxpool.Pool, dataDir string, st *stats) error {
	known, err := knownSymbols(ctx, pool)
	if err != nil {
		return err
	}
	type rec struct {
		Symbol   string   `json:"symbol"`
		ExDate   string   `json:"ex_date"`
		Amount   *float64 `json:"amount"`
		Currency string   `json:"currency"`
		Source   string   `json:"source"`
	}
	seen := map[string]bool{}
	var rows [][]any
	err = streamArray(filepath.Join(dataDir, "market/dividends.json"), func(r rec) error {
		d, perr := time.Parse("2006-01-02", r.ExDate)
		switch {
		case !known[r.Symbol]:
			st.skip("dividends", "symbol not in assets")
		case r.Amount == nil || *r.Amount <= 0:
			st.skip("dividends", "amount null or <= 0")
		case perr != nil:
			st.skip("dividends", "bad ex_date")
		case seen[r.Symbol+"|"+r.ExDate]:
			st.skip("dividends", "duplicate (symbol, ex_date)")
		default:
			seen[r.Symbol+"|"+r.ExDate] = true
			rows = append(rows, []any{r.Symbol, d, *r.Amount, nz(r.Currency), orDefault(r.Source, "unknown")})
		}
		return nil
	})
	if err != nil {
		return err
	}
	st.add("dividends.rows", int64(len(rows)))
	return replaceTable(ctx, pool, "dividends", []string{"symbol", "ex_date", "amount", "currency", "source"}, rows)
}

var tenorMonths = map[string]float64{"1M": 1, "1_5M": 1.5, "2M": 2, "3M": 3, "4M": 4, "6M": 6, "1Y": 12, "2Y": 24, "3Y": 36, "5Y": 60, "7Y": 84, "10Y": 120, "20Y": 240, "30Y": 360}

func loadYields(ctx context.Context, pool *pgxpool.Pool, dataDir string, st *stats) error {
	type rec struct {
		Date  string   `json:"trade_date"`
		Tenor string   `json:"tenor"`
		Yield *float64 `json:"yield_pct"`
	}
	seen := map[string]bool{}
	var rows [][]any
	err := streamArray(filepath.Join(dataDir, "market/treasury_yields.json"), func(r rec) error {
		d, perr := time.Parse("2006-01-02", r.Date)
		var tm any
		if m, ok := tenorMonths[r.Tenor]; ok {
			tm = m
		}
		switch {
		case perr != nil || r.Yield == nil:
			st.skip("treasury_yields", "bad date or null yield")
		case seen[r.Date+"|"+r.Tenor]:
			st.skip("treasury_yields", "duplicate (date, tenor)")
		default:
			seen[r.Date+"|"+r.Tenor] = true
			rows = append(rows, []any{d, r.Tenor, tm, *r.Yield})
		}
		return nil
	})
	if err != nil {
		return err
	}
	st.add("treasury_yields.rows", int64(len(rows)))
	return replaceTable(ctx, pool, "treasury_yields", []string{"trade_date", "tenor", "tenor_months", "yield_pct"}, rows)
}

func loadFundamentals(ctx context.Context, pool *pgxpool.Pool, dataDir string, st *stats) error {
	known, err := knownSymbols(ctx, pool)
	if err != nil {
		return err
	}
	var rows [][]any
	err = streamObject(filepath.Join(dataDir, "fundamentals/id_fundamentals.json"), func(sym string, dec *json.Decoder) error {
		var v struct {
			Info       json.RawMessage `json:"info"`
			Statements json.RawMessage `json:"statements"`
		}
		if err := dec.Decode(&v); err != nil {
			return err
		}
		if !known[sym] {
			st.skip("fundamentals", "symbol not in assets")
			return nil
		}
		var info struct {
			Sector    *string  `json:"sector"`
			Industry  *string  `json:"industry"`
			MarketCap *float64 `json:"marketCap"`
		}
		_ = json.Unmarshal(v.Info, &info)
		if len(v.Statements) == 0 {
			v.Statements = json.RawMessage(`{}`)
		}
		rows = append(rows, []any{sym, info.Sector, info.Industry, info.MarketCap, string(v.Info), string(v.Statements)})
		return nil
	})
	if err != nil {
		return err
	}
	// sec_metrics.json is `{}` in the current data: nothing to load.
	st.add("fundamentals.rows", int64(len(rows)))
	return replaceTable(ctx, pool, "fundamentals", []string{"symbol", "sector", "industry", "market_cap", "info", "statements"}, rows)
}

func loadNews(ctx context.Context, pool *pgxpool.Pool, dataDir string, st *stats) error {
	type rec struct {
		ID              string          `json:"id"`
		Source          string          `json:"source"`
		Channel         string          `json:"channel"`
		Title           string          `json:"title"`
		Summary         string          `json:"summary"`
		URL             string          `json:"url"`
		PublishedAt     string          `json:"published_at"`
		Language        string          `json:"language"`
		Region          string          `json:"region"`
		Tickers         []string        `json:"tickers"`
		Sentiment       json.RawMessage `json:"sentiment"`
		TickerSentiment json.RawMessage `json:"ticker_sentiment"`
	}
	ids, urls := map[string]bool{}, map[string]bool{}
	var rows [][]any
	err := streamArray(filepath.Join(dataDir, "news/articles.json"), func(r rec) error {
		ts, perr := time.Parse(time.RFC3339Nano, r.PublishedAt)
		switch {
		case r.ID == "" || r.Title == "" || r.URL == "" || perr != nil:
			st.skip("news", "missing id/title/url or bad published_at")
			return nil
		case ids[r.ID] || urls[r.URL]:
			st.skip("news", "duplicate id or url")
			return nil
		}
		ids[r.ID], urls[r.URL] = true, true
		var sent any
		var fv float64
		if json.Unmarshal(r.Sentiment, &fv) == nil && len(r.Sentiment) > 0 && string(r.Sentiment) != "null" {
			sent = fv
		}
		tsent := "{}"
		if len(r.TickerSentiment) > 0 && string(r.TickerSentiment) != "null" {
			tsent = string(r.TickerSentiment)
		}
		if r.Tickers == nil {
			r.Tickers = []string{}
		}
		rows = append(rows, []any{r.ID, r.Source, r.Channel, domain.NewsCategory(r.Source, r.Channel, r.Region, r.Title), r.Title, r.Summary, r.URL, ts.UTC(), nz(r.Language), nz(r.Region), r.Tickers, sent, tsent})
		return nil
	})
	if err != nil {
		return err
	}
	st.add("news_articles.rows", int64(len(rows)))
	return replaceTable(ctx, pool, "news_articles", []string{"id", "source", "channel", "category", "title", "summary", "url", "published_at", "language", "region", "tickers", "sentiment", "ticker_sentiment"}, rows)
}
