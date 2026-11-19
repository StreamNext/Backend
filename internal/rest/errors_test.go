package rest

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/StreamNext/Backend/internal/apperr"
)

func TestWriteErrorMapsKinds(t *testing.T) {
	s := newHarness(t).srv
	rec := httptest.NewRecorder()
	s.writeError(rec, apperr.New(apperr.NotFound, "no such show"))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.JSONEq(t, `{"code":"not_found","message":"no such show"}`, rec.Body.String())

	rec = httptest.NewRecorder()
	s.writeError(rec, errors.New("secret detail"))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.JSONEq(t, `{"code":"internal","message":"internal error"}`, rec.Body.String())
}

func TestUnknownRouteIsJSON404(t *testing.T) {
	h := newHarness(t).handler
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		rec := do(t, h, method, "/v1/nope")
		assert.Equal(t, http.StatusNotFound, rec.Code, method)
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"), method)
		assert.JSONEq(t, `{"code":"not_found","message":"route not found"}`, rec.Body.String(), method)
	}
}

func TestUnknownRouteIsNotAuthChallenged(t *testing.T) {
	// A missing route is a 404 for everyone, not a 401 that hints at what exists.
	assert.Equal(t, http.StatusNotFound, do(t, newHarness(t).handler, http.MethodGet, "/v1/nope").Code)
}

func TestWrongMethodIsJSON405WithAllow(t *testing.T) {
	rec := do(t, newHarness(t).handler, http.MethodPost, "/v1/health")
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Equal(t, "GET", rec.Header().Get("Allow"))
	assert.JSONEq(t, `{"code":"method_not_allowed","message":"method not allowed"}`, rec.Body.String())

	rec = do(t, newHarness(t).handler, http.MethodPut, "/v1/me")
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Equal(t, "GET, PATCH, DELETE", rec.Header().Get("Allow"))
}
