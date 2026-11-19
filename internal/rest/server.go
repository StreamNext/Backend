// Package rest implements the generated OpenAPI server interface.
// One file per concern: server.go (wiring), middleware.go, ratelimit.go, authmw.go, errors.go,
// request.go, convert.go, then one file per API domain (health.go, auth.go, me.go, ...).
package rest

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/StreamNext/Backend/gen/api"
	"github.com/StreamNext/Backend/internal/apperr"
	"github.com/StreamNext/Backend/internal/service"
)

// Pinger is satisfied by *pgxpool.Pool.
type Pinger interface {
	Ping(ctx context.Context) error
}

// The interfaces below are what the handlers need from the service layer (satisfied by internal/service).

type AuthService interface {
	Register(ctx context.Context, in service.RegisterInput) (service.Session, error)
	Login(ctx context.Context, in service.LoginInput) (service.Session, error)
	Refresh(ctx context.Context, in service.RefreshInput) (service.Session, error)
	Logout(ctx context.Context, refreshToken string) error
}

type ProfileService interface {
	Get(ctx context.Context, id uuid.UUID) (service.User, error)
	Update(ctx context.Context, id uuid.UUID, in service.UpdateProfileInput) (service.User, error)
}

// AccessTokenParser validates a bearer token and returns the user it was issued to.
type AccessTokenParser interface {
	Parse(token string) (uuid.UUID, error)
}

// Deps are everything a Server needs. Limits left at the zero value disable rate limiting;
// Now defaults to time.Now.
type Deps struct {
	DB      Pinger
	Auth    AuthService
	Profile ProfileService
	Tokens  AccessTokenParser
	Limits  Limits
	Now     func() time.Time
	Log     *zap.Logger
}

// Server implements api.ServerInterface; its endpoint methods live in one file per domain.
type Server struct {
	db      Pinger
	auth    AuthService
	profile ProfileService
	tokens  AccessTokenParser
	log     *zap.Logger
	now     func() time.Time

	secured    map[string]bool // route patterns (e.g. "GET /v1/me") the contract protects with a bearer token
	globalRate *ipLimiters
	authRate   *ipLimiters
}

func NewServer(d Deps) (*Server, error) {
	secured, err := securedRoutes()
	if err != nil {
		return nil, err
	}
	now := d.Now
	if now == nil {
		now = time.Now
	}
	return &Server{
		db: d.DB, auth: d.Auth, profile: d.Profile, tokens: d.Tokens, log: d.Log, now: now,
		secured:    secured,
		globalRate: newIPLimiters(d.Limits.Global),
		authRate:   newIPLimiters(d.Limits.Auth),
	}, nil
}

// Handler returns the API router. Outermost first: panic recovery, request logging, rate limiting,
// bearer-token check, then the generated routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	// "/" is the lowest-priority pattern, so it only catches what no generated route matches.
	mux.HandleFunc("/", s.noRoute(mux))
	api.HandlerWithOptions(s, api.StdHTTPServerOptions{
		BaseRouter: mux,
		ErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			s.writeError(w, apperr.New(apperr.InvalidArgument, err.Error()))
		},
	})
	return s.recoverer(s.logging(s.rateLimit(s.authenticate(mux))))
}
