package infrastructure

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"shenzen/backend/internal/features/discover/domain"
)

type Catalog struct{ pool *pgxpool.Pool }

func NewCatalog(p *pgxpool.Pool) Catalog { return Catalog{p} }

const selectCols = `m.symbol, m.name, m.asset_class, COALESCE(m.exchange, ''), COALESCE(m.currency, ''), m.last_price,
	to_char(m.last_trade_date, 'YYYY-MM-DD'), m.ret_7d, m.ret_1y, m.market_cap, m.valuation_metric, m.valuation_value,
	m.growth_metric, m.growth_pct, m.div_yield_pct, m.industry`

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// where builds the filter clause shared by the page and the count (no cursor).
func where(q domain.Query) (string, []any) {
	var conds []string
	var args []any
	arg := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
	if q.Q != "" {
		p := arg(escapeLike(strings.ToLower(q.Q)))
		conds = append(conds, fmt.Sprintf("(m.symbol_lc LIKE %s || '%%' OR m.name_lc LIKE '%%' || %s || '%%' OR m.symbol_lc LIKE '%%' || %s || '%%')", p, p, p))
	}
	if q.Class != "" {
		conds = append(conds, "m.class_group = "+arg(q.Class))
	}
	if q.Exchange != "" {
		conds = append(conds, "m.exchange = "+arg(q.Exchange))
	}
	if q.Industry != "" {
		conds = append(conds, "lower(m.industry) = "+arg(strings.ToLower(q.Industry)))
	}
	if q.WatchlistOnly {
		conds = append(conds, "EXISTS (SELECT 1 FROM watchlist w WHERE w.user_id = "+arg(q.UserID)+" AND w.symbol = m.symbol)")
	}
	if len(conds) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// cursorCond is the keyset predicate for order (col dir NULLS LAST, symbol ASC).
func cursorCond(c domain.Cursor, col string, arg func(any) string) string {
	sym := arg(c.Symbol)
	if c.Value == nil {
		return fmt.Sprintf("(m.%s IS NULL AND m.symbol > %s)", col, sym)
	}
	cast := "::float8"
	if c.Sort == "name" {
		cast = "::text"
	}
	v := arg(c.Value) + cast
	op := ">"
	if c.Dir == "desc" {
		op = "<"
	}
	return fmt.Sprintf("(m.%[1]s %[2]s %[3]s OR (m.%[1]s = %[3]s AND m.symbol > %[4]s) OR m.%[1]s IS NULL)", col, op, v, sym)
}

func (c Catalog) List(ctx context.Context, q domain.Query) (domain.Page, error) {
	col, _ := domain.SortColumn(q.Sort)
	w, args := where(q)
	var page domain.Page
	if err := c.pool.QueryRow(ctx, "SELECT count(*) FROM asset_metrics m"+w, args...).Scan(&page.Total); err != nil {
		return page, err
	}
	if q.Cursor != nil {
		cond := cursorCond(*q.Cursor, col, func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) })
		if w == "" {
			w = " WHERE " + cond
		} else {
			w += " AND " + cond
		}
	}
	dir := "DESC"
	if q.Dir == "asc" {
		dir = "ASC"
	}
	args = append(args, q.Limit+1)
	sql := fmt.Sprintf("SELECT %s FROM asset_metrics m%s ORDER BY m.%s %s NULLS LAST, m.symbol LIMIT $%d", selectCols, w, col, dir, len(args))
	rows, err := c.pool.Query(ctx, sql, args...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var m domain.Metrics
		var vm, gm *string
		var vv, gp *float64
		var date *string
		if err := rows.Scan(&m.Symbol, &m.Name, &m.AssetClass, &m.Exchange, &m.Currency, &m.LastPrice, &date, &m.Ret7D, &m.Ret1Y, &m.MarketCap,
			&vm, &vv, &gm, &gp, &m.DivYieldPct, &m.Industry); err != nil {
			return page, err
		}
		if date != nil {
			m.LastTradeDate = *date
		}
		if vm != nil && vv != nil {
			m.Valuation = &domain.Valuation{Metric: *vm, Value: *vv}
		}
		if gm != nil && gp != nil {
			m.Growth = &domain.Growth{Metric: *gm, Pct: *gp}
		}
		page.Rows = append(page.Rows, m)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if len(page.Rows) > q.Limit {
		page.Rows, page.More = page.Rows[:q.Limit], true
	}
	return page, nil
}

func (c Catalog) AsOf(ctx context.Context) (time.Time, error) {
	var d *time.Time // DATE scans as midnight UTC; no timezone cast in SQL
	err := c.pool.QueryRow(ctx, `SELECT max(last_trade_date) FROM asset_metrics`).Scan(&d)
	if d == nil {
		return time.Time{}, err
	}
	return *d, err
}

func (c Catalog) Facets(ctx context.Context) (domain.Facets, error) {
	var f domain.Facets
	read := func(sql string) ([]domain.Facet, error) {
		rows, err := c.pool.Query(ctx, sql)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []domain.Facet
		for rows.Next() {
			var x domain.Facet
			if err := rows.Scan(&x.Value, &x.Count); err != nil {
				return nil, err
			}
			out = append(out, x)
		}
		return out, rows.Err()
	}
	var err error
	if f.Classes, err = read(`SELECT class_group, count(*) FROM asset_metrics GROUP BY 1`); err != nil {
		return f, err
	}
	order := map[string]int{}
	for i, g := range domain.ClassGroups {
		order[g] = i
	}
	sortFacets(f.Classes, func(a, b domain.Facet) bool { return order[a.Value] < order[b.Value] })
	if f.Exchanges, err = read(`SELECT exchange, count(*) FROM asset_metrics WHERE exchange IS NOT NULL GROUP BY 1 ORDER BY 2 DESC, 1`); err != nil {
		return f, err
	}
	f.Industries, err = read(`SELECT industry, count(*) FROM asset_metrics GROUP BY 1 ORDER BY 2 DESC, 1 LIMIT 200`)
	return f, err
}

func sortFacets(s []domain.Facet, less func(a, b domain.Facet) bool) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && less(s[j], s[j-1]); j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
