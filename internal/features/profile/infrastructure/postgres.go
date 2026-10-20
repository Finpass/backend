package infrastructure

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"shenzen/backend/internal/features/profile/domain"
	"shenzen/backend/internal/platform/crypto"
)

type Repo struct {
	pool *pgxpool.Pool
	keys *crypto.Keyring // nil outside production: answers are then stored as plain JSON
}

func NewRepo(p *pgxpool.Pool, keys *crypto.Keyring) Repo { return Repo{p, keys} }

const (
	answersLabel = "profiles.answers"
	inputLabel   = "profile_assessments.input"
	traceLabel   = "profile_assessments.trace"
	outputLabel  = "profile_assessments.output"
)

func mapErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return err
}

func (r Repo) Consent(ctx context.Context, userID string) (domain.Consent, error) {
	c := domain.Consent{UserID: userID}
	err := r.pool.QueryRow(ctx, `SELECT accepted, whatsapp_opt_in, accepted_at FROM profile_consents WHERE user_id = $1`, userID).
		Scan(&c.Accepted, &c.WhatsappOptIn, &c.AcceptedAt)
	if c.AcceptedAt != nil {
		*c.AcceptedAt = c.AcceptedAt.UTC()
	}
	return c, mapErr(err)
}

// accepted_at keeps the first acceptance time on re-consent.
func (r Repo) SaveConsent(ctx context.Context, c domain.Consent, now time.Time) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO profile_consents (user_id, accepted, whatsapp_opt_in, accepted_at, updated_at) VALUES ($1, $2, $3, $4, $4)
		 ON CONFLICT (user_id) DO UPDATE SET accepted = EXCLUDED.accepted, whatsapp_opt_in = EXCLUDED.whatsapp_opt_in, updated_at = EXCLUDED.updated_at`,
		c.UserID, c.Accepted, c.WhatsappOptIn, now)
	return err
}

func (r Repo) Profile(ctx context.Context, userID string) (domain.Profile, error) {
	p := domain.Profile{UserID: userID}
	var raw json.RawMessage
	err := r.pool.QueryRow(ctx, `SELECT status, current_section, answers, opt_out, updated_at FROM profiles WHERE user_id = $1`, userID).
		Scan(&p.Status, &p.CurrentSection, &raw, &p.OptOut, &p.UpdatedAt)
	if err == nil {
		err = r.keys.RevealJSON(answersLabel, userID, raw, &p.Answers)
	}
	p.UpdatedAt = p.UpdatedAt.UTC()
	if p.Answers == nil {
		p.Answers = domain.Answers{}
	}
	if p.OptOut == nil {
		p.OptOut = []string{}
	}
	return p, mapErr(err)
}

func (r Repo) SaveDraft(ctx context.Context, p domain.Profile) error {
	answers, err := r.keys.ProtectJSON(answersLabel, p.UserID, p.Answers)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO profiles (user_id, status, current_section, answers, opt_out, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $6)
		 ON CONFLICT (user_id) DO UPDATE SET status = EXCLUDED.status, current_section = EXCLUDED.current_section,
		   answers = EXCLUDED.answers, opt_out = EXCLUDED.opt_out, updated_at = EXCLUDED.updated_at`,
		p.UserID, p.Status, p.CurrentSection, answers, nonNil(p.OptOut), p.UpdatedAt)
	return err
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (r Repo) LatestAssessment(ctx context.Context, userID string) (domain.Assessment, error) {
	a := domain.Assessment{UserID: userID}
	var in, trace, out json.RawMessage
	err := r.pool.QueryRow(ctx,
		`SELECT id, questionnaire_version, rule_version, input, trace, output, explanation, created_at
		 FROM profile_assessments WHERE user_id = $1 ORDER BY created_at DESC LIMIT 1`, userID).
		Scan(&a.ID, &a.QuestionnaireVersion, &a.RuleVersion, &in, &trace, &out, &a.Explanation, &a.CreatedAt)
	if err != nil {
		return a, mapErr(err)
	}
	for _, f := range []struct {
		label string
		raw   json.RawMessage
		dst   any
	}{{inputLabel, in, &a.Input}, {traceLabel, trace, &a.Trace}, {outputLabel, out, &a.Output}} {
		if err := r.keys.RevealJSON(f.label, a.ID, f.raw, f.dst); err != nil {
			return a, err
		}
	}
	return a, nil
}

func (r Repo) AssessmentTimes(ctx context.Context, userID string, since time.Time) ([]time.Time, error) {
	rows, err := r.pool.Query(ctx, `SELECT created_at FROM profile_assessments WHERE user_id = $1 AND created_at > $2`, userID, since)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[time.Time])
}

func (r Repo) SaveAssessment(ctx context.Context, a domain.Assessment) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Only a draft can be completed, which also stops two concurrent submits from both succeeding.
	tag, err := tx.Exec(ctx, `UPDATE profiles SET status = 'completed', updated_at = $2 WHERE user_id = $1 AND status = 'draft'`, a.UserID, a.CreatedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrSubmitted
	}
	in, err := r.keys.ProtectJSON(inputLabel, a.ID, a.Input)
	if err != nil {
		return err
	}
	trace, err := r.keys.ProtectJSON(traceLabel, a.ID, a.Trace)
	if err != nil {
		return err
	}
	out, err := r.keys.ProtectJSON(outputLabel, a.ID, a.Output)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO profile_assessments (id, user_id, questionnaire_version, rule_version, input, trace, output, explanation, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		a.ID, a.UserID, a.QuestionnaireVersion, a.RuleVersion, in, trace, out, a.Explanation, a.CreatedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// FirstName reads the shared users table; it is only used to address the explanation text.
func (r Repo) FirstName(ctx context.Context, userID string) (string, error) {
	var name string
	err := r.pool.QueryRow(ctx, `SELECT name FROM users WHERE id = $1`, userID).Scan(&name)
	return name, mapErr(err)
}

func (r Repo) Delete(ctx context.Context, userID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, t := range []string{"profile_assessments", "profiles", "profile_consents"} {
		if _, err := tx.Exec(ctx, `DELETE FROM `+t+` WHERE user_id = $1`, userID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// Backfill seals answers saved before encryption was switched on. Assessments are append-only (a trigger blocks
// UPDATE), so older ones stay as written and are still read.
func (r Repo) Backfill(ctx context.Context) error {
	if r.keys == nil {
		return nil
	}
	rows, err := r.pool.Query(ctx, `SELECT user_id, answers FROM profiles WHERE jsonb_typeof(answers) = 'object'`)
	if err != nil {
		return err
	}
	type row struct {
		id  string
		raw json.RawMessage
	}
	var todo []row
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.id, &x.raw); err != nil {
			rows.Close()
			return err
		}
		todo = append(todo, x)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, x := range todo {
		var v map[string]any
		if err := json.Unmarshal(x.raw, &v); err != nil {
			return err
		}
		sealed, err := r.keys.ProtectJSON(answersLabel, x.id, v)
		if err != nil {
			return err
		}
		if _, err := r.pool.Exec(ctx, `UPDATE profiles SET answers = $2 WHERE user_id = $1`, x.id, sealed); err != nil {
			return err
		}
	}
	return nil
}
