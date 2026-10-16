package infrastructure

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"shenzen/backend/internal/features/matching/domain"
)

// Integration test against the local market database; skipped when it is unreachable or empty.
func TestCatalogCandidates(t *testing.T) {
	_ = godotenv.Load("../../../../.env")
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Skip(err)
	}
	defer pool.Close()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM asset_classifications`).Scan(&n); err != nil || n == 0 {
		t.Skip("market data not loaded")
	}
	c := NewCatalog(pool)
	cands, err := c.Candidates(context.Background())
	if err != nil || len(cands) < 100 {
		t.Fatalf("%d candidates, %v", len(cands), err)
	}
	classes := map[string]int{}
	for _, x := range cands {
		classes[x.Class]++
		if x.Vol <= 0 || x.Volatility == "unknown" || x.Liquidity == "unknown" || len(x.Rets) < 30 {
			t.Fatalf("incomplete candidate %+v", x.Symbol)
		}
	}
	for _, k := range []string{domain.ClassLargeCap, domain.ClassGlobal, domain.ClassETF} {
		if classes[k] == 0 {
			t.Errorf("no %s candidates: %v", k, classes)
		}
	}
	if rf, err := c.RiskFree(context.Background()); err != nil || rf <= 0 {
		t.Errorf("risk free %v %v", rf, err)
	}
	again, _ := c.Candidates(context.Background())
	if len(again) != len(cands) {
		t.Error("cache")
	}
}
