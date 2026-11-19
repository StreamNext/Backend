package rest

import (
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/StreamNext/Backend/gen/api"
	"github.com/StreamNext/Backend/internal/apperr"
	"github.com/StreamNext/Backend/internal/service"
)

func TestSignInWithApple(t *testing.T) {
	h := newHarness(t)
	rec := send(t, h.handler, http.MethodPost, "/v1/auth/apple",
		`{"identityToken":"apple-jwt","nonce":"raw","deviceId":"`+devID+`","displayName":"Ana"}`, nil)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	s := decodeInto[api.Session](t, rec)
	assert.Equal(t, "acc3", s.AccessToken)
	assert.Equal(t, "ref3", s.RefreshToken)
	assert.Equal(t, service.SocialInput{
		Provider: service.ProviderApple, Token: "apple-jwt", Nonce: "raw", DisplayName: "Ana", DeviceID: uuid.MustParse(devID),
	}, h.social.got)
}

func TestSignInWithGoogle(t *testing.T) {
	h := newHarness(t)
	rec := send(t, h.handler, http.MethodPost, "/v1/auth/google", `{"idToken":"google-jwt","deviceId":"`+devID+`"}`, nil)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, service.SocialInput{Provider: service.ProviderGoogle, Token: "google-jwt", DeviceID: uuid.MustParse(devID)}, h.social.got)
}

func TestSocialEndpointsNeedNoBearerToken(t *testing.T) {
	h := newHarness(t)
	for _, c := range []struct{ path, body string }{
		{"/v1/auth/apple", `{"identityToken":"t","nonce":"n","deviceId":"` + devID + `"}`},
		{"/v1/auth/google", `{"idToken":"t","deviceId":"` + devID + `"}`},
	} {
		assert.Equal(t, http.StatusOK, send(t, h.handler, http.MethodPost, c.path, c.body, nil).Code, c.path)
	}
}

func TestSocialErrorsKeepTheirStatusAndCode(t *testing.T) {
	cases := map[string]struct {
		err    error
		status int
		code   string
	}{
		"bad token":          {apperr.New(apperr.Unauthenticated, "invalid sign-in token"), http.StatusUnauthorized, "unauthenticated"},
		"email in use":       {apperr.New(apperr.Conflict, "an account with this email already exists"), http.StatusConflict, "conflict"},
		"not configured":     {apperr.New(apperr.Unavailable, "apple sign-in is not configured"), http.StatusServiceUnavailable, "unavailable"},
		"unexpected failure": {errors.New("pq: down"), http.StatusInternalServerError, "internal"},
	}
	for name, c := range cases {
		for _, e := range []struct{ path, body string }{
			{"/v1/auth/apple", `{"identityToken":"t","nonce":"n","deviceId":"` + devID + `"}`},
			{"/v1/auth/google", `{"idToken":"t","deviceId":"` + devID + `"}`},
		} {
			t.Run(name+" "+e.path, func(t *testing.T) {
				h := newHarness(t)
				h.social.err = c.err
				rec := send(t, h.handler, http.MethodPost, e.path, e.body, nil)
				requireError(t, rec, c.status, c.code)
				assert.NotContains(t, rec.Body.String(), "pq:")
			})
		}
	}
}

func TestSocialRejectsMalformedBodies(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{"/v1/auth/apple", "/v1/auth/google"} {
		for name, body := range map[string]string{
			"empty": "", "not json": "token=abc", "wrong type": `{"identityToken":1,"idToken":2,"nonce":3,"deviceId":"` + devID + `"}`,
			"device not a uuid": `{"identityToken":"t","idToken":"t","nonce":"n","deviceId":"nope"}`,
		} {
			t.Run(path+" "+name, func(t *testing.T) {
				requireError(t, send(t, h.handler, http.MethodPost, path, body, nil), http.StatusBadRequest, "invalid_argument")
			})
		}
	}
	assert.Equal(t, service.SocialInput{}, h.social.got, "the service is never called with a malformed body")
}
