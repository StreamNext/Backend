package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad(t *testing.T) {
	t.Run("defaults port", func(t *testing.T) {
		t.Setenv("PORT", "")
		t.Setenv("DATABASE_URL", "postgres://x")
		c, err := Load()
		require.NoError(t, err)
		assert.Equal(t, "8080", c.Port)
		assert.Equal(t, "postgres://x", c.DatabaseURL)
	})
	t.Run("explicit port", func(t *testing.T) {
		t.Setenv("PORT", "9000")
		t.Setenv("DATABASE_URL", "postgres://x")
		c, err := Load()
		require.NoError(t, err)
		assert.Equal(t, "9000", c.Port)
	})
	t.Run("missing database url fails fast", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "")
		_, err := Load()
		assert.ErrorContains(t, err, "DATABASE_URL")
	})
}
