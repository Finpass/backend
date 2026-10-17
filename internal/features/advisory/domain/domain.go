// Package domain holds the advisory chat model and the ports the slice needs.
package domain

import (
	"context"
	"encoding/json"
	"time"

	"shenzen/backend/internal/shared/apperr"
)

var (
	ErrNoProfile = apperr.New(apperr.NotFound, "Complete the financial profile first")
	ErrNotFound  = apperr.New(apperr.NotFound, "Chat session not found")
)

const (
	HistoryWindow = 6 // messages sent to the AI service
	MaxMessage    = 1000
	SessionLimit  = 50
	TitleLen      = 60
)

type Citation struct {
	N          int    `json:"n"`
	Title      string `json:"title"`
	Page       *int   `json:"page"`
	SourceType string `json:"sourceType"`
	Key        string `json:"key"`
}

// TurnEvent is one user-facing progress step of a reply ("Searching sources...").
type TurnEvent struct {
	Sequence  int    `json:"sequence"`
	Title     string `json:"title"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
}

type Message struct {
	ID        int64       `json:"id"`
	Role      string      `json:"role"`
	Content   string      `json:"content"`
	Citations []Citation  `json:"citations"`
	Turns     []TurnEvent `json:"turns"`
	CreatedAt time.Time   `json:"createdAt"`
}

type Session struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// UserContext is sent to the AI service. It deliberately has no name, email or phone.
type UserContext struct {
	Profile     ProfileFacts  `json:"profile"`
	Holdings    []HoldingFact `json:"holdings"`
	Recommended []ClassFact   `json:"recommended"`
}

type ProfileFacts struct {
	RiskProfile  string  `json:"riskProfile"`
	LadderLevel  int     `json:"ladderLevel"`
	Sharia       string  `json:"sharia"`
	HorizonYears float64 `json:"horizonYears"`
	Goal         *string `json:"goal"`
}

type HoldingFact struct {
	Symbol     string  `json:"symbol"`
	Name       string  `json:"name"`
	AssetClass string  `json:"assetClass"`
	WeightPct  float64 `json:"weightPct"`
}

type ClassFact struct {
	Class         string      `json:"class"`
	AllocationPct float64     `json:"allocationPct"`
	Assets        []AssetFact `json:"assets"`
}

type AssetFact struct {
	Ticker    string  `json:"ticker"`
	WeightPct float64 `json:"weightPct"`
}

type Turn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Message     string      `json:"message"`
	Lang        string      `json:"lang"`
	History     []Turn      `json:"history"`
	UserContext UserContext `json:"userContext"`
	RunID       string      `json:"runId,omitempty"`
}

type Answer struct {
	Answer    string          `json:"answer"`
	Citations []Citation      `json:"citations"`
	Grounded  bool            `json:"grounded"`
	Source    string          `json:"source"`
	Trace     json.RawMessage `json:"trace"`
	Turns     []TurnEvent     `json:"turns"`
}

// Store persists sessions and messages. Every session read is scoped by user id.
type Store interface {
	CreateSession(ctx context.Context, id, userID, title string, now time.Time) error
	Owns(ctx context.Context, userID, sessionID string) (bool, error)
	Title(ctx context.Context, userID, sessionID string) (string, error) // ErrNotFound when not the user's
	AddMessage(ctx context.Context, sessionID, role, content string, citations []Citation, trace json.RawMessage, now time.Time) (Message, error)
	Recent(ctx context.Context, sessionID string, n int) ([]Message, error) // oldest first
	Messages(ctx context.Context, sessionID string) ([]Message, error)
	Sessions(ctx context.Context, userID string, limit int) ([]Session, error)
	Delete(ctx context.Context, userID, sessionID string) (bool, error)
}

// Contexts assembles the user context; it returns ErrNoProfile when the profile is not completed.
type Contexts interface {
	Build(ctx context.Context, userID string) (UserContext, error)
}

type AI interface {
	Chat(ctx context.Context, req ChatRequest) (Answer, error)
	Turns(ctx context.Context, runID string, since int) ([]TurnEvent, error)
}
