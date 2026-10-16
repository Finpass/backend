package application

import (
	"context"
	"time"

	"shenzen/backend/internal/features/note/domain"
)

type Service struct {
	repo  domain.Repository
	newID func() string
	now   func() time.Time
}

func NewService(repo domain.Repository, newID func() string, now func() time.Time) *Service {
	return &Service{repo: repo, newID: newID, now: now}
}

type CreateCommand struct{ Title, Body string }

func (s *Service) Create(ctx context.Context, c CreateCommand) (domain.Note, error) {
	n, err := domain.New(s.newID(), c.Title, c.Body, s.now())
	if err != nil {
		return domain.Note{}, err
	}
	return n, s.repo.Save(ctx, n)
}

func (s *Service) Get(ctx context.Context, id string) (domain.Note, error) {
	return s.repo.FindByID(ctx, id)
}
