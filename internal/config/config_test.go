package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const goodSecret = "0123456789abcdef0123456789abcdef"

// setEnv sets the required variables and clears every optional one, so tests start from the defaults.
func setEnv(t *testing.T, overrides map[string]string) {
	t.Helper()
	base := map[string]string{
		"PORT": "", "DATABASE_URL": "postgres://x", "JWT_SECRET": goodSecret, "REFRESH_GRACE_SECONDS": "",
		"ENV": "", "APP_BASE_URL": "", "MAIL_PROVIDER": "", "MAIL_FROM": "", "RESEND_API_KEY": "", "APPLE_CLIENT_ID": "", "GOOGLE_CLIENT_IDS": "",
	}
	for k, v := range overrides {
		base[k] = v
	}
	for k, v := range base {
		t.Setenv(k, v)
	}
}

func TestLoadDefaults(t *testing.T) {
	setEnv(t, nil)
	c, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "8080", c.Port)
	assert.Equal(t, "postgres://x", c.DatabaseURL)
	assert.Equal(t, goodSecret, c.JWTSecret)
	assert.Equal(t, 10*time.Second, c.RefreshGrace)
	assert.Equal(t, "dev", c.Env)
	assert.Equal(t, "http://localhost:3000", c.AppBaseURL)
	assert.Equal(t, "log", c.Mail.Provider)
	assert.Equal(t, SocialConfig{}, c.Social, "no provider is enabled unless its client id is set")
}

func TestLoadSocialProviders(t *testing.T) {
	setEnv(t, map[string]string{"APPLE_CLIENT_ID": " app.streamnext.ios ", "GOOGLE_CLIENT_IDS": "ios-id.apps.googleusercontent.com, android-id ,, web-id,"})
	c, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "app.streamnext.ios", c.Social.AppleClientID)
	assert.Equal(t, []string{"ios-id.apps.googleusercontent.com", "android-id", "web-id"}, c.Social.GoogleClientIDs, "spaces and empty entries are dropped")
}

func TestLoadOverrides(t *testing.T) {
	setEnv(t, map[string]string{"PORT": "9000", "REFRESH_GRACE_SECONDS": "0", "APP_BASE_URL": "https://streamnext.app/"})
	c, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "9000", c.Port)
	assert.Equal(t, time.Duration(0), c.RefreshGrace, "0 disables the grace window")
	assert.Equal(t, "https://streamnext.app", c.AppBaseURL, "trailing slash is removed so links join cleanly")
}

func TestLoadProdWithResend(t *testing.T) {
	setEnv(t, map[string]string{"ENV": "prod", "MAIL_PROVIDER": "resend", "MAIL_FROM": "StreamNext <hi@streamnext.app>", "RESEND_API_KEY": "re_123"})
	c, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "prod", c.Env)
	assert.Equal(t, MailConfig{Provider: "resend", From: "StreamNext <hi@streamnext.app>", ResendAPIKey: "re_123"}, c.Mail)
}

func TestLoadFailsFast(t *testing.T) {
	cases := map[string]struct {
		env  map[string]string
		want string
	}{
		"missing database url":  {map[string]string{"DATABASE_URL": ""}, "DATABASE_URL"},
		"missing jwt secret":    {map[string]string{"JWT_SECRET": ""}, "JWT_SECRET"},
		"short jwt secret":      {map[string]string{"JWT_SECRET": "too-short"}, "JWT_SECRET"},
		"grace not a number":    {map[string]string{"REFRESH_GRACE_SECONDS": "soon"}, "REFRESH_GRACE_SECONDS"},
		"grace negative":        {map[string]string{"REFRESH_GRACE_SECONDS": "-1"}, "REFRESH_GRACE_SECONDS"},
		"unknown env":           {map[string]string{"ENV": "staging"}, "ENV"},
		"relative base url":     {map[string]string{"APP_BASE_URL": "/verify"}, "APP_BASE_URL"},
		"non-http base url":     {map[string]string{"APP_BASE_URL": "ftp://x.com"}, "APP_BASE_URL"},
		"unknown mail provider": {map[string]string{"MAIL_PROVIDER": "smtp"}, "MAIL_PROVIDER"},
		"log mail in prod":      {map[string]string{"ENV": "prod"}, "not allowed"},
		"resend without key":    {map[string]string{"MAIL_PROVIDER": "resend", "MAIL_FROM": "a@b.co"}, "RESEND_API_KEY"},
		"resend without from":   {map[string]string{"MAIL_PROVIDER": "resend", "RESEND_API_KEY": "k"}, "MAIL_FROM"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			setEnv(t, c.env)
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
