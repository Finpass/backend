package application

import (
	"context"
	"errors"

	"shenzen/backend/internal/features/auth/domain"
)

func (s *Service) ForgotPassword(ctx context.Context, rawEmail string) error {
	email, err := domain.NormalizeEmail(rawEmail)
	if err != nil {
		return err
	}
	u, err := s.users.FindByEmail(ctx, email)
	if errors.Is(err, domain.ErrNotFound) {
		return nil // always succeed so the endpoint cannot enumerate accounts
	}
	if err != nil {
		return err
	}
	return s.sendOTP(ctx, u, domain.ResetPassword, true)
}

type ResetCommand struct{ Email, Code, NewPassword string }

func (s *Service) ResetPassword(ctx context.Context, c ResetCommand) error {
	if err := domain.ValidatePassword(c.NewPassword); err != nil {
		return err
	}
	u, err := s.userByEmail(ctx, c.Email)
	if err != nil {
		return err
	}
	if err := s.checkOTP(ctx, u.ID, domain.ResetPassword, c.Code); err != nil {
		return err
	}
	hash, err := s.hasher.Hash(c.NewPassword)
	if err != nil {
		return err
	}
	if err := s.users.SetPassword(ctx, u.ID, hash); err != nil {
		return err
	}
	if !u.EmailVerified {
		if err := s.users.MarkVerified(ctx, u.ID); err != nil {
			return err
		}
	}
	return s.sessions.RevokeAll(ctx, u.ID, s.now())
}
