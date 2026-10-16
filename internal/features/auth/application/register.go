package application

import (
	"context"
	"errors"

	"shenzen/backend/internal/features/auth/domain"
)

type RegisterCommand struct{ Name, Email, Password string }

func (s *Service) Register(ctx context.Context, c RegisterCommand) (string, error) {
	email, err := domain.NormalizeEmail(c.Email)
	if err != nil {
		return "", err
	}
	name, err := domain.NormalizeName(c.Name)
	if err != nil {
		return "", err
	}
	if err := domain.ValidatePassword(c.Password); err != nil {
		return "", err
	}
	hash, err := s.hasher.Hash(c.Password)
	if err != nil {
		return "", err
	}

	u, err := s.users.FindByEmail(ctx, email)
	switch {
	case err == nil && u.EmailVerified:
		return "", domain.ErrEmailTaken
	case err == nil:
		// an unverified signup may be retried with new credentials
		if err := s.users.UpdateCredentials(ctx, u.ID, name, hash); err != nil {
			return "", err
		}
		u.Name = name
	case errors.Is(err, domain.ErrNotFound):
		u = domain.NewUser(s.newID(), email, name, hash, false, s.now())
		if err := s.users.Create(ctx, u); err != nil {
			return "", err
		}
	default:
		return "", err
	}
	return email, s.sendOTP(ctx, u, domain.VerifyEmail, false)
}

type VerifyCommand struct{ Email, Code, UserAgent string }

func (s *Service) VerifyEmail(ctx context.Context, c VerifyCommand) (Result, error) {
	u, err := s.userByEmail(ctx, c.Email)
	if err != nil {
		return Result{}, err
	}
	if err := s.checkOTP(ctx, u.ID, domain.VerifyEmail, c.Code); err != nil {
		return Result{}, err
	}
	if err := s.users.MarkVerified(ctx, u.ID); err != nil {
		return Result{}, err
	}
	u.EmailVerified = true
	return s.issue(ctx, u, false, c.UserAgent)
}

func (s *Service) ResendVerification(ctx context.Context, rawEmail string) error {
	u, err := s.userByEmail(ctx, rawEmail)
	if errors.Is(err, domain.ErrInvalidCode) || u.EmailVerified {
		return nil // never reveal whether the account exists
	}
	if err != nil {
		return err
	}
	return s.sendOTP(ctx, u, domain.VerifyEmail, true)
}

// userByEmail maps unknown emails to ErrInvalidCode so OTP endpoints do not leak accounts.
func (s *Service) userByEmail(ctx context.Context, raw string) (domain.User, error) {
	email, err := domain.NormalizeEmail(raw)
	if err != nil {
		return domain.User{}, err
	}
	u, err := s.users.FindByEmail(ctx, email)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.User{}, domain.ErrInvalidCode
	}
	return u, err
}
