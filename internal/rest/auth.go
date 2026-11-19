package rest

import (
	"net/http"

	"github.com/StreamNext/Backend/gen/api"
	"github.com/StreamNext/Backend/internal/service"
)

// Register implements POST /v1/auth/register.
func (s *Server) Register(w http.ResponseWriter, r *http.Request) {
	var req api.RegisterRequest
	if !s.decode(w, r, &req) {
		return
	}
	sess, err := s.auth.Register(r.Context(), service.RegisterInput{
		Email: req.Email, Password: req.Password, DisplayName: orZero(req.DisplayName),
		Timezone: orZero(req.Timezone), DeviceID: req.DeviceId, RememberMe: orZero(req.RememberMe),
	})
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toAPISession(sess))
}

// Login implements POST /v1/auth/login.
func (s *Server) Login(w http.ResponseWriter, r *http.Request) {
	var req api.LoginRequest
	if !s.decode(w, r, &req) {
		return
	}
	sess, err := s.auth.Login(r.Context(), service.LoginInput{
		Email: req.Email, Password: req.Password, DeviceID: req.DeviceId, RememberMe: orZero(req.RememberMe),
	})
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPISession(sess))
}

// Refresh implements POST /v1/auth/refresh.
func (s *Server) Refresh(w http.ResponseWriter, r *http.Request) {
	var req api.RefreshRequest
	if !s.decode(w, r, &req) {
		return
	}
	sess, err := s.auth.Refresh(r.Context(), service.RefreshInput{RefreshToken: req.RefreshToken, DeviceID: req.DeviceId})
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPISession(sess))
}

// Logout implements POST /v1/auth/logout.
func (s *Server) Logout(w http.ResponseWriter, r *http.Request) {
	var req api.LogoutRequest
	if !s.decode(w, r, &req) {
		return
	}
	if err := s.auth.Logout(r.Context(), req.RefreshToken); err != nil {
		s.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}
