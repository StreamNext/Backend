package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/StreamNext/Backend/internal/service"
)

func emailGrant(userID uuid.UUID, purpose service.EmailPurpose, hash string) service.NewEmailToken {
	return service.NewEmailToken{UserID: userID, Purpose: purpose, TokenHash: hash, ExpiresAt: now.Add(time.Hour)}
}

func TestEmailTokenConsumeIsSingleUse(t *testing.T) {
	reset(t)
	ctx, repo := context.Background(), NewEmailTokens(testPool)
	u := newUser(t, "ana@example.com")
	require.NoError(t, repo.Replace(ctx, emailGrant(u.ID, service.PurposeReset, "h1"), now))

	id, err := repo.Consume(ctx, "h1", service.PurposeReset, now.Add(time.Minute))
	require.NoError(t, err)
	assert.Equal(t, u.ID, id)

	_, err = repo.Consume(ctx, "h1", service.PurposeReset, now.Add(2*time.Minute))
	assert.ErrorIs(t, err, service.ErrNotFound, "a second use fails")
}

func TestEmailTokenConsumeChecksExpiryPurposeAndExistence(t *testing.T) {
	reset(t)
	ctx, repo := context.Background(), NewEmailTokens(testPool)
	u := newUser(t, "ana@example.com")
	require.NoError(t, repo.Replace(ctx, emailGrant(u.ID, service.PurposeVerify, "h1"), now))

	_, err := repo.Consume(ctx, "h1", service.PurposeReset, now)
	assert.ErrorIs(t, err, service.ErrNotFound, "a verify token can't be used to reset")
	_, err = repo.Consume(ctx, "unknown", service.PurposeVerify, now)
	assert.ErrorIs(t, err, service.ErrNotFound)
	_, err = repo.Consume(ctx, "h1", service.PurposeVerify, now.Add(time.Hour))
	assert.ErrorIs(t, err, service.ErrNotFound, "expired exactly at expires_at")

	_, err = repo.Consume(ctx, "h1", service.PurposeVerify, now.Add(time.Hour-time.Second))
	assert.NoError(t, err, "the failed attempts above did not use the token up")
}

func TestEmailTokenReplaceSupersedesOlderUnusedOnesOfTheSamePurpose(t *testing.T) {
	reset(t)
	ctx, repo := context.Background(), NewEmailTokens(testPool)
	u, other := newUser(t, "ana@example.com"), newUser(t, "bob@example.com")
	require.NoError(t, repo.Replace(ctx, emailGrant(u.ID, service.PurposeVerify, "old-verify"), now))
	require.NoError(t, repo.Replace(ctx, emailGrant(u.ID, service.PurposeReset, "reset"), now))
	require.NoError(t, repo.Replace(ctx, emailGrant(other.ID, service.PurposeVerify, "bobs"), now))

	require.NoError(t, repo.Replace(ctx, emailGrant(u.ID, service.PurposeVerify, "new-verify"), now.Add(time.Minute)))

	_, err := repo.Consume(ctx, "old-verify", service.PurposeVerify, now.Add(2*time.Minute))
	assert.ErrorIs(t, err, service.ErrNotFound, "superseded")
	_, err = repo.Consume(ctx, "new-verify", service.PurposeVerify, now.Add(2*time.Minute))
	assert.NoError(t, err)
	_, err = repo.Consume(ctx, "reset", service.PurposeReset, now.Add(2*time.Minute))
	assert.NoError(t, err, "other purposes are untouched")
	_, err = repo.Consume(ctx, "bobs", service.PurposeVerify, now.Add(2*time.Minute))
	assert.NoError(t, err, "other users are untouched")
}

func TestEmailTokenLastCreated(t *testing.T) {
	reset(t)
	ctx, repo := context.Background(), NewEmailTokens(testPool)
	u := newUser(t, "ana@example.com")

	_, err := repo.LastCreated(ctx, u.ID, service.PurposeVerify)
	assert.ErrorIs(t, err, service.ErrNotFound, "none yet")

	require.NoError(t, repo.Replace(ctx, emailGrant(u.ID, service.PurposeVerify, "a"), now))
	require.NoError(t, repo.Replace(ctx, emailGrant(u.ID, service.PurposeVerify, "b"), now.Add(30*time.Second)))
	at, err := repo.LastCreated(ctx, u.ID, service.PurposeVerify)
	require.NoError(t, err)
	assert.True(t, at.Equal(now.Add(30*time.Second)), "the most recent one, got %v", at)

	_, err = repo.LastCreated(ctx, u.ID, service.PurposeReset)
	assert.ErrorIs(t, err, service.ErrNotFound, "per purpose")

	_, err = repo.Consume(ctx, "b", service.PurposeVerify, now.Add(time.Minute))
	require.NoError(t, err)
	at, err = repo.LastCreated(ctx, u.ID, service.PurposeVerify)
	require.NoError(t, err)
	assert.True(t, at.Equal(now.Add(30*time.Second)), "a used token still counts toward the cooldown")
}

func TestEmailTokenConcurrentConsumeHasExactlyOneWinner(t *testing.T) {
	reset(t)
	ctx, repo := context.Background(), NewEmailTokens(testPool)
	u := newUser(t, "ana@example.com")
	require.NoError(t, repo.Replace(ctx, emailGrant(u.ID, service.PurposeReset, "race"), now))

	const racers = 8
	wins := make(chan bool, racers)
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.Consume(ctx, "race", service.PurposeReset, now.Add(time.Minute))
			wins <- err == nil
		}()
	}
	wg.Wait()
	close(wins)

	won := 0
	for ok := range wins {
		if ok {
			won++
		}
	}
	assert.Equal(t, 1, won)
}

func TestEmailTokenDatabaseRejectsAnUnknownPurposeAndDuplicateHashes(t *testing.T) {
	reset(t)
	ctx, repo := context.Background(), NewEmailTokens(testPool)
	u := newUser(t, "ana@example.com")

	assert.Error(t, repo.Replace(ctx, emailGrant(u.ID, "marketing", "h"), now), "the CHECK constraint")
	require.NoError(t, repo.Replace(ctx, emailGrant(u.ID, service.PurposeVerify, "same"), now))
	assert.Error(t, repo.Replace(ctx, emailGrant(u.ID, service.PurposeReset, "same"), now), "token hashes are unique")
}

func TestEmailTokenReplaceRollsBackWhenTheInsertFails(t *testing.T) {
	reset(t)
	ctx, repo := context.Background(), NewEmailTokens(testPool)
	u := newUser(t, "ana@example.com")
	require.NoError(t, repo.Replace(ctx, emailGrant(u.ID, service.PurposeVerify, "keep"), now))

	// Same purpose, but the hash already exists: the insert fails after the invalidate step.
	assert.Error(t, repo.Replace(ctx, emailGrant(u.ID, service.PurposeVerify, "keep"), now.Add(time.Minute)))
	_, err := repo.Consume(ctx, "keep", service.PurposeVerify, now.Add(2*time.Minute))
	assert.NoError(t, err, "the invalidation was rolled back, so the user still has a working link")
}

func TestEmailTokenErrorsPassThrough(t *testing.T) {
	reset(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	repo := NewEmailTokens(testPool)
	assert.Error(t, repo.Replace(ctx, emailGrant(uuid.New(), service.PurposeVerify, "x"), now))
	_, err := repo.LastCreated(ctx, uuid.New(), service.PurposeVerify)
	assert.Error(t, err)
	assert.NotErrorIs(t, err, service.ErrNotFound)
	_, err = repo.Consume(ctx, "x", service.PurposeVerify, now)
	assert.Error(t, err)
	assert.NotErrorIs(t, err, service.ErrNotFound)
}
