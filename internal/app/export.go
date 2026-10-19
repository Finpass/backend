package app

import (
	"context"
	"errors"
	"time"

	"shenzen/backend/internal/shared/apperr"
)

// exportSet gathers one section per slice for the data-subject export (UU PDP right of access). Each slice registers
// what it stores; the auth handler calls Export when the signed-in user asks for a copy.
type exportSet struct {
	names []string
	fns   []func(ctx context.Context, userID string) (any, error)
}

func (e *exportSet) add(name string, fn func(ctx context.Context, userID string) (any, error)) {
	e.names, e.fns = append(e.names, name), append(e.fns, fn)
}

func (e *exportSet) Export(ctx context.Context, userID string) (map[string]any, error) {
	out := map[string]any{"exportedAt": time.Now().UTC().Format(time.RFC3339), "format": "finpass-export-v1"}
	for i, fn := range e.fns {
		v, err := fn(ctx, userID)
		if ae, ok := apperr.As(err); ok && ae.Code == apperr.NotFound {
			continue // the slice holds nothing for this user
		}
		if err != nil {
			return nil, errors.Join(errors.New("export "+e.names[i]), err)
		}
		out[e.names[i]] = v
	}
	return out, nil
}
