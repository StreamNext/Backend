package rest

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/StreamNext/Backend/gen/api"
	"github.com/StreamNext/Backend/internal/apperr"
)

type userIDKey struct{}

// userID is the authenticated user of a request that passed the bearer check.
func userID(ctx context.Context) uuid.UUID {
	id, _ := ctx.Value(userIDKey{}).(uuid.UUID)
	return id
}

// securedRoutes lists the operations the contract protects (a non-empty `security`), keyed like the
// ServeMux patterns the generated code registers, e.g. "GET /v1/me". Because it is read from the
// contract, a route marked secure there cannot be left unprotected here.
func securedRoutes() (map[string]bool, error) {
	spec, err := api.GetSwagger()
	if err != nil {
		return nil, err
	}
	secured := map[string]bool{}
	for path, item := range spec.Paths.Map() {
		for method, op := range item.Operations() {
			requirements := spec.Security // an operation inherits the document-level default...
			if op.Security != nil {
				requirements = *op.Security // ...unless it sets its own (`security: []` makes it public)
			}
			if len(requirements) > 0 {
				secured[method+" "+path] = true
			}
		}
	}
	return secured, nil
}

// authenticate rejects requests to secured routes that lack a valid bearer token, and otherwise
// puts the user id in the request context for the handler.
func (s *Server) authenticate(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, pattern := mux.Handler(r); s.secured[pattern] {
			token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok || token == "" {
				s.writeError(w, apperr.New(apperr.Unauthenticated, "missing bearer token"))
				return
			}
			id, err := s.tokens.Parse(token)
			if err != nil {
				s.writeError(w, apperr.New(apperr.Unauthenticated, "invalid or expired token"))
				return
			}
			r = r.WithContext(context.WithValue(r.Context(), userIDKey{}, id))
		}
		mux.ServeHTTP(w, r)
	})
}
