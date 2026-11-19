package rest

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/StreamNext/Backend/gen/api"
	"github.com/StreamNext/Backend/internal/apperr"
)

func TestGetMe(t *testing.T) {
	h := newHarness(t)
	rec := send(t, h.handler, http.MethodGet, "/v1/me", "", bearer("good-token"))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, userA, h.profile.gotID, "the handler uses the user from the token, never from the request")
	u := decodeInto[api.User](t, rec)
	assert.Equal(t, "ana@example.com", u.Email)
	assert.Equal(t, "Ana", u.DisplayName)
}

func TestUpdateMe(t *testing.T) {
	h := newHarness(t)
	rec := send(t, h.handler, http.MethodPatch, "/v1/me", `{"displayName":"Ana M"}`, bearer("good-token"))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, userA, h.profile.gotID)
	require.NotNil(t, h.profile.gotUpdate.DisplayName)
	assert.Equal(t, "Ana M", *h.profile.gotUpdate.DisplayName)
	assert.Nil(t, h.profile.gotUpdate.Timezone, "fields not sent stay nil so they are not changed")
}

func TestMeErrors(t *testing.T) {
	h := newHarness(t)
	h.profile.err = apperr.New(apperr.InvalidArgument, "nothing to update")
	requireError(t, send(t, h.handler, http.MethodPatch, "/v1/me", `{}`, bearer("good-token")), http.StatusBadRequest, "invalid_argument")

	requireError(t, send(t, h.handler, http.MethodPatch, "/v1/me", `not json`, bearer("good-token")), http.StatusBadRequest, "invalid_argument")

	h.profile.err = errors.New("db down")
	requireError(t, send(t, h.handler, http.MethodGet, "/v1/me", "", bearer("good-token")), http.StatusInternalServerError, "internal")
}
