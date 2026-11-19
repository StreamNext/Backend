package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/StreamNext/Backend/internal/service"
)

func TestUsersCreateAndGet(t *testing.T) {
	reset(t)
	ctx, repo := context.Background(), NewUsers(testPool)
	pw := "phc-string"
	created, err := repo.Create(ctx, "ana@example.com", &pw, "Ana", "America/Bogota")
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, created.ID, "the database assigns the id")
	require.NotNil(t, created.PasswordHash)
	assert.Equal(t, "phc-string", *created.PasswordHash)
	assert.Nil(t, created.EmailVerifiedAt)

	byID, err := repo.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created, byID)

	byEmail, err := repo.GetByEmail(ctx, "ANA@Example.COM")
	require.NoError(t, err, "email lookup is case-insensitive (citext)")
	assert.Equal(t, created.ID, byEmail.ID)
}

func TestUsersSocialOnlyAccountHasNoPasswordHash(t *testing.T) {
	reset(t)
	u, err := NewUsers(testPool).Create(context.Background(), "social@example.com", nil, "S", "UTC")
	require.NoError(t, err)
	assert.Nil(t, u.PasswordHash)
}

func TestUsersDuplicateEmailIgnoresCase(t *testing.T) {
	reset(t)
	newUser(t, "ana@example.com")
	_, err := NewUsers(testPool).Create(context.Background(), "ANA@EXAMPLE.COM", nil, "Other", "UTC")
	assert.ErrorIs(t, err, service.ErrEmailTaken)
}

func TestUsersNotFound(t *testing.T) {
	reset(t)
	repo := NewUsers(testPool)
	_, err := repo.GetByID(context.Background(), uuid.New())
	assert.ErrorIs(t, err, service.ErrNotFound)
	_, err = repo.GetByEmail(context.Background(), "nobody@example.com")
	assert.ErrorIs(t, err, service.ErrNotFound)
	_, err = repo.UpdateProfile(context.Background(), uuid.New(), nil, nil, now)
	assert.ErrorIs(t, err, service.ErrNotFound)
}

func TestUsersUpdateProfileChangesOnlyGivenFields(t *testing.T) {
	reset(t)
	ctx, repo := context.Background(), NewUsers(testPool)
	u := newUser(t, "ana@example.com")

	name := "Ana María"
	got, err := repo.UpdateProfile(ctx, u.ID, &name, nil, now)
	require.NoError(t, err)
	assert.Equal(t, "Ana María", got.DisplayName)
	assert.Equal(t, "UTC", got.Timezone, "timezone untouched")

	tz := "Europe/Madrid"
	got, err = repo.UpdateProfile(ctx, u.ID, nil, &tz, now)
	require.NoError(t, err)
	assert.Equal(t, "Europe/Madrid", got.Timezone)
	assert.Equal(t, "Ana María", got.DisplayName, "name untouched")
}

func TestUsersErrorsPassThrough(t *testing.T) {
	reset(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // every call now fails with a context error, which is not "not found"
	repo := NewUsers(testPool)
	_, err := repo.Create(ctx, "x@y.co", nil, "X", "UTC")
	assert.Error(t, err)
	assert.NotErrorIs(t, err, service.ErrEmailTaken)
	_, err = repo.GetByID(ctx, uuid.New())
	assert.Error(t, err)
	assert.NotErrorIs(t, err, service.ErrNotFound)
}
