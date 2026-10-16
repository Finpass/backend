package infrastructure

import (
	"context"
	"sync"

	"shenzen/backend/internal/features/note/domain"
)

type MemoryRepo struct {
	mu    sync.RWMutex
	notes map[string]domain.Note
}

func NewMemoryRepo() *MemoryRepo { return &MemoryRepo{notes: map[string]domain.Note{}} }

func (m *MemoryRepo) Save(_ context.Context, n domain.Note) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notes[n.ID] = n
	return nil
}

func (m *MemoryRepo) FindByID(_ context.Context, id string) (domain.Note, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n, ok := m.notes[id]
	if !ok {
		return domain.Note{}, domain.ErrNotFound(id)
	}
	return n, nil
}
