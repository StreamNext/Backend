package rest

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/StreamNext/Backend/gen/api"
	"github.com/StreamNext/Backend/internal/version"
)

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
