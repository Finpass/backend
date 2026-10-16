package domain

import (
	"context"
	"strings"
	"time"

	"shenzen/backend/internal/shared/apperr"
)

const maxTitleLen = 120

type Note struct {
	ID        string
	Title     string
	Body      string
	CreatedAt time.Time
}

func New(id, title, body string, now time.Time) (Note, error) {
	title = strings.TrimSpace(title)
	if title == "" || len([]rune(title)) > maxTitleLen {
		return Note{}, apperr.Validation(apperr.Detail{Field: "title", Message: "must be 1-120 characters"})
	}
	return Note{ID: id, Title: title, Body: body, CreatedAt: now}, nil
}

type Repository interface {
	Save(ctx context.Context, n Note) error
	FindByID(ctx context.Context, id string) (Note, error)
}

func ErrNotFound(id string) error {
	return apperr.New(apperr.NotFound, "Note "+id+" not found")
}
