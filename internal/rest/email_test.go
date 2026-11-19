package rest

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/StreamNext/Backend/internal/apperr"
)

func TestVerifyEmail(t *testing.T) {
	h := newHarness(t)
	rec := send(t, h.handler, http.MethodPost, "/v1/auth/verify-email", `{"token":"abc"}`, nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Body.String())
	assert.Equal(t, "abc", h.verification.confirmToken)
}

func TestResendVerificationAndForgotPasswordAnswer202(t *testing.T) {
	h := newHarness(t)

	rec := send(t, h.handler, http.MethodPost, "/v1/auth/resend-verification", `{"email":"ana@example.com"}`, nil)
	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.Empty(t, rec.Body.String())
	assert.Equal(t, "ana@example.com", h.verification.resendEmail)

	rec = send(t, h.handler, http.MethodPost, "/v1/auth/forgot-password", `{"email":"ana@example.com"}`, nil)
	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.Equal(t, "ana@example.com", h.reset.requestEmail)
}

func TestResetPassword(t *testing.T) {
	h := newHarness(t)
	rec := send(t, h.handler, http.MethodPost, "/v1/auth/reset-password", `{"token":"abc","newPassword":"a-brand-new-pass"}`, nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "abc", h.reset.resetToken)
	assert.Equal(t, "a-brand-new-pass", h.reset.resetPassword)
}

func TestEmailEndpointsNeedNoBearerToken(t *testing.T) {
	h := newHarness(t)
	for _, c := range []struct{ path, body string }{
		{"/v1/auth/verify-email", `{"token":"t"}`},
		{"/v1/auth/resend-verification", `{"email":"a@b.co"}`},
		{"/v1/auth/forgot-password", `{"email":"a@b.co"}`},
		{"/v1/auth/reset-password", `{"token":"t","newPassword":"a-brand-new-pass"}`},
	} {
		assert.Less(t, send(t, h.handler, http.MethodPost, c.path, c.body, nil).Code, 300, c.path)
	}
}

func TestEmailEndpointErrorsKeepTheirStatusAndCode(t *testing.T) {
	endpoints := []struct{ path, body string }{
		{"/v1/auth/verify-email", `{"token":"t"}`},
		{"/v1/auth/resend-verification", `{"email":"a@b.co"}`},
		{"/v1/auth/forgot-password", `{"email":"a@b.co"}`},
		{"/v1/auth/reset-password", `{"token":"t","newPassword":"a-brand-new-pass"}`},
	}
	for _, e := range endpoints {
		t.Run(e.path, func(t *testing.T) {
			h := newHarness(t)
			h.verification.err = apperr.New(apperr.InvalidArgument, "invalid or expired token")
			h.reset.err = h.verification.err
			requireError(t, send(t, h.handler, http.MethodPost, e.path, e.body, nil), http.StatusBadRequest, "invalid_argument")

			h.verification.err = errors.New("pq: connection reset")
			h.reset.err = h.verification.err
			rec := send(t, h.handler, http.MethodPost, e.path, e.body, nil)
			requireError(t, rec, http.StatusInternalServerError, "internal")
			assert.NotContains(t, rec.Body.String(), "pq:")
		})
	}
}

func TestEmailEndpointsRejectMalformedBodies(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{"/v1/auth/verify-email", "/v1/auth/resend-verification", "/v1/auth/forgot-password", "/v1/auth/reset-password"} {
		for name, body := range map[string]string{"empty": "", "not json": "token=abc", "wrong type": `{"token":1,"email":2,"newPassword":3}`} {
			t.Run(path+" "+name, func(t *testing.T) {
				requireError(t, send(t, h.handler, http.MethodPost, path, body, nil), http.StatusBadRequest, "invalid_argument")
			})
		}
	}
	assert.Empty(t, h.verification.confirmToken+h.verification.resendEmail+h.reset.requestEmail+h.reset.resetToken, "the services were never called")
	require.Empty(t, h.reset.resetPassword)
}
