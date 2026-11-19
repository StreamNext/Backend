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
	"github.com/StreamNext/Backend/internal/mail"
	"github.com/StreamNext/Backend/internal/repository"
	"github.com/StreamNext/Backend/internal/rest"
	"github.com/StreamNext/Backend/internal/service"
	"github.com/StreamNext/Backend/internal/social"
	"github.com/StreamNext/Backend/internal/tvdb"
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
	hasher := auth.NewHasher(auth.DefaultPasswordParams)
	mailer := mail.New(mailTransport(cfg.Mail, log))

	verification := service.NewVerification(service.VerificationDeps{
		Users: users, Tokens: repository.NewEmailTokens(pool), Mailer: mailer, BaseURL: cfg.AppBaseURL, Now: time.Now, Log: log,
	})
	authService := service.NewAuthService(service.AuthDeps{
		Users: users, Tokens: tokens, Hasher: hasher, Access: jwt,
		Grace: cfg.RefreshGrace, Now: time.Now, Log: log, Verifier: verification,
	})
	passwordReset := service.NewPasswordReset(service.PasswordResetDeps{
		Users: users, Sessions: tokens, Tokens: repository.NewEmailTokens(pool), Mailer: mailer, Hasher: hasher,
		BaseURL: cfg.AppBaseURL, Now: time.Now, Log: log,
	})
	identities := social.New(
		social.Config{AppleClientID: cfg.Social.AppleClientID, GoogleClientIDs: cfg.Social.GoogleClientIDs},
		&http.Client{Timeout: 10 * time.Second}, time.Now)
	socialAuth := service.NewSocialAuth(service.SocialDeps{
		Users: users, OAuth: repository.NewOAuthAccounts(pool), Sessions: tokens, Verifier: identities, Auth: authService,
		Verification: verification, Now: time.Now, Log: log,
	})
	devices := service.NewDevices(repository.NewDevices(pool), time.Now)
	log.Info("sign-in providers", zap.Bool("apple", cfg.Social.AppleClientID != ""), zap.Int("google_client_ids", len(cfg.Social.GoogleClientIDs)))
	catalog := service.NewCatalog(service.CatalogDeps{
		Source: tvdb.New(tvdb.Config{APIKey: cfg.TVDB.APIKey, BaseURL: cfg.TVDB.BaseURL}, &http.Client{Timeout: 30 * time.Second}, time.Now),
		Repo:   repository.NewCatalog(pool), Users: users, Now: time.Now, Log: log,
	})
	log.Info("catalog", zap.Bool("tvdb_configured", cfg.TVDB.APIKey != ""))
	account := service.NewAccount(service.AccountDeps{Users: users, Sessions: tokens, Hasher: hasher, Auth: authService, Now: time.Now, Log: log})

	api, err := rest.NewServer(rest.Deps{
		DB: pool, Auth: authService, Verification: verification, Reset: passwordReset, Account: account, Social: socialAuth, Devices: devices, Catalog: catalog,
		Profile: service.NewProfileService(users, time.Now), Tokens: jwt, Limits: rest.DefaultLimits(), Log: log,
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

// mailTransport picks how emails leave the process: printed to the log (dev only; config refuses it in
// prod) or sent through Resend.
func mailTransport(c config.MailConfig, log *zap.Logger) mail.Transport {
	if c.Provider == "resend" {
		return mail.NewResend(c.ResendAPIKey, c.From)
	}
	return mail.NewLogTransport(log)
}
