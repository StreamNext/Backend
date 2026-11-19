// Package testdb starts a throwaway Postgres 16 (the same engine as prod) for tests that need a real database.
package testdb

import (
	"context"
	"fmt"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Start returns the connection URL of a fresh, empty database and a function that removes the container.
// Call it from TestMain so one container is shared by every test in the package.
func Start(ctx context.Context) (url string, stop func(), err error) {
	c, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("streamnext"), postgres.WithUsername("u"), postgres.WithPassword("p"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		return "", nil, fmt.Errorf("start postgres (is Docker running?): %w", err)
	}
	stop = func() { _ = testcontainers.TerminateContainer(c) }
	if url, err = c.ConnectionString(ctx, "sslmode=disable"); err != nil {
		stop()
		return "", nil, err
	}
	return url, stop, nil
}
