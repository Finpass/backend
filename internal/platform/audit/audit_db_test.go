package audit

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Needs a database with the audit migration: SEC_TEST_DATABASE_URL=postgres://... go test ./internal/platform/audit
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("SEC_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SEC_TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestTrailIsAppendOnlyAndTamperEvident(t *testing.T) {
	ctx, pool := context.Background(), testPool(t)
	_, _ = pool.Exec(ctx, `TRUNCATE audit_events`)
	rec := New(pool)
	rec.Record(ctx, Event{Actor: "u1", Action: "auth.login.ok"})
	rec.Record(ctx, Event{Action: "auth.login.failed", Meta: map[string]any{"n": 2, "a": "x"}})
	rec.Record(ctx, Event{Actor: "u1", Action: "account.export"})

	if bad, err := rec.Verify(ctx); err != nil || bad != 0 {
		t.Fatalf("fresh trail must verify: bad=%d err=%v", bad, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE audit_events SET action = 'x'`); err == nil {
		t.Fatal("UPDATE must be refused")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM audit_events`); err == nil {
		t.Fatal("DELETE must be refused")
	}

	// an operator with table ownership bypasses the trigger; the chain must still expose the edit
	_, _ = pool.Exec(ctx, `ALTER TABLE audit_events DISABLE TRIGGER audit_events_no_change`)
	defer pool.Exec(ctx, `ALTER TABLE audit_events ENABLE TRIGGER audit_events_no_change`)
	var id int64
	_ = pool.QueryRow(ctx, `SELECT id FROM audit_events WHERE action = 'auth.login.failed'`).Scan(&id)
	_, _ = pool.Exec(ctx, `UPDATE audit_events SET actor_id = 'someone-else' WHERE id = $1`, id)
	if bad, _ := rec.Verify(ctx); bad != id {
		t.Fatalf("tampered row %d must be reported, got %d", id, bad)
	}
}
