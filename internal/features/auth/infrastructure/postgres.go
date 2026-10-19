package infrastructure

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"shenzen/backend/internal/features/auth/domain"
)

const uniqueViolation = "23505"

type db struct{ pool *pgxpool.Pool }

type (
	UserRepo    struct{ db }
	OTPRepo     struct{ db }
	SessionRepo struct{ db }
)

func NewUserRepo(p *pgxpool.Pool) UserRepo       { return UserRepo{db{p}} }
func NewOTPRepo(p *pgxpool.Pool) OTPRepo         { return OTPRepo{db{p}} }
func NewSessionRepo(p *pgxpool.Pool) SessionRepo { return SessionRepo{db{p}} }

func mapErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == uniqueViolation {
		return domain.ErrEmailTaken
	}
	return err
}

const userCols = `id, email, name, password_hash, COALESCE(google_sub, ''), email_verified, created_at`

func scanUser(row pgx.Row) (domain.User, error) {
	var u domain.User
	err := row.Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.GoogleSub, &u.EmailVerified, &u.CreatedAt)
	return u, mapErr(err)
}

func (p UserRepo) Create(ctx context.Context, u domain.User) error {
	_, err := p.pool.Exec(ctx,
		`INSERT INTO users (id, email, name, password_hash, google_sub, email_verified, created_at)
		 VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, $7)`,
		u.ID, u.Email, u.Name, u.PasswordHash, u.GoogleSub, u.EmailVerified, u.CreatedAt)
	return mapErr(err)
}

func (p UserRepo) FindByEmail(ctx context.Context, email string) (domain.User, error) {
	return scanUser(p.pool.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE email = $1`, email))
}

func (p UserRepo) FindByID(ctx context.Context, id string) (domain.User, error) {
	return scanUser(p.pool.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE id = $1`, id))
}

func (p UserRepo) FindByGoogleSub(ctx context.Context, sub string) (domain.User, error) {
	return scanUser(p.pool.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE google_sub = $1`, sub))
}

func (p db) exec(ctx context.Context, sql string, args ...any) error {
	_, err := p.pool.Exec(ctx, sql, args...)
	return mapErr(err)
}

func (p UserRepo) UpdateCredentials(ctx context.Context, id, name, hash string) error {
	return p.exec(ctx, `UPDATE users SET name = $2, password_hash = $3, updated_at = now() WHERE id = $1`, id, name, hash)
}

func (p UserRepo) SetPassword(ctx context.Context, id, hash string) error {
	return p.exec(ctx, `UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1`, id, hash)
}

func (p UserRepo) MarkVerified(ctx context.Context, id string) error {
	return p.exec(ctx, `UPDATE users SET email_verified = TRUE, updated_at = now() WHERE id = $1`, id)
}

func (p UserRepo) LinkGoogle(ctx context.Context, id, sub string) error {
	return p.exec(ctx, `UPDATE users SET google_sub = $2, email_verified = TRUE, updated_at = now() WHERE id = $1`, id, sub)
}

func (p UserRepo) Delete(ctx context.Context, id string) error {
	return p.exec(ctx, `DELETE FROM users WHERE id = $1`, id)
}

func (p OTPRepo) Replace(ctx context.Context, o domain.OTP) error {
	return p.exec(ctx,
		`INSERT INTO auth_otps (id, user_id, purpose, code_hash, expires_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (user_id, purpose) DO UPDATE
		 SET id = EXCLUDED.id, code_hash = EXCLUDED.code_hash, expires_at = EXCLUDED.expires_at,
		     created_at = EXCLUDED.created_at, attempts = 0, consumed = FALSE`,
		o.ID, o.UserID, string(o.Purpose), o.CodeHash, o.ExpiresAt, o.CreatedAt)
}

func (p OTPRepo) Latest(ctx context.Context, userID string, purpose domain.Purpose) (domain.OTP, error) {
	var o domain.OTP
	var pur string
	err := p.pool.QueryRow(ctx,
		`SELECT id, user_id, purpose, code_hash, attempts, consumed, expires_at, created_at
		 FROM auth_otps WHERE user_id = $1 AND purpose = $2`, userID, string(purpose)).
		Scan(&o.ID, &o.UserID, &pur, &o.CodeHash, &o.Attempts, &o.Consumed, &o.ExpiresAt, &o.CreatedAt)
	o.Purpose = domain.Purpose(pur)
	return o, mapErr(err)
}

func (p OTPRepo) AddAttempt(ctx context.Context, id string) error {
	return p.exec(ctx, `UPDATE auth_otps SET attempts = attempts + 1 WHERE id = $1`, id)
}

func (p OTPRepo) Consume(ctx context.Context, id string) error {
	return p.exec(ctx, `UPDATE auth_otps SET consumed = TRUE WHERE id = $1`, id)
}

func (p SessionRepo) Create(ctx context.Context, s domain.Session) error {
	return p.exec(ctx,
		`INSERT INTO auth_sessions (id, user_id, token_hash, remember, user_agent, expires_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		s.ID, s.UserID, s.TokenHash, s.Remember, s.UserAgent, s.ExpiresAt, s.CreatedAt)
}

func (p SessionRepo) FindByTokenHash(ctx context.Context, hash string) (domain.Session, error) {
	var s domain.Session
	var revokedAt *time.Time
	err := p.pool.QueryRow(ctx,
		`SELECT id, user_id, token_hash, remember, user_agent, revoked_at, expires_at, created_at
		 FROM auth_sessions WHERE token_hash = $1`, hash).
		Scan(&s.ID, &s.UserID, &s.TokenHash, &s.Remember, &s.UserAgent, &revokedAt, &s.ExpiresAt, &s.CreatedAt)
	if revokedAt != nil {
		s.RevokedAt = *revokedAt
	}
	return s, mapErr(err)
}

func (p SessionRepo) Revoke(ctx context.Context, id string, at time.Time) error {
	return p.exec(ctx, `UPDATE auth_sessions SET revoked_at = COALESCE(revoked_at, $2) WHERE id = $1`, id, at)
}

func (p SessionRepo) RevokeAll(ctx context.Context, userID string, at time.Time) error {
	return p.exec(ctx, `UPDATE auth_sessions SET revoked_at = $2 WHERE user_id = $1 AND revoked_at IS NULL`, userID, at)
}
