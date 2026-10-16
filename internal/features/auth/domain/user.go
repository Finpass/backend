package domain

import (
	"net/mail"
	"strings"
	"time"
	"unicode"

	"shenzen/backend/internal/shared/apperr"
)

const (
	MaxEmailLen    = 254
	MaxNameLen     = 80
	MinPasswordLen = 8
	MaxPasswordLen = 72 // bcrypt input limit
)

type User struct {
	ID            string
	Email         string
	Name          string
	PasswordHash  string
	GoogleSub     string
	EmailVerified bool
	CreatedAt     time.Time
}

func (u User) HasPassword() bool { return u.PasswordHash != "" }

func NormalizeEmail(raw string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Address != s || len(s) > MaxEmailLen {
		return "", apperr.Validation(apperr.Detail{Field: "email", Message: "must be a valid email address"})
	}
	return s, nil
}

func NormalizeName(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if n := len([]rune(s)); n == 0 || n > MaxNameLen {
		return "", apperr.Validation(apperr.Detail{Field: "name", Message: "must be 1-80 characters"})
	}
	return s, nil
}

func ValidatePassword(p string) error {
	var letter, digit bool
	for _, r := range p {
		letter = letter || unicode.IsLetter(r)
		digit = digit || unicode.IsDigit(r)
	}
	if len(p) < MinPasswordLen || len(p) > MaxPasswordLen || !letter || !digit {
		return apperr.Validation(apperr.Detail{Field: "password", Message: "must be 8-72 characters with a letter and a digit"})
	}
	return nil
}

func NewUser(id, email, name, passwordHash string, verified bool, now time.Time) User {
	return User{ID: id, Email: email, Name: name, PasswordHash: passwordHash, EmailVerified: verified, CreatedAt: now}
}
