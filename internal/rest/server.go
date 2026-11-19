// Package rest implements the generated OpenAPI server interface.
package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"runtime/debug"
	"time"

	"go.uber.org/zap"

	"github.com/StreamNext/Backend/gen/api"
	"github.com/StreamNext/Backend/internal/apperr"
	"github.com/StreamNext/Backend/internal/version"
)

// Pinger is satisfied by *pgxpool.Pool.
type Pinger interface {
	Ping(ctx context.Context) error
}

type Server struct {
	db  Pinger
	log *zap.Logger
}

func NewServer(db Pinger, log *zap.Logger) *Server { return &Server{db: db, log: log} }

// Handler returns the API router wrapped in recover + request logging.
func (s *Server) Handler() http.Handler {
	h := api.HandlerWithOptions(s, api.StdHTTPServerOptions{
		ErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			s.writeError(w, apperr.New(apperr.InvalidArgument, err.Error()))
		},
	})
	return s.recoverer(s.logging(h))
}

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

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("panic", zap.Any("panic", rec), zap.ByteString("stack", debug.Stack()))
				s.writeError(w, apperr.New(apperr.Internal, "internal error"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.log.Info("request", zap.String("method", r.Method), zap.String("path", r.URL.Path),
			zap.Int("status", rec.status), zap.Duration("took", time.Since(start)))
	})
}
