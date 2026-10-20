package transport

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"shenzen/backend/internal/features/whatsapp/application"
	"shenzen/backend/internal/platform/audit"
	"shenzen/backend/internal/platform/httpx"
	"shenzen/backend/internal/shared/apperr"
)

type Handler struct {
	svc    *application.Service
	auth   func(http.Handler) http.Handler
	userID func(context.Context) string
	// per-user caps on messages that reach a phone, whichever number is saved: a code or test text costs us sender reputation
	codes, tests *httpx.Limiter
	audit        *audit.Recorder // nil records nothing
}

// Audited makes the handler record opt-in changes and number verification in the security trail.
func (h *Handler) Audited(r *audit.Recorder) *Handler { h.audit = r; return h }

func New(svc *application.Service, auth func(http.Handler) http.Handler, userID func(context.Context) string) *Handler {
	return &Handler{svc: svc, auth: auth, userID: userID, codes: httpx.NewLimiter(5, time.Hour), tests: httpx.NewLimiter(5, time.Hour)}
}

func capped(w http.ResponseWriter, l *httpx.Limiter, key string) error {
	if ok, retry := l.Take(key); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(retry))
		return apperr.New(apperr.TooManyRequests, "Too many messages, try again later")
	}
	return nil
}

func (h *Handler) Routes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(h.auth)
		r.Method(http.MethodGet, "/whatsapp/settings", httpx.HandlerFunc(h.getSettings))
		r.Method(http.MethodPut, "/whatsapp/settings", httpx.HandlerFunc(h.putSettings))
		r.Method(http.MethodPost, "/whatsapp/verify/start", httpx.HandlerFunc(h.verifyStart))
		r.Method(http.MethodPost, "/whatsapp/verify/confirm", httpx.HandlerFunc(h.verifyConfirm))
		r.Method(http.MethodGet, "/briefing/preview", httpx.HandlerFunc(h.preview))
		r.Method(http.MethodPost, "/briefing/send-test", httpx.HandlerFunc(h.sendTest))
	})
	// public: authenticated by the HMAC signature header instead of a user session
	r.Method(http.MethodPost, "/whatsapp/webhook", httpx.HandlerFunc(h.webhook))
}

// locale: ?locale= wins, then Accept-Language; "" lets the service fall back to the saved preference.
func locale(r *http.Request) string {
	if l := r.URL.Query().Get("locale"); l == "id" || l == "en" {
		return l
	}
	tag := strings.ToLower(strings.TrimSpace(strings.SplitN(r.Header.Get("Accept-Language"), ",", 2)[0]))
	switch {
	case tag == "en" || strings.HasPrefix(tag, "en-"):
		return "en"
	case tag == "id" || strings.HasPrefix(tag, "id-"):
		return "id"
	}
	return ""
}

func (h *Handler) getSettings(w http.ResponseWriter, r *http.Request) error {
	s, err := h.svc.Settings(r.Context(), h.userID(r.Context()))
	if err != nil {
		return err
	}
	httpx.OK(w, http.StatusOK, "WhatsApp settings fetched", s)
	return nil
}

type settingsRequest struct {
	Phone         string `json:"phone"`
	OptIn         bool   `json:"optIn"`
	SendTimeLocal string `json:"sendTimeLocal"`
	Frequency     string `json:"frequency"`
	Timezone      string `json:"timezone"`
	Language      string `json:"language"` // briefing language; empty keeps the saved one
}

func (h *Handler) putSettings(w http.ResponseWriter, r *http.Request) error {
	req, err := httpx.Decode[settingsRequest](w, r)
	if err != nil {
		return err
	}
	lang := req.Language
	if lang == "" {
		lang = locale(r)
	} else if lang != "id" && lang != "en" {
		return apperr.Validation(apperr.Detail{Field: "language", Message: "must_be_id_or_en"})
	}
	s, err := h.svc.UpdateSettings(r.Context(), h.userID(r.Context()), application.SettingsUpdate{
		Phone: req.Phone, OptIn: req.OptIn, SendTimeLocal: req.SendTimeLocal, Frequency: req.Frequency, Timezone: req.Timezone, Locale: lang})
	if err != nil {
		return err
	}
	h.audit.Record(r.Context(), audit.Event{Actor: h.userID(r.Context()), Action: "whatsapp.settings.changed", Meta: map[string]any{"optIn": req.OptIn, "frequency": req.Frequency}})
	httpx.OK(w, http.StatusOK, "WhatsApp settings saved", s)
	return nil
}

func (h *Handler) verifyStart(w http.ResponseWriter, r *http.Request) error {
	if err := capped(w, h.codes, h.userID(r.Context())); err != nil {
		return err
	}
	if err := h.svc.VerifyStart(r.Context(), h.userID(r.Context())); err != nil {
		return err
	}
	httpx.OK(w, http.StatusOK, "Verification code sent", map[string]bool{"sent": true})
	return nil
}

type confirmRequest struct {
	Code string `json:"code" validate:"required,len=6,numeric"`
}

func (h *Handler) verifyConfirm(w http.ResponseWriter, r *http.Request) error {
	req, err := httpx.Decode[confirmRequest](w, r)
	if err != nil {
		return err
	}
	s, err := h.svc.VerifyConfirm(r.Context(), h.userID(r.Context()), req.Code)
	if err != nil {
		return err
	}
	h.audit.Record(r.Context(), audit.Event{Actor: h.userID(r.Context()), Action: "whatsapp.number.verified"})
	httpx.OK(w, http.StatusOK, "WhatsApp number verified", s)
	return nil
}

func (h *Handler) preview(w http.ResponseWriter, r *http.Request) error {
	p, err := h.svc.Preview(r.Context(), h.userID(r.Context()), locale(r))
	if err != nil {
		return err
	}
	httpx.OK(w, http.StatusOK, "Briefing preview", p)
	return nil
}

func (h *Handler) sendTest(w http.ResponseWriter, r *http.Request) error {
	if err := capped(w, h.tests, h.userID(r.Context())); err != nil {
		return err
	}
	p, err := h.svc.SendTest(r.Context(), h.userID(r.Context()), locale(r))
	if err != nil {
		return err
	}
	httpx.OK(w, http.StatusOK, "Test briefing sent", p)
	return nil
}

func (h *Handler) webhook(w http.ResponseWriter, r *http.Request) error {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		return apperr.Wrap(apperr.BadRequest, "Unreadable body", err)
	}
	if err := h.svc.HandleWebhook(r.Context(), body, r.Header.Get("X-Hub-Signature-256")); err != nil {
		return err
	}
	httpx.OK(w, http.StatusOK, "OK", map[string]bool{"received": true})
	return nil
}
