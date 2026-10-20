package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"shenzen/backend/internal/platform/audit"
	"shenzen/backend/internal/platform/config"
)

// Needs a database with all migrations: SEC_TEST_DATABASE_URL=postgres://...
func TestRegistrationAndLogoutAreInTheAuditTrail(t *testing.T) {
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
	cfg := config.Config{Env: "development", CORSOrigins: []string{"http://localhost:3000"},
		Auth: config.AuthConfig{JWTSecret: strings.Repeat("k", 40), AccessTTL: time.Minute, RefreshTTL: time.Hour, RememberTTL: time.Hour}}
	// the tamper-evidence test leaves one forged row behind on purpose, so start from an empty trail
	if _, err := pool.Exec(ctx, `TRUNCATE audit_events`); err != nil {
		t.Fatal(err)
	}
	r, err := NewRouter(Deps{Config: cfg, DB: pool, Pool: pool})
	if err != nil {
		t.Fatal(err)
	}
	count := func(action string) (n int) {
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action = $1`, action).Scan(&n)
		return
	}
	beforeReg, beforeOut := count("auth.register"), count("auth.logout")

	_, _ = pool.Exec(ctx, `DELETE FROM users WHERE email = 'audit-evt@example.com'`)
	defer pool.Exec(ctx, `DELETE FROM users WHERE email = 'audit-evt@example.com'`)
	post := func(path, body string) int {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}
	if c := post("/api/v1/auth/register", `{"name":"Audit","email":"audit-evt@example.com","password":"secret123"}`); c != http.StatusCreated {
		t.Fatalf("register: %d", c)
	}
	post("/api/v1/auth/logout", `{}`)
	if count("auth.register") != beforeReg+1 || count("auth.logout") != beforeOut+1 {
		t.Fatal("register and logout must each add one audit event")
	}
	if bad, err := audit.New(pool).Verify(ctx); err != nil || bad != 0 {
		t.Fatalf("the chain must stay intact: bad=%d err=%v", bad, err)
	}
}
