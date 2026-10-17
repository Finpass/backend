package transport_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"shenzen/backend/internal/features/discover/application"
	"shenzen/backend/internal/features/discover/domain"
	"shenzen/backend/internal/features/discover/transport"
	"shenzen/backend/internal/platform/httpx"
	"shenzen/backend/internal/shared/apperr"
)

type cat struct{}

func (cat) List(_ context.Context, q domain.Query) (domain.Page, error) {
	p := 5225.0
	return domain.Page{Total: 1, Rows: []domain.Metrics{{Symbol: "BBCA.JK", Name: "BCA", AssetClass: "equity", Currency: "IDR", LastPrice: &p, Industry: "Banks"}}}, nil
}
func (cat) Facets(context.Context) (domain.Facets, error) { return domain.Facets{}, nil }
func (cat) AsOf(context.Context) (time.Time, error) {
	return time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC), nil
}

type stars map[string]bool

func (s stars) Add(_ context.Context, u, sym string) error {
	if sym == "NOPE" {
		return domain.ErrAssetNotFound
	}
	s[u+"|"+sym] = true
	return nil
}
func (s stars) Remove(_ context.Context, u, sym string) error { delete(s, u+"|"+sym); return nil }
func (s stars) Marked(_ context.Context, u string, syms []string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, x := range syms {
		out[x] = s[u+"|"+x]
	}
	return out, nil
}

type key struct{}

func router() http.Handler {
	svc := application.NewService(cat{}, stars{})
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

func call(h http.Handler, method, path, user string) (int, map[string]any) {
	req := httptest.NewRequest(method, path, nil)
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
	for _, p := range []string{"/discover/assets", "/discover/facets"} {
		if c, _ := call(h, "GET", p, ""); c != 401 {
			t.Fatalf("%s unauthenticated: %d", p, c)
		}
	}
	if c, _ := call(h, "PUT", "/discover/watchlist/BBCA.JK", ""); c != 401 {
		t.Fatalf("put unauthenticated: %d", c)
	}
	c, b := call(h, "GET", "/discover/assets?class=equity&sort=ret7d&dir=asc&limit=10", "a")
	if c != 200 || b["success"] != true {
		t.Fatalf("%d %v", c, b)
	}
	d := b["data"].(map[string]any)
	it := d["items"].([]any)[0].(map[string]any)
	for _, k := range []string{"symbol", "name", "assetClass", "assetClassLabelId", "assetClassLabelEn", "exchange", "currency", "lastPrice", "lastTradeDate",
		"ret7d", "ret1y", "marketCap", "analystTarget", "valuation", "growth", "divYieldPct", "industry", "starred"} {
		if _, ok := it[k]; !ok {
			t.Errorf("missing key %s", k)
		}
	}
	if it["ret7d"] != nil || it["analystTarget"] != nil || it["lastPrice"].(float64) != 5225 || d["total"].(float64) != 1 || d["asOf"] != "2026-10-16" {
		t.Fatalf("%v", d)
	}
	if _, ok := d["nextCursor"]; !ok || d["nextCursor"] != nil {
		t.Fatalf("nextCursor must be present and null: %v", d)
	}
	for _, q := range []string{"limit=abc", "limit=500", "class=bond", "sort=x", "watchlist=maybe", "cursor=zz"} {
		if c, _ := call(h, "GET", "/discover/assets?"+q, "a"); c != 400 && c != 422 {
			t.Errorf("%s: %d", q, c)
		}
	}
	if c, _ := call(h, "PUT", "/discover/watchlist/BBCA.JK", "a"); c != 204 {
		t.Fatalf("put %d", c)
	}
	if c, _ := call(h, "PUT", "/discover/watchlist/BBCA.JK", "a"); c != 204 {
		t.Fatalf("idempotent put %d", c)
	}
	if _, b = call(h, "GET", "/discover/assets", "a"); !b["data"].(map[string]any)["items"].([]any)[0].(map[string]any)["starred"].(bool) {
		t.Fatal("starred for a")
	}
	if _, b = call(h, "GET", "/discover/assets", "b"); b["data"].(map[string]any)["items"].([]any)[0].(map[string]any)["starred"].(bool) {
		t.Fatal("starred leaked to b")
	}
	if c, _ := call(h, "PUT", "/discover/watchlist/NOPE", "a"); c != 404 {
		t.Fatalf("unknown %d", c)
	}
	if c, _ := call(h, "DELETE", "/discover/watchlist/BBCA.JK", "a"); c != 204 {
		t.Fatalf("delete %d", c)
	}
	if c, b := call(h, "GET", "/discover/facets", "a"); c != 200 || b["data"].(map[string]any)["classes"] == nil {
		t.Fatalf("facets %d %v", c, b)
	}
}
