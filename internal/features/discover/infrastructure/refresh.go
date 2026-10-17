package infrastructure

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"shenzen/backend/internal/features/discover/domain"
)

// RefreshMetrics recomputes asset_metrics from assets, prices_daily, dividends, fundamentals and the classifier
// output, in one transaction (readers see the old table until commit). Returns the number of rows written.
func RefreshMetrics(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	funds, err := loadFundamentals(ctx, pool)
	if err != nil {
		return 0, fmt.Errorf("fundamentals: %w", err)
	}
	// Three primary-key lookups per asset (latest close, close on or before -7 days, close on or before -1 year).
	// Dividends only count when their currency equals the asset currency (GBp is pence, never mixed with GBP).
	rows, err := pool.Query(ctx, `
		SELECT a.symbol, a.name, a.asset_class, COALESCE(a.currency, ''), COALESCE(a.exchange, ''), COALESCE(c.sector, ''),
		       lp.close, lp.trade_date, p7.close, p1.close, dv.total
		FROM assets a
		LEFT JOIN asset_classifications c ON c.symbol = a.symbol
		LEFT JOIN LATERAL (SELECT trade_date, close FROM prices_daily WHERE symbol = a.symbol ORDER BY trade_date DESC LIMIT 1) lp ON TRUE
		LEFT JOIN LATERAL (SELECT close FROM prices_daily WHERE symbol = a.symbol AND trade_date <= lp.trade_date - 7 ORDER BY trade_date DESC LIMIT 1) p7 ON lp.trade_date IS NOT NULL
		LEFT JOIN LATERAL (SELECT close FROM prices_daily WHERE symbol = a.symbol AND trade_date <= (lp.trade_date - INTERVAL '1 year')::date ORDER BY trade_date DESC LIMIT 1) p1 ON lp.trade_date IS NOT NULL
		LEFT JOIN LATERAL (SELECT sum(amount) AS total FROM dividends d WHERE d.symbol = a.symbol AND d.currency = a.currency
		                   AND d.ex_date > lp.trade_date - 365 AND d.ex_date <= lp.trade_date) dv ON lp.trade_date IS NOT NULL
		ORDER BY a.symbol`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var out [][]any
	for rows.Next() {
		var in domain.Inputs
		var date *time.Time
		if err := rows.Scan(&in.Symbol, &in.Name, &in.AssetClass, &in.Currency, &in.Exchange, &in.ClassSector,
			&in.LastPrice, &date, &in.Base7D, &in.Base1Y, &in.TTMDividends); err != nil {
			return 0, err
		}
		if date != nil {
			in.LastTradeDate = date.Format(time.DateOnly)
		}
		in.Fund = funds[in.Symbol]
		out = append(out, record(domain.Derive(in)))
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	rows.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `TRUNCATE asset_metrics`); err != nil {
		return 0, err
	}
	cols := []string{"symbol", "name", "name_lc", "symbol_lc", "asset_class", "class_group", "exchange", "currency", "last_price", "last_trade_date",
		"ret_7d", "ret_1y", "market_cap", "valuation_metric", "valuation_value", "growth_metric", "growth_pct", "div_yield_pct", "industry"}
	n, err := tx.CopyFrom(ctx, pgx.Identifier{"asset_metrics"}, cols, pgx.CopyFromRows(out))
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	_, _ = pool.Exec(ctx, `ANALYZE asset_metrics`)
	return int(n), nil
}

func record(m domain.Metrics) []any {
	nz := func(s string) any {
		if s == "" {
			return nil
		}
		return s
	}
	var vm, gm any
	var vv, gp any
	if m.Valuation != nil {
		vm, vv = m.Valuation.Metric, m.Valuation.Value
	}
	if m.Growth != nil {
		gm, gp = m.Growth.Metric, m.Growth.Pct
	}
	var date any
	if m.LastTradeDate != "" {
		t, _ := time.Parse(time.DateOnly, m.LastTradeDate)
		date = t
	}
	f := func(p *float64) any {
		if p == nil {
			return nil
		}
		return *p
	}
	return []any{m.Symbol, m.Name, strings.ToLower(m.Name), strings.ToLower(m.Symbol), m.AssetClass, m.ClassGroup, nz(m.Exchange), nz(m.Currency), f(m.LastPrice), date,
		f(m.Ret7D), f(m.Ret1Y), f(m.MarketCap), vm, vv, gm, gp, f(m.DivYieldPct), m.Industry}
}

type stmtColumn struct {
	NetIncome *float64 `json:"Net Income"`
	Revenue   *float64 `json:"Total Revenue"`
}

func loadFundamentals(ctx context.Context, pool *pgxpool.Pool) (map[string]*domain.Fundamentals, error) {
	rows, err := pool.Query(ctx, `SELECT symbol, COALESCE(sector, ''), COALESCE(industry, ''), market_cap, info, statements->'income_stmt' FROM fundamentals`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*domain.Fundamentals{}
	for rows.Next() {
		var sym string
		f := &domain.Fundamentals{}
		var info map[string]any
		var infoRaw, stmtRaw []byte
		if err := rows.Scan(&sym, &f.Sector, &f.Industry, &f.MarketCap, &infoRaw, &stmtRaw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(infoRaw, &info); err != nil {
			return nil, fmt.Errorf("%s info: %w", sym, err)
		}
		num := func(k string) *float64 {
			if v, ok := info[k].(float64); ok {
				return &v
			}
			return nil
		}
		if v := num("marketCap"); v != nil {
			f.MarketCap = v
		}
		f.TrailingPE, f.PriceToBook, f.TotalRevenue, f.DividendYieldPct = num("trailingPE"), num("priceToBook"), num("totalRevenue"), num("dividendYield")
		if len(stmtRaw) > 0 {
			var stmt map[string]stmtColumn
			if err := json.Unmarshal(stmtRaw, &stmt); err != nil {
				return nil, fmt.Errorf("%s income_stmt: %w", sym, err)
			}
			for date, c := range stmt {
				f.Periods = append(f.Periods, domain.Period{Date: date, NetIncome: c.NetIncome, Revenue: c.Revenue})
			}
		}
		out[sym] = f
	}
	return out, rows.Err()
}
