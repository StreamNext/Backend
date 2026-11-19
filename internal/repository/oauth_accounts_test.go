package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/StreamNext/Backend/internal/service"
)

func identity(p service.Provider, subject, email string) service.Identity {
	return service.Identity{Provider: p, Subject: subject, Email: email, EmailVerified: true}
}

func TestOAuthLinkAndFind(t *testing.T) {
	reset(t)
	ctx, repo := context.Background(), NewOAuthAccounts(testPool)
	u := newUser(t, "ana@example.com")

	_, err := repo.FindUser(ctx, service.ProviderGoogle, "sub-1")
	assert.ErrorIs(t, err, service.ErrNotFound)

	require.NoError(t, repo.Link(ctx, u.ID, identity(service.ProviderGoogle, "sub-1", "ana@example.com"), now))
	got, err := repo.FindUser(ctx, service.ProviderGoogle, "sub-1")
	require.NoError(t, err)
	assert.Equal(t, u.ID, got)

	_, err = repo.FindUser(ctx, service.ProviderApple, "sub-1")
	assert.ErrorIs(t, err, service.ErrNotFound, "the same subject at another provider is a different identity")
	_, err = repo.FindUser(ctx, service.ProviderGoogle, "sub-2")
	assert.ErrorIs(t, err, service.ErrNotFound)
}

func TestOAuthAnIdentityLinksToExactlyOneUser(t *testing.T) {
	reset(t)
	ctx, repo := context.Background(), NewOAuthAccounts(testPool)
	ana, bob := newUser(t, "ana@example.com"), newUser(t, "bob@example.com")
	require.NoError(t, repo.Link(ctx, ana.ID, identity(service.ProviderGoogle, "sub-1", ""), now))

	assert.ErrorIs(t, repo.Link(ctx, bob.ID, identity(service.ProviderGoogle, "sub-1", ""), now), service.ErrIdentityTaken, "another user")
	assert.ErrorIs(t, repo.Link(ctx, ana.ID, identity(service.ProviderGoogle, "sub-1", ""), now), service.ErrIdentityTaken, "the same user again")

	require.NoError(t, repo.Link(ctx, ana.ID, identity(service.ProviderApple, "sub-1", ""), now), "one user may have several providers")
	got, _ := repo.FindUser(ctx, service.ProviderGoogle, "sub-1")
	assert.Equal(t, ana.ID, got, "the failed attempts changed nothing")
}

func TestOAuthEmailIsOptionalAndStoredAsNull(t *testing.T) {
	reset(t)
	ctx, repo := context.Background(), NewOAuthAccounts(testPool)
	u := newUser(t, "ana@example.com")
	require.NoError(t, repo.Link(ctx, u.ID, identity(service.ProviderApple, "relay", ""), now))

	var email *string
	require.NoError(t, testPool.QueryRow(ctx, "SELECT email FROM oauth_account WHERE subject='relay'").Scan(&email))
	assert.Nil(t, email, "Apple may share no email on later sign-ins")
}

func TestOAuthDatabaseRejectsUnknownProvidersAndOrphans(t *testing.T) {
	reset(t)
	ctx, repo := context.Background(), NewOAuthAccounts(testPool)
	u := newUser(t, "ana@example.com")
	assert.Error(t, repo.Link(ctx, u.ID, identity("facebook", "x", ""), now), "the CHECK constraint")
	assert.Error(t, repo.Link(ctx, uuid.New(), identity(service.ProviderGoogle, "x", ""), now), "the user must exist")
}

func TestOAuthIdentitiesAreDeletedWithTheirUser(t *testing.T) {
	reset(t)
	ctx, repo := context.Background(), NewOAuthAccounts(testPool)
	u := newUser(t, "ana@example.com")
	require.NoError(t, repo.Link(ctx, u.ID, identity(service.ProviderGoogle, "sub-1", ""), now))

	require.NoError(t, NewUsers(testPool).Delete(ctx, u.ID))
	_, err := repo.FindUser(ctx, service.ProviderGoogle, "sub-1")
	assert.ErrorIs(t, err, service.ErrNotFound, "ON DELETE CASCADE")
}

func TestOAuthErrorsPassThrough(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	repo := NewOAuthAccounts(testPool)
	_, err := repo.FindUser(ctx, service.ProviderGoogle, "x")
	assert.Error(t, err)
	assert.NotErrorIs(t, err, service.ErrNotFound)
	err = repo.Link(ctx, uuid.New(), identity(service.ProviderGoogle, "x", ""), now)
	assert.Error(t, err)
	assert.NotErrorIs(t, err, service.ErrIdentityTaken)
}
