package rest

import (
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/StreamNext/Backend/internal/apperr"
	"github.com/StreamNext/Backend/internal/service"
)

func TestRegisterDevice(t *testing.T) {
	h := newHarness(t)
	rec := send(t, h.handler, http.MethodPut, "/v1/devices/"+devID, `{"fcmToken":"fcm-abc","platform":"android"}`, bearer("good-token"))

	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Empty(t, rec.Body.String())
	assert.Equal(t, userA, h.devices.gotUser, "the owner comes from the token")
	assert.Equal(t, uuid.MustParse(devID), h.devices.gotDevice, "the device comes from the path")
	assert.Equal(t, "fcm-abc", h.devices.gotToken)
	assert.Equal(t, service.PlatformAndroid, h.devices.gotPlatform)
}

func TestRemoveDevice(t *testing.T) {
	h := newHarness(t)
	rec := send(t, h.handler, http.MethodDelete, "/v1/devices/"+devID, "", bearer("good-token"))
	require.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, userA, h.devices.gotUser)
	assert.Equal(t, uuid.MustParse(devID), h.devices.gotDevice)
}

// The first route with a path parameter: a malformed id is rejected by the generated wrapper, through
// our error handler, as the contract's 400 error body.
func TestAMalformedDeviceIdInThePathIsABadRequest(t *testing.T) {
	h := newHarness(t)
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		rec := send(t, h.handler, method, "/v1/devices/not-a-uuid", `{"fcmToken":"t","platform":"ios"}`, bearer("good-token"))
		requireError(t, rec, http.StatusBadRequest, "invalid_argument")
	}
	assert.Zero(t, h.devices.registerCalls+h.devices.removeCalls, "the service never saw it")
}

func TestDeviceEndpointsAreAuthenticatedBeforeTheyAreParsed(t *testing.T) {
	// Without a token even a malformed id is a 401, so nothing is learned about the route's inputs.
	rec := send(t, newHarness(t).handler, http.MethodPut, "/v1/devices/not-a-uuid", `{}`, nil)
	requireError(t, rec, http.StatusUnauthorized, "unauthenticated")
}

func TestDeviceErrors(t *testing.T) {
	cases := map[string]struct {
		err    error
		status int
		code   string
	}{
		"invalid input": {apperr.New(apperr.InvalidArgument, "platform must be ios or android"), http.StatusBadRequest, "invalid_argument"},
		"deleted user":  {apperr.New(apperr.Unauthenticated, "invalid credentials"), http.StatusUnauthorized, "unauthenticated"},
		"unexpected":    {errors.New("pq: down"), http.StatusInternalServerError, "internal"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.devices.err = c.err
			requireError(t, send(t, h.handler, http.MethodPut, "/v1/devices/"+devID, `{"fcmToken":"t","platform":"ios"}`, bearer("good-token")), c.status, c.code)
			requireError(t, send(t, h.handler, http.MethodDelete, "/v1/devices/"+devID, "", bearer("good-token")), c.status, c.code)
		})
	}
}

func TestRegisterDeviceBodies(t *testing.T) {
	h := newHarness(t)
	for name, body := range map[string]string{"empty": "", "not json": "nope", "wrong type": `{"fcmToken":1,"platform":"ios"}`} {
		t.Run(name, func(t *testing.T) {
			requireError(t, send(t, h.handler, http.MethodPut, "/v1/devices/"+devID, body, bearer("good-token")), http.StatusBadRequest, "invalid_argument")
		})
	}
	assert.Zero(t, h.devices.registerCalls)

	// An unknown platform is well-formed JSON: the service owns that rule, so the handler passes it on.
	send(t, h.handler, http.MethodPut, "/v1/devices/"+devID, `{"fcmToken":"t","platform":"windows"}`, bearer("good-token"))
	assert.Equal(t, service.Platform("windows"), h.devices.gotPlatform)
}

func TestDeviceRoutesWrongMethod(t *testing.T) {
	rec := send(t, newHarness(t).handler, http.MethodGet, "/v1/devices/"+devID, "", bearer("good-token"))
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Equal(t, "PUT, DELETE", rec.Header().Get("Allow"))
}
