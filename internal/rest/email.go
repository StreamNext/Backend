package rest

import (
	"net/http"

	"github.com/StreamNext/Backend/gen/api"
)

// VerifyEmail implements POST /v1/auth/verify-email.
func (s *Server) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	var req api.VerifyEmailRequest
	if !s.decode(w, r, &req) {
		return
	}
	if err := s.verification.Confirm(r.Context(), req.Token); err != nil {
		s.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// ResendVerification implements POST /v1/auth/resend-verification. It answers 202 whether or not the
// address has an account.
func (s *Server) ResendVerification(w http.ResponseWriter, r *http.Request) {
	var req api.EmailRequest
	if !s.decode(w, r, &req) {
		return
	}
	if err := s.verification.Resend(r.Context(), req.Email); err != nil {
		s.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// ForgotPassword implements POST /v1/auth/forgot-password. It answers 202 whether or not the address has an account.
func (s *Server) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req api.EmailRequest
	if !s.decode(w, r, &req) {
		return
	}
	if err := s.reset.Request(r.Context(), req.Email); err != nil {
		s.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// ResetPassword implements POST /v1/auth/reset-password.
func (s *Server) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req api.ResetPasswordRequest
	if !s.decode(w, r, &req) {
		return
	}
	if err := s.reset.Reset(r.Context(), req.Token, req.NewPassword); err != nil {
		s.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}
