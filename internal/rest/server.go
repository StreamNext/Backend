// Package rest implements the generated OpenAPI server interface.
// One file per concern: server.go (wiring), middleware.go, errors.go, then one file per API domain.
package rest

import (
	"context"
	"net/http"

	"go.uber.org/zap"

	"github.com/StreamNext/Backend/gen/api"
	"github.com/StreamNext/Backend/internal/apperr"
)

// Pinger is satisfied by *pgxpool.Pool.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Server implements api.ServerInterface; its endpoint methods live in one file per domain.
type Server struct {
	db  Pinger
	log *zap.Logger
}

func NewServer(db Pinger, log *zap.Logger) *Server { return &Server{db: db, log: log} }

// Handler returns the API router wrapped in recover + request logging.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	// "/" is the lowest-priority pattern, so it only catches what no generated route matches.
	mux.HandleFunc("/", s.noRoute(mux))
	h := api.HandlerWithOptions(s, api.StdHTTPServerOptions{
		BaseRouter: mux,
		ErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			s.writeError(w, apperr.New(apperr.InvalidArgument, err.Error()))
		},
	})
	return s.recoverer(s.logging(h))
}
