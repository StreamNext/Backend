package rest

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var epochTime = time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

func TestSecuredRoutesComeFromTheContract(t *testing.T) {
	secured, err := securedRoutes()
	require.NoError(t, err)
	// Pins exactly which operations the contract protects: a new public route, or an
	// accidentally unprotected one, shows up as a diff here.
	assert.Equal(t, map[string]bool{"GET /v1/me": true, "PATCH /v1/me": true}, secured)
}

func TestSecuredRoutesRejectMissingOrBadCredentials(t *testing.T) {
	h := newHarness(t)
	for name, headers := range map[string]map[string]string{
		"no header":        nil,
		"wrong scheme":     {"Authorization": "Basic Zm9vOmJhcg=="},
		"empty bearer":     {"Authorization": "Bearer "},
		"bearer no space":  {"Authorization": "Bearertoken"},
		"unknown token":    bearer("nope"),
		"lowercase prefix": {"Authorization": "bearer good-token"},
	} {
		t.Run(name, func(t *testing.T) {
			for _, call := range []struct{ method, body string }{{http.MethodGet, ""}, {http.MethodPatch, `{"displayName":"x"}`}} {
				requireError(t, send(t, h.handler, call.method, "/v1/me", call.body, headers), http.StatusUnauthorized, "unauthenticated")
			}
		})
	}
	assert.Zero(t, h.profile.gotID, "the handler never ran")
}

func TestHeadRequestToASecuredRouteIsAlsoChecked(t *testing.T) {
	rec := send(t, newHarness(t).handler, http.MethodHead, "/v1/me", "", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestUserIDIsZeroWithoutAuthentication(t *testing.T) {
	assert.Zero(t, userID(httpRequest(t).Context()))
}

// The bearer check matches on the router's own pattern, so odd URL spellings must not slip past it:
// either they are rejected, or the router redirects them to the canonical (checked) path without
// ever running the handler.
func TestSecuredRouteCannotBeReachedWithoutAToken(t *testing.T) {
	for _, path := range []string{
		"/v1/me", "/v1/me?x=1", "/v1/me#frag", "//v1/me", "/v1//me", "/v1/./me", "/v1/x/../me", "/v1/me/", "/v1/ME", "/V1/me", "/v1/me%2F", "/v1/%6De",
	} {
		for _, method := range []string{http.MethodGet, http.MethodPatch, http.MethodHead} {
			h := newHarness(t)
			rec := send(t, h.handler, method, path, `{"displayName":"x"}`, nil)
			assert.NotEqual(t, http.StatusOK, rec.Code, "%s %s", method, path)
			assert.Zero(t, h.profile.gotID, "the profile handler must not run: %s %s", method, path)
		}
	}
}

func TestAValidTokenWorksOnTheCanonicalPathOnly(t *testing.T) {
	h := newHarness(t)
	assert.Equal(t, http.StatusOK, send(t, h.handler, http.MethodGet, "/v1/me", "", bearer("good-token")).Code)
	assert.Equal(t, http.StatusOK, send(t, h.handler, http.MethodGet, "/v1/me?x=1", "", bearer("good-token")).Code, "query strings are fine")
}
