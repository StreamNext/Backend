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

func TestChangePassword(t *testing.T) {
	h := newHarness(t)
	rec := send(t, h.handler, http.MethodPost, "/v1/me/change-password",
		`{"currentPassword":"old-password-1","newPassword":"a-brand-new-pass","deviceId":"`+devID+`","rememberMe":true}`, bearer("good-token"))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	s := decodeInto[api.Session](t, rec)
	assert.Equal(t, "acc2", s.AccessToken)
	assert.Equal(t, "ref2", s.RefreshToken)
	assert.Equal(t, userA, h.account.gotUser, "the user comes from the token")
	assert.Equal(t, service.ChangePasswordInput{
		CurrentPassword: "old-password-1", NewPassword: "a-brand-new-pass", DeviceID: uuid.MustParse(devID), RememberMe: true,
	}, h.account.gotChange)
}

func TestChangePasswordErrors(t *testing.T) {
	body := `{"currentPassword":"x","newPassword":"a-brand-new-pass","deviceId":"` + devID + `"}`
	for name, c := range map[string]struct {
		err    error
		status int
		code   string
	}{
		"wrong current password": {apperr.New(apperr.Forbidden, "password is incorrect"), http.StatusForbidden, "forbidden"},
		"weak new password":      {apperr.New(apperr.InvalidArgument, "password must be 10 to 128 characters"), http.StatusBadRequest, "invalid_argument"},
		"unexpected":             {errors.New("pq: down"), http.StatusInternalServerError, "internal"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.account.err = c.err
			requireError(t, send(t, h.handler, http.MethodPost, "/v1/me/change-password", body, bearer("good-token")), c.status, c.code)
		})
	}

	h := newHarness(t)
	for _, bad := range []string{"", "nope", `{"currentPassword":1}`, `{"currentPassword":"x","newPassword":"y","deviceId":"not-a-uuid"}`} {
		requireError(t, send(t, h.handler, http.MethodPost, "/v1/me/change-password", bad, bearer("good-token")), http.StatusBadRequest, "invalid_argument")
	}
	assert.Zero(t, h.account.gotUser, "the service is never called with a malformed body")
}

func TestDeleteMe(t *testing.T) {
	h := newHarness(t)

	rec := send(t, h.handler, http.MethodDelete, "/v1/me", `{"password":"correct-horse"}`, bearer("good-token"))
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Body.String())
	assert.Equal(t, userA, h.account.gotUser)
	assert.Equal(t, "correct-horse", h.account.deletePassword)
}

func TestDeleteMeBodyIsOptional(t *testing.T) {
	h := newHarness(t)
	rec := send(t, h.handler, http.MethodDelete, "/v1/me", "", bearer("good-token"))
	assert.Equal(t, http.StatusNoContent, rec.Code, "social-only accounts send no body")
	assert.Equal(t, 1, h.account.deleteCalls)
	assert.Equal(t, "", h.account.deletePassword)

	rec = send(t, h.handler, http.MethodDelete, "/v1/me", `{}`, bearer("good-token"))
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestDeleteMeErrors(t *testing.T) {
	h := newHarness(t)
	requireError(t, send(t, h.handler, http.MethodDelete, "/v1/me", "not json", bearer("good-token")), http.StatusBadRequest, "invalid_argument")
	assert.Zero(t, h.account.deleteCalls)

	h.account.err = apperr.New(apperr.Forbidden, "password is incorrect")
	requireError(t, send(t, h.handler, http.MethodDelete, "/v1/me", `{"password":"nope"}`, bearer("good-token")), http.StatusForbidden, "forbidden")

	h.account.err = errors.New("pq: down")
	requireError(t, send(t, h.handler, http.MethodDelete, "/v1/me", `{"password":"x"}`, bearer("good-token")), http.StatusInternalServerError, "internal")
}
