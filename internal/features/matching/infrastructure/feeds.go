package infrastructure

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"shenzen/backend/internal/features/matching/domain"
)

// Macro derives indicators from data we actually have: USD/IDR from prices, BI rate / inflation / GDP only as Indonesian headlines
// (there is no BPS or BI numeric feed in the data, so headline items carry no invented numbers).
type Macro struct{ pool *pgxpool.Pool }

func NewMacro(p *pgxpool.Pool) Macro { return Macro{p} }

var headlineIndicators = []struct {
	key   string
	label domain.Text
	re    string
}{
	{"bi_rate", domain.Text{ID: "BI Rate", EN: "BI Rate"}, biRateSQL},
	{"inflation", domain.Text{ID: "Inflasi", EN: "Inflation"}, inflationSQL},
	{"gdp", domain.Text{ID: "PDB", EN: "GDP"}, `(\mPDB\M|pertumbuhan ekonomi)`},
}

func (m Macro) Indicators(ctx context.Context, since time.Time) ([]domain.MacroIndicator, error) {
	var out []domain.MacroIndicator
	rows, err := m.pool.Query(ctx, `SELECT trade_date, close FROM prices_daily WHERE symbol = 'IDR=X' ORDER BY trade_date DESC LIMIT 2`)
	if err != nil {
		return nil, err
	}
	var d [2]time.Time
	var c [2]float64
	n := 0
	for rows.Next() && n < 2 {
		if err := rows.Scan(&d[n], &c[n]); err != nil {
			rows.Close()
			return nil, err
		}
		n++
	}
	rows.Close()
	if n == 2 && c[1] > 0 {
		chg := (c[0]/c[1] - 1) * 100
		out = append(out, domain.MacroIndicator{Key: "usd_idr", Label: domain.Text{ID: "USD/IDR", EN: "USD/IDR"}, ValueText: formatIDR(c[0]), Change: &chg, AsOf: d[0], Source: "Yahoo Finance (IDR=X)"})
	}
	for _, h := range headlineIndicators {
		var title, src string
		var at time.Time
		err := m.pool.QueryRow(ctx, `SELECT title, source, published_at FROM news_articles WHERE published_at >= $1 AND (region = 'ID' OR region IS NULL OR region = '')
			AND title ~* $2 AND title !~ $3 ORDER BY published_at DESC LIMIT 1`, since, h.re, notForeignSQL).Scan(&title, &src, &at)
		if err == nil {
			out = append(out, domain.MacroIndicator{Key: h.key, Label: h.label, Headline: title, AsOf: at, Source: src})
		}
	}
	return out, nil
}

func formatIDR(v float64) string { return domain.Rp(int64(v + 0.5)) }

// News serves recent articles for the daily content layers: those mentioning held tickers plus the latest general ones.
type News struct{ pool *pgxpool.Pool }

func NewNews(p *pgxpool.Pool) News { return News{p} }

func (n News) Recent(ctx context.Context, since time.Time, tickers []string, limit int) ([]domain.Article, error) {
	re := ""
	for _, t := range tickers { // IDX articles are not ticker-tagged: also match held symbols in the title
		if i := strings.Index(t, "."); i > 0 {
			t = t[:i]
		}
		if len(t) >= 3 {
			re += "|" + regexp.QuoteMeta(t)
		}
	}
	if re == "" {
		re = "|^$"
	}
	rows, err := n.pool.Query(ctx, `(SELECT id, source, category, title, summary, url, published_at, tickers, sentiment FROM news_articles
		   WHERE published_at >= $1 AND category <> 'global' ORDER BY published_at DESC LIMIT $3)
		UNION
		(SELECT id, source, category, title, summary, url, published_at, tickers, sentiment FROM news_articles
		   WHERE published_at >= $1 AND (tickers && $2::text[] OR title ~ ('\m(' || $4 || ')\M')) ORDER BY published_at DESC LIMIT $3)
		ORDER BY published_at DESC`, since, tickers, limit, re[1:])
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Article
	for rows.Next() {
		var a domain.Article
		if err := rows.Scan(&a.ID, &a.Source, &a.Category, &a.Title, &a.Summary, &a.URL, &a.PublishedAt, &a.Tickers, &a.Sentiment); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
