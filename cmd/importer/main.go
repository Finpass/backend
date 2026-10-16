// Command importer loads the raw market data (assets, 2 years of prices, dividends, yields,
// fundamentals, news) into Postgres. Streaming JSON + pgx CopyFrom; safe to re-run.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	_ "github.com/joho/godotenv/autoload"

	"shenzen/backend/internal/platform/database"
)

type stats struct {
	mu      sync.Mutex
	start   time.Time
	counts  map[string]int64
	skipped map[string]map[string]int64
	notes   []string
	last    time.Time
}

func (s *stats) add(k string, n int64) { s.mu.Lock(); s.counts[k] += n; s.mu.Unlock() }
func (s *stats) skip(table, why string) {
	s.mu.Lock()
	if s.skipped[table] == nil {
		s.skipped[table] = map[string]int64{}
	}
	s.skipped[table][why]++
	s.mu.Unlock()
}
func (s *stats) note(table, f string, a ...any) {
	m := table + ": " + fmt.Sprintf(f, a...)
	s.mu.Lock()
	s.notes = append(s.notes, m)
	s.mu.Unlock()
	fmt.Println(m)
}
func (s *stats) progress() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Since(s.last) < 3*time.Second {
		return
	}
	s.last = time.Now()
	fmt.Printf("  [%s] prices: %d files, %d rows\n", time.Since(s.start).Round(time.Second), s.counts["prices.files"], s.counts["prices.rows"])
}

func main() {
	dataDir := flag.String("data", os.Getenv("DATA_DIR"), "market data folder (DATA_DIR)")
	only := flag.String("only", "assets,prices,dividends,yields,fundamentals,news", "comma separated steps")
	force := flag.Bool("force", false, "reload every price file even if already imported")
	years := flag.Int("years", 2, "price window in years, relative to the newest trade_date")
	workers := flag.Int("workers", 4, "price file decoders")
	flag.Parse()
	if *dataDir == "" || os.Getenv("DATABASE_URL") == "" {
		log.Fatal("DATA_DIR and DATABASE_URL are required")
	}
	if _, err := os.Stat(filepath.Join(*dataDir, "market/assets.json")); err != nil {
		log.Fatalf("DATA_DIR %q does not look like the market data folder: %v", *dataDir, err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	pool, err := database.Connect(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	st := &stats{start: time.Now(), counts: map[string]int64{}, skipped: map[string]map[string]int64{}}
	steps := map[string]func() error{
		"assets":       func() error { return loadAssets(ctx, pool, *dataDir, st) },
		"prices":       func() error { return loadPrices(ctx, pool, *dataDir, *force, *workers, *years, st) },
		"dividends":    func() error { return loadDividends(ctx, pool, *dataDir, st) },
		"yields":       func() error { return loadYields(ctx, pool, *dataDir, st) },
		"fundamentals": func() error { return loadFundamentals(ctx, pool, *dataDir, st) },
		"news":         func() error { return loadNews(ctx, pool, *dataDir, st) },
	}
	for _, name := range strings.Split(*only, ",") {
		fn, ok := steps[strings.TrimSpace(name)]
		if !ok {
			log.Fatalf("unknown step %q", name)
		}
		t := time.Now()
		fmt.Printf("== %s\n", name)
		if err := fn(); err != nil {
			log.Fatalf("%s failed (re-run to resume): %v", name, err)
		}
		fmt.Printf("== %s done in %s\n", name, time.Since(t).Round(time.Millisecond))
	}

	fmt.Println("\nSkipped:")
	for _, t := range sortedKeys(st.skipped) {
		for why, n := range st.skipped[t] {
			fmt.Printf("  %-16s %8d  %s\n", t, n, why)
		}
	}
	fmt.Println("\nTable counts:")
	for _, t := range []string{"assets", "prices_daily", "dividends", "treasury_yields", "fundamentals", "news_articles"} {
		var n int64
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM `+t).Scan(&n); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("  %-16s %10d\n", t, n)
	}
	var lo, hi string
	_ = pool.QueryRow(ctx, `SELECT coalesce(min(trade_date)::text,''), coalesce(max(trade_date)::text,'') FROM prices_daily`).Scan(&lo, &hi)
	fmt.Printf("prices_daily range: %s .. %s\ntotal duration: %s\n", lo, hi, time.Since(st.start).Round(time.Second))
}

func sortedKeys[V any](m map[string]V) []string {
	k := make([]string, 0, len(m))
	for x := range m {
		k = append(k, x)
	}
	sort.Strings(k)
	return k
}
