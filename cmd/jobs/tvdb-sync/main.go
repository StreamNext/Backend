// Command tvdb-sync applies TheTVDB's changes to the series we store, so the API never has to refresh them.
// Run it with `make tvdb-sync`; once deployed, Cloud Scheduler runs it hourly. It exits non-zero if any series
// could not be refreshed, and the next run repeats that window.
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"time"

	"go.uber.org/zap"

	"github.com/StreamNext/Backend/internal/config"
	"github.com/StreamNext/Backend/internal/db"
	"github.com/StreamNext/Backend/internal/repository"
	"github.com/StreamNext/Backend/internal/service"
	"github.com/StreamNext/Backend/internal/tvdb"
)

func main() {
	log, _ := zap.NewProduction()
	defer func() { _ = log.Sync() }()
	if err := run(log); err != nil {
		log.Error("tvdb-sync failed", zap.Error(err))
		_ = log.Sync()
		os.Exit(1)
	}
}

func run(log *zap.Logger) error {
	dbURL, err := config.DatabaseURL()
	if err != nil {
		return err
	}
	cfg, err := config.TVDBFromEnv()
	if err != nil {
		return err
	}
	if cfg.APIKey == "" {
		return errors.New("TVDB_API_KEY is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	pool, err := db.Connect(ctx, dbURL, "streamnext-tvdb-sync")
	if err != nil {
		return err
	}
	defer pool.Close()

	sync := service.NewCatalogSync(service.CatalogSyncDeps{
		Source: tvdb.New(tvdb.Config{APIKey: cfg.APIKey, BaseURL: cfg.BaseURL}, &http.Client{Timeout: 30 * time.Second}, time.Now),
		Repo:   repository.NewCatalog(pool), State: repository.NewSyncState(pool), Now: time.Now, Log: log,
	})
	_, err = sync.Run(ctx)
	return err
}
