package rest

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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

func TestStrictLimitCoversEndpointsThatCheckOrTriggerThings(t *testing.T) {
	cases := []struct {
		method, path string
		strict       bool
	}{
		{http.MethodPost, "/v1/auth/login", true},
		{http.MethodPost, "/v1/auth/forgot-password", true},
		{http.MethodPost, "/v1/me/change-password", true},
		{http.MethodDelete, "/v1/me", true},
		{http.MethodGet, "/v1/me", false},
		{http.MethodPatch, "/v1/me", false},
		{http.MethodGet, "/v1/health", false},
	}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, c.path, nil)
		assert.Equal(t, c.strict, isStrict(r), "%s %s", c.method, c.path)
	}
}

func TestPasswordCheckingEndpointsShareTheStrictBudget(t *testing.T) {
	now := epochTime
	h := at(t, &now, Limits{Auth: LimitRule{Rate: 0.1, Burst: 2}})
	call := func(method, path, body string) int {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer good-token")
		req.RemoteAddr = "198.51.100.1:4000"
		rec := httptest.NewRecorder()
		h.handler.ServeHTTP(rec, req)
		return rec.Code
	}
	changeBody := `{"currentPassword":"x","newPassword":"a-brand-new-pass","deviceId":"` + devID + `"}`

	assert.Equal(t, http.StatusOK, call(http.MethodPost, "/v1/me/change-password", changeBody))
	assert.Equal(t, http.StatusNoContent, call(http.MethodDelete, "/v1/me", ""))
	assert.Equal(t, http.StatusTooManyRequests, call(http.MethodPost, "/v1/me/change-password", changeBody), "a stolen token can't guess the current password at will")
	assert.Equal(t, http.StatusOK, call(http.MethodGet, "/v1/me", ""), "reading the profile is not limited by it")
}
