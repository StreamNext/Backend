// Package config reads process configuration from environment variables only.
// Locally they are injected by `sops exec-env` (see Makefile); on Cloud Run by the service config.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const minJWTSecretLen = 32

type Config struct {
	Port         string
	DatabaseURL  string
	JWTSecret    string
	RefreshGrace time.Duration // see the grace window in docs/features/auth.md

	Env        string // "dev" or "prod"
	AppBaseURL string // where email links point (the Web repo's pages), no trailing slash
	Mail       MailConfig
	Social     SocialConfig
}

// SocialConfig lists the client ids ID tokens must be issued for. A provider with none is disabled
// (its endpoint answers 503).
type SocialConfig struct {
	AppleClientID   string   // the app's bundle id (or the Services id for web)
	GoogleClientIDs []string // the iOS, Android and web client ids
}

type MailConfig struct {
	Provider     string // "log" (prints emails, dev only) or "resend"
	From         string
	ResendAPIKey string
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
	c := Config{
		Port: env("PORT", "8080"), DatabaseURL: dbURL, JWTSecret: os.Getenv("JWT_SECRET"), RefreshGrace: 10 * time.Second,
		Env: env("ENV", "dev"), AppBaseURL: env("APP_BASE_URL", "http://localhost:3000"),
		Mail:   MailConfig{Provider: env("MAIL_PROVIDER", "log"), From: os.Getenv("MAIL_FROM"), ResendAPIKey: os.Getenv("RESEND_API_KEY")},
		Social: SocialConfig{AppleClientID: strings.TrimSpace(os.Getenv("APPLE_CLIENT_ID")), GoogleClientIDs: splitList(os.Getenv("GOOGLE_CLIENT_IDS"))},
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
	if c.Env != "dev" && c.Env != "prod" {
		return Config{}, errors.New(`config: ENV must be "dev" or "prod"`)
	}
	if c.AppBaseURL, err = cleanBaseURL(c.AppBaseURL); err != nil {
		return Config{}, err
	}
	if err := c.Mail.validate(c.Env); err != nil {
		return Config{}, err
	}
	return c, nil
}

func (m MailConfig) validate(env string) error {
	switch m.Provider {
	case "log":
		if env == "prod" { // the log provider prints the links, and with them working tokens
			return errors.New(`config: MAIL_PROVIDER "log" is not allowed when ENV is "prod"`)
		}
	case "resend":
		if m.From == "" || m.ResendAPIKey == "" {
			return errors.New(`config: MAIL_PROVIDER "resend" needs MAIL_FROM and RESEND_API_KEY`)
		}
	default:
		return errors.New(`config: MAIL_PROVIDER must be "log" or "resend"`)
	}
	return nil
}

func cleanBaseURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("config: APP_BASE_URL must be an absolute http(s) URL")
	}
	return strings.TrimRight(raw, "/"), nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// splitList reads a comma-separated list, ignoring spaces and empty entries.
func splitList(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
