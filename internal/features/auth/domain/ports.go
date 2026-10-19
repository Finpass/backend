package domain

import (
	"context"
	"time"
)

type UserRepository interface {
	Create(ctx context.Context, u User) error
	FindByEmail(ctx context.Context, email string) (User, error)
	FindByID(ctx context.Context, id string) (User, error)
	FindByGoogleSub(ctx context.Context, sub string) (User, error)
	UpdateCredentials(ctx context.Context, id, name, passwordHash string) error
	SetPassword(ctx context.Context, id, passwordHash string) error
	MarkVerified(ctx context.Context, id string) error
	LinkGoogle(ctx context.Context, id, sub string) error
	Delete(ctx context.Context, id string) error // cascades to every row that references the user
}

type OTPRepository interface {
	Replace(ctx context.Context, o OTP) error
	Latest(ctx context.Context, userID string, p Purpose) (OTP, error)
	AddAttempt(ctx context.Context, id string) error
	Consume(ctx context.Context, id string) error
}

type SessionRepository interface {
	Create(ctx context.Context, s Session) error
	FindByTokenHash(ctx context.Context, hash string) (Session, error)
	Revoke(ctx context.Context, id string, at time.Time) error
	RevokeAll(ctx context.Context, userID string, at time.Time) error
}

type PasswordHasher interface {
	Hash(plain string) (string, error)
	Compare(hash, plain string) bool
}

type AccessTokens interface {
	Issue(userID string, now time.Time) (token string, expiresAt time.Time, err error)
	Parse(token string, now time.Time) (userID string, err error)
}

type Mailer interface {
	SendOTP(ctx context.Context, to, name string, p Purpose, code string) error
}

type GoogleProfile struct {
	Sub           string
	Email         string
	Name          string
	EmailVerified bool
}

type GoogleVerifier interface {
	Verify(ctx context.Context, accessToken string) (GoogleProfile, error)
}
