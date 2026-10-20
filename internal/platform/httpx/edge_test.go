package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serve(h http.Handler, method, target, body string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestRejectNULStopsControlBytesBeforeTheHandler(t *testing.T) {
	called := false
	h := RequestIDMiddleware(RejectNUL(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })))
	for _, target := range []string{"/a?q=%00", "/a?q=x%00y", "/a/%00", "/a?q=%2500x%00"} {
		called = false
		if w := serve(h, "GET", target, "", nil); w.Code != 400 || called {
			t.Errorf("%s -> %d (handler called: %v)", target, w.Code, called)
		}
	}
	for _, target := range []string{"/a?q=ok", "/a/b%20c", "/a?q=%2500"} { // %2500 is a literal "%00" text, not a NUL
		called = false
		if w := serve(h, "GET", target, "", nil); w.Code != 200 || !called {
			t.Errorf("%s -> %d (handler called: %v)", target, w.Code, called)
		}
	}
}

func TestDecodeRefusesAnEscapedNUL(t *testing.T) {
	type in struct {
		Name string `json:"name"`
	}
	// the second body is a literal backslash followed by "u0000": refused too, a conservative false positive nobody types
	for body, want := range map[string]bool{`{"name":"a\u0000b"}`: false, `{"name":"a\\u0000b"}`: false, `{"name":"ok"}`: true} {
		w := httptest.NewRecorder()
		_, err := Decode[in](w, httptest.NewRequest("POST", "/", strings.NewReader(body)))
		if (err == nil) != want {
			t.Errorf("%s: err=%v, want ok=%v", body, err, want)
		}
	}
}

func TestRequestIDOnlyEchoesPlainTokens(t *testing.T) {
	h := RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	for _, id := range []string{strings.Repeat("x", 65), "has space", `a"b`, "<b>", "a;b", "é", "a\tb"} {
		got := serve(h, "GET", "/", "", map[string]string{"X-Request-Id": id}).Header().Get("X-Request-Id")
		if got == id || len(got) != 16 {
			t.Errorf("%.20q was echoed as %q", id, got)
		}
	}
	if got := serve(h, "GET", "/", "", map[string]string{"X-Request-Id": "abc-123_DEF"}).Header().Get("X-Request-Id"); got != "abc-123_DEF" {
		t.Errorf("a plain token must be kept, got %q", got)
	}
}
