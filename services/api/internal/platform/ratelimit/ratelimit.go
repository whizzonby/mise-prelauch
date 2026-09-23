// Package ratelimit provides per-key token-bucket limiting held in process
// memory. With more than one API instance the effective limit is multiplied by
// the instance count; the AWS WAF rate rule is the global backstop.
package ratelimit

import (
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"mise.tt/api/internal/platform/httpx"
)

type entry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type Limiter struct {
	mu      sync.Mutex
	entries map[string]*entry
	limit   rate.Limit
	burst   int
}

// New allows `events` per `per` for each key, with bursts up to `burst`.
func New(events int, per time.Duration, burst int) *Limiter {
	l := &Limiter{
		entries: make(map[string]*entry),
		limit:   rate.Every(per / time.Duration(events)),
		burst:   burst,
	}
	go l.sweep(per * 2)
	return l
}

func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[key]
	if !ok {
		e = &entry{limiter: rate.NewLimiter(l.limit, l.burst)}
		l.entries[key] = e
	}
	e.lastSeen = time.Now()
	return e.limiter.Allow()
}

func (l *Limiter) sweep(idle time.Duration) {
	if idle < time.Minute {
		idle = time.Minute
	}
	for range time.Tick(idle) {
		cutoff := time.Now().Add(-idle)
		l.mu.Lock()
		for k, e := range l.entries {
			if e.lastSeen.Before(cutoff) {
				delete(l.entries, k)
			}
		}
		l.mu.Unlock()
	}
}

// ByIP limits a handler per client address.
func (l *Limiter) ByIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.Allow(httpx.ClientIP(r.Context())) {
			w.Header().Set("Retry-After", "60")
			httpx.Fail(w, r, httpx.ErrRateLimited)
			return
		}
		next.ServeHTTP(w, r)
	})
}
