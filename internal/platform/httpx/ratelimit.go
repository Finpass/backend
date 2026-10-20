package httpx

import (
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

// Limiter is a fixed-window counter per key; in-memory, so it is per instance (use an edge limiter when scaling out).
type Limiter struct {
	mu    sync.Mutex
	limit int
	per   time.Duration
	seen  map[string]*window
}

func NewLimiter(limit int, per time.Duration) *Limiter {
	return &Limiter{limit: limit, per: per, seen: map[string]*window{}}
}

// current returns the live window for key (nil when none); callers hold mu.
func (l *Limiter) current(key string, now time.Time) *window {
	if len(l.seen) > 10_000 {
		for k, v := range l.seen {
			if now.After(v.resetAt) {
				delete(l.seen, k)
			}
		}
	}
	if w := l.seen[key]; w != nil && !now.After(w.resetAt) {
		return w
	}
	return nil
}

// Take counts one attempt and reports whether it is still within the limit, plus seconds until the window resets.
func (l *Limiter) Take(key string) (bool, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	w := l.current(key, now)
	if w == nil {
		w = &window{resetAt: now.Add(l.per)}
		l.seen[key] = w
	}
	w.count++
	return w.count <= l.limit, int(time.Until(w.resetAt).Seconds()) + 1
}

// Blocked reports whether key has already used its limit, without counting an attempt (for counting failures only).
func (l *Limiter) Blocked(key string) (bool, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	w := l.current(key, time.Now())
	if w == nil || w.count < l.limit {
		return false, 0
	}
	return true, int(time.Until(w.resetAt).Seconds()) + 1
}

func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	delete(l.seen, key)
	l.mu.Unlock()
}

// TooMany writes the 429 response shared by every limiter.
func TooMany(w http.ResponseWriter, r *http.Request, retry int) {
	w.Header().Set("Retry-After", strconv.Itoa(retry))
	WriteError(w, r, apperr.New(apperr.TooManyRequests, "Too many requests, try again shortly"))
}

// RateLimit is a per-IP limiter as middleware.
func RateLimit(limit int, per time.Duration) func(http.Handler) http.Handler {
	l := NewLimiter(limit, per)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ok, retry := l.Take(ClientIP(r)); !ok {
				TooMany(w, r, retry)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
