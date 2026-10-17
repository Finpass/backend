package domain

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

const (
	DefaultLimit = 50
	MaxLimit     = 100
)

// Query is a validated list request.
type Query struct {
	Q, Class, Exchange, Industry string
	Sort, Dir                    string
	Limit                        int
	Cursor                       *Cursor
	WatchlistOnly                bool
	UserID                       string
}

// Cursor is the keyset position after the last returned row: the sort value (nil = NULL) and the symbol tiebreaker.
// Order is always (sort column dir NULLS LAST, symbol ASC), which is total because symbol is unique.
type Cursor struct {
	Sort   string `json:"s"`
	Dir    string `json:"d"`
	Value  any    `json:"v"` // float64 for numeric sorts, string for name, nil for NULL
	Symbol string `json:"y"`
}

func (c Cursor) Encode() string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

// DecodeCursor returns false for anything that is not a cursor this API produced.
func DecodeCursor(s string) (Cursor, bool) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return Cursor{}, false
	}
	var c Cursor
	if err := json.Unmarshal(b, &c); err != nil || c.Symbol == "" {
		return Cursor{}, false
	}
	if _, ok := SortColumn(c.Sort); !ok || (c.Dir != "asc" && c.Dir != "desc") {
		return Cursor{}, false
	}
	switch c.Value.(type) {
	case nil, float64:
		if c.Sort == "name" && c.Value != nil {
			return Cursor{}, false
		}
	case string:
		if c.Sort != "name" {
			return Cursor{}, false
		}
	default:
		return Cursor{}, false
	}
	return c, true
}

// CursorFor builds the cursor that follows row m under the given sort.
func CursorFor(m Metrics, sort, dir string) Cursor {
	c := Cursor{Sort: sort, Dir: dir, Symbol: m.Symbol}
	var v *float64
	switch sort {
	case "marketCap":
		v = m.MarketCap
	case "lastPrice":
		v = m.LastPrice
	case "ret7d":
		v = m.Ret7D
	case "ret1y":
		v = m.Ret1Y
	case "divYield":
		v = m.DivYieldPct
	case "name":
		c.Value = strings.ToLower(m.Name)
		return c
	}
	if v != nil {
		c.Value = *v
	}
	return c
}

type Page struct {
	Rows  []Metrics
	Total int
	More  bool // a row exists after the last one in Rows
}

type Facet struct {
	Value string
	Count int
}

type Facets struct {
	Classes    []Facet // by class group
	Exchanges  []Facet
	Industries []Facet
}

// Catalog reads the screener table.
type Catalog interface {
	List(ctx context.Context, q Query) (Page, error)
	Facets(ctx context.Context) (Facets, error)
	AsOf(ctx context.Context) (time.Time, error)
}

// Watchlist stores the per-user starred symbols.
type Watchlist interface {
	Add(ctx context.Context, userID, symbol string) error // ErrAssetNotFound for an unknown symbol
	Remove(ctx context.Context, userID, symbol string) error
	Marked(ctx context.Context, userID string, symbols []string) (map[string]bool, error)
}
