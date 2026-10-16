package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/go-chi/chi/v5"

	"shenzen/backend/internal/features/health"
	noteapp "shenzen/backend/internal/features/note/application"
	noteinfra "shenzen/backend/internal/features/note/infrastructure"
	notehttp "shenzen/backend/internal/features/note/transport"
	"shenzen/backend/internal/platform/config"
	"shenzen/backend/internal/platform/httpx"
)

type Deps struct {
	Config config.Config
	DB     interface{ Ping(context.Context) error }
}

func NewRouter(d Deps) *chi.Mux {
	r := httpx.NewRouter(d.Config.CORSOrigins)

	checks := map[string]health.Pinger{}
	if d.DB != nil {
		checks["database"] = d.DB
	}
	health.New(checks).Routes(r)

	noteSvc := noteapp.NewService(noteinfra.NewMemoryRepo(), newID, time.Now)
	r.Route("/api/v1", func(r chi.Router) {
		notehttp.New(noteSvc).Routes(r)
	})
	return r
}

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
