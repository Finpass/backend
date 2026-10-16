package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type priceRec struct {
	Symbol string   `json:"symbol"`
	Class  string   `json:"asset_class"`
	Date   string   `json:"trade_date"`
	Open   *float64 `json:"open"`
	High   *float64 `json:"high"`
	Low    *float64 `json:"low"`
	Close  *float64 `json:"close"`
	Volume *float64 `json:"volume"`
	Source string   `json:"source"`
}

func decodeFirst(r io.Reader, v any) error {
	dec := json.NewDecoder(bufio.NewReader(r))
	if t, err := dec.Token(); err != nil || t != json.Delim('[') {
		return fmt.Errorf("not an array")
	}
	return dec.Decode(v)
}

var dateRe = regexp.MustCompile(`"trade_date":\s*"(\d{4}-\d{2}-\d{2})"`)

// newestDate reads only the tail of each file: the last trade_date there is that file's newest row.
func newestDate(files []string) (string, error) {
	var max string
	buf := make([]byte, 4096)
	for _, p := range files {
		f, err := os.Open(p)
		if err != nil {
			return "", err
		}
		fi, _ := f.Stat()
		off := fi.Size() - int64(len(buf))
		if off < 0 {
			off = 0
		}
		n, _ := f.ReadAt(buf, off)
		f.Close()
		if m := dateRe.FindAllSubmatch(buf[:n], -1); len(m) > 0 {
			if d := string(m[len(m)-1][1]); d > max {
				max = d
			}
		}
	}
	if max == "" {
		return "", fmt.Errorf("no trade_date found in price files")
	}
	return max, nil
}

type loaded struct {
	path   string
	symbol string
	size   int64
	mtime  time.Time
	rows   [][]any
}

func loadPrices(ctx context.Context, pool *pgxpool.Pool, dataDir string, force bool, workers int, years int, st *stats) error {
	files, _ := filepath.Glob(filepath.Join(dataDir, "market/prices/*/*.json"))
	sort.Strings(files)
	newest, err := newestDate(files)
	if err != nil {
		return err
	}
	nt, _ := time.Parse("2006-01-02", newest)
	cutoff := nt.AddDate(-years, 0, 0)
	st.note("prices", "newest trade_date in data=%s, keeping trade_date >= %s (%d years)", newest, cutoff.Format("2006-01-02"), years)

	done := map[string]struct {
		size  int64
		mtime time.Time
		cut   time.Time
	}{}
	if !force {
		rows, err := pool.Query(ctx, `SELECT path, size, mtime, cutoff FROM import_files`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var p string
			var d struct {
				size  int64
				mtime time.Time
				cut   time.Time
			}
			if err := rows.Scan(&p, &d.size, &d.mtime, &d.cut); err != nil {
				return err
			}
			done[p] = d
		}
		rows.Close()
	}
	known := map[string]bool{}
	ar, err := pool.Query(ctx, `SELECT symbol FROM assets`)
	if err != nil {
		return err
	}
	for ar.Next() {
		var s string
		ar.Scan(&s)
		known[s] = true
	}
	ar.Close()

	in := make(chan string)
	out := make(chan loaded, workers*2)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range in {
				l, err := decodePriceFile(p, rel(dataDir, p), cutoff, known, st)
				if err != nil {
					st.skip("prices", "unreadable file")
					fmt.Fprintf(os.Stderr, "skip %s: %v\n", p, err)
					continue
				}
				out <- l
			}
		}()
	}
	go func() {
		for _, p := range files {
			fi, err := os.Stat(p)
			if err != nil {
				continue
			}
			if d, ok := done[rel(dataDir, p)]; ok && d.size == fi.Size() && d.mtime.Equal(fi.ModTime().UTC().Truncate(time.Microsecond)) && d.cut.Equal(cutoff) {
				st.skip("prices", "file already imported (resume)")
				continue
			}
			in <- p
		}
		close(in)
		wg.Wait()
		close(out)
	}()

	// prices_summary.json is the only price source for symbols that have no per-symbol file (SPY, pax-gold).
	bySym := map[string][]priceRec{}
	if err := streamArray(filepath.Join(dataDir, "market/prices_summary.json"), func(r priceRec) error {
		bySym[r.Symbol] = append(bySym[r.Symbol], r)
		return nil
	}); err != nil {
		return err
	}
	hasFile := map[string]bool{}
	for _, p := range files {
		hasFile[strings.TrimSuffix(filepath.Base(p), ".json")] = true
	}
	var summaries []loaded
	for sym, recs := range bySym {
		if hasFile[sym] {
			st.skip("prices", "prices_summary rows for a symbol that has its own file")
			continue
		}
		rows, symbol := priceRows(recs, cutoff, known, st)
		summaries = append(summaries, loaded{path: "market/prices_summary.json#" + sym, symbol: symbol, rows: rows, mtime: time.Unix(0, 0).UTC()})
	}

	var batch []loaded
	var n int
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := writePriceBatch(ctx, pool, batch, cutoff); err != nil {
			return err
		}
		st.add("prices.rows", int64(n))
		st.add("prices.files", int64(len(batch)))
		batch, n = nil, 0
		return nil
	}
	for l := range out {
		batch = append(batch, l)
		n += len(l.rows)
		if n >= 100_000 {
			if err := flush(); err != nil {
				return err
			}
			st.progress()
		}
	}
	for _, l := range summaries {
		batch = append(batch, l)
		n += len(l.rows)
	}
	return flush()
}

func rel(dataDir, p string) string { r, _ := filepath.Rel(dataDir, p); return r }

func decodePriceFile(path, relPath string, cutoff time.Time, known map[string]bool, st *stats) (loaded, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return loaded{}, err
	}
	l := loaded{path: relPath, size: fi.Size(), mtime: fi.ModTime().UTC().Truncate(time.Microsecond)}
	var recs []priceRec
	err = streamArray(path, func(r priceRec) error { recs = append(recs, r); return nil })
	if err != nil {
		return l, err
	}
	l.rows, l.symbol = priceRows(recs, cutoff, known, st)
	return l, nil
}

func priceRows(recs []priceRec, cutoff time.Time, known map[string]bool, st *stats) (rows [][]any, symbol string) {
	l := loaded{}
	sort.SliceStable(recs, func(i, j int) bool { return recs[i].Date < recs[j].Date })
	var prev string
	for i, r := range recs {
		switch {
		case !known[r.Symbol]:
			st.skip("prices", "symbol not in assets")
			continue
		case r.Close == nil || *r.Close <= 0:
			st.skip("prices", "close null or <= 0")
			continue
		}
		d, err := time.Parse("2006-01-02", r.Date)
		if err != nil {
			st.skip("prices", "bad trade_date")
			continue
		}
		if d.Before(cutoff) {
			st.skip("prices", "older than the 2-year window")
			continue
		}
		if i > 0 && r.Date == prev { // keep the last of duplicate dates
			st.skip("prices", "duplicate (symbol, trade_date)")
			if len(l.rows) > 0 {
				l.rows = l.rows[:len(l.rows)-1]
			}
		}
		prev = r.Date
		l.symbol = r.Symbol
		l.rows = append(l.rows, []any{r.Symbol, d, r.Open, r.High, r.Low, *r.Close, r.Volume, orDefault(r.Source, "unknown")})
	}
	return l.rows, l.symbol
}

func writePriceBatch(ctx context.Context, pool *pgxpool.Pool, batch []loaded, cutoff time.Time) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	syms := make([]string, 0, len(batch))
	var rows [][]any
	for _, l := range batch {
		if l.symbol != "" {
			syms = append(syms, l.symbol)
		}
		rows = append(rows, l.rows...)
	}
	// Full replace per symbol keeps the load idempotent and drops rows that fell out of the window.
	if _, err := tx.Exec(ctx, `DELETE FROM prices_daily WHERE symbol = ANY($1)`, syms); err != nil {
		return err
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"prices_daily"}, []string{"symbol", "trade_date", "open", "high", "low", "close", "volume", "source"}, pgx.CopyFromRows(rows)); err != nil {
		return err
	}
	for _, l := range batch {
		if _, err := tx.Exec(ctx, `INSERT INTO import_files (path,size,mtime,cutoff,rows) VALUES ($1,$2,$3,$4,$5)
			ON CONFLICT (path) DO UPDATE SET size=$2, mtime=$3, cutoff=$4, rows=$5, loaded_at=now()`, l.path, l.size, l.mtime, cutoff, len(l.rows)); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
