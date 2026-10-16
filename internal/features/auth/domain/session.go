package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"time"
)

type Session struct {
	ID        string
	UserID    string
	TokenHash string
	Remember  bool
	UserAgent string
	ExpiresAt time.Time
	RevokedAt time.Time // zero while active
	CreatedAt time.Time
}

const RefreshGrace = 10 * time.Second

func (s Session) Revoked() bool { return !s.RevokedAt.IsZero() }

func (s Session) Active(now time.Time) bool { return !s.Revoked() && now.Before(s.ExpiresAt) }

// Replayed reports a rotated token used after the grace window that tolerates parallel tabs.
func (s Session) Replayed(now time.Time) bool {
	return s.Revoked() && now.Sub(s.RevokedAt) > RefreshGrace
}

func NewRefreshToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
