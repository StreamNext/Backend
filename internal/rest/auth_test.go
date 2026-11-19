package rest

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/StreamNext/Backend/gen/api"
	"github.com/StreamNext/Backend/internal/apperr"
	"github.com/StreamNext/Backend/internal/service"
)

func TestRegister(t *testing.T) {
	h := newHarness(t)
	rec := send(t, h.handler, http.MethodPost, "/v1/auth/register",
		`{"email":"ana@example.com","password":"correct-horse","deviceId":"`+devID+`","displayName":"Ana","timezone":"America/Bogota","rememberMe":true}`, nil)

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	s := decodeInto[api.Session](t, rec)
	assert.Equal(t, "acc", s.AccessToken)
	assert.Equal(t, "ref", s.RefreshToken)
	assert.Equal(t, 900, s.ExpiresIn)
	assert.Equal(t, "ana@example.com", s.User.Email)
	assert.False(t, s.User.EmailVerified)

	assert.Equal(t, service.RegisterInput{
		Email: "ana@example.com", Password: "correct-horse", DisplayName: "Ana", Timezone: "America/Bogota",
		DeviceID: uuid.MustParse(devID), RememberMe: true,
	}, h.auth.register)
}

func TestRegisterOptionalFieldsDefaultToZero(t *testing.T) {
	h := newHarness(t)
	send(t, h.handler, http.MethodPost, "/v1/auth/register", `{"email":"a@b.co","password":"correct-horse","deviceId":"`+devID+`"}`, nil)
	assert.Equal(t, "", h.auth.register.DisplayName)
	assert.Equal(t, "", h.auth.register.Timezone)
	assert.False(t, h.auth.register.RememberMe)
}

func TestVerifiedEmailIsReported(t *testing.T) {
	h := newHarness(t)
	verified := fixture
	now := fixedClock(epochTime)()
	verified.EmailVerifiedAt = &now
	h.auth.session.User = verified
	rec := send(t, h.handler, http.MethodPost, "/v1/auth/login", `{"email":"a@b.co","password":"x","deviceId":"`+devID+`"}`, nil)
	assert.True(t, decodeInto[api.Session](t, rec).User.EmailVerified)
}

func TestLoginRefreshAndLogout(t *testing.T) {
	h := newHarness(t)

	rec := send(t, h.handler, http.MethodPost, "/v1/auth/login", `{"email":"a@b.co","password":"pw","deviceId":"`+devID+`","rememberMe":true}`, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, service.LoginInput{Email: "a@b.co", Password: "pw", DeviceID: uuid.MustParse(devID), RememberMe: true}, h.auth.login)

	rec = send(t, h.handler, http.MethodPost, "/v1/auth/refresh", `{"refreshToken":"tok","deviceId":"`+devID+`"}`, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, service.RefreshInput{RefreshToken: "tok", DeviceID: uuid.MustParse(devID)}, h.auth.refresh)

	rec = send(t, h.handler, http.MethodPost, "/v1/auth/logout", `{"refreshToken":"tok"}`, nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Body.String())
	assert.Equal(t, "tok", h.auth.logout)
}

func TestAuthEndpointsNeedNoBearerToken(t *testing.T) {
	h := newHarness(t)
	for _, c := range []struct{ path, body string }{
		{"/v1/auth/register", `{"email":"a@b.co","password":"correct-horse","deviceId":"` + devID + `"}`},
		{"/v1/auth/login", `{"email":"a@b.co","password":"pw","deviceId":"` + devID + `"}`},
		{"/v1/auth/refresh", `{"refreshToken":"t","deviceId":"` + devID + `"}`},
		{"/v1/auth/logout", `{"refreshToken":"t"}`},
	} {
		rec := send(t, h.handler, http.MethodPost, c.path, c.body, nil)
		assert.Less(t, rec.Code, 300, c.path)
	}
}

func TestServiceErrorsKeepTheirStatusAndCode(t *testing.T) {
	cases := map[string]struct {
		err    error
		status int
		code   string
	}{
		"conflict":     {apperr.New(apperr.Conflict, "email already registered"), http.StatusConflict, "conflict"},
		"invalid":      {apperr.New(apperr.InvalidArgument, "email is invalid"), http.StatusBadRequest, "invalid_argument"},
		"unauthorized": {apperr.New(apperr.Unauthenticated, "invalid credentials"), http.StatusUnauthorized, "unauthenticated"},
		"unexpected":   {errors.New("pq: connection reset"), http.StatusInternalServerError, "internal"},
	}
	endpoints := []struct{ path, body string }{
		{"/v1/auth/register", `{"email":"a@b.co","password":"correct-horse","deviceId":"` + devID + `"}`},
		{"/v1/auth/login", `{"email":"a@b.co","password":"pw","deviceId":"` + devID + `"}`},
		{"/v1/auth/refresh", `{"refreshToken":"t","deviceId":"` + devID + `"}`},
		{"/v1/auth/logout", `{"refreshToken":"t"}`},
	}
	for name, c := range cases {
		for _, e := range endpoints {
			t.Run(name+" "+e.path, func(t *testing.T) {
				h := newHarness(t)
				h.auth.err = c.err
				rec := send(t, h.handler, http.MethodPost, e.path, e.body, nil)
				requireError(t, rec, c.status, c.code)
				assert.NotContains(t, rec.Body.String(), "pq:", "unexpected errors never leak details")
			})
		}
	}
}

func TestMalformedBodiesAreBadRequests(t *testing.T) {
	h := newHarness(t)
	authPaths := []string{"/v1/auth/register", "/v1/auth/login", "/v1/auth/refresh", "/v1/auth/logout"}

	// Malformed for every endpoint, whatever its fields are.
	for _, path := range authPaths {
		for name, body := range map[string]string{
			"empty":     "",
			"not json":  "email=a@b.co",
			"too large": `{"email":"` + strings.Repeat("a", 70<<10) + `"}`,
		} {
			t.Run(path+" "+name, func(t *testing.T) {
				requireError(t, send(t, h.handler, http.MethodPost, path, body, nil), http.StatusBadRequest, "invalid_argument")
			})
		}
	}

	// Well-formed JSON with a field of the wrong type, per endpoint.
	for name, c := range map[string]struct{ path, body string }{
		"register email not a string": {"/v1/auth/register", `{"email":123,"password":"x","deviceId":"` + devID + `"}`},
		"login email not a string":    {"/v1/auth/login", `{"email":123,"password":"x","deviceId":"` + devID + `"}`},
		"refresh token not a string":  {"/v1/auth/refresh", `{"refreshToken":123,"deviceId":"` + devID + `"}`},
		"logout token not a string":   {"/v1/auth/logout", `{"refreshToken":123}`},
		"register device not a uuid":  {"/v1/auth/register", `{"email":"a@b.co","password":"x","deviceId":"nope"}`},
		"login device not a uuid":     {"/v1/auth/login", `{"email":"a@b.co","password":"x","deviceId":"nope"}`},
		"refresh device not a uuid":   {"/v1/auth/refresh", `{"refreshToken":"t","deviceId":"nope"}`},
	} {
		t.Run(name, func(t *testing.T) {
			requireError(t, send(t, h.handler, http.MethodPost, c.path, c.body, nil), http.StatusBadRequest, "invalid_argument")
		})
	}

	assert.Equal(t, service.RegisterInput{}, h.auth.register, "the service is never called with a malformed body")
	assert.Equal(t, service.LoginInput{}, h.auth.login)
	assert.Equal(t, service.RefreshInput{}, h.auth.refresh)
	assert.Empty(t, h.auth.logout)
}

func TestMissingFieldsAreLeftToTheServiceToValidate(t *testing.T) {
	h := newHarness(t)
	h.auth.err = apperr.New(apperr.InvalidArgument, "email is invalid")
	rec := send(t, h.handler, http.MethodPost, "/v1/auth/register", `{}`, nil)
	requireError(t, rec, http.StatusBadRequest, "invalid_argument")
	assert.Equal(t, service.RegisterInput{}, h.auth.register, "the empty input did reach the service, which owns the rules")
}
