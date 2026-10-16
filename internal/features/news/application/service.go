// Package application serves the news feed with per-user bookmark and holdings relevance flags.
package application

import (
	"context"
	"encoding/base64"
	"slices"
	"strings"
	"time"

	"shenzen/backend/internal/features/news/domain"
	"shenzen/backend/internal/shared/apperr"
)

type Service struct {
	feed      domain.Feed
	bookmarks domain.Bookmarks
	holdings  domain.Holdings
}

func NewService(f domain.Feed, b domain.Bookmarks, h domain.Holdings) *Service {
	return &Service{feed: f, bookmarks: b, holdings: h}
}

type Feed struct {
	Items         []domain.Article `json:"items"`
	NextCursor    *string          `json:"nextCursor"`
	LastUpdatedAt string           `json:"lastUpdatedAt"`
}

func (s *Service) List(ctx context.Context, userID string, f domain.Filter) (Feed, error) {
	if f.Category == "" {
		f.Category = "all"
	}
	if !slices.Contains(domain.Categories, f.Category) {
		return Feed{}, apperr.Validation(apperr.Detail{Field: "category", Message: "must be one of all, capital_markets, macro, global, real_sector"})
	}
	if f.Cursor != "" && !validCursor(f.Cursor) {
		return Feed{}, apperr.Validation(apperr.Detail{Field: "cursor", Message: "invalid cursor"})
	}
	if f.Limit < 1 || f.Limit > 100 {
		f.Limit = 20
	}
	page, err := s.feed.List(ctx, f)
	if err != nil {
		return Feed{}, err
	}
	out := Feed{Items: page.Items}
	if out.Items == nil {
		out.Items = []domain.Article{}
	}
	if page.NextCursor != "" {
		out.NextCursor = &page.NextCursor
	}
	if t, err := s.feed.LastUpdated(ctx); err == nil && !t.IsZero() {
		out.LastUpdatedAt = t.UTC().Format(time.RFC3339)
	}
	return out, s.flag(ctx, userID, out.Items)
}

func (s *Service) flag(ctx context.Context, userID string, items []domain.Article) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]string, len(items))
	for i, a := range items {
		ids[i] = a.ID
	}
	marked, err := s.bookmarks.Marked(ctx, userID, ids)
	if err != nil {
		return err
	}
	symbols, err := s.holdings.Symbols(ctx, userID)
	if err != nil {
		return err
	}
	for i := range items {
		items[i].Bookmarked = marked[items[i].ID]
		items[i].RelevantToHoldings = domain.Relevant(items[i].Tickers, symbols)
	}
	return nil
}

func (s *Service) Bookmark(ctx context.Context, userID, id string) error {
	return s.bookmarks.Add(ctx, userID, id)
}

func (s *Service) Unbookmark(ctx context.Context, userID, id string) error {
	return s.bookmarks.Remove(ctx, userID, id)
}

func (s *Service) Bookmarked(ctx context.Context, userID string) ([]domain.Article, error) {
	items, err := s.bookmarks.List(ctx, userID, 200)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []domain.Article{}
	}
	symbols, err := s.holdings.Symbols(ctx, userID)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].Bookmarked = true
		items[i].RelevantToHoldings = domain.Relevant(items[i].Tickers, symbols)
	}
	return items, nil
}

// validCursor checks the shape of the opaque keyset cursor so a bad one is a 422, not a database error.
func validCursor(c string) bool {
	b, err := base64.RawURLEncoding.DecodeString(c)
	return err == nil && strings.Contains(string(b), "|")
}
