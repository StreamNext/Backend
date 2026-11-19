package rest

import (
	"context"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/StreamNext/Backend/gen/api"
	"github.com/StreamNext/Backend/internal/apperr"
	"github.com/StreamNext/Backend/internal/version"
)

// GetHealth implements GET /v1/health.
func (s *Server) GetHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.db.Ping(ctx); err != nil {
		s.log.Error("health: db ping failed", zap.Error(err))
		s.writeError(w, apperr.New(apperr.Unavailable, "database unavailable"))
		return
	}
	writeJSON(w, http.StatusOK, api.Health{Status: api.Ok, Version: version.Version})
}
