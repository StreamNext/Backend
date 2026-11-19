package rest

import (
	"net/http"

	"github.com/StreamNext/Backend/gen/api"
	"github.com/StreamNext/Backend/internal/service"
)

// ChangePassword implements POST /v1/me/change-password. The bearer check already ran.
func (s *Server) ChangePassword(w http.ResponseWriter, r *http.Request) {
	var req api.ChangePasswordRequest
	if !s.decode(w, r, &req) {
		return
	}
	sess, err := s.account.ChangePassword(r.Context(), userID(r.Context()), service.ChangePasswordInput{
		CurrentPassword: req.CurrentPassword, NewPassword: req.NewPassword, DeviceID: req.DeviceId, RememberMe: orZero(req.RememberMe),
	})
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPISession(sess))
}

// DeleteMe implements DELETE /v1/me. The password is optional in the body (accounts without one don't send it).
func (s *Server) DeleteMe(w http.ResponseWriter, r *http.Request) {
	var req api.DeleteAccountRequest
	if !s.decodeOptional(w, r, &req) {
		return
	}
	if err := s.account.Delete(r.Context(), userID(r.Context()), orZero(req.Password)); err != nil {
		s.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
