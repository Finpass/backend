// Package application serves the Discover screener: validated queries, labels, per-user stars.
package application

import (
	"context"
	"slices"
	"strings"
	"time"

	"shenzen/backend/internal/features/discover/domain"
	"shenzen/backend/internal/shared/apperr"
)

type Service struct {
	catalog   domain.Catalog
	watchlist domain.Watchlist
}

func NewService(c domain.Catalog, w domain.Watchlist) *Service {
	return &Service{catalog: c, watchlist: w}
}

// Item is one table row of GET /discover/assets.
type Item struct {
	Symbol            string            `json:"symbol"`
	Name              string            `json:"name"`
	AssetClass        string            `json:"assetClass"`
	AssetClassLabelID string            `json:"assetClassLabelId"`
	AssetClassLabelEN string            `json:"assetClassLabelEn"`
	Exchange          *string           `json:"exchange"`
	Currency          *string           `json:"currency"`
	LastPrice         *float64          `json:"lastPrice"`
	LastTradeDate     *string           `json:"lastTradeDate"`
	Ret7D             *float64          `json:"ret7d"`
	Ret1Y             *float64          `json:"ret1y"`
	MarketCap         *float64          `json:"marketCap"`
	AnalystTarget     *float64          `json:"analystTarget"` // always null: no analyst data in the dataset
	Valuation         *domain.Valuation `json:"valuation"`
	Growth            *domain.Growth    `json:"growth"`
	DivYieldPct       *float64          `json:"divYieldPct"`
	Industry          string            `json:"industry"`
	Starred           bool              `json:"starred"`
}

type List struct {
	Items      []Item  `json:"items"`
	NextCursor *string `json:"nextCursor"`
	Total      int     `json:"total"`
	AsOf       *string `json:"asOf"`
}

// Params are the raw query-string values.
type Params struct {
	Q, Class, Exchange, Industry, Sort, Dir, Cursor string
	Limit                                           int
	Watchlist                                       bool
}

func (s *Service) List(ctx context.Context, userID string, p Params) (List, error) {
	q, err := buildQuery(userID, p)
	if err != nil {
		return List{}, err
	}
	page, err := s.catalog.List(ctx, q)
	if err != nil {
		return List{}, err
	}
	out := List{Items: make([]Item, len(page.Rows)), Total: page.Total}
	symbols := make([]string, len(page.Rows))
	for i, m := range page.Rows {
		out.Items[i] = toItem(m)
		symbols[i] = m.Symbol
	}
	if len(symbols) > 0 {
		marked, err := s.watchlist.Marked(ctx, userID, symbols)
		if err != nil {
			return List{}, err
		}
		for i := range out.Items {
			out.Items[i].Starred = marked[out.Items[i].Symbol]
		}
	}
	if page.More && len(page.Rows) > 0 {
		c := domain.CursorFor(page.Rows[len(page.Rows)-1], q.Sort, q.Dir).Encode()
		out.NextCursor = &c
	}
	if t, err := s.catalog.AsOf(ctx); err == nil && !t.IsZero() {
		d := t.UTC().Format(time.DateOnly)
		out.AsOf = &d
	}
	return out, nil
}

func buildQuery(userID string, p Params) (domain.Query, error) {
	q := domain.Query{UserID: userID, Q: strings.TrimSpace(p.Q), Exchange: strings.ToUpper(strings.TrimSpace(p.Exchange)),
		Industry: strings.TrimSpace(p.Industry), Sort: p.Sort, Dir: p.Dir, Limit: p.Limit, WatchlistOnly: p.Watchlist}
	var bad []apperr.Detail
	if len(q.Q) > 100 {
		bad = append(bad, apperr.Detail{Field: "q", Message: "must be at most 100 characters"})
	}
	switch c := strings.ToLower(strings.TrimSpace(p.Class)); {
	case c == "" || c == "all":
	case slices.Contains(domain.ClassGroups, c):
		q.Class = c
	default:
		bad = append(bad, apperr.Detail{Field: "class", Message: "must be one of all, equity, etf, crypto, commodity, fx"})
	}
	if q.Sort == "" {
		q.Sort = "marketCap"
	}
	if _, ok := domain.SortColumn(q.Sort); !ok {
		bad = append(bad, apperr.Detail{Field: "sort", Message: "must be one of marketCap, lastPrice, ret7d, ret1y, divYield, name"})
	}
	switch q.Dir {
	case "":
		q.Dir = "desc"
		if q.Sort == "name" {
			q.Dir = "asc"
		}
	case "asc", "desc":
	default:
		bad = append(bad, apperr.Detail{Field: "dir", Message: "must be asc or desc"})
	}
	if q.Limit == 0 {
		q.Limit = domain.DefaultLimit
	}
	if q.Limit < 1 || q.Limit > domain.MaxLimit {
		bad = append(bad, apperr.Detail{Field: "limit", Message: "must be between 1 and 100"})
	}
	if p.Cursor != "" {
		c, ok := domain.DecodeCursor(p.Cursor)
		if !ok || c.Sort != q.Sort || c.Dir != q.Dir {
			bad = append(bad, apperr.Detail{Field: "cursor", Message: "invalid cursor"})
		} else {
			q.Cursor = &c
		}
	}
	if len(bad) > 0 {
		return domain.Query{}, apperr.Validation(bad...)
	}
	return q, nil
}

func toItem(m domain.Metrics) Item {
	id, en := domain.ClassLabels(m.AssetClass)
	it := Item{Symbol: m.Symbol, Name: m.Name, AssetClass: m.AssetClass, AssetClassLabelID: id, AssetClassLabelEN: en,
		Exchange: nonEmpty(m.Exchange), Currency: nonEmpty(m.Currency), LastPrice: m.LastPrice, LastTradeDate: nonEmpty(m.LastTradeDate),
		Ret7D: m.Ret7D, Ret1Y: m.Ret1Y, MarketCap: m.MarketCap, Valuation: m.Valuation, Growth: m.Growth,
		DivYieldPct: m.DivYieldPct, Industry: m.Industry}
	return it
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

type ClassFacet struct {
	Value   string `json:"value"`
	LabelID string `json:"labelId"`
	LabelEN string `json:"labelEn"`
	Count   int    `json:"count"`
}

type ValueFacet struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

type Facets struct {
	Classes    []ClassFacet `json:"classes"`
	Exchanges  []ValueFacet `json:"exchanges"`
	Industries []ValueFacet `json:"industries"`
}

func (s *Service) Facets(ctx context.Context) (Facets, error) {
	f, err := s.catalog.Facets(ctx)
	if err != nil {
		return Facets{}, err
	}
	out := Facets{Classes: []ClassFacet{}, Exchanges: []ValueFacet{}, Industries: []ValueFacet{}}
	for _, c := range f.Classes {
		id, en := domain.GroupLabels(c.Value)
		out.Classes = append(out.Classes, ClassFacet{c.Value, id, en, c.Count})
	}
	for _, e := range f.Exchanges {
		out.Exchanges = append(out.Exchanges, ValueFacet{e.Value, e.Count})
	}
	for _, i := range f.Industries {
		out.Industries = append(out.Industries, ValueFacet{i.Value, i.Count})
	}
	return out, nil
}

func (s *Service) Star(ctx context.Context, userID, symbol string) error {
	return s.watchlist.Add(ctx, userID, symbol)
}

func (s *Service) Unstar(ctx context.Context, userID, symbol string) error {
	return s.watchlist.Remove(ctx, userID, symbol)
}
