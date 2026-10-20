package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientIP(t *testing.T) {
	defer SetProxyHops(0)
	req := func(remote, xff string) *http.Request {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = remote
		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}
		return r
	}
	for _, c := range []struct {
		name, remote, xff string
		hops              int
		want              string
	}{
		{"no proxy trusted: header is ignored", "10.0.0.1:5555", "6.6.6.6", 0, "10.0.0.1"},
		{"one hop: the entry our proxy added", "10.0.0.1:5555", "6.6.6.6, 1.2.3.4", 1, "1.2.3.4"},
		{"a forged left entry does not count", "10.0.0.1:5555", "9.9.9.9, 1.2.3.4", 1, "1.2.3.4"},
		{"two hops", "10.0.0.1:5555", "1.1.1.1, 2.2.2.2, 10.9.9.9", 2, "2.2.2.2"},
		{"header shorter than the hops: fall back", "10.0.0.1:5555", "1.2.3.4", 2, "10.0.0.1"},
		{"not an address: fall back", "10.0.0.1:5555", "nonsense", 1, "10.0.0.1"},
		{"no header: socket address", "10.0.0.1:5555", "", 1, "10.0.0.1"},
	} {
		SetProxyHops(c.hops)
		if got := ClientIP(req(c.remote, c.xff)); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
}
