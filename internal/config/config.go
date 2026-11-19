// Package config reads process configuration from environment variables only.
// Locally they are injected by `sops exec-env` (see Makefile); on Cloud Run by the service config.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

const minJWTSecretLen = 32

type Config struct {
	Port         string
	DatabaseURL  string
	JWTSecret    string
	RefreshGrace time.Duration // see the grace window in docs/features/auth.md
}

// DatabaseURL is all the migrate command needs.
func DatabaseURL() (string, error) {
	u := os.Getenv("DATABASE_URL")
	if u == "" {
		return "", errors.New("config: DATABASE_URL is required")
	}
	return u, nil
}

// Load fails fast when a required variable is missing or invalid.
func Load() (Config, error) {
	dbURL, err := DatabaseURL()
	if err != nil {
		return Config{}, err
	}
	c := Config{Port: os.Getenv("PORT"), DatabaseURL: dbURL, JWTSecret: os.Getenv("JWT_SECRET"), RefreshGrace: 10 * time.Second}
	if c.Port == "" {
		c.Port = "8080"
	}
	if len(c.JWTSecret) < minJWTSecretLen {
		return Config{}, fmt.Errorf("config: JWT_SECRET is required and must be at least %d bytes", minJWTSecretLen)
	}
	if v := os.Getenv("REFRESH_GRACE_SECONDS"); v != "" {
		secs, err := strconv.Atoi(v)
		if err != nil || secs < 0 {
			return Config{}, errors.New("config: REFRESH_GRACE_SECONDS must be a non-negative integer")
		}
		c.RefreshGrace = time.Duration(secs) * time.Second
	}
	return c, nil
}
