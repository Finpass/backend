package infrastructure

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type Bcrypt struct{ cost int }

func NewBcrypt(cost int) Bcrypt { return Bcrypt{cost: cost} }

func (b Bcrypt) Hash(plain string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(plain), b.cost)
	return string(h), err
}

func (b Bcrypt) Compare(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

type JWT struct {
	secret []byte
	issuer string
	ttl    time.Duration
}

func NewJWT(secret, issuer string, ttl time.Duration) JWT {
	return JWT{secret: []byte(secret), issuer: issuer, ttl: ttl}
}

func (j JWT) Issue(userID string, now time.Time) (string, time.Time, error) {
	exp := now.Add(j.ttl)
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject: userID, Issuer: j.issuer, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(exp),
	})
	s, err := tok.SignedString(j.secret)
	return s, exp, err
}

func (j JWT) Parse(token string, now time.Time) (string, error) {
	claims := jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(token, &claims, func(*jwt.Token) (any, error) { return j.secret, nil },
		jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(j.issuer), jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(func() time.Time { return now }))
	if err != nil || claims.Subject == "" {
		return "", errors.New("invalid token")
	}
	return claims.Subject, nil
}
