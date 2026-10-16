// Package domain defines the news feed read model, bookmarks and the holdings relevance rule.
package domain

import (
	"context"
	"strings"
	"time"

	"shenzen/backend/internal/shared/apperr"
)

var (
	ErrArticleNotFound = apperr.New(apperr.NotFound, "Article not found")
	Categories         = []string{"all", "capital_markets", "macro", "global", "real_sector"}
)

type Article struct {
	ID                 string   `json:"id"`
	Source             string   `json:"source"`
	Category           string   `json:"category"`
	Title              string   `json:"title"`
	Summary            string   `json:"summary"`
	URL                string   `json:"url"`
	PublishedAt        string   `json:"publishedAt"`
	Tickers            []string `json:"tickers"`
	Sentiment          *string  `json:"sentiment"`
	RelevantToHoldings bool     `json:"relevantToHoldings"`
	Bookmarked         bool     `json:"bookmarked"`
}

type Filter struct {
	Category string
	Q        string
	Limit    int
	Cursor   string
}

type Page struct {
	Items      []Article
	NextCursor string
}

// Feed is the market news read side (adapter over the market slice).
type Feed interface {
	List(ctx context.Context, f Filter) (Page, error)
	LastUpdated(ctx context.Context) (time.Time, error)
}

type Bookmarks interface {
	Add(ctx context.Context, userID, articleID string) error // ErrArticleNotFound for an unknown article; idempotent
	Remove(ctx context.Context, userID, articleID string) error
	Marked(ctx context.Context, userID string, ids []string) (map[string]bool, error)
	List(ctx context.Context, userID string, limit int) ([]Article, error) // newest bookmark first
}

// Holdings is implemented by an adapter over the portfolio slice.
type Holdings interface {
	Symbols(ctx context.Context, userID string) ([]string, error)
}

// SentimentLabel maps the numeric score of the source (-1..1) to positive/neutral/negative; nil stays nil.
func SentimentLabel(s *float64) *string {
	if s == nil {
		return nil
	}
	l := "neutral"
	if *s >= 0.15 {
		l = "positive"
	} else if *s <= -0.15 {
		l = "negative"
	}
	return &l
}

func base(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	if i := strings.IndexByte(s, '.'); i > 0 {
		return s[:i]
	}
	return s
}

// Relevant reports whether an article mentions any held symbol ("BBCA.JK" matches the ticker "BBCA" and the reverse).
func Relevant(tickers, symbols []string) bool {
	if len(tickers) == 0 || len(symbols) == 0 {
		return false
	}
	held := make(map[string]struct{}, len(symbols)*2)
	for _, s := range symbols {
		held[strings.ToUpper(s)] = struct{}{}
		held[base(s)] = struct{}{}
	}
	for _, t := range tickers {
		if _, ok := held[strings.ToUpper(t)]; ok {
			return true
		}
		if _, ok := held[base(t)]; ok {
			return true
		}
	}
	return false
}
