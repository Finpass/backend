package application

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"shenzen/backend/internal/features/news/domain"
	"shenzen/backend/internal/shared/apperr"
)

type feed struct{ got domain.Filter }

func (f *feed) List(_ context.Context, fl domain.Filter) (domain.Page, error) {
	f.got = fl
	return domain.Page{Items: []domain.Article{{ID: "a1", Tickers: []string{"BBCA"}}, {ID: "a2", Tickers: []string{"NVDA"}}}, NextCursor: "next"}, nil
}
func (*feed) LastUpdated(context.Context) (time.Time, error) {
	return time.Date(2026, 10, 16, 7, 0, 0, 0, time.UTC), nil
}

type marks map[string]map[string]bool // user -> article

func (m marks) Add(_ context.Context, u, a string) error {
	if m[u] == nil {
		m[u] = map[string]bool{}
	}
	m[u][a] = true
	return nil
}
func (m marks) Remove(_ context.Context, u, a string) error { delete(m[u], a); return nil }
func (m marks) Marked(_ context.Context, u string, ids []string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, id := range ids {
		out[id] = m[u][id]
	}
	return out, nil
}
func (m marks) List(_ context.Context, u string, _ int) ([]domain.Article, error) {
	var out []domain.Article
	for id := range m[u] {
		out = append(out, domain.Article{ID: id, Tickers: []string{"BBCA"}})
	}
	return out, nil
}

type held map[string][]string

func (h held) Symbols(_ context.Context, u string) ([]string, error) { return h[u], nil }

func TestListFlagsPerUser(t *testing.T) {
	ctx := context.Background()
	m := marks{}
	s := NewService(&feed{}, m, held{"alice": {"BBCA.JK"}})
	_ = s.Bookmark(ctx, "alice", "a2")
	f, err := s.List(ctx, "alice", domain.Filter{})
	if err != nil || len(f.Items) != 2 || !f.Items[0].RelevantToHoldings || f.Items[1].RelevantToHoldings || f.Items[0].Bookmarked || !f.Items[1].Bookmarked {
		t.Fatalf("%+v %v", f, err)
	}
	if *f.NextCursor != "next" || f.LastUpdatedAt != "2026-10-16T07:00:00Z" {
		t.Fatalf("%+v", f)
	}
	g, _ := s.List(ctx, "bob", domain.Filter{})
	if g.Items[0].RelevantToHoldings || g.Items[1].Bookmarked {
		t.Fatal("flags leaked across users")
	}
	b, _ := s.Bookmarked(ctx, "bob")
	if b == nil || len(b) != 0 {
		t.Fatalf("bob bookmarks %v", b)
	}
	_ = s.Unbookmark(ctx, "alice", "a2")
	if b, _ := s.Bookmarked(ctx, "alice"); len(b) != 0 {
		t.Fatal("unbookmark")
	}
}

func TestListValidatesInput(t *testing.T) {
	fd := &feed{}
	s := NewService(fd, marks{}, held{})
	for name, f := range map[string]domain.Filter{"category": {Category: "sports"}, "cursor": {Cursor: "%%%"}, "cursor shape": {Cursor: base64.RawURLEncoding.EncodeToString([]byte("nopipe"))}} {
		if _, err := s.List(context.Background(), "u", f); err == nil {
			t.Errorf("%s accepted", name)
		} else if ae, _ := apperr.As(err); ae.Code != apperr.ValidationFailed {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := s.List(context.Background(), "u", domain.Filter{Limit: 5000, Cursor: base64.RawURLEncoding.EncodeToString([]byte("1|x"))}); err != nil || fd.got.Limit != 20 || fd.got.Category != "all" {
		t.Fatalf("defaults %+v %v", fd.got, err)
	}
}
