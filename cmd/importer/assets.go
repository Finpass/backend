package main

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type assetRec struct {
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	Class    string `json:"asset_class"`
	Currency string `json:"currency"`
	Source   string `json:"source"`
	Exchange string `json:"exchange"`
	Country  string `json:"country"`
	ISIN     string `json:"isin"`
	FIGI     string `json:"figi"`
	stub     bool
}

func nz(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// dirExchange maps the price folder to the exchange code used by assets.json for stubs.
var dirExchange = map[string]string{"US": "US", "DE": "GER", "JK": "JKT", "L": "LSE", "HK": "HKG", "SZ": "SHZ", "SS": "SHH", "NS": "NSI", "BK": "SET", "TO": "TOR", "KL": "KLS", "PA": "PAR", "SI": "SES"}
var dirCurrency = map[string]string{"US": "USD", "DE": "EUR"}

func normalizeClass(a *assetRec) {
	if strings.HasSuffix(a.Symbol, "=X") {
		a.Class = "fx" // source labels USD/IDR as commodity
	}
}

// loadAssets = assets.json (priority) + universe/*.json + stubs for price files with no record.
func loadAssets(ctx context.Context, pool *pgxpool.Pool, dataDir string, st *stats) error {
	recs := map[string]assetRec{}
	add := func(a assetRec) error {
		if a.Symbol == "" || a.Name == "" {
			st.skip("assets", "missing symbol or name")
			return nil
		}
		if _, ok := recs[a.Symbol]; ok {
			st.skip("assets", "duplicate symbol (assets.json wins over universe)")
			return nil
		}
		normalizeClass(&a)
		recs[a.Symbol] = a
		return nil
	}
	if err := streamArray(filepath.Join(dataDir, "market/assets.json"), add); err != nil {
		return err
	}
	fromJSON := len(recs)
	uni, _ := filepath.Glob(filepath.Join(dataDir, "market/universe/*.json"))
	sort.Strings(uni)
	for _, p := range uni {
		if err := streamArray(p, add); err != nil {
			return err
		}
	}
	fromUniverse := len(recs) - fromJSON

	files, _ := filepath.Glob(filepath.Join(dataDir, "market/prices/*/*.json"))
	sort.Strings(files)
	stubs := 0
	for _, p := range files {
		sym := strings.TrimSuffix(filepath.Base(p), ".json")
		if _, ok := recs[sym]; ok {
			continue
		}
		dir := filepath.Base(filepath.Dir(p))
		var first priceRec
		if err := firstElement(p, &first); err != nil {
			st.skip("assets", "unreadable price file for stub: "+sym)
			continue
		}
		a := assetRec{Symbol: sym, Name: sym, Class: first.Class, Source: first.Source, stub: true}
		if first.Class == "crypto" {
			a.Currency = "IDR" // matches bitcoin/ethereum in assets.json: coingecko prices are IDR
		} else {
			a.Exchange, a.Currency = dirExchange[dir], dirCurrency[dir]
		}
		normalizeClass(&a)
		recs[sym] = a
		stubs++
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `CREATE TEMP TABLE assets_stage (LIKE assets INCLUDING DEFAULTS) ON COMMIT DROP`); err != nil {
		return err
	}
	rows := make([][]any, 0, len(recs))
	for _, a := range recs {
		rows = append(rows, []any{a.Symbol, a.Name, a.Class, nz(a.Currency), nz(a.Exchange), nz(a.Country), nz(a.ISIN), nz(a.FIGI), orDefault(a.Source, "unknown"), !a.stub})
	}
	cols := []string{"symbol", "name", "asset_class", "currency", "exchange", "country", "isin", "figi", "source", "has_metadata"}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"assets_stage"}, cols, pgx.CopyFromRows(rows)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO assets (`+strings.Join(cols, ",")+`)
		SELECT `+strings.Join(cols, ",")+` FROM assets_stage
		ON CONFLICT (symbol) DO UPDATE SET name=EXCLUDED.name, asset_class=EXCLUDED.asset_class, currency=EXCLUDED.currency,
		  exchange=EXCLUDED.exchange, country=EXCLUDED.country, isin=EXCLUDED.isin, figi=EXCLUDED.figi, source=EXCLUDED.source,
		  has_metadata=EXCLUDED.has_metadata, updated_at=now()`); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	st.note("assets", "assets.json=%d universe(extra)=%d stubs from price files=%d total upserted=%d", fromJSON, fromUniverse, stubs, len(recs))
	return nil
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func firstElement(path string, v any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return decodeFirst(f, v)
}
