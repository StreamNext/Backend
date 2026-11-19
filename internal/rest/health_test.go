package rest

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/StreamNext/Backend/gen/api"
	"github.com/StreamNext/Backend/internal/version"
)

func TestHealthOK(t *testing.T) {
	rec := do(t, newHarness(t).handler, http.MethodGet, "/v1/health")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	got := decodeInto[api.Health](t, rec)
	assert.Equal(t, api.Ok, got.Status)
	assert.Equal(t, version.Version, got.Version)
}

func TestHealthDBDown(t *testing.T) {
	h := newHarness(t, func(d *Deps) { d.DB = fakeDB{errors.New("conn refused")} })
	rec := do(t, h.handler, http.MethodGet, "/v1/health")
	requireError(t, rec, http.StatusServiceUnavailable, "unavailable")
	assert.NotContains(t, rec.Body.String(), "conn refused", "internal cause must not reach the client")
}

func TestHealthNeedsNoToken(t *testing.T) {
	assert.Equal(t, http.StatusOK, do(t, newHarness(t).handler, http.MethodGet, "/v1/health").Code)
}
