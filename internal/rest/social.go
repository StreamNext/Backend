package rest

import (
	"net/http"

	"github.com/StreamNext/Backend/gen/api"
	"github.com/StreamNext/Backend/internal/service"
)

// SignInWithApple implements POST /v1/auth/apple.
func (s *Server) SignInWithApple(w http.ResponseWriter, r *http.Request) {
	var req api.AppleSignInRequest
	if !s.decode(w, r, &req) {
		return
	}
	sess, err := s.social.SignIn(r.Context(), service.SocialInput{
		Provider: service.ProviderApple, Token: req.IdentityToken, Nonce: req.Nonce,
		DisplayName: orZero(req.DisplayName), DeviceID: req.DeviceId,
	})
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPISession(sess))
}

// SignInWithGoogle implements POST /v1/auth/google.
func (s *Server) SignInWithGoogle(w http.ResponseWriter, r *http.Request) {
	var req api.GoogleSignInRequest
	if !s.decode(w, r, &req) {
		return
	}
	sess, err := s.social.SignIn(r.Context(), service.SocialInput{
		Provider: service.ProviderGoogle, Token: req.IdToken, DeviceID: req.DeviceId,
	})
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPISession(sess))
}
