package application

import (
	"context"
	"errors"

	"shenzen/backend/internal/features/auth/domain"
)

// sendOTP issues a fresh code; with enforceCooldown it silently skips when one was just sent.
func (s *Service) sendOTP(ctx context.Context, u domain.User, p domain.Purpose, enforceCooldown bool) error {
	now := s.now()
	if enforceCooldown {
		last, err := s.otps.Latest(ctx, u.ID, p)
		if err == nil && last.CoolingDown(now) {
			return nil
		}
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
	}
	code, err := domain.GenerateCode()
	if err != nil {
		return err
	}
	if err := s.otps.Replace(ctx, domain.NewOTP(s.newID(), u.ID, p, code, now)); err != nil {
		return err
	}
	return s.mailer.SendOTP(ctx, u.Email, u.Name, p, code)
}

// checkOTP validates and consumes the latest code of the given purpose.
func (s *Service) checkOTP(ctx context.Context, userID string, p domain.Purpose, code string) error {
	o, err := s.otps.Latest(ctx, userID, p)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.ErrInvalidCode
	}
	if err != nil {
		return err
	}
	if !o.Usable(s.now()) {
		return domain.ErrInvalidCode
	}
	if o.Exhausted() {
		return domain.ErrTooManyAttempts
	}
	if !o.Matches(code) {
		if err := s.otps.AddAttempt(ctx, o.ID); err != nil {
			return err
		}
		return domain.ErrInvalidCode
	}
	return s.otps.Consume(ctx, o.ID)
}
