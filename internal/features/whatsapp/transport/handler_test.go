package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"shenzen/backend/internal/features/whatsapp/application"
	"shenzen/backend/internal/features/whatsapp/domain"
	"shenzen/backend/internal/platform/httpx"
	"shenzen/backend/internal/shared/apperr"
)

type store struct{ domain.Store }

func (store) ContactByPhone(context.Context, string) (domain.Contact, error) {
	return domain.Contact{}, domain.ErrNotFound
}
func (store) Contact(context.Context, string) (domain.Contact, error) {
	return domain.Contact{}, domain.ErrNotFound
}

func router(t *testing.T) http.Handler {
	svc := application.NewService(application.Deps{Store: store{}, Secret: "sec", Now: time.Now})
	deny := func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			httpx.WriteError(w, r, apperr.New(apperr.Unauthorized, "no token"))
		})
	}
	r := chi.NewRouter()
	New(svc, deny, func(context.Context) string { return "u" }).Routes(r)
	return r
}

func TestWebhookIsPublicButSigned(t *testing.T) {
	body := `{"event":"message","payload":{"id":"1","from":"6280000000000@s.whatsapp.net","body":"MORE"}}`
	do := func(sig string) int {
		req := httptest.NewRequest(http.MethodPost, "/whatsapp/webhook", strings.NewReader(body))
		if sig != "" {
			req.Header.Set("X-Hub-Signature-256", sig)
		}
		w := httptest.NewRecorder()
		router(t).ServeHTTP(w, req)
		return w.Code
	}
	if c := do(""); c != http.StatusUnauthorized {
		t.Errorf("unsigned: %d", c)
	}
	if c := do("sha256=abcd"); c != http.StatusUnauthorized {
		t.Errorf("bad signature: %d", c)
	}
	if c := do(domain.Sign("sec", []byte(body))); c != http.StatusOK { // unknown number is ignored, still 200
		t.Errorf("signed: %d", c)
	}
}

func TestUserRoutesNeedAuth(t *testing.T) {
	for _, tc := range [][2]string{{"GET", "/whatsapp/settings"}, {"PUT", "/whatsapp/settings"}, {"POST", "/whatsapp/verify/start"}, {"POST", "/whatsapp/verify/confirm"}, {"GET", "/briefing/preview"}, {"POST", "/briefing/send-test"}} {
		w := httptest.NewRecorder()
		router(t).ServeHTTP(w, httptest.NewRequest(tc[0], tc[1], strings.NewReader("{}")))
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: %d", tc[0], tc[1], w.Code)
		}
	}
}

func TestLocale(t *testing.T) {
	for q, want := range map[string]string{"/x?locale=en": "en", "/x?locale=id": "id", "/x": "", "/x?locale=fr": ""} {
		if got := locale(httptest.NewRequest("GET", q, nil)); got != want {
			t.Errorf("%s: %q", q, got)
		}
	}
	r := httptest.NewRequest("GET", "/x", nil)
	r.Header.Set("Accept-Language", "en-US,en;q=0.9")
	if locale(r) != "en" {
		t.Error("accept-language")
	}
}

func TestCodesAndTestsAreCappedPerUser(t *testing.T) {
	svc := application.NewService(application.Deps{Store: store{}, Secret: "sec", Now: time.Now})
	pass := func(next http.Handler) http.Handler { return next }
	r := chi.NewRouter()
	r.Use(httpx.Recover) // the stub service has no dependencies: the point here is only the cap
	New(svc, pass, func(context.Context) string { return "u" }).Routes(r)
	for _, path := range []string{"/whatsapp/verify/start", "/briefing/send-test"} {
		var last int
		for i := 0; i < 6; i++ {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}")))
			last = w.Code
			if i < 5 && last == http.StatusTooManyRequests {
				t.Fatalf("%s: capped too early at request %d", path, i+1)
			}
		}
		if last != http.StatusTooManyRequests {
			t.Errorf("%s: the sixth request got %d, want 429", path, last)
		}
	}
}
