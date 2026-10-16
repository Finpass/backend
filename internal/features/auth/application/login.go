package application

import (
	"context"
	"errors"
	"strings"

	"shenzen/backend/internal/features/auth/domain"
)

type LoginCommand struct {
	Email, Password, UserAgent string
	Remember                   bool
}

func (s *Service) Login(ctx context.Context, c LoginCommand) (Result, error) {
	email, err := domain.NormalizeEmail(c.Email)
	if err != nil {
		return Result{}, domain.ErrInvalidLogin
	}
	u, err := s.users.FindByEmail(ctx, email)
	if errors.Is(err, domain.ErrNotFound) {
		s.hasher.Compare(s.dummy, c.Password) // keep timing uniform
		return Result{}, domain.ErrInvalidLogin
	}
	if err != nil {
		return Result{}, err
	}
	if !u.HasPassword() || !s.hasher.Compare(u.PasswordHash, c.Password) {
		return Result{}, domain.ErrInvalidLogin
	}
	if !u.EmailVerified {
		if err := s.sendOTP(ctx, u, domain.VerifyEmail, true); err != nil {
			return Result{}, err
		}
		return Result{}, domain.ErrNotVerified
	}
	return s.issue(ctx, u, c.Remember, c.UserAgent)
}

type GoogleCommand struct {
	AccessToken, UserAgent string
	Remember               bool
}

func (s *Service) LoginWithGoogle(ctx context.Context, c GoogleCommand) (Result, error) {
	if s.google == nil {
		return Result{}, domain.ErrGoogleDisabled
	}
	p, err := s.google.Verify(ctx, c.AccessToken)
	if err != nil {
		return Result{}, err
	}
	u, err := s.resolveGoogleUser(ctx, p)
	if err != nil {
		return Result{}, err
	}
	return s.issue(ctx, u, c.Remember, c.UserAgent)
}

func (s *Service) resolveGoogleUser(ctx context.Context, p domain.GoogleProfile) (domain.User, error) {
	u, err := s.users.FindByGoogleSub(ctx, p.Sub)
	if err == nil {
		return u, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return domain.User{}, err
	}
	email, err := domain.NormalizeEmail(p.Email)
	if err != nil {
		return domain.User{}, domain.ErrGoogleInvalid
	}
	u, err = s.users.FindByEmail(ctx, email)
	switch {
	case err == nil:
		// Google asserts the email, so linking is safe and also verifies it
		if err := s.users.LinkGoogle(ctx, u.ID, p.Sub); err != nil {
			return domain.User{}, err
		}
		u.GoogleSub, u.EmailVerified = p.Sub, true
		return u, nil
	case errors.Is(err, domain.ErrNotFound):
		name := p.Name
		if name == "" {
			name, _, _ = strings.Cut(email, "@")
		}
		if name, err = domain.NormalizeName(truncate(name, domain.MaxNameLen)); err != nil {
			return domain.User{}, err
		}
		u = domain.NewUser(s.newID(), email, name, "", true, s.now())
		u.GoogleSub = p.Sub
		return u, s.users.Create(ctx, u)
	default:
		return domain.User{}, err
	}
}
