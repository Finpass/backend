package domain

import (
	"errors"
	"time"

	"shenzen/backend/internal/shared/apperr"
)

var ErrNotFound = errors.New("not found")

var (
	ErrConsentRequired = apperr.New(apperr.Forbidden, "Consent is required before answering the questionnaire")
	ErrSubmitted       = apperr.New(apperr.Conflict, "The questionnaire was already submitted; start a retake to change answers")
	ErrNoProfile       = apperr.New(apperr.NotFound, "No profile yet")
	ErrNoResult        = apperr.New(apperr.NotFound, "No result yet")
)

func ErrRetakeNotAllowed(next time.Time) error {
	return &apperr.Error{
		Code:    apperr.Conflict,
		Message: "Retake is not allowed yet",
		Details: []apperr.Detail{{Field: "nextAllowedAt", Message: next.UTC().Format(time.RFC3339)}},
	}
}
