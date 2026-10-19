package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSecurityHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	SecurityHeaders(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	for _, k := range []string{"Strict-Transport-Security", "X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy", "Cache-Control", "Content-Security-Policy"} {
		if rec.Header().Get(k) == "" {
			t.Errorf("missing %s", k)
		}
	}
}

func TestLimiter(t *testing.T) {
	l := NewLimiter(2, time.Minute)
	if ok, _ := l.Take("a"); !ok {
		t.Fatal("1st take should pass")
	}
	if blocked, _ := l.Blocked("a"); blocked {
		t.Fatal("not blocked after 1 of 2")
	}
	l.Take("a")
	if blocked, _ := l.Blocked("a"); !blocked {
		t.Fatal("blocked once the limit is used")
	}
	if ok, _ := l.Take("a"); ok {
		t.Fatal("3rd take must fail")
	}
	if ok, _ := l.Take("b"); !ok {
		t.Fatal("keys are independent")
	}
	l.Reset("a")
	if blocked, _ := l.Blocked("a"); blocked {
		t.Fatal("reset clears the key")
	}
}
