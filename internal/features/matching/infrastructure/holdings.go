package infrastructure

import (
	"context"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"shenzen/backend/internal/features/matching/domain"
)

// Holdings reads the portfolio table (owned by the portfolio slice) and the market tables; read-only.
type Holdings struct{ pool *pgxpool.Pool }

func NewHoldings(p *pgxpool.Pool) Holdings { return Holdings{p} }

var manualClass = map[string]string{"bond_sbn": domain.ClassBondGov, "deposit": domain.ClassDeposit, "mutual_fund": domain.ClassFund}

func (h Holdings) Holdings(ctx context.Context, userID string) ([]domain.Holding, error) {
	rows, err := h.pool.Query(ctx, `SELECT h.symbol, coalesce(a.name, h.manual_name, ''), coalesce(c.asset_class, h.manual_class, 'other'), coalesce(c.sector, ''),
		coalesce(a.currency, h.manual_currency, 'IDR'), h.symbol IS NULL, h.units::float8, coalesce(h.manual_price::float8, 0), c.alert_threshold_pct
		FROM holdings h LEFT JOIN assets a ON a.symbol = h.symbol LEFT JOIN asset_classifications c ON c.symbol = h.symbol
		WHERE h.user_id = $1 ORDER BY h.created_at, h.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Holding
	var symbols []string
	manualPrice := map[int]float64{}
	for rows.Next() {
		var sym *string
		var x domain.Holding
		var price float64
		if err := rows.Scan(&sym, &x.Name, &x.Class, &x.Sector, &x.Currency, &x.Manual, &x.Units, &price, &x.AlertThresholdPct); err != nil {
			return nil, err
		}
		if sym != nil {
			x.Symbol = *sym
			symbols = append(symbols, *sym)
		} else {
			if c, ok := manualClass[x.Class]; ok {
				x.Class = c
			}
			manualPrice[len(out)] = price
		}
		out = append(out, x)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}
	usd, _ := h.latestClose(ctx, "IDR=X")
	fx := func(ccy string) float64 {
		switch ccy {
		case "IDR", "":
			return 1
		case "USD":
			return usd
		}
		return 0
	}
	series, err := h.closes(ctx, symbols)
	if err != nil {
		return nil, err
	}
	divs, err := h.recentDividends(ctx, symbols)
	if err != nil {
		return nil, err
	}
	for i := range out {
		x := &out[i]
		x.FX = fx(x.Currency)
		if x.Manual {
			x.Last = manualPrice[i]
			x.ValueIDR = x.Units * x.Last * x.FX
			continue
		}
		if s, ok := series[x.Symbol]; ok && len(s.closes) > 0 {
			x.Last, x.LastDate = s.closes[0], s.date
			if len(s.closes) > 1 {
				x.Prev = s.closes[1]
			}
			if len(s.closes) >= 200 {
				hi, lo := s.closes[1], s.closes[1]
				for _, c := range s.closes[1:] {
					hi, lo = math.Max(hi, c), math.Min(lo, c)
				}
				x.Hi52Prev, x.Lo52Prev = hi, lo
			}
			x.ValueIDR = x.Units * x.Last * x.FX
		}
		if d, ok := divs[x.Symbol]; ok {
			dd := d.date
			x.ExDate, x.DividendPerShare = &dd, d.amount
		}
	}
	return out, nil
}

type closeSeries struct {
	closes []float64 // newest first, up to 252
	date   time.Time
}

func (h Holdings) latestClose(ctx context.Context, symbol string) (float64, error) {
	var v float64
	err := h.pool.QueryRow(ctx, `SELECT close FROM prices_daily WHERE symbol = $1 ORDER BY trade_date DESC LIMIT 1`, symbol).Scan(&v)
	return v, err
}

func (h Holdings) closes(ctx context.Context, symbols []string) (map[string]closeSeries, error) {
	out := map[string]closeSeries{}
	if len(symbols) == 0 {
		return out, nil
	}
	rows, err := h.pool.Query(ctx, `SELECT symbol, max(trade_date), array_agg(close ORDER BY trade_date DESC)
		FROM (SELECT symbol, trade_date, close, row_number() OVER (PARTITION BY symbol ORDER BY trade_date DESC) rn
		      FROM prices_daily WHERE symbol = ANY($1)) t WHERE rn <= 252 GROUP BY symbol`, symbols)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		var cs closeSeries
		if err := rows.Scan(&s, &cs.date, &cs.closes); err != nil {
			return nil, err
		}
		out[s] = cs
	}
	return out, rows.Err()
}

type div struct {
	date   time.Time
	amount float64
}

func (h Holdings) recentDividends(ctx context.Context, symbols []string) (map[string]div, error) {
	out := map[string]div{}
	rows, err := h.pool.Query(ctx, `SELECT DISTINCT ON (symbol) symbol, ex_date, amount FROM dividends
		WHERE symbol = ANY($1) AND ex_date >= current_date - 5 ORDER BY symbol, ex_date DESC`, symbols)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		var d div
		if err := rows.Scan(&s, &d.date, &d.amount); err != nil {
			return nil, err
		}
		out[s] = d
	}
	return out, rows.Err()
}
