// Package audit writes the tamper-evident security trail (logins, exports, deletions, consent changes).
package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"shenzen/backend/internal/platform/httpx"
)

// lockID serialises writers so the chain has one linear order.
const lockID = 7203001

type Event struct {
	Actor  string // user id, or "" when unknown (failed sign-in)
	Action string // dotted verb: auth.login.ok, account.export, ...
	Object string // what it touched, never personal data
	Meta   map[string]any
}

type Recorder struct{ pool *pgxpool.Pool }

func New(p *pgxpool.Pool) *Recorder { return &Recorder{p} }

// Chain is the row hash: it commits to the previous hash and every field of the event.
func Chain(prev string, at time.Time, actor, action, object, requestID string, meta []byte) string {
	h := sha256.New()
	for _, part := range []string{prev, at.UTC().Format(time.RFC3339Nano), actor, action, object, requestID, string(meta)} {
		h.Write([]byte(part))
		h.Write([]byte{0}) // separator, so ("ab","c") and ("a","bc") differ
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Record never fails the request it audits: a broken trail is logged loudly instead. A nil Recorder does nothing.
func (r *Recorder) Record(ctx context.Context, e Event) {
	if r == nil {
		return
	}
	if err := r.write(ctx, e); err != nil {
		slog.ErrorContext(ctx, "audit: event not recorded", "action", e.Action, "err", err)
	}
}

func (r *Recorder) write(ctx context.Context, e Event) error {
	meta, err := json.Marshal(e.Meta)
	if err != nil || e.Meta == nil {
		meta = []byte("{}")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, lockID); err != nil {
		return err
	}
	var prev string
	if err := tx.QueryRow(ctx, `SELECT row_hash FROM audit_events ORDER BY id DESC LIMIT 1`).Scan(&prev); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	at, rid := time.Now().UTC().Truncate(time.Microsecond), httpx.RequestID(ctx) // Postgres keeps microseconds
	if _, err := tx.Exec(ctx, `INSERT INTO audit_events (at, actor_id, action, object, request_id, meta, prev_hash, row_hash) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		at, e.Actor, e.Action, e.Object, rid, meta, prev, Chain(prev, at, e.Actor, e.Action, e.Object, rid, meta)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Verify walks the whole trail and returns the id of the first row whose hash or link does not match (0 = intact).
func (r *Recorder) Verify(ctx context.Context) (int64, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, at, actor_id, action, object, request_id, meta::text, prev_hash, row_hash FROM audit_events ORDER BY id`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var prev string
	for rows.Next() {
		var (
			id                                               int64
			at                                               time.Time
			actor, action, object, rid, meta, prevH, rowHash string
		)
		if err := rows.Scan(&id, &at, &actor, &action, &object, &rid, &meta, &prevH, &rowHash); err != nil {
			return 0, err
		}
		// jsonb normalises key order and spacing, so compare through a re-marshal of the stored value
		var m map[string]any
		_ = json.Unmarshal([]byte(meta), &m)
		canon, _ := json.Marshal(m)
		if prevH != prev || Chain(prevH, at, actor, action, object, rid, canon) != rowHash {
			return id, nil
		}
		prev = rowHash
	}
	return 0, rows.Err()
}
