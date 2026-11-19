package rest

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/StreamNext/Backend/internal/apperr"
)

// LimitRule is a token bucket per client IP: Rate requests/second refill, up to Burst at once.
// A zero Burst disables the rule.
type LimitRule struct {
	Rate  rate.Limit
	Burst int
}

type Limits struct {
	Global LimitRule // every request
	Auth   LimitRule // additionally, the endpoints where passwords can be guessed or emails triggered (see isStrict)
}

func DefaultLimits() Limits {
	return Limits{
		Global: LimitRule{Rate: 10, Burst: 60},
		Auth:   LimitRule{Rate: 0.5, Burst: 40}, // 30 attempts a minute, after an initial burst of 40
	}
}

const (
	sweepEvery = time.Minute
	idleAfter  = 10 * time.Minute
)

// ipLimiters keeps one token bucket per IP.
// shortcut: in memory and per instance, so with several Cloud Run instances the effective limit is
// higher; move to Postgres or Redis if abused. It also keys on the connection's address, which
// behind Cloud Run's proxy is the proxy: switch to X-Forwarded-For when deploying there.
type ipLimiters struct {
	rule LimitRule

	mu        sync.Mutex
	buckets   map[string]*bucket
	lastSweep time.Time
}

type bucket struct {
	limiter *rate.Limiter
	seen    time.Time
}

func newIPLimiters(rule LimitRule) *ipLimiters {
	return &ipLimiters{rule: rule, buckets: map[string]*bucket{}}
}

// allow takes one token for ip; if none is available it returns how long to wait.
func (l *ipLimiters) allow(ip string, now time.Time) (bool, time.Duration) {
	if l.rule.Burst == 0 {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	if now.Sub(l.lastSweep) > sweepEvery { // drop idle buckets so the map can't grow forever
		for k, b := range l.buckets {
			if now.Sub(b.seen) > idleAfter {
				delete(l.buckets, k)
			}
		}
		l.lastSweep = now
	}
	b, ok := l.buckets[ip]
	if !ok {
		b = &bucket{limiter: rate.NewLimiter(l.rule.Rate, l.rule.Burst)}
		l.buckets[ip] = b
	}
	b.seen = now

	res := b.limiter.ReserveN(now, 1)
	if wait := res.DelayFrom(now); wait > 0 {
		res.CancelAt(now) // don't spend a token on a rejected request
		return false, wait
	}
	return true, 0
}

func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, now := clientIP(r), s.now()
		limiters := []*ipLimiters{s.globalRate}
		if isStrict(r) {
			limiters = append(limiters, s.authRate)
		}
		for _, l := range limiters {
			if ok, wait := l.allow(ip, now); !ok {
				w.Header().Set("Retry-After", strconv.Itoa(max(1, int(math.Ceil(wait.Seconds())))))
				s.writeError(w, apperr.New(apperr.TooManyRequests, "too many requests"))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// isStrict marks the requests that get the stricter limit on top of the global one: everything under
// /v1/auth/ (login, register, refresh, email flows) and the two signed-in endpoints that check the
// current password, which a stolen access token could otherwise use to guess it.
func isStrict(r *http.Request) bool {
	p := r.URL.Path
	return strings.HasPrefix(p, "/v1/auth/") || p == "/v1/me/change-password" || (p == "/v1/me" && r.Method == http.MethodDelete)
}

func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
