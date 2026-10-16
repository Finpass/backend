package httpx

import (
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"shenzen/backend/internal/shared/apperr"
)

type window struct {
	count   int
	resetAt time.Time
}

// RateLimit is a per-IP fixed window; in-memory, so it is per instance (use an edge limiter when scaling out).
func RateLimit(limit int, per time.Duration) func(http.Handler) http.Handler {
	var mu sync.Mutex
	seen := map[string]*window{}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = r.RemoteAddr
			}
			now := time.Now()
			mu.Lock()
			if len(seen) > 10_000 {
				for k, v := range seen {
					if now.After(v.resetAt) {
						delete(seen, k)
					}
				}
			}
			win := seen[ip]
			if win == nil || now.After(win.resetAt) {
				win = &window{resetAt: now.Add(per)}
				seen[ip] = win
			}
			win.count++
			over, retry := win.count > limit, int(time.Until(win.resetAt).Seconds())+1
			mu.Unlock()
			if over {
				w.Header().Set("Retry-After", strconv.Itoa(retry))
				WriteError(w, r, apperr.New(apperr.TooManyRequests, "Too many requests, try again shortly"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
