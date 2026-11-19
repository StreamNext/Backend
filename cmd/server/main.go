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

	"github.com/StreamNext/Backend/internal/config"
	"github.com/StreamNext/Backend/internal/db"
	"github.com/StreamNext/Backend/internal/rest"
)

func main() {
	log, _ := zap.NewProduction()
	defer func() { _ = log.Sync() }()
	if err := run(log); err != nil {
		log.Fatal("server stopped", zap.Error(err))
	}
}

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

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: rest.NewServer(pool, log).Handler(), ReadHeaderTimeout: 5 * time.Second}
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
