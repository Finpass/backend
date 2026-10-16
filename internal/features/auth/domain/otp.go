package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"math/big"
	"time"
)

type Purpose string

const (
	VerifyEmail   Purpose = "verify_email"
	ResetPassword Purpose = "reset_password"
)

const (
	OTPTTL         = 10 * time.Minute
	OTPMaxAttempts = 5
	OTPCooldown    = 60 * time.Second
	otpDigits      = 6
)

type OTP struct {
	ID        string
	UserID    string
	Purpose   Purpose
	CodeHash  string
	ExpiresAt time.Time
	Attempts  int
	CreatedAt time.Time
	Consumed  bool
}

func GenerateCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%0*d", otpDigits, n.Int64()), nil
}

func HashCode(userID string, p Purpose, code string) string {
	sum := sha256.Sum256([]byte(userID + "|" + string(p) + "|" + code))
	return hex.EncodeToString(sum[:])
}

func NewOTP(id, userID string, p Purpose, code string, now time.Time) OTP {
	return OTP{ID: id, UserID: userID, Purpose: p, CodeHash: HashCode(userID, p, code), ExpiresAt: now.Add(OTPTTL), CreatedAt: now}
}

func (o OTP) Usable(now time.Time) bool { return !o.Consumed && now.Before(o.ExpiresAt) }

func (o OTP) Exhausted() bool { return o.Attempts >= OTPMaxAttempts }

func (o OTP) Matches(code string) bool {
	return subtle.ConstantTimeCompare([]byte(o.CodeHash), []byte(HashCode(o.UserID, o.Purpose, code))) == 1
}

func (o OTP) CoolingDown(now time.Time) bool { return now.Before(o.CreatedAt.Add(OTPCooldown)) }
