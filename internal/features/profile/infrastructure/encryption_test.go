package infrastructure

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	dashinfra "shenzen/backend/internal/features/dashboard/infrastructure"
	"shenzen/backend/internal/features/profile/domain"
	"shenzen/backend/internal/platform/crypto"
)

func key() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

// Needs a database with the auth and profile migrations: SEC_TEST_DATABASE_URL=postgres://...
func TestProfileAnswersAndAssessmentAreStoredEncrypted(t *testing.T) {
	url := os.Getenv("SEC_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SEC_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	keys, _ := crypto.NewKeyring("k1", key(), key(), nil)
	repo := NewRepo(pool, keys)

	_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = 'enc-prof'`)
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, name) VALUES ('enc-prof', 'enc-prof@x.co', 'Rina Putri')`); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM users WHERE id = 'enc-prof'`)

	now := time.Now().UTC()
	p := domain.Profile{UserID: "enc-prof", Status: "draft", CurrentSection: "s1", UpdatedAt: now,
		Answers: domain.Answers{"q2": float64(20000000), "q3": float64(9000000), "q22": "yes"}}
	if err := repo.SaveDraft(ctx, p); err != nil {
		t.Fatal(err)
	}

	var raw string
	_ = pool.QueryRow(ctx, `SELECT answers::text FROM profiles WHERE user_id = 'enc-prof'`).Scan(&raw)
	if strings.Contains(raw, "20000000") || !strings.Contains(raw, "v1:") {
		t.Fatalf("answers must be stored sealed, column holds: %s", raw)
	}
	got, err := repo.Profile(ctx, "enc-prof")
	if err != nil || got.Answers["q2"] != float64(20000000) || got.Answers["q22"] != "yes" {
		t.Fatalf("read back: %+v %v", got.Answers, err)
	}

	a := domain.Assessment{ID: "enc-asm-1", UserID: "enc-prof", QuestionnaireVersion: "v4", RuleVersion: "4.0", CreatedAt: now}
	a.Output.FinalProfile = "Moderate"
	a.Output.Metrics.MonthlyIncome = 20000000
	if err := repo.SaveAssessment(ctx, a); err != nil {
		t.Fatal(err)
	}
	_ = pool.QueryRow(ctx, `SELECT output::text FROM profile_assessments WHERE id = 'enc-asm-1'`).Scan(&raw)
	if strings.Contains(raw, "20000000") || strings.Contains(raw, "Moderate") || !strings.Contains(raw, "v1:") {
		t.Fatalf("assessment output must be stored sealed, column holds: %s", raw)
	}
	back, err := repo.LatestAssessment(ctx, "enc-prof")
	if err != nil || back.Output.FinalProfile != "Moderate" || back.Output.Metrics.MonthlyIncome != 20000000 {
		t.Fatalf("assessment read back: %+v %v", back.Output.FinalProfile, err)
	}

	// the dashboard reads the same tables with its own query and must see through the sealing
	dp, err := dashinfra.NewProfileRepo(pool, keys).Read(ctx, "enc-prof")
	if err != nil || dp.Income != 20000000 || dp.RiskProfile != "Moderate" || dp.Sharia != "yes" {
		t.Fatalf("dashboard read: %+v %v", dp, err)
	}

	// answers written before encryption was switched on are sealed by the backfill and still read
	_, _ = pool.Exec(ctx, `UPDATE profiles SET answers = '{"q2": 5000000}'::jsonb WHERE user_id = 'enc-prof'`)
	if err := repo.Backfill(ctx); err != nil {
		t.Fatal(err)
	}
	_ = pool.QueryRow(ctx, `SELECT answers::text FROM profiles WHERE user_id = 'enc-prof'`).Scan(&raw)
	if strings.Contains(raw, "5000000") {
		t.Fatalf("backfill must seal old plaintext, column holds: %s", raw)
	}
	if got, _ := repo.Profile(ctx, "enc-prof"); got.Answers["q2"] != float64(5000000) {
		t.Fatalf("backfilled answers must read back: %+v", got.Answers)
	}
}
