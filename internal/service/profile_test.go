package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/StreamNext/Backend/internal/apperr"
)

func str(s string) *string { return &s }

func profileEnv(t *testing.T) (*ProfileService, *fakeUsers, User) {
	t.Helper()
	users := newFakeUsers()
	u, err := users.Create(context.Background(), NewUser{Email: "ana@example.com", DisplayName: "Ana", Timezone: "UTC"})
	require.NoError(t, err)
	return NewProfileService(users, (&clock{t: epoch}).Now), users, u
}

func TestProfileGet(t *testing.T) {
	svc, _, u := profileEnv(t)
	got, err := svc.Get(context.Background(), u.ID)
	require.NoError(t, err)
	assert.Equal(t, "Ana", got.DisplayName)

	_, err = svc.Get(context.Background(), uuid.New())
	requireKind(t, err, apperr.Unauthenticated)
}

func TestProfileGetInfrastructureError(t *testing.T) {
	svc, users, u := profileEnv(t)
	users.fail["GetByID"] = errors.New("db down")
	_, err := svc.Get(context.Background(), u.ID)
	requireKind(t, err, apperr.Internal)
}

func TestProfileUpdate(t *testing.T) {
	svc, _, u := profileEnv(t)
	ctx := context.Background()

	got, err := svc.Update(ctx, u.ID, UpdateProfileInput{DisplayName: str("  Ana M  ")})
	require.NoError(t, err)
	assert.Equal(t, "Ana M", got.DisplayName)
	assert.Equal(t, "UTC", got.Timezone, "fields not sent are unchanged")

	got, err = svc.Update(ctx, u.ID, UpdateProfileInput{Timezone: str("America/Bogota")})
	require.NoError(t, err)
	assert.Equal(t, "America/Bogota", got.Timezone)
	assert.Equal(t, "Ana M", got.DisplayName)
}

func TestProfileUpdateValidation(t *testing.T) {
	svc, _, u := profileEnv(t)
	for name, in := range map[string]UpdateProfileInput{
		"nothing":      {},
		"blank name":   {DisplayName: str(" ")},
		"long name":    {DisplayName: str(string(make([]rune, 41)) + "x")},
		"bad timezone": {Timezone: str("Nowhere/Land")},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := svc.Update(context.Background(), u.ID, in)
			requireKind(t, err, apperr.InvalidArgument)
		})
	}
}

func TestProfileUpdateEmptyTimezoneMeansUTC(t *testing.T) {
	svc, _, u := profileEnv(t)
	got, err := svc.Update(context.Background(), u.ID, UpdateProfileInput{Timezone: str("")})
	require.NoError(t, err)
	assert.Equal(t, "UTC", got.Timezone)
}

func TestProfileUpdateErrors(t *testing.T) {
	svc, users, u := profileEnv(t)
	_, err := svc.Update(context.Background(), uuid.New(), UpdateProfileInput{DisplayName: str("x")})
	requireKind(t, err, apperr.Unauthenticated)

	users.fail["UpdateProfile"] = errors.New("db down")
	_, err = svc.Update(context.Background(), u.ID, UpdateProfileInput{DisplayName: str("x")})
	requireKind(t, err, apperr.Internal)
}

func TestProfileLanguage(t *testing.T) {
	svc, users, u := profileEnv(t)
	ctx := context.Background()

	got, err := svc.Update(ctx, u.ID, UpdateProfileInput{Language: str("es")})
	require.NoError(t, err)
	assert.Equal(t, LanguageSpanish, got.Language)
	assert.Equal(t, "Ana", got.DisplayName, "only the language changed")
	assert.Equal(t, LanguageSpanish, users.byID[u.ID].Language)

	got, err = svc.Update(ctx, u.ID, UpdateProfileInput{Language: str("en")})
	require.NoError(t, err)
	assert.Equal(t, LanguageEnglish, got.Language)

	for _, bad := range []string{"fr", "", "EN", "eng"} {
		_, err := svc.Update(ctx, u.ID, UpdateProfileInput{Language: str(bad)})
		requireKind(t, err, apperr.InvalidArgument, bad)
	}
}
