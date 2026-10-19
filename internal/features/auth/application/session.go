package application

import (
	"context"
	"errors"

	"shenzen/backend/internal/features/auth/domain"
)

func (s *Service) Refresh(ctx context.Context, raw, userAgent string) (Result, error) {
	sess, err := s.sessions.FindByTokenHash(ctx, domain.HashToken(raw))
	if errors.Is(err, domain.ErrNotFound) {
		return Result{}, domain.ErrInvalidSession
	}
	if err != nil {
		return Result{}, err
	}
	if sess.Revoked() {
		if sess.Replayed(s.now()) { // late replay of a rotated token: assume theft, end every session
			if err := s.sessions.RevokeAll(ctx, sess.UserID, s.now()); err != nil {
				return Result{}, err
			}
		}
		return Result{}, domain.ErrInvalidSession
	}
	if !sess.Active(s.now()) {
		return Result{}, domain.ErrInvalidSession
	}
	u, err := s.users.FindByID(ctx, sess.UserID)
	if err != nil {
		return Result{}, domain.ErrInvalidSession
	}
	if err := s.sessions.Revoke(ctx, sess.ID, s.now()); err != nil {
		return Result{}, err
	}
	return s.issue(ctx, u, sess.Remember, userAgent)
}

func (s *Service) Logout(ctx context.Context, raw string) error {
	sess, err := s.sessions.FindByTokenHash(ctx, domain.HashToken(raw))
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.sessions.Revoke(ctx, sess.ID, s.now())
}

func (s *Service) Authenticate(token string) (string, error) {
	id, err := s.tokens.Parse(token, s.now())
	if err != nil {
		return "", domain.ErrInvalidSession
	}
	return id, nil
}

func (s *Service) Me(ctx context.Context, userID string) (domain.User, error) {
	u, err := s.users.FindByID(ctx, userID)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.User{}, domain.ErrInvalidSession
	}
	return u, err
}

// DeleteAccount permanently removes the user and, through ON DELETE CASCADE, everything stored about them (UU PDP).
// It asks for the password again when the account has one: a stolen session alone must not be enough to erase it.
func (s *Service) DeleteAccount(ctx context.Context, userID, password string) error {
	u, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return err
	}
	if u.HasPassword() && !s.hasher.Compare(u.PasswordHash, password) {
		return domain.ErrInvalidLogin
	}
	if err := s.sessions.RevokeAll(ctx, userID, s.now()); err != nil {
		return err
	}
	return s.users.Delete(ctx, userID)
}
