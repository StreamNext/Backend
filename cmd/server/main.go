package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	_ "time/tzdata" // the distroless image has no zoneinfo; embedding it lets us validate IANA timezones

	"github.com/StreamNext/Backend/internal/auth"
	"github.com/StreamNext/Backend/internal/config"
	"github.com/StreamNext/Backend/internal/db"
	"github.com/StreamNext/Backend/internal/repository"
	"github.com/StreamNext/Backend/internal/rest"
	"github.com/StreamNext/Backend/internal/service"
)

const accessTokenTTL = 15 * time.Minute

func main() {
	log, _ := zap.NewProduction()
	defer func() { _ = log.Sync() }()
	if err := run(log); err != nil {
		log.Fatal("server stopped", zap.Error(err))
	}
}

// run wires the layers together: repositories -> services -> REST. Kept thin and logic-free;
// it is outside the coverage gate.
func run(log *zap.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL, "streamnext-api")
	if err != nil {
		return err
	}
	defer pool.Close()

	users := repository.NewUsers(pool)
	tokens := repository.NewRefreshTokens(pool)
	jwt := auth.NewTokenIssuer(cfg.JWTSecret, accessTokenTTL, time.Now)

	authService := service.NewAuthService(service.AuthDeps{
		Users: users, Tokens: tokens, Hasher: auth.NewHasher(auth.DefaultPasswordParams), Access: jwt,
		Grace: cfg.RefreshGrace, Now: time.Now, Log: log,
	})
	api, err := rest.NewServer(rest.Deps{
		DB: pool, Auth: authService, Profile: service.NewProfileService(users, time.Now),
		Tokens: jwt, Limits: rest.DefaultLimits(), Log: log,
	})
	if err != nil {
		return err
	}

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	log.Info("listening", zap.String("addr", srv.Addr))
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
