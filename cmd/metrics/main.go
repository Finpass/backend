// Command metrics rebuilds the Discover screener table asset_metrics. Run it after import-market / classify-assets.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	_ "github.com/joho/godotenv/autoload"

	"shenzen/backend/internal/features/discover/infrastructure"
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
	n, err := infrastructure.RefreshMetrics(ctx, pool)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("asset_metrics: %d rows in %s\n", n, time.Since(start).Round(time.Millisecond))
}
