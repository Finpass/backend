package domain

import (
	"errors"

	"shenzen/backend/internal/shared/apperr"
)

var ErrNotFound = errors.New("not found")

var (
	ErrEmailTaken      = apperr.New(apperr.Conflict, "Email is already registered")
	ErrInvalidLogin    = apperr.New(apperr.Unauthorized, "Invalid email or password")
	ErrInvalidCode     = apperr.New(apperr.Unprocessable, "Invalid or expired code")
	ErrTooManyAttempts = apperr.New(apperr.TooManyRequests, "Too many attempts, request a new code")
	ErrInvalidSession  = apperr.New(apperr.Unauthorized, "Session expired, please sign in again")
	ErrNotVerified     = &apperr.Error{
		Code:    apperr.Forbidden,
		Message: "Email is not verified",
		Details: []apperr.Detail{{Field: "email", Message: "not_verified"}},
	}
	ErrGoogleDisabled = apperr.New(apperr.Unavailable, "Google sign-in is not configured")
	ErrGoogleInvalid  = apperr.New(apperr.Unauthorized, "Google sign-in failed")
)
