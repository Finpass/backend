package httpx

import (
	"net"
	"net/http"
	"strings"
	"sync/atomic"
)

var proxyHops atomic.Int32

// SetProxyHops tells ClientIP how many reverse proxies sit in front of the API and append to X-Forwarded-For.
// 0 (the default) trusts only the socket address. Set it only to the real number of hops: with too many, a client
// can forge the header and dodge the limits.
func SetProxyHops(n int) { proxyHops.Store(int32(max(n, 0))) }

// ClientIP is the address used to key per-IP limits. With n trusted hops it takes the n-th entry from the right of
// X-Forwarded-For, which is the one our own proxy added; anything further left is client-supplied.
func ClientIP(r *http.Request) string {
	if n := int(proxyHops.Load()); n > 0 {
		parts := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
		if len(parts) >= n {
			if ip := strings.TrimSpace(parts[len(parts)-n]); net.ParseIP(ip) != nil {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
