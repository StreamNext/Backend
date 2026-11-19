package repository

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/StreamNext/Backend/internal/service"
)

var (
	phone  = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	tablet = uuid.MustParse("22222222-2222-4222-8222-222222222222")
)

func liveCount(t *testing.T, userID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, testPool.QueryRow(context.Background(),
		"SELECT count(*) FROM refresh_token WHERE user_id=$1 AND revoked_at IS NULL", userID).Scan(&n))
	return n
}

func TestTokensIssueAndLookup(t *testing.T) {
	reset(t)
	ctx, repo := context.Background(), NewRefreshTokens(testPool)
	u := newUser(t, "ana@example.com")
	require.NoError(t, repo.Issue(ctx, grant(u.ID, phone, "hash-1"), now))

	got, err := repo.GetByHash(ctx, "hash-1")
	require.NoError(t, err)
	assert.Equal(t, u.ID, got.UserID)
	assert.Equal(t, phone, got.DeviceID)
	assert.Equal(t, 86400, got.LifetimeSeconds)
	assert.Equal(t, now.Add(24*3600e9), got.ExpiresAt.UTC())
	assert.Nil(t, got.RevokedAt)
	assert.Nil(t, got.ReplacedBy)

	byID, err := repo.GetByID(ctx, got.ID)
	require.NoError(t, err)
	assert.Equal(t, got, byID)

	_, err = repo.GetByHash(ctx, "missing")
	assert.ErrorIs(t, err, service.ErrNotFound)
	_, err = repo.GetByID(ctx, uuid.New())
	assert.ErrorIs(t, err, service.ErrNotFound)
}

func TestTokensIssueKeepsOneLiveTokenPerDevice(t *testing.T) {
	reset(t)
	ctx, repo := context.Background(), NewRefreshTokens(testPool)
	u := newUser(t, "ana@example.com")
	require.NoError(t, repo.Issue(ctx, grant(u.ID, phone, "phone-1"), now))
	require.NoError(t, repo.Issue(ctx, grant(u.ID, tablet, "tablet-1"), now))
	assert.Equal(t, 2, liveCount(t, u.ID), "different devices are independent")

	require.NoError(t, repo.Issue(ctx, grant(u.ID, phone, "phone-2"), now))
	assert.Equal(t, 2, liveCount(t, u.ID))
	old, _ := repo.GetByHash(ctx, "phone-1")
	assert.NotNil(t, old.RevokedAt, "the phone's previous token was revoked")
	assert.Nil(t, old.ReplacedBy)
}

func TestTokensDatabaseRejectsTwoLiveTokensForOneDevice(t *testing.T) {
	reset(t)
	u := newUser(t, "ana@example.com")
	insert := func(hash string) error {
		_, err := testPool.Exec(context.Background(),
			"INSERT INTO refresh_token (user_id, device_id, token_hash, lifetime_seconds, expires_at) VALUES ($1,$2,$3,60,now())", u.ID, phone, hash)
		return err
	}
	require.NoError(t, insert("a"))
	assert.Error(t, insert("b"), "the partial unique index is the backstop behind the service logic")
}

func TestTokensRotate(t *testing.T) {
	reset(t)
	ctx, repo := context.Background(), NewRefreshTokens(testPool)
	u := newUser(t, "ana@example.com")
	require.NoError(t, repo.Issue(ctx, grant(u.ID, phone, "old"), now))
	old, _ := repo.GetByHash(ctx, "old")

	ok, err := repo.Rotate(ctx, old.ID, grant(u.ID, phone, "new"), now)
	require.NoError(t, err)
	require.True(t, ok)

	oldAfter, _ := repo.GetByID(ctx, old.ID)
	fresh, err := repo.GetByHash(ctx, "new")
	require.NoError(t, err)
	assert.NotNil(t, oldAfter.RevokedAt)
	require.NotNil(t, oldAfter.ReplacedBy)
	assert.Equal(t, fresh.ID, *oldAfter.ReplacedBy)
	assert.Nil(t, fresh.RevokedAt)
	assert.Equal(t, 1, liveCount(t, u.ID))
}

func TestTokensRotateOfANonLiveTokenChangesNothing(t *testing.T) {
	reset(t)
	ctx, repo := context.Background(), NewRefreshTokens(testPool)
	u := newUser(t, "ana@example.com")
	require.NoError(t, repo.Issue(ctx, grant(u.ID, phone, "old"), now))
	old, _ := repo.GetByHash(ctx, "old")
	require.NoError(t, repo.Revoke(ctx, old.ID, now))

	ok, err := repo.Rotate(ctx, old.ID, grant(u.ID, phone, "new"), now)
	require.NoError(t, err)
	assert.False(t, ok)
	_, err = repo.GetByHash(ctx, "new")
	assert.ErrorIs(t, err, service.ErrNotFound, "no replacement row was left behind")

	ok, err = repo.Rotate(ctx, uuid.New(), grant(u.ID, phone, "other"), now)
	require.NoError(t, err)
	assert.False(t, ok, "unknown id")
}

func TestTokensRotateRollsBackWhenTheInsertFails(t *testing.T) {
	reset(t)
	ctx, repo := context.Background(), NewRefreshTokens(testPool)
	u := newUser(t, "ana@example.com")
	require.NoError(t, repo.Issue(ctx, grant(u.ID, phone, "old"), now))
	require.NoError(t, repo.Issue(ctx, grant(u.ID, tablet, "taken"), now))
	old, _ := repo.GetByHash(ctx, "old")

	ok, err := repo.Rotate(ctx, old.ID, grant(u.ID, phone, "taken"), now) // hash already exists
	assert.Error(t, err)
	assert.False(t, ok)
	after, _ := repo.GetByID(ctx, old.ID)
	assert.Nil(t, after.RevokedAt, "the revoke was rolled back, so the client keeps a working token")
}

func TestTokensConcurrentRotationHasExactlyOneWinner(t *testing.T) {
	reset(t)
	ctx, repo := context.Background(), NewRefreshTokens(testPool)
	u := newUser(t, "ana@example.com")
	require.NoError(t, repo.Issue(ctx, grant(u.ID, phone, "old"), now))
	old, _ := repo.GetByHash(ctx, "old")

	const racers = 8
	results := make(chan bool, racers)
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ok, err := repo.Rotate(ctx, old.ID, grant(u.ID, phone, "next-"+string(rune('a'+i))), now)
			assert.NoError(t, err)
			results <- ok
		}(i)
	}
	wg.Wait()
	close(results)

	winners := 0
	for ok := range results {
		if ok {
			winners++
		}
	}
	assert.Equal(t, 1, winners)
	assert.Equal(t, 1, liveCount(t, u.ID), "one live token, however many raced")
}

func TestTokensRevokeAndRevokeAll(t *testing.T) {
	reset(t)
	ctx, repo := context.Background(), NewRefreshTokens(testPool)
	ana, bob := newUser(t, "ana@example.com"), newUser(t, "bob@example.com")
	require.NoError(t, repo.Issue(ctx, grant(ana.ID, phone, "ana-phone"), now))
	require.NoError(t, repo.Issue(ctx, grant(ana.ID, tablet, "ana-tablet"), now))
	require.NoError(t, repo.Issue(ctx, grant(bob.ID, phone, "bob-phone"), now))

	one, _ := repo.GetByHash(ctx, "ana-phone")
	require.NoError(t, repo.Revoke(ctx, one.ID, now))
	require.NoError(t, repo.Revoke(ctx, one.ID, now), "revoking twice is fine")
	require.NoError(t, repo.Revoke(ctx, uuid.New(), now), "revoking an unknown id is fine")
	assert.Equal(t, 1, liveCount(t, ana.ID))

	require.NoError(t, repo.RevokeAllForUser(ctx, ana.ID, now))
	assert.Equal(t, 0, liveCount(t, ana.ID))
	assert.Equal(t, 1, liveCount(t, bob.ID), "other users are untouched")
}

func TestTokensAreDeletedWithTheirUser(t *testing.T) {
	reset(t)
	ctx, repo := context.Background(), NewRefreshTokens(testPool)
	u := newUser(t, "ana@example.com")
	require.NoError(t, repo.Issue(ctx, grant(u.ID, phone, "gone"), now))

	_, err := testPool.Exec(ctx, "DELETE FROM users WHERE id=$1", u.ID)
	require.NoError(t, err)
	_, err = repo.GetByHash(ctx, "gone")
	assert.ErrorIs(t, err, service.ErrNotFound, "ON DELETE CASCADE")
}

func TestTokensErrorsPassThrough(t *testing.T) {
	reset(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	repo := NewRefreshTokens(testPool)
	u := uuid.New()
	assert.Error(t, repo.Issue(ctx, grant(u, phone, "x"), now))
	_, err := repo.GetByHash(ctx, "x")
	assert.Error(t, err)
	assert.NotErrorIs(t, err, service.ErrNotFound)
	_, err = repo.Rotate(ctx, uuid.New(), grant(u, phone, "y"), now)
	assert.Error(t, err)
}
