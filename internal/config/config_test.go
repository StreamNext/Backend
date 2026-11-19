package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const goodSecret = "0123456789abcdef0123456789abcdef"

func setEnv(t *testing.T, port, db, secret, grace string) {
	t.Helper()
	t.Setenv("PORT", port)
	t.Setenv("DATABASE_URL", db)
	t.Setenv("JWT_SECRET", secret)
	t.Setenv("REFRESH_GRACE_SECONDS", grace)
}

func TestLoadDefaults(t *testing.T) {
	setEnv(t, "", "postgres://x", goodSecret, "")
	c, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "8080", c.Port)
	assert.Equal(t, "postgres://x", c.DatabaseURL)
	assert.Equal(t, goodSecret, c.JWTSecret)
	assert.Equal(t, 10*time.Second, c.RefreshGrace)
}

func TestLoadOverrides(t *testing.T) {
	setEnv(t, "9000", "postgres://x", goodSecret, "0")
	c, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "9000", c.Port)
	assert.Equal(t, time.Duration(0), c.RefreshGrace, "0 disables the grace window")
}

func TestLoadFailsFast(t *testing.T) {
	cases := []struct{ name, db, secret, grace, want string }{
		{"missing database url", "", goodSecret, "", "DATABASE_URL"},
		{"missing jwt secret", "postgres://x", "", "", "JWT_SECRET"},
		{"short jwt secret", "postgres://x", "too-short", "", "JWT_SECRET"},
		{"grace not a number", "postgres://x", goodSecret, "soon", "REFRESH_GRACE_SECONDS"},
		{"grace negative", "postgres://x", goodSecret, "-1", "REFRESH_GRACE_SECONDS"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setEnv(t, "", c.db, c.secret, c.grace)
			_, err := Load()
			assert.ErrorContains(t, err, c.want)
		})
	}
}

func TestDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	u, err := DatabaseURL()
	require.NoError(t, err)
	assert.Equal(t, "postgres://x", u)

	t.Setenv("DATABASE_URL", "")
	_, err = DatabaseURL()
	assert.Error(t, err)
}
