package repository

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/StreamNext/Backend/internal/db"
	"github.com/StreamNext/Backend/internal/service"
	"github.com/StreamNext/Backend/internal/testdb"
)

var testPool *pgxpool.Pool

// TestMain starts one Postgres, runs the real migrations, and shares the pool across the package.
func TestMain(m *testing.M) {
	ctx := context.Background()
	url, stop, err := testdb.Start(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := func() int {
		defer stop()
		if testPool, err = db.Connect(ctx, url, "streamnext-repo-test"); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer testPool.Close()
		if err = db.Migrate(ctx, testPool); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return m.Run()
	}()
	os.Exit(code)
}

var now = time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

// reset empties the tables so each test starts clean.
func reset(t *testing.T) {
	t.Helper()
	_, err := testPool.Exec(context.Background(), "TRUNCATE users CASCADE")
	require.NoError(t, err)
}

func newUser(t *testing.T, email string) service.User {
	t.Helper()
	pw := "hash"
	u, err := NewUsers(testPool).Create(context.Background(), service.NewUser{Email: email, PasswordHash: &pw, DisplayName: "Ana", Timezone: "UTC"})
	require.NoError(t, err)
	return u
}

func grant(userID, deviceID uuid.UUID, hash string) service.NewToken {
	return service.NewToken{UserID: userID, DeviceID: deviceID, TokenHash: hash, LifetimeSeconds: 86400, ExpiresAt: now.Add(24 * time.Hour)}
}
