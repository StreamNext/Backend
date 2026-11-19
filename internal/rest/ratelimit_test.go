package rest

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func httpRequest(t *testing.T) *http.Request {
	t.Helper()
	return httptest.NewRequest(http.MethodGet, "/", nil)
}

// at builds a harness whose clock the test controls.
func at(t *testing.T, now *time.Time, limits Limits) *harness {
	return newHarness(t, func(d *Deps) {
		d.Limits = limits
		d.Now = func() time.Time { return *now }
	})
}

func fromIP(t *testing.T, h http.Handler, ip, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = ip + ":4000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestGlobalLimitReturns429WithRetryAfter(t *testing.T) {
	now := epochTime
	h := at(t, &now, Limits{Global: LimitRule{Rate: 1, Burst: 2}}).handler

	assert.Equal(t, http.StatusOK, fromIP(t, h, "198.51.100.1", "/v1/health").Code)
	assert.Equal(t, http.StatusOK, fromIP(t, h, "198.51.100.1", "/v1/health").Code)
	rec := fromIP(t, h, "198.51.100.1", "/v1/health")
	requireError(t, rec, http.StatusTooManyRequests, "rate_limited")
	retry, err := strconv.Atoi(rec.Header().Get("Retry-After"))
	require.NoError(t, err)
	assert.GreaterOrEqual(t, retry, 1)
}

func TestLimitsAreCountedPerIP(t *testing.T) {
	now := epochTime
	h := at(t, &now, Limits{Global: LimitRule{Rate: 1, Burst: 1}}).handler

	assert.Equal(t, http.StatusOK, fromIP(t, h, "198.51.100.1", "/v1/health").Code)
	assert.Equal(t, http.StatusTooManyRequests, fromIP(t, h, "198.51.100.1", "/v1/health").Code)
	assert.Equal(t, http.StatusOK, fromIP(t, h, "198.51.100.2", "/v1/health").Code, "another client is unaffected")
}

func TestTokensRefillOverTime(t *testing.T) {
	now := epochTime
	h := at(t, &now, Limits{Global: LimitRule{Rate: 1, Burst: 1}}).handler

	assert.Equal(t, http.StatusOK, fromIP(t, h, "198.51.100.1", "/v1/health").Code)
	assert.Equal(t, http.StatusTooManyRequests, fromIP(t, h, "198.51.100.1", "/v1/health").Code)
	now = now.Add(1100 * time.Millisecond)
	assert.Equal(t, http.StatusOK, fromIP(t, h, "198.51.100.1", "/v1/health").Code, "one token came back")
}

func TestRejectedRequestsDoNotSpendTokens(t *testing.T) {
	now := epochTime
	h := at(t, &now, Limits{Global: LimitRule{Rate: 1, Burst: 1}}).handler

	fromIP(t, h, "198.51.100.1", "/v1/health")
	for i := 0; i < 20; i++ { // hammering while blocked must not push the recovery time out
		fromIP(t, h, "198.51.100.1", "/v1/health")
	}
	now = now.Add(1100 * time.Millisecond)
	assert.Equal(t, http.StatusOK, fromIP(t, h, "198.51.100.1", "/v1/health").Code)
}

func TestAuthLimitOnlyAppliesToAuthEndpoints(t *testing.T) {
	now := epochTime
	h := at(t, &now, Limits{Auth: LimitRule{Rate: 0.1, Burst: 1}}).handler

	post := func(path string) int {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.RemoteAddr = "198.51.100.1:4000"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	assert.Equal(t, http.StatusBadRequest, post("/v1/auth/login"), "first attempt reaches the handler (bad body)")
	assert.Equal(t, http.StatusTooManyRequests, post("/v1/auth/login"))
	assert.Equal(t, http.StatusTooManyRequests, post("/v1/auth/register"), "one shared budget across the auth endpoints")
	assert.Equal(t, http.StatusOK, fromIP(t, h, "198.51.100.1", "/v1/health").Code, "other endpoints are not affected")
}

func TestZeroLimitsDisableRateLimiting(t *testing.T) {
	now := epochTime
	h := at(t, &now, Limits{}).handler
	for i := 0; i < 100; i++ {
		require.Equal(t, http.StatusOK, fromIP(t, h, "198.51.100.1", "/v1/health").Code)
	}
}

func TestIdleBucketsAreSwept(t *testing.T) {
	now := epochTime
	l := newIPLimiters(LimitRule{Rate: 1, Burst: 1})
	l.allow("198.51.100.1", now)
	l.allow("198.51.100.2", now)
	require.Len(t, l.buckets, 2)

	later := now.Add(idleAfter + 2*sweepEvery)
	l.allow("198.51.100.3", later)
	assert.Len(t, l.buckets, 1, "the two idle clients were forgotten, so the map can't grow forever")
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.9:5555"
	assert.Equal(t, "203.0.113.9", clientIP(r))
	r.RemoteAddr = "no-port"
	assert.Equal(t, "no-port", clientIP(r))
}

func TestDefaultLimitsAreEnabled(t *testing.T) {
	d := DefaultLimits()
	assert.NotZero(t, d.Global.Burst)
	assert.NotZero(t, d.Auth.Burst)
	assert.Less(t, float64(d.Auth.Rate), float64(d.Global.Rate), "auth endpoints are stricter")
}
