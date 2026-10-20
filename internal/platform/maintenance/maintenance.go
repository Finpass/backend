// Package maintenance runs the daily housekeeping: it deletes data past its retention period and checks that the
// audit trail has not been altered.
package maintenance

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"shenzen/backend/internal/platform/audit"
)

// Policy is how long each kind of data is kept, in days. Zero keeps it forever. One-time codes and sessions have
// fixed short windows because nothing needs them after they expire.
type Policy struct {
	ChatDays            int // chat sessions and their messages, counted from the last message
	WhatsAppMessageDays int // copies of messages sent to and received from the user
	EventDays           int // WhatsApp engagement events and daily briefing run records
}

// Default is the policy we document in the processing register.
var Default = Policy{ChatDays: 365, WhatsAppMessageDays: 90, EventDays: 180}

// Purge deletes expired rows and returns how many it removed per table. The audit trail is excluded on purpose:
// it holds pseudonymous ids only and is append-only so that tampering is visible.
func Purge(ctx context.Context, pool *pgxpool.Pool, p Policy, now time.Time) (map[string]int64, error) {
	type job struct {
		name, sql string
		arg       time.Time
		on        bool
	}
	day := func(n int) time.Time { return now.AddDate(0, 0, -n) }
	jobs := []job{
		{"auth_otps", `DELETE FROM auth_otps WHERE expires_at < $1`, day(1), true},
		{"auth_sessions", `DELETE FROM auth_sessions WHERE expires_at < $1 OR revoked_at < $1`, day(30), true},
		{"whatsapp_verifications", `DELETE FROM whatsapp_verifications WHERE expires_at < $1`, day(1), true},
		{"whatsapp_messages", `DELETE FROM whatsapp_messages WHERE created_at < $1`, day(p.WhatsAppMessageDays), p.WhatsAppMessageDays > 0},
		{"whatsapp_events", `DELETE FROM whatsapp_events WHERE created_at < $1`, day(p.EventDays), p.EventDays > 0},
		{"briefing_runs", `DELETE FROM briefing_runs WHERE created_at < $1`, day(p.EventDays), p.EventDays > 0},
		{"chat_sessions", `DELETE FROM chat_sessions WHERE updated_at < $1`, day(p.ChatDays), p.ChatDays > 0},
	}
	removed := map[string]int64{}
	for _, j := range jobs {
		if !j.on {
			continue
		}
		tag, err := pool.Exec(ctx, j.sql, j.arg)
		if err != nil {
			return removed, fmt.Errorf("purge %s: %w", j.name, err)
		}
		removed[j.name] = tag.RowsAffected()
	}
	return removed, nil
}

// Run purges and verifies the audit chain shortly after start and then every 24 hours until ctx ends. Failures are
// logged loudly and never stop the API.
func Run(ctx context.Context, pool *pgxpool.Pool, rec *audit.Recorder, p Policy) {
	once := func() {
		if removed, err := Purge(ctx, pool, p, time.Now()); err != nil {
			slog.ErrorContext(ctx, "maintenance: purge failed", "err", err)
		} else {
			slog.InfoContext(ctx, "maintenance: purge done", "removed", removed)
		}
		if rec == nil {
			return
		}
		if bad, err := rec.Verify(ctx); err != nil {
			slog.ErrorContext(ctx, "maintenance: audit verification failed to run", "err", err)
		} else if bad != 0 {
			slog.ErrorContext(ctx, "SECURITY: audit trail was altered", "firstBadRow", bad)
		} else {
			slog.InfoContext(ctx, "maintenance: audit trail intact")
		}
	}
	t := time.NewTimer(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			once()
			t.Reset(24 * time.Hour)
		}
	}
}
