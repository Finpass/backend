package transport_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"shenzen/backend/internal/features/advisory/application"
	"shenzen/backend/internal/features/advisory/domain"
	"shenzen/backend/internal/features/advisory/transport"
	"shenzen/backend/internal/platform/httpx"
	"shenzen/backend/internal/shared/apperr"
)

// minimal fakes: the service logic is covered in application tests, here only the HTTP shape
type store struct{ domain.Store }

func (store) CreateSession(context.Context, string, string, string, time.Time) error { return nil }
func (store) Recent(context.Context, string, int) ([]domain.Message, error)          { return nil, nil }
func (store) AddMessage(_ context.Context, _, role, c string, cs []domain.Citation, _ json.RawMessage, now time.Time) (domain.Message, error) {
	return domain.Message{ID: 7, Role: role, Content: c, Citations: cs, CreatedAt: now}, nil
}
func (store) Sessions(context.Context, string, int) ([]domain.Session, error) { return nil, nil }
func (store) Title(context.Context, string, string) (string, error)           { return "", domain.ErrNotFound }
func (store) Delete(context.Context, string, string) (bool, error)            { return false, nil }

type ctxs struct{}

func (ctxs) Build(context.Context, string) (domain.UserContext, error) {
	return domain.UserContext{}, nil
}

type ai struct{}

func (ai) Turns(context.Context, string, int) ([]domain.TurnEvent, error) {
	return []domain.TurnEvent{{Sequence: 1, Title: "x", Type: "processing", Status: "in_progress"}}, nil
}

func (ai) Chat(context.Context, domain.ChatRequest) (domain.Answer, error) {
	return domain.Answer{Answer: "hi", Grounded: true}, nil
}

type key struct{}

func router() http.Handler {
	svc := application.NewService(application.Deps{Store: store{}, Contexts: ctxs{}, AI: ai{}, NewID: func() string { return "00000000-0000-4000-8000-000000000001" }, Now: time.Now})
	auth := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := r.Header.Get("X-User")
			if u == "" {
				httpx.WriteError(w, r, apperr.New(apperr.Unauthorized, "no"))
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), key{}, u)))
		})
	}
	r := chi.NewRouter()
	transport.New(svc, auth, func(c context.Context) string { s, _ := c.Value(key{}).(string); return s }).Routes(r)
	return r
}

func call(method, path, user, body string) (int, map[string]any) {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if user != "" {
		req.Header.Set("X-User", user)
	}
	w := httptest.NewRecorder()
	router().ServeHTTP(w, req)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestRoutes(t *testing.T) {
	for _, c := range [][2]string{{"POST", "/advisory/chat"}, {"GET", "/advisory/sessions"}, {"GET", "/advisory/sessions/x"}, {"DELETE", "/advisory/sessions/x"}} {
		if code, _ := call(c[0], c[1], "", "{}"); code != 401 {
			t.Errorf("%v unauthenticated: %d", c, code)
		}
	}
	code, b := call("POST", "/advisory/chat", "u", `{"message":"halo","lang":"id"}`)
	d, _ := b["data"].(map[string]any)
	m, _ := d["message"].(map[string]any)
	if code != 200 || d["sessionId"] == nil || d["grounded"] != true || m["role"] != "assistant" || m["id"].(float64) != 7 {
		t.Fatalf("%d %v", code, b)
	}
	if cs, ok := m["citations"].([]any); !ok || len(cs) != 0 {
		t.Fatalf("citations must be an empty array: %v", m["citations"])
	}
	for _, body := range []string{`{"message":"","lang":"id"}`, `{"message":"x","lang":"fr"}`, `{"message":"x","lang":"id","sessionId":"nope"}`, `{"message":"x","lang":"id","extra":1}`} {
		if code, _ := call("POST", "/advisory/chat", "u", body); code != 400 && code != 422 {
			t.Errorf("%s: %d", body, code)
		}
	}
	code, b = call("GET", "/advisory/sessions", "u", "")
	if items, ok := b["data"].(map[string]any)["items"].([]any); code != 200 || !ok || len(items) != 0 {
		t.Fatalf("%d %v", code, b)
	}
	for _, id := range []string{"bad", "00000000-0000-4000-8000-000000000009"} {
		if code, _ := call("GET", "/advisory/sessions/"+id, "u", ""); code != 404 {
			t.Errorf("get %s: %d", id, code)
		}
		if code, _ := call("DELETE", "/advisory/sessions/"+id, "u", ""); code != 404 {
			t.Errorf("delete %s: %d", id, code)
		}
	}
}

func TestTurnsEndpoint(t *testing.T) {
	h := router()
	get := func(path, user string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if user != "" {
			req.Header.Set("X-User", user)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	if rec := get("/advisory/runs/11111111-1111-4111-8111-111111111111/turns", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated: %d", rec.Code)
	}
	rec := get("/advisory/runs/11111111-1111-4111-8111-111111111111/turns?since=0", "u1")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"title":"x"`) {
		t.Errorf("turns: %d %s", rec.Code, rec.Body)
	}
	rec = get("/advisory/runs/not-a-uuid/turns", "u1")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Errorf("malformed run id must give an empty list: %d %s", rec.Code, rec.Body)
	}
}
