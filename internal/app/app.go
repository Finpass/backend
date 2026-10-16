package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	authapp "shenzen/backend/internal/features/auth/application"
	authdomain "shenzen/backend/internal/features/auth/domain"
	authinfra "shenzen/backend/internal/features/auth/infrastructure"
	authhttp "shenzen/backend/internal/features/auth/transport"
	"shenzen/backend/internal/features/health"
	noteapp "shenzen/backend/internal/features/note/application"
	noteinfra "shenzen/backend/internal/features/note/infrastructure"
	notehttp "shenzen/backend/internal/features/note/transport"
	profileapp "shenzen/backend/internal/features/profile/application"
	profileinfra "shenzen/backend/internal/features/profile/infrastructure"
	profilehttp "shenzen/backend/internal/features/profile/transport"
	"shenzen/backend/internal/platform/aiclient"
	"shenzen/backend/internal/platform/config"
	"shenzen/backend/internal/platform/httpx"
)

type Deps struct {
	Ctx    context.Context // lifetime of background jobs (WhatsApp scheduler); nil means context.Background()
	Config config.Config
	DB     interface{ Ping(context.Context) error }
	Pool   *pgxpool.Pool
}

func NewRouter(d Deps) (*chi.Mux, error) {
	r := httpx.NewRouter(d.Config.CORSOrigins)

	checks := map[string]health.Pinger{}
	if d.DB != nil {
		checks["database"] = d.DB
	}
	health.New(checks).Routes(r)

	noteSvc := noteapp.NewService(noteinfra.NewMemoryRepo(), newID, time.Now)
	authSvc, err := newAuthService(d)
	if err != nil {
		return nil, err
	}
	r.Route("/api/v1", func(r chi.Router) {
		notehttp.New(noteSvc).Routes(r)
		if authSvc != nil {
			authH := authhttp.New(authSvc, d.Config.IsProduction())
			authH.Routes(r)
			if d.Pool != nil {
				profileSvc := newProfileService(d)
				profilehttp.New(profileSvc, authH.RequireAuth, authhttp.UserID).Routes(r)
				mountAdvice(r, d, authH, profileSvc) // matching engine + WhatsApp (advice_wiring.go)
				mountDashboard(r, d, authH)          // portfolio, dashboard, news (dashboard_wiring.go)
			}
		}
	})
	return r, nil
}

// newAuthService returns nil when there is no database, so the rest of the API still runs.
func newAuthService(d Deps) (*authapp.Service, error) {
	if d.Pool == nil {
		slog.Warn("auth disabled: DATABASE_URL is not set")
		return nil, nil
	}
	c := d.Config.Auth
	var mailer authdomain.Mailer = authinfra.LogMailer{}
	if c.SMTPHost != "" {
		mailer = authinfra.NewSMTPMailer(authinfra.SMTPConfig{Host: c.SMTPHost, Port: c.SMTPPort, User: c.SMTPUser, Password: c.SMTPPassword, From: c.MailFrom})
	}
	var google authdomain.GoogleVerifier
	if c.GoogleClientID != "" {
		google = authinfra.NewGoogleVerifier(c.GoogleClientID)
	}
	return authapp.NewService(authapp.Deps{
		Users: authinfra.NewUserRepo(d.Pool), OTPs: authinfra.NewOTPRepo(d.Pool), Sessions: authinfra.NewSessionRepo(d.Pool),
		Hasher: authinfra.NewBcrypt(12), Tokens: authinfra.NewJWT(c.JWTSecret, "shenzen", c.AccessTTL),
		Mailer: mailer, Google: google, NewID: newID, Now: time.Now,
		Config: authapp.Config{RefreshTTL: c.RefreshTTL, RememberTTL: c.RememberTTL},
	})
}

func newProfileService(d Deps) *profileapp.Service {
	ai := profileinfra.NewExplainer(aiclient.New(d.Config.AIServiceURL, d.Config.AIInternalKey, 15*time.Second))
	return profileapp.NewService(profileinfra.NewRepo(d.Pool), ai, newID, time.Now)
}

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
