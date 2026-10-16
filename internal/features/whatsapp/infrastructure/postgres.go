package infrastructure

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"shenzen/backend/internal/features/whatsapp/domain"
)

type Repo struct{ pool *pgxpool.Pool }

func NewRepo(p *pgxpool.Pool) Repo { return Repo{p} }

func mapErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return err
}

func isUnique(err error, constraint string) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505" && (constraint == "" || pe.ConstraintName == constraint)
}

const contactCols = `user_id, coalesce(phone,''), verified, verified_at, opt_in, paused_at, send_time_local, timezone, frequency, locale, last_sent_at, last_inbound_at, fail_streak`

func scanContact(row pgx.Row) (domain.Contact, error) {
	var c domain.Contact
	err := row.Scan(&c.UserID, &c.Phone, &c.Verified, &c.VerifiedAt, &c.OptIn, &c.PausedAt, &c.SendTime, &c.Timezone, &c.Frequency, &c.Locale, &c.LastSentAt, &c.LastInboundAt, &c.FailStreak)
	return c, mapErr(err)
}

func (r Repo) Contact(ctx context.Context, userID string) (domain.Contact, error) {
	return scanContact(r.pool.QueryRow(ctx, `SELECT `+contactCols+` FROM whatsapp_contacts WHERE user_id = $1`, userID))
}

func (r Repo) SaveContact(ctx context.Context, c domain.Contact) error {
	var phone any
	if c.Phone != "" {
		phone = c.Phone
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO whatsapp_contacts (user_id, phone, verified, verified_at, opt_in, paused_at, send_time_local, timezone, frequency, locale)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (user_id) DO UPDATE SET phone = $2, verified = $3, verified_at = $4, opt_in = $5, paused_at = $6, send_time_local = $7,
		  timezone = $8, frequency = $9, locale = $10, updated_at = now()`,
		c.UserID, phone, c.Verified, c.VerifiedAt, c.OptIn, c.PausedAt, c.SendTime, c.Timezone, c.Frequency, c.Locale)
	if isUnique(err, "whatsapp_contacts_phone_verified_uq") {
		return domain.ErrPhoneUsed
	}
	return err
}

func (r Repo) ContactByPhone(ctx context.Context, phone string) (domain.Contact, error) {
	return scanContact(r.pool.QueryRow(ctx, `SELECT `+contactCols+` FROM whatsapp_contacts WHERE phone = $1 AND verified`, phone))
}

func (r Repo) ListActive(ctx context.Context) ([]domain.Contact, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+contactCols+` FROM whatsapp_contacts WHERE verified AND opt_in AND paused_at IS NULL AND phone IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Contact
	for rows.Next() {
		c, err := scanContact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r Repo) TouchInbound(ctx context.Context, userID string, at time.Time) error {
	_, err := r.pool.Exec(ctx, `UPDATE whatsapp_contacts SET last_inbound_at = $2 WHERE user_id = $1`, userID, at)
	return err
}

// SetSent records a delivery (at set, streak reset) or a terminal failure (at zero: only the streak changes).
func (r Repo) SetSent(ctx context.Context, userID string, at time.Time, failStreak int) error {
	if at.IsZero() {
		_, err := r.pool.Exec(ctx, `UPDATE whatsapp_contacts SET fail_streak = $2 WHERE user_id = $1`, userID, failStreak)
		return err
	}
	_, err := r.pool.Exec(ctx, `UPDATE whatsapp_contacts SET last_sent_at = $2, fail_streak = $3 WHERE user_id = $1`, userID, at, failStreak)
	return err
}

func (r Repo) SaveVerification(ctx context.Context, v domain.Verification) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO whatsapp_verifications (user_id, phone, code_hash, attempts, expires_at, created_at) VALUES ($1,$2,$3,0,$4,$5)
		ON CONFLICT (user_id) DO UPDATE SET phone = $2, code_hash = $3, attempts = 0, expires_at = $4, created_at = $5`, v.UserID, v.Phone, v.CodeHash, v.ExpiresAt, v.CreatedAt)
	return err
}

func (r Repo) Verification(ctx context.Context, userID string) (domain.Verification, error) {
	var v domain.Verification
	err := r.pool.QueryRow(ctx, `SELECT user_id, phone, code_hash, attempts, expires_at, created_at FROM whatsapp_verifications WHERE user_id = $1`, userID).
		Scan(&v.UserID, &v.Phone, &v.CodeHash, &v.Attempts, &v.ExpiresAt, &v.CreatedAt)
	return v, mapErr(err)
}

func (r Repo) AddVerificationAttempt(ctx context.Context, userID string) error {
	_, err := r.pool.Exec(ctx, `UPDATE whatsapp_verifications SET attempts = attempts + 1 WHERE user_id = $1`, userID)
	return err
}

func (r Repo) DeleteVerification(ctx context.Context, userID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM whatsapp_verifications WHERE user_id = $1`, userID)
	return err
}

// ClaimRun is the idempotency point: one row per (user, local date). A fresh insert, a due retry and a run stuck in
// 'sending' for 15 minutes can be claimed; a sent or failed run cannot.
func (r Repo) ClaimRun(ctx context.Context, userID, date string, now time.Time) (int, bool, error) {
	var attempt int
	err := r.pool.QueryRow(ctx, `INSERT INTO briefing_runs (id, user_id, run_date, status, attempts, created_at, updated_at)
		VALUES (md5(random()::text || clock_timestamp()::text || $1::text), $1, $2::date, 'sending', 1, $3, $3)
		ON CONFLICT (user_id, run_date) DO UPDATE SET status = 'sending', attempts = briefing_runs.attempts + 1, updated_at = $3
		WHERE (briefing_runs.status = 'retry' AND briefing_runs.next_attempt_at <= $3)
		   OR (briefing_runs.status = 'sending' AND briefing_runs.updated_at < $3 - interval '15 minutes')
		RETURNING attempts`, userID, date, now).Scan(&attempt)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	return attempt, err == nil, err
}

func (r Repo) FinishRun(ctx context.Context, userID, date string, res domain.RunResult) error {
	keys, _ := json.Marshal(res.Keys)
	if res.Keys == nil {
		keys = []byte("{}")
	}
	_, err := r.pool.Exec(ctx, `UPDATE briefing_runs SET status = $3, fallback = $4, content = $5, char_count = $6, error = $7, next_attempt_at = $8, updated_at = now()
		WHERE user_id = $1 AND run_date = $2::date`, userID, date, res.Status, res.Fallback, keys, res.CharCount, res.Error, res.NextAttempt)
	return err
}

func (r Repo) SentCount(ctx context.Context, userID string) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `SELECT count(*) FROM briefing_runs WHERE user_id = $1 AND status = 'sent'`, userID).Scan(&n)
	return n, err
}

func (r Repo) LogMessage(ctx context.Context, m domain.Message) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO whatsapp_messages (id, user_id, direction, kind, body, status, provider_id, error) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		m.ID, m.UserID, m.Direction, m.Kind, m.Body, m.Status, m.ProviderID, m.Error)
	if isUnique(err, "") {
		return domain.ErrDuplicate
	}
	return err
}

func (r Repo) LogEvent(ctx context.Context, e domain.Event) error {
	meta, _ := json.Marshal(e.Meta)
	if e.Meta == nil {
		meta = []byte("{}")
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO whatsapp_events (user_id, kind, slot, meta) VALUES ($1,$2,$3,$4)`, e.UserID, e.Kind, e.Slot, meta)
	return err
}

func (r Repo) Erase(ctx context.Context, userID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, t := range []string{"whatsapp_events", "briefing_runs", "whatsapp_messages", "whatsapp_verifications", "whatsapp_contacts"} {
		if _, err := tx.Exec(ctx, `DELETE FROM `+t+` WHERE user_id = $1`, userID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
