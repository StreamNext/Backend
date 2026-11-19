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
	assert.Equal(t, map[string]bool{
		"GET /v1/me": true, "PATCH /v1/me": true, "DELETE /v1/me": true, "POST /v1/me/change-password": true,
		"PUT /v1/devices/{deviceId}": true, "DELETE /v1/devices/{deviceId}": true,
	}, secured)
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
			for _, call := range []struct{ method, path, body string }{
				{http.MethodGet, "/v1/me", ""},
				{http.MethodPatch, "/v1/me", `{"displayName":"x"}`},
				{http.MethodDelete, "/v1/me", ""},
				{http.MethodPost, "/v1/me/change-password", `{}`},
				{http.MethodPut, "/v1/devices/" + devID, `{"fcmToken":"t","platform":"ios"}`},
				{http.MethodDelete, "/v1/devices/" + devID, ""},
			} {
				requireError(t, send(t, h.handler, call.method, call.path, call.body, headers), http.StatusUnauthorized, "unauthenticated")
			}
		})
	}
	assert.Zero(t, h.profile.gotID, "the handlers never ran")
	assert.Zero(t, h.account.deleteCalls)
	assert.Zero(t, h.account.gotUser)
	assert.Zero(t, h.devices.registerCalls+h.devices.removeCalls)
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
	for _, base := range []string{"/v1/me", "/v1/me/change-password"} {
		for _, path := range []string{
			base, base + "?x=1", base + "#frag", "/" + base, "/v1//" + base[4:], "/v1/./" + base[4:], "/v1/x/../" + base[4:], base + "/", base + "%2F",
			"/v1/ME" + base[len("/v1/me"):], "/V1/" + base[4:],
		} {
			for _, method := range []string{http.MethodGet, http.MethodPatch, http.MethodHead, http.MethodDelete, http.MethodPost} {
				h := newHarness(t)
				rec := send(t, h.handler, method, path, `{"displayName":"x"}`, nil)
				assert.Less(t, 299, rec.Code, "%s %s must not succeed without a token", method, path)
				assert.Zero(t, h.profile.gotID, "%s %s", method, path)
				assert.Zero(t, h.account.deleteCalls, "%s %s", method, path)
				assert.Zero(t, h.account.gotUser, "%s %s", method, path)
			}
		}
	}
}

func TestAValidTokenWorksOnTheCanonicalPathOnly(t *testing.T) {
	h := newHarness(t)
	assert.Equal(t, http.StatusOK, send(t, h.handler, http.MethodGet, "/v1/me", "", bearer("good-token")).Code)
	assert.Equal(t, http.StatusOK, send(t, h.handler, http.MethodGet, "/v1/me?x=1", "", bearer("good-token")).Code, "query strings are fine")
}

// Parameterised routes: the route pattern contains {deviceId}, so odd spellings of the path matter too.
func TestDeviceRoutesCannotBeReachedWithoutAToken(t *testing.T) {
	for _, path := range []string{
		"/v1/devices/" + devID, "/v1/devices/" + devID + "?x=1", "//v1/devices/" + devID, "/v1/devices//" + devID,
		"/v1/devices/./" + devID, "/v1/x/../devices/" + devID, "/v1/devices/" + devID + "/", "/v1/devices/not-a-uuid",
		"/v1/DEVICES/" + devID, "/v1/devices/" + devID + "%2F..", "/v1/devices/",
	} {
		for _, method := range []string{http.MethodPut, http.MethodDelete, http.MethodGet, http.MethodHead, http.MethodPost} {
			h := newHarness(t)
			rec := send(t, h.handler, method, path, `{"fcmToken":"t","platform":"ios"}`, nil)
			assert.Less(t, 299, rec.Code, "%s %s must not succeed without a token", method, path)
			assert.Zero(t, h.devices.registerCalls+h.devices.removeCalls, "%s %s", method, path)
		}
	}
}
