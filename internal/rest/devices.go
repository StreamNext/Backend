package rest

import (
	"net/http"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/StreamNext/Backend/gen/api"
	"github.com/StreamNext/Backend/internal/service"
)

// RegisterDevice implements PUT /v1/devices/{deviceId}. The bearer check already ran, and the generated
// wrapper has already parsed deviceId (a malformed one never gets here: it is a 400).
func (s *Server) RegisterDevice(w http.ResponseWriter, r *http.Request, deviceId openapi_types.UUID) {
	var req api.RegisterDeviceRequest
	if !s.decode(w, r, &req) {
		return
	}
	if err := s.devices.Register(r.Context(), userID(r.Context()), deviceId, req.FcmToken, service.Platform(req.Platform)); err != nil {
		s.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RemoveDevice implements DELETE /v1/devices/{deviceId}.
func (s *Server) RemoveDevice(w http.ResponseWriter, r *http.Request, deviceId openapi_types.UUID) {
	if err := s.devices.Remove(r.Context(), userID(r.Context()), deviceId); err != nil {
		s.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
