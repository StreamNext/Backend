package rest

import (
	"net/http"

	"github.com/StreamNext/Backend/gen/api"
	"github.com/StreamNext/Backend/internal/service"
)

// GetMe implements GET /v1/me. The bearer check already ran, so userID is set.
func (s *Server) GetMe(w http.ResponseWriter, r *http.Request) {
	u, err := s.profile.Get(r.Context(), userID(r.Context()))
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIUser(u))
}

// UpdateMe implements PATCH /v1/me.
func (s *Server) UpdateMe(w http.ResponseWriter, r *http.Request) {
	var req api.UpdateProfileRequest
	if !s.decode(w, r, &req) {
		return
	}
	u, err := s.profile.Update(r.Context(), userID(r.Context()), service.UpdateProfileInput{DisplayName: req.DisplayName, Timezone: req.Timezone, Language: languageString(req.Language)})
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIUser(u))
}

func languageString(l *api.Language) *string {
	if l == nil {
		return nil
	}
	s := string(*l)
	return &s
}
