package domain

import (
	"context"
	"errors"
	"time"
)

var ErrNoProfile = errors.New("whatsapp: no completed profile")

type Message struct {
	ID, UserID, Direction, Kind, Body, Status, ProviderID, Error string
}

type Event struct {
	UserID, Kind, Slot string
	Meta               map[string]any
}

// RunResult is stored on briefing_runs; Keys feed the matching engine's suppression and variety rules.
type RunResult struct {
	Status      string // sent | failed | retry
	Fallback    string
	Keys        map[string]string // alertKey, newsId, newsTopic, nudgeKey
	CharCount   int
	Error       string
	NextAttempt *time.Time
}

type Store interface {
	Contact(ctx context.Context, userID string) (Contact, error)
	SaveContact(ctx context.Context, c Contact) error // upsert; ErrPhoneUsed when a verified contact holds the number
	ContactByPhone(ctx context.Context, phone string) (Contact, error)
	ListActive(ctx context.Context) ([]Contact, error)
	TouchInbound(ctx context.Context, userID string, at time.Time) error
	SetSent(ctx context.Context, userID string, at time.Time, failStreak int) error

	SaveVerification(ctx context.Context, v Verification) error
	Verification(ctx context.Context, userID string) (Verification, error)
	AddVerificationAttempt(ctx context.Context, userID string) error
	DeleteVerification(ctx context.Context, userID string) error

	// ClaimRun inserts the run for (user, local date) or re-claims a retry/stuck one; ok=false when another run owns it.
	ClaimRun(ctx context.Context, userID, date string, now time.Time) (attempt int, ok bool, err error)
	FinishRun(ctx context.Context, userID, date string, r RunResult) error
	SentCount(ctx context.Context, userID string) (int, error)

	LogMessage(ctx context.Context, m Message) error // ErrDuplicate when the id already exists
	LogEvent(ctx context.Context, e Event) error
	Erase(ctx context.Context, userID string) error
}

// Matcher is implemented in app wiring on top of the matching service.
type Matcher interface {
	Content(ctx context.Context, userID string, localNow time.Time) (Content, map[string]string, error)
}

// Sender delivers text through the WhatsApp gateway (GoWA).
type Sender interface {
	Send(ctx context.Context, e164, text string) (providerID string, err error)
	Connected(ctx context.Context) (bool, error)
}

// Names returns the display first name; used for greetings.
type Names interface {
	FirstName(ctx context.Context, userID string) (string, error)
}
