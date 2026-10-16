package transport_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"shenzen/backend/internal/features/dashboard/application"
	"shenzen/backend/internal/features/dashboard/domain"
	"shenzen/backend/internal/features/dashboard/transport"
)

type pf struct{}

func (pf) Snapshot(context.Context, string, time.Time) (domain.Snapshot, error) {
	return domain.Snapshot{}, nil
}

type pr struct{}

func (pr) Read(context.Context, string) (domain.Profile, error) {
	return domain.Profile{Status: "none"}, nil
}

func TestEmptyStatesAndBadRange(t *testing.T) {
	svc := application.NewService(pf{}, pr{}, application.RulesRecommender{}, time.Now)
	r := chi.NewRouter()
	transport.New(svc, func(n http.Handler) http.Handler { return n }, func(context.Context) string { return "u" }).Routes(r)
	get := func(p string) (int, map[string]any) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		var b map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &b)
		return w.Code, b
	}
	for _, p := range []string{"/dashboard/home", "/dashboard/home?range=ALL", "/dashboard/portfolio", "/dashboard/portfolio?range=1M"} {
		c, b := get(p)
		if c != 200 || b["success"] != true || b["data"] == nil {
			t.Errorf("%s: %d %v", p, c, b)
		}
	}
	c, b := get("/dashboard/home?range=9Y")
	if c != 422 || b["success"] != false {
		t.Fatalf("%d %v", c, b)
	}
	_, b = get("/dashboard/portfolio")
	if h := b["data"].(map[string]any)["holdings"]; h == nil || len(h.([]any)) != 0 {
		t.Fatalf("holdings must be an empty array: %v", h)
	}
}
