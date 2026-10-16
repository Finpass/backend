package infrastructure

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"shenzen/backend/internal/features/market/domain"
)

const classifyBatch = 500

// ClassifyAll runs the Asset Classifier for every asset in batches and upserts asset_classifications.
// It returns the number of rows written. progress (optional) is called after each batch.
func ClassifyAll(ctx context.Context, pool *pgxpool.Pool, progress func(done, total int)) (int, error) {
	fx := map[string]float64{}
	var usdidr float64
	if err := pool.QueryRow(ctx, `SELECT close FROM prices_daily WHERE symbol='IDR=X' ORDER BY trade_date DESC LIMIT 1`).Scan(&usdidr); err == nil {
		fx["USD"] = usdidr
	}

	type fund struct {
		sector, industry string
		cap              *float64
	}
	funds := map[string]fund{}
	fr, err := pool.Query(ctx, `SELECT symbol, coalesce(sector,''), coalesce(industry,''), market_cap FROM fundamentals`)
	if err != nil {
		return 0, err
	}
	for fr.Next() {
		var s string
		var f fund
		if err := fr.Scan(&s, &f.sector, &f.industry, &f.cap); err != nil {
			return 0, err
		}
		funds[s] = f
	}
	fr.Close()

	var symbols []string
	var total int
	ar, err := pool.Query(ctx, `SELECT symbol FROM assets ORDER BY symbol`)
	if err != nil {
		return 0, err
	}
	for ar.Next() {
		var s string
		ar.Scan(&s)
		symbols = append(symbols, s)
	}
	ar.Close()
	total = len(symbols)

	written := 0
	for i := 0; i < total; i += classifyBatch {
		batch := symbols[i:min(i+classifyBatch, total)]
		inputs, err := buildInputs(ctx, pool, batch, fx, func(s string) (string, string, *float64) {
			f := funds[s]
			return f.sector, f.industry, f.cap
		})
		if err != nil {
			return written, err
		}
		if err := storeBatch(ctx, pool, inputs); err != nil {
			return written, err
		}
		written += len(inputs)
		if progress != nil {
			progress(written, total)
		}
	}
	return written, nil
}

type classified struct {
	in  domain.Input
	res domain.Result
}

func buildInputs(ctx context.Context, pool *pgxpool.Pool, batch []string, fx map[string]float64, fund func(string) (string, string, *float64)) ([]classified, error) {
	rows, err := pool.Query(ctx, `SELECT symbol, name, asset_class, coalesce(exchange,''), coalesce(country,''), coalesce(currency,'') FROM assets WHERE symbol = ANY($1) ORDER BY symbol`, batch)
	if err != nil {
		return nil, err
	}
	var ins []domain.Input
	for rows.Next() {
		var in domain.Input
		if err := rows.Scan(&in.Symbol, &in.Name, &in.SourceClass, &in.Exchange, &in.Country, &in.Currency); err != nil {
			return nil, err
		}
		ins = append(ins, in)
	}
	rows.Close()

	closes := map[string][]float64{}
	vols := map[string][]float64{}
	pr, err := pool.Query(ctx, `SELECT symbol, close, volume FROM prices_daily WHERE symbol = ANY($1) ORDER BY symbol, trade_date`, batch)
	if err != nil {
		return nil, err
	}
	for pr.Next() {
		var s string
		var c float64
		var v *float64
		if err := pr.Scan(&s, &c, &v); err != nil {
			return nil, err
		}
		closes[s] = append(closes[s], c)
		if v == nil {
			vols[s] = append(vols[s], math.NaN())
		} else {
			vols[s] = append(vols[s], *v)
		}
	}
	pr.Close()

	out := make([]classified, 0, len(ins))
	for _, in := range ins {
		sec, ind, capLocal := fund(in.Symbol)
		in.FundSector, in.FundIndustry = sec, ind
		factor, fxOK := domain.IDRFactor(in.Currency, fx)
		if capLocal != nil && fxOK {
			v := *capLocal * factor
			in.MarketCapIDR = &v
		}
		if c := closes[in.Symbol]; len(c) > 0 {
			if v, n := domain.AnnualizedVol(c); n > 0 {
				in.AnnualizedVol, in.VolObs = &v, n
			}
			switch dv, ok := domain.AvgDailyValue(c, vols[in.Symbol]); {
			case !ok:
				in.LiquidityNote = "no volume in the price source"
			case !fxOK:
				in.LiquidityNote = fmt.Sprintf("no FX rate %s->IDR in the data (only USD/IDR is available)", in.Currency)
			default:
				v := dv * factor
				in.AvgDailyValueIDR = &v
			}
		} else {
			in.LiquidityNote = "no price history"
		}
		out = append(out, classified{in, domain.Classify(in)})
	}
	return out, nil
}

func storeBatch(ctx context.Context, pool *pgxpool.Pool, cs []classified) error {
	rows := make([][]any, 0, len(cs))
	now := time.Now().UTC()
	for _, c := range cs {
		tr, _ := json.Marshal(c.res.Trace)
		rows = append(rows, []any{c.res.Symbol, c.res.AssetClass, c.res.Sector, c.res.Sharia, c.res.Geography, c.res.Volatility, c.res.Liquidity,
			c.in.AnnualizedVol, c.in.AvgDailyValueIDR, c.in.MarketCapIDR, c.res.AlertThresholdPc, string(tr), c.res.Version, now})
	}
	cols := []string{"symbol", "asset_class", "sector", "sharia_status", "geography", "volatility_class", "liquidity_class", "annualized_vol", "avg_daily_value_idr", "market_cap_idr", "alert_threshold_pct", "trace", "classifier_version", "classified_at"}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `CREATE TEMP TABLE cls_stage (LIKE asset_classifications) ON COMMIT DROP`); err != nil {
		return err
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"cls_stage"}, cols, pgx.CopyFromRows(rows)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO asset_classifications SELECT * FROM cls_stage
		ON CONFLICT (symbol) DO UPDATE SET asset_class=EXCLUDED.asset_class, sector=EXCLUDED.sector, sharia_status=EXCLUDED.sharia_status,
		geography=EXCLUDED.geography, volatility_class=EXCLUDED.volatility_class, liquidity_class=EXCLUDED.liquidity_class,
		annualized_vol=EXCLUDED.annualized_vol, avg_daily_value_idr=EXCLUDED.avg_daily_value_idr, market_cap_idr=EXCLUDED.market_cap_idr,
		alert_threshold_pct=EXCLUDED.alert_threshold_pct, trace=EXCLUDED.trace, classifier_version=EXCLUDED.classifier_version,
		classified_at=EXCLUDED.classified_at`); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
