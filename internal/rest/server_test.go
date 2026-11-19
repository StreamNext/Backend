package rest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/StreamNext/Backend/gen/api"
	"github.com/StreamNext/Backend/internal/apperr"
	"github.com/StreamNext/Backend/internal/version"
)

type fakeDB struct{ err error }

func (f fakeDB) Ping(context.Context) error { return f.err }

func do(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestHealthOK(t *testing.T) {
	rec := do(t, NewServer(fakeDB{}, zap.NewNop()).Handler(), http.MethodGet, "/v1/health")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	var got api.Health
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, api.Ok, got.Status)
	assert.Equal(t, version.Version, got.Version)
}

func TestHealthDBDown(t *testing.T) {
	rec := do(t, NewServer(fakeDB{errors.New("conn refused")}, zap.NewNop()).Handler(), http.MethodGet, "/v1/health")
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	var got api.Error
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, "unavailable", got.Code)
	assert.NotContains(t, rec.Body.String(), "conn refused", "internal cause must not reach the client")
	assert.Contains(t, rec.Body.String(), `"code"`, "lowercase keys per contract")
}

func TestUnknownRouteIs404(t *testing.T) {
	rec := do(t, NewServer(fakeDB{}, zap.NewNop()).Handler(), http.MethodGet, "/v1/nope")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestWriteErrorMapsKinds(t *testing.T) {
	s := NewServer(fakeDB{}, zap.NewNop())
	rec := httptest.NewRecorder()
	s.writeError(rec, apperr.New(apperr.NotFound, "no such show"))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.JSONEq(t, `{"code":"not_found","message":"no such show"}`, rec.Body.String())

	rec = httptest.NewRecorder()
	s.writeError(rec, errors.New("secret detail"))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.JSONEq(t, `{"code":"internal","message":"internal error"}`, rec.Body.String())
}

func TestPanicIsRecoveredAs500(t *testing.T) {
	s := NewServer(fakeDB{}, zap.NewNop())
	h := s.recoverer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	rec := do(t, h, http.MethodGet, "/")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.JSONEq(t, `{"code":"internal","message":"internal error"}`, rec.Body.String())
}
