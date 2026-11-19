package db

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var testURL string

// TestMain starts one throwaway Postgres 16 (same engine as prod) shared by all tests in the package.
func TestMain(m *testing.M) {
	ctx := context.Background()
	c, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("streamnext"), postgres.WithUsername("u"), postgres.WithPassword("p"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "start postgres (is Docker running?):", err)
		os.Exit(1)
	}
	if testURL, err = c.ConnectionString(ctx, "sslmode=disable"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = testcontainers.TerminateContainer(c)
	os.Exit(code)
}

func TestConnectSetsApplicationName(t *testing.T) {
	ctx := context.Background()
	pool, err := Connect(ctx, testURL, "streamnext-test")
	require.NoError(t, err)
	defer pool.Close()

	var name string
	require.NoError(t, pool.QueryRow(ctx, "SHOW application_name").Scan(&name))
	assert.Equal(t, "streamnext-test", name)
}

func TestConnectErrors(t *testing.T) {
	ctx := context.Background()
	_, err := Connect(ctx, "://not a url", "x")
	assert.ErrorContains(t, err, "parse url")

	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err = Connect(ctx, "postgres://u:p@127.0.0.1:1/x?sslmode=disable&connect_timeout=1", "x")
	assert.Error(t, err)
}

func TestMigrateIsIdempotentAndCreatesExtensions(t *testing.T) {
	ctx := context.Background()
	pool, err := Connect(ctx, testURL, "streamnext-test")
	require.NoError(t, err)
	defer pool.Close()

	require.NoError(t, Migrate(ctx, pool))
	require.NoError(t, Migrate(ctx, pool), "second run is a no-op")

	var ext string
	require.NoError(t, pool.QueryRow(ctx, "SELECT extname FROM pg_extension WHERE extname='citext'").Scan(&ext))
	assert.Equal(t, "citext", ext)
}

func TestMigrateFailsOnClosedPool(t *testing.T) {
	ctx := context.Background()
	pool, err := Connect(ctx, testURL, "streamnext-test")
	require.NoError(t, err)
	pool.Close()
	assert.Error(t, Migrate(ctx, pool))
}
