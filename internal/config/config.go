// Package config reads process configuration from environment variables only.
// Locally they are injected by `sops exec-env` (see Makefile); on Cloud Run by the service config.
package config

import (
	"errors"
	"os"
)

type Config struct {
	Port        string
	DatabaseURL string
}

// Load fails fast when a required variable is missing.
func Load() (Config, error) {
	c := Config{Port: os.Getenv("PORT"), DatabaseURL: os.Getenv("DATABASE_URL")}
	if c.Port == "" {
		c.Port = "8080"
	}
	if c.DatabaseURL == "" {
		return Config{}, errors.New("config: DATABASE_URL is required")
	}
	return c, nil
}
