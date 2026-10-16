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

	"shenzen/backend/internal/features/portfolio/application"
	"shenzen/backend/internal/features/portfolio/domain"
	"shenzen/backend/internal/features/portfolio/transport"
	"shenzen/backend/internal/platform/httpx"
	"shenzen/backend/internal/shared/apperr"
)

type mem struct{ rows []domain.Holding }

func (m *mem) Insert(_ context.Context, h domain.Holding, _ int) error {
	m.rows = append(m.rows, h)
	return nil
}
func (m *mem) List(_ context.Context, u string) ([]domain.Holding, error) {
	var o []domain.Holding
	for _, r := range m.rows {
		if r.UserID == u {
			o = append(o, r)
		}
	}
	return o, nil
}
func (m *mem) Get(_ context.Context, u, id string) (domain.Holding, error) {
	for _, r := range m.rows {
		if r.UserID == u && r.ID == id {
			return r, nil
		}
	}
	return domain.Holding{}, domain.ErrNotFound
}
func (m *mem) Update(_ context.Context, h domain.Holding) error {
	for i, r := range m.rows {
		if r.ID == h.ID && r.UserID == h.UserID {
			m.rows[i] = h
			return nil
		}
	}
	return domain.ErrNotFound
}
func (m *mem) Delete(_ context.Context, u, id string) error {
	for i, r := range m.rows {
		if r.ID == id && r.UserID == u {
			m.rows = append(m.rows[:i], m.rows[i+1:]...)
			return nil
		}
	}
	return domain.ErrNotFound
}

type mkt struct{}

func (mkt) Search(context.Context, string, string, int) ([]domain.AssetHit, error) {
	return []domain.AssetHit{{Symbol: "BBCA.JK", Name: "BCA", AssetClass: "equity", Currency: "IDR", Exchange: "JKT", LastPrice: 5225, LastTradeDate: "2026-10-16"}}, nil
}
func (mkt) Assets(_ context.Context, s []string) (map[string]domain.AssetInfo, error) {
	out := map[string]domain.AssetInfo{}
	for _, x := range s {
		if x == "BBCA.JK" {
			out[x] = domain.AssetInfo{Symbol: x, Name: "BCA", AssetClass: "equity", Currency: "IDR", Exchange: "JKT"}
		}
	}
	return out, nil
}
func (mkt) Series(_ context.Context, s []string, _ time.Time) (map[string][]domain.Point, error) {
	out := map[string][]domain.Point{}
	for _, x := range s {
		out[x] = []domain.Point{{Date: time.Now().UTC().Truncate(24 * time.Hour), Value: 5000}}
	}
	return out, nil
}
func (mkt) Dividends(context.Context, []string) (map[string][]domain.Dividend, error) {
	return nil, nil
}
func (mkt) USDIDR(context.Context, time.Time) ([]domain.Point, error) { return nil, nil }

type key struct{}

func router() http.Handler {
	n := 0
	svc := application.NewService(&mem{}, mkt{}, func() string { n++; return "id" + string(rune('0'+n)) }, time.Now)
	auth := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := r.Header.Get("X-User")
			if u == "" {
				httpx.WriteError(w, r, httpxUnauthorized())
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), key{}, u)))
		})
	}
	r := chi.NewRouter()
	transport.New(svc, auth, func(c context.Context) string { s, _ := c.Value(key{}).(string); return s }).Routes(r)
	return r
}

func call(h http.Handler, method, path, user, body string) (int, map[string]any) {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if user != "" {
		req.Header.Set("X-User", user)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestRoutes(t *testing.T) {
	h := router()
	if c, _ := call(h, "GET", "/portfolio/holdings", "", ""); c != 401 {
		t.Fatalf("unauthenticated: %d", c)
	}
	c, b := call(h, "GET", "/assets/search?q=bbca", "a", "")
	if c != 200 || b["success"] != true || len(b["data"].([]any)) != 1 {
		t.Fatalf("search %d %v", c, b)
	}
	c, b = call(h, "POST", "/portfolio/holdings", "a", `{"symbol":"BBCA.JK","units":10,"avgCost":4000,"acquiredAt":"2026-01-01"}`)
	if c != 201 {
		t.Fatalf("create %d %v", c, b)
	}
	d := b["data"].(map[string]any)
	id := d["id"].(string)
	if d["marketValue"].(float64) != 50000 || d["symbol"] != "BBCA.JK" {
		t.Fatalf("view %v", d)
	}
	for name, body := range map[string]string{
		"missing avgCost": `{"symbol":"BBCA.JK","units":1,"acquiredAt":"2026-01-01"}`,
		"units zero":      `{"symbol":"BBCA.JK","units":0,"avgCost":1,"acquiredAt":"2026-01-01"}`,
		"unknown field":   `{"symbol":"BBCA.JK","units":1,"avgCost":1,"acquiredAt":"2026-01-01","x":1}`,
		"not json":        `nope`,
	} {
		c, b = call(h, "POST", "/portfolio/holdings", "a", body)
		if (name == "unknown field" || name == "not json") && c != 400 || (name != "unknown field" && name != "not json") && c != 422 {
			t.Errorf("%s: %d %v", name, c, b)
		}
		if b["success"] != false || b["error"] == nil {
			t.Errorf("%s: envelope %v", name, b)
		}
	}
	if c, _ = call(h, "PATCH", "/portfolio/holdings/"+id, "b", `{"units":1}`); c != 404 {
		t.Fatalf("other user patch: %d", c)
	}
	if c, b = call(h, "PATCH", "/portfolio/holdings/"+id, "a", `{"units":20}`); c != 200 || b["data"].(map[string]any)["units"].(float64) != 20 {
		t.Fatalf("patch %d %v", c, b)
	}
	if c, _ = call(h, "DELETE", "/portfolio/holdings/"+id, "b", ""); c != 404 {
		t.Fatalf("other user delete: %d", c)
	}
	if c, _ = call(h, "DELETE", "/portfolio/holdings/"+id, "a", ""); c != 204 {
		t.Fatalf("delete: %d", c)
	}
	c, b = call(h, "GET", "/portfolio/holdings", "a", "")
	if c != 200 || len(b["data"].(map[string]any)["items"].([]any)) != 0 {
		t.Fatalf("list %d %v", c, b)
	}
}

func httpxUnauthorized() error { return apperrUnauthorized }

var apperrUnauthorized = apperr.New(apperr.Unauthorized, "no")
