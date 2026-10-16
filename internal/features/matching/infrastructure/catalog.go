package infrastructure

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"shenzen/backend/internal/features/matching/domain"
)

// Catalog builds scoring candidates from asset_classifications + fundamentals + prices_daily. It is user independent, so it is cached.
type Catalog struct {
	pool *pgxpool.Pool
	ttl  time.Duration
	mu   sync.Mutex
	at   time.Time
	last []domain.Candidate
}

func NewCatalog(p *pgxpool.Pool) *Catalog { return &Catalog{pool: p, ttl: 30 * time.Minute} }

const perClass = 40

var catalogClasses = []string{domain.ClassLargeCap, domain.ClassMidSmall, domain.ClassGlobal, domain.ClassETF, domain.ClassGold, domain.ClassCrypto, domain.ClassProperty}

func (c *Catalog) Candidates(ctx context.Context) ([]domain.Candidate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.last != nil && time.Since(c.at) < c.ttl {
		return c.last, nil
	}
	rows, err := c.pool.Query(ctx, `
		SELECT symbol, name, currency, asset_class, sector, sharia_status, geography, volatility_class, liquidity_class, annualized_vol, info FROM (
		  SELECT c.symbol, a.name, coalesce(a.currency,'') currency, c.asset_class, c.sector, c.sharia_status, c.geography, c.volatility_class, c.liquidity_class,
		         c.annualized_vol, f.info,
		         row_number() OVER (PARTITION BY c.asset_class ORDER BY c.avg_daily_value_idr DESC NULLS LAST, c.symbol) rn
		  FROM asset_classifications c JOIN assets a ON a.symbol = c.symbol LEFT JOIN fundamentals f ON f.symbol = c.symbol
		  WHERE c.asset_class = ANY($1) AND c.volatility_class <> 'unknown' AND c.liquidity_class <> 'unknown' AND c.annualized_vol IS NOT NULL
		) t WHERE rn <= $2`, catalogClasses, perClass)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cands []domain.Candidate
	var symbols []string
	for rows.Next() {
		var x domain.Candidate
		var vol *float64
		var info []byte
		if err := rows.Scan(&x.Symbol, &x.Name, &x.Currency, &x.Class, &x.Sector, &x.Sharia, &x.Geography, &x.Volatility, &x.Liquidity, &vol, &info); err != nil {
			return nil, err
		}
		if x.Sector == "unknown" {
			x.Sector = ""
		}
		if vol != nil {
			x.Vol = *vol * 100 // stored as a fraction
		}
		applyInfo(&x, info)
		cands = append(cands, x)
		symbols = append(symbols, x.Symbol)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	series, err := c.series(ctx, symbols)
	if err != nil {
		return nil, err
	}
	out := cands[:0]
	for _, x := range cands {
		if s, ok := series[x.Symbol]; ok && fillStats(&x, s) {
			out = append(out, x)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Symbol < out[j].Symbol })
	c.last, c.at = out, time.Now()
	return out, nil
}

func applyInfo(x *domain.Candidate, info []byte) {
	if len(info) == 0 {
		return
	}
	var m map[string]any
	if json.Unmarshal(info, &m) != nil {
		return
	}
	get := func(k string, scale float64) *float64 {
		var f float64
		switch v := m[k].(type) {
		case float64:
			f = v
		case string:
			p, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return nil
			}
			f = p
		default:
			return nil
		}
		f *= scale
		return &f
	}
	x.PER, x.PBV, x.ROE, x.DivYield, x.Beta = get("trailingPE", 1), get("priceToBook", 1), get("returnOnEquity", 100), get("dividendYield", 1), get("beta", 1)
	if x.DivYield != nil && *x.DivYield == 0 {
		x.DivYield = nil
	}
}

type point struct {
	d time.Time
	c float64
}

func (c *Catalog) series(ctx context.Context, symbols []string) (map[string][]point, error) {
	rows, err := c.pool.Query(ctx, `SELECT symbol, trade_date, close FROM prices_daily
		WHERE symbol = ANY($1) AND trade_date >= (SELECT max(trade_date) FROM prices_daily) - 380 ORDER BY symbol, trade_date`, symbols)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]point{}
	for rows.Next() {
		var s string
		var p point
		if err := rows.Scan(&s, &p.d, &p.c); err != nil {
			return nil, err
		}
		out[s] = append(out[s], p)
	}
	return out, rows.Err()
}

// fillStats derives 1-year return, positive months and daily returns; it needs about half a year of history.
func fillStats(x *domain.Candidate, s []point) bool {
	if len(s) < 120 {
		return false
	}
	last := s[len(s)-1]
	first := s[0]
	for _, p := range s {
		if !p.d.Before(last.d.AddDate(-1, 0, 0)) {
			first = p
			break
		}
	}
	if first.c <= 0 {
		return false
	}
	x.Return1Y = (last.c/first.c - 1) * 100
	x.Rets = map[int]float64{}
	for i := max(1, len(s)-150); i < len(s); i++ {
		if s[i-1].c > 0 {
			x.Rets[s[i].d.Year()*10000+int(s[i].d.Month())*100+s[i].d.Day()] = s[i].c/s[i-1].c - 1
		}
	}
	// month-end closes over the last 13 month-ends
	var me []float64
	for i, p := range s {
		if i == len(s)-1 || s[i+1].d.Month() != p.d.Month() {
			me = append(me, p.c)
		}
	}
	if len(me) > 13 {
		me = me[len(me)-13:]
	}
	for i := 1; i < len(me); i++ {
		if me[i] > me[i-1] {
			x.PositiveMonths++
		}
	}
	return !math.IsNaN(x.Return1Y)
}

// RiskFree is the latest 1Y yield (percent) of the treasury curve in the data.
func (c *Catalog) RiskFree(ctx context.Context) (float64, error) {
	var v float64
	err := c.pool.QueryRow(ctx, `SELECT yield_pct::float8 FROM treasury_yields WHERE tenor = '1Y' ORDER BY trade_date DESC LIMIT 1`).Scan(&v)
	return v, err
}

// Signals: rupiah weakening is measured on IDR=X (> 1% over 30 days); BI rate and inflation direction come from headlines.
func (c *Catalog) Signals(ctx context.Context) (domain.MacroSignals, error) {
	var s domain.MacroSignals
	var chg *float64
	err := c.pool.QueryRow(ctx, `WITH l AS (SELECT close FROM prices_daily WHERE symbol='IDR=X' ORDER BY trade_date DESC LIMIT 1),
		o AS (SELECT close FROM prices_daily WHERE symbol='IDR=X' AND trade_date <= (SELECT max(trade_date) FROM prices_daily WHERE symbol='IDR=X') - 30 ORDER BY trade_date DESC LIMIT 1)
		SELECT (l.close / o.close - 1) * 100 FROM l, o`).Scan(&chg)
	if err == nil && chg != nil && *chg > 1 {
		s.RupiahWeak = true
	}
	s.BIRateUp = c.headlineMatch(ctx, biRateSQL, `(naik|menaikkan|kerek|hawkish)`)
	s.InflationUp = c.headlineMatch(ctx, inflationSQL, `(naik|meningkat|melonjak|menanjak)`)
	return s, nil
}

const (
	biRateSQL     = `(bi[- ]?rate|suku bunga acuan)`
	inflationSQL  = `inflasi`
	notForeignSQL = `(\mAS\M|amerika|\mUS\M|global|eropa|china|jepang)`
)

func (c *Catalog) headlineMatch(ctx context.Context, topic, dir string) bool {
	var ok bool
	_ = c.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM news_articles WHERE published_at >= (SELECT max(published_at) FROM news_articles) - interval '7 days'
		AND title ~* $1 AND title !~ $3 AND title ~* $2)`, topic, dir, notForeignSQL).Scan(&ok)
	return ok
}
