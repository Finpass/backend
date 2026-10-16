package domain

import (
	"context"
	"time"
)

const (
	StatusNone      = "none"
	StatusDraft     = "draft"
	StatusCompleted = "completed"
)

type Consent struct {
	UserID        string     `json:"-"`
	Accepted      bool       `json:"accepted"`
	WhatsappOptIn bool       `json:"whatsappOptIn"`
	AcceptedAt    *time.Time `json:"acceptedAt"`
}

type Profile struct {
	UserID         string
	Status         string
	CurrentSection string
	Answers        Answers
	OptOut         []string
	UpdatedAt      time.Time
}

type ExplainRequest struct {
	Locale    string
	FirstName string
	Result    Result // Explanation is nil
	Answers   Answers
}

type Repository interface {
	Consent(ctx context.Context, userID string) (Consent, error)
	SaveConsent(ctx context.Context, c Consent, now time.Time) error
	Profile(ctx context.Context, userID string) (Profile, error)
	SaveDraft(ctx context.Context, p Profile) error
	LatestAssessment(ctx context.Context, userID string) (Assessment, error)
	AssessmentTimes(ctx context.Context, userID string, since time.Time) ([]time.Time, error)
	// SaveAssessment stores the record and marks the profile completed in one transaction; ErrSubmitted if it was not a draft.
	SaveAssessment(ctx context.Context, a Assessment) error
	FirstName(ctx context.Context, userID string) (string, error)
	Delete(ctx context.Context, userID string) error
}

// Explainer asks the AI service for prose; implementations must treat any failure as an error, never a partial result.
type Explainer interface {
	Explain(ctx context.Context, req ExplainRequest) (*Explanation, error)
}
