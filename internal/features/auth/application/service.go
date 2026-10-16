package application

import (
	"context"
	"time"

	"shenzen/backend/internal/features/auth/domain"
)

type Config struct {
	RefreshTTL  time.Duration
	RememberTTL time.Duration
}

type Service struct {
	users    domain.UserRepository
	otps     domain.OTPRepository
	sessions domain.SessionRepository
	hasher   domain.PasswordHasher
	tokens   domain.AccessTokens
	mailer   domain.Mailer
	google   domain.GoogleVerifier
	newID    func() string
	now      func() time.Time
	cfg      Config
	dummy    string
}

type Deps struct {
	Users    domain.UserRepository
	OTPs     domain.OTPRepository
	Sessions domain.SessionRepository
	Hasher   domain.PasswordHasher
	Tokens   domain.AccessTokens
	Mailer   domain.Mailer
	Google   domain.GoogleVerifier // nil disables Google sign-in
	NewID    func() string
	Now      func() time.Time
	Config   Config
}

func NewService(d Deps) (*Service, error) {
	dummy, err := d.Hasher.Hash("timing-equalizer-1")
	if err != nil {
		return nil, err
	}
	return &Service{
		users: d.Users, otps: d.OTPs, sessions: d.Sessions, hasher: d.Hasher, tokens: d.Tokens,
		mailer: d.Mailer, google: d.Google, newID: d.NewID, now: d.Now, cfg: d.Config, dummy: dummy,
	}, nil
}

type Tokens struct {
	Access           string
	AccessExpiresAt  time.Time
	Refresh          string
	RefreshExpiresAt time.Time
}

type Result struct {
	User   domain.User
	Tokens Tokens
}

func (s *Service) issue(ctx context.Context, u domain.User, remember bool, userAgent string) (Result, error) {
	now := s.now()
	access, accessExp, err := s.tokens.Issue(u.ID, now)
	if err != nil {
		return Result{}, err
	}
	refresh, err := domain.NewRefreshToken()
	if err != nil {
		return Result{}, err
	}
	ttl := s.cfg.RefreshTTL
	if remember {
		ttl = s.cfg.RememberTTL
	}
	sess := domain.Session{
		ID: s.newID(), UserID: u.ID, TokenHash: domain.HashToken(refresh), Remember: remember,
		UserAgent: truncate(userAgent, 255), ExpiresAt: now.Add(ttl), CreatedAt: now,
	}
	if err := s.sessions.Create(ctx, sess); err != nil {
		return Result{}, err
	}
	return Result{User: u, Tokens: Tokens{Access: access, AccessExpiresAt: accessExp, Refresh: refresh, RefreshExpiresAt: sess.ExpiresAt}}, nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
