package rest

import (
	"encoding/json"
	"net/http"
	"strings"

	"go.uber.org/zap"

	"github.com/StreamNext/Backend/gen/api"
	"github.com/StreamNext/Backend/internal/apperr"
)

// writeError is the single place errors become HTTP responses: lowercase {code,message}.
func (s *Server) writeError(w http.ResponseWriter, err error) {
	e := apperr.From(err)
	if e.Kind == apperr.Internal {
		s.log.Error("internal error", zap.Error(err))
	}
	writeJSON(w, e.Kind.Status(), api.Error{Code: e.Kind.Code(), Message: e.Message})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// noRoute answers requests no generated route matched, in our JSON error format instead of Go's
// plain text. If the path exists under another method it is a 405 with an Allow header, else 404.
func (s *Server) noRoute(mux *http.ServeMux) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var allowed []string
		for _, m := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
			probe := r.Clone(r.Context())
			probe.Method = m
			if _, pattern := mux.Handler(probe); pattern != "" && pattern != "/" {
				allowed = append(allowed, m)
			}
		}
		if len(allowed) > 0 {
			w.Header().Set("Allow", strings.Join(allowed, ", "))
			s.writeError(w, apperr.New(apperr.MethodNotAllowed, "method not allowed"))
			return
		}
		s.writeError(w, apperr.New(apperr.NotFound, "route not found"))
	}
}
