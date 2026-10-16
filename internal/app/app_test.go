package app_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"shenzen/backend/internal/app"
	"shenzen/backend/internal/platform/config"
)

func newRouter(t *testing.T) http.Handler {
	t.Helper()
	h, err := app.NewRouter(app.Deps{Config: config.Config{}})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func call(t *testing.T, method, path, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	h := newRouter(t)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("response is not JSON: %q", rec.Body.String())
	}
	return rec, out
}

func errCode(out map[string]any) string {
	e, _ := out["error"].(map[string]any)
	code, _ := e["code"].(string)
	return code
}

func TestErrorEnvelopes(t *testing.T) {
	cases := []struct {
		name, method, path, body, code string
		status                         int
	}{
		{"unknown route", "GET", "/nope", "", "NOT_FOUND", 404},
		{"wrong method", "DELETE", "/healthz", "", "METHOD_NOT_ALLOWED", 405},
		{"missing note", "GET", "/api/v1/notes/zzz", "", "NOT_FOUND", 404},
		{"invalid json", "POST", "/api/v1/notes", "{", "BAD_REQUEST", 400},
		{"unknown field", "POST", "/api/v1/notes", `{"title":"a","x":1}`, "BAD_REQUEST", 400},
		{"missing title", "POST", "/api/v1/notes", `{}`, "VALIDATION_FAILED", 422},
		{"blank title", "POST", "/api/v1/notes", `{"title":"   "}`, "VALIDATION_FAILED", 422},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec, out := call(t, c.method, c.path, c.body)
			if rec.Code != c.status || errCode(out) != c.code || out["success"] != false {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if rec.Header().Get("X-Request-Id") == "" || out["requestId"] == "" {
				t.Fatal("request id missing")
			}
		})
	}
}

func TestCreateThenGet(t *testing.T) {
	h := newRouter(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/notes", strings.NewReader(`{"title":"  hello ","body":"b"}`)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Data struct{ ID, Title string } `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.Data.Title != "hello" {
		t.Fatalf("title not trimmed: %+v", created)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/notes/"+created.Data.ID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("get: %d %s", rec.Code, rec.Body.String())
	}
}
