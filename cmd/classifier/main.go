// Command classifier runs the deterministic Asset Classifier over every asset and stores the result.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	_ "github.com/joho/godotenv/autoload"

	"shenzen/backend/internal/features/market/domain"
	"shenzen/backend/internal/features/market/infrastructure"
	"shenzen/backend/internal/platform/database"
)

func main() {
	if os.Getenv("DATABASE_URL") == "" {
		log.Fatal("DATABASE_URL is required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	pool, err := database.Connect(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	start := time.Now()
	last := time.Now()
	n, err := infrastructure.ClassifyAll(ctx, pool, func(done, total int) {
		if time.Since(last) > 2*time.Second || done == total {
			last = time.Now()
			fmt.Printf("  classified %d / %d\n", done, total)
		}
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("classifier %s: %d assets in %s\n", domain.ClassifierVersion, n, time.Since(start).Round(time.Second))
	for _, dim := range []string{"asset_class", "sharia_status", "geography", "volatility_class", "liquidity_class", "sector"} {
		fmt.Printf("\n%s:\n", dim)
		rows, err := pool.Query(ctx, `SELECT `+dim+`, count(*) FROM asset_classifications GROUP BY 1 ORDER BY 2 DESC`)
		if err != nil {
			log.Fatal(err)
		}
		for rows.Next() {
			var v string
			var c int
			rows.Scan(&v, &c)
			fmt.Printf("  %-24s %d\n", v, c)
		}
		rows.Close()
	}
}
