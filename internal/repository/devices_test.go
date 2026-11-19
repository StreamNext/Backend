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

type deviceRow struct {
	UserID, DeviceID uuid.UUID
	Token, Platform  string
}

func deviceRows(t *testing.T) []deviceRow {
	t.Helper()
	rows, err := testPool.Query(context.Background(), "SELECT user_id, device_id, fcm_token, platform FROM device ORDER BY fcm_token")
	require.NoError(t, err)
	defer rows.Close()
	var out []deviceRow
	for rows.Next() {
		var r deviceRow
		require.NoError(t, rows.Scan(&r.UserID, &r.DeviceID, &r.Token, &r.Platform))
		out = append(out, r)
	}
	return out
}

func dev(userID, deviceID uuid.UUID, token string) service.Device {
	return service.Device{UserID: userID, DeviceID: deviceID, FCMToken: token, Platform: service.PlatformIOS}
}

func TestDevicesUpsertInsertsThenUpdatesInPlace(t *testing.T) {
	reset(t)
	ctx, repo := context.Background(), NewDevices(testPool)
	u := newUser(t, "ana@example.com")

	require.NoError(t, repo.Upsert(ctx, dev(u.ID, phone, "token-1"), now))
	assert.Equal(t, []deviceRow{{u.ID, phone, "token-1", "ios"}}, deviceRows(t))

	changed := service.Device{UserID: u.ID, DeviceID: phone, FCMToken: "token-2", Platform: service.PlatformAndroid}
	require.NoError(t, repo.Upsert(ctx, changed, now.Add(1)))
	assert.Equal(t, []deviceRow{{u.ID, phone, "token-2", "android"}}, deviceRows(t), "same device: the row is updated, not duplicated")

	require.NoError(t, repo.Upsert(ctx, changed, now.Add(2)), "registering the same thing again is fine")
	assert.Len(t, deviceRows(t), 1)
}

func TestDevicesOneUserManyDevices(t *testing.T) {
	reset(t)
	ctx, repo := context.Background(), NewDevices(testPool)
	u := newUser(t, "ana@example.com")
	require.NoError(t, repo.Upsert(ctx, dev(u.ID, phone, "phone-token"), now))
	require.NoError(t, repo.Upsert(ctx, dev(u.ID, tablet, "tablet-token"), now))
	assert.Len(t, deviceRows(t), 2)
}

func TestDevicesATokenMovesToWhoeverRegistersItLast(t *testing.T) {
	reset(t)
	ctx, repo := context.Background(), NewDevices(testPool)
	ana, bob := newUser(t, "ana@example.com"), newUser(t, "bob@example.com")
	require.NoError(t, repo.Upsert(ctx, dev(ana.ID, phone, "shared"), now))
	require.NoError(t, repo.Upsert(ctx, dev(ana.ID, tablet, "tablets-own"), now))

	// Bob signs in on Ana's phone: same device id, same FCM token.
	require.NoError(t, repo.Upsert(ctx, dev(bob.ID, phone, "shared"), now))

	assert.Equal(t, []deviceRow{{bob.ID, phone, "shared", "ios"}, {ana.ID, tablet, "tablets-own", "ios"}}, deviceRows(t),
		"Ana keeps her tablet, but the phone's token now belongs to Bob only")
}

func TestDevicesATokenMovesBetweenDevicesOfTheSameUser(t *testing.T) {
	reset(t)
	ctx, repo := context.Background(), NewDevices(testPool)
	u := newUser(t, "ana@example.com")
	require.NoError(t, repo.Upsert(ctx, dev(u.ID, phone, "tok"), now))
	require.NoError(t, repo.Upsert(ctx, dev(u.ID, tablet, "tok"), now))
	assert.Equal(t, []deviceRow{{u.ID, tablet, "tok", "ios"}}, deviceRows(t))
}

func TestDevicesConcurrentRegistrationOfOneTokenLeavesOneOwner(t *testing.T) {
	reset(t)
	ctx, repo := context.Background(), NewDevices(testPool)
	ana, bob := newUser(t, "ana@example.com"), newUser(t, "bob@example.com")

	for round := 0; round < 10; round++ {
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		for _, u := range []service.User{ana, bob} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs <- repo.Upsert(ctx, dev(u.ID, phone, "contested"), now)
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err, "round %d: the loser retries instead of failing", round)
		}
		assert.Len(t, deviceRows(t), 1, "round %d: exactly one owner", round)
	}
}

func TestDevicesDeleteOnlyTouchesTheCallersRow(t *testing.T) {
	reset(t)
	ctx, repo := context.Background(), NewDevices(testPool)
	ana, bob := newUser(t, "ana@example.com"), newUser(t, "bob@example.com")
	require.NoError(t, repo.Upsert(ctx, dev(ana.ID, phone, "a"), now))
	require.NoError(t, repo.Upsert(ctx, dev(bob.ID, phone, "b"), now))

	require.NoError(t, repo.Delete(ctx, ana.ID, phone))
	assert.Equal(t, []deviceRow{{bob.ID, phone, "b", "ios"}}, deviceRows(t))
	require.NoError(t, repo.Delete(ctx, ana.ID, phone), "again")
	require.NoError(t, repo.Delete(ctx, ana.ID, uuid.New()), "never registered")
}

func TestDevicesUnknownUserIsNotFound(t *testing.T) {
	reset(t)
	err := NewDevices(testPool).Upsert(context.Background(), dev(uuid.New(), phone, "tok"), now)
	assert.ErrorIs(t, err, service.ErrNotFound)
}

func TestDevicesDatabaseRejectsAnUnknownPlatform(t *testing.T) {
	reset(t)
	u := newUser(t, "ana@example.com")
	err := NewDevices(testPool).Upsert(context.Background(), service.Device{UserID: u.ID, DeviceID: phone, FCMToken: "t", Platform: "windows"}, now)
	assert.Error(t, err, "the CHECK constraint")
	assert.NotErrorIs(t, err, service.ErrNotFound)
}

func TestDevicesAreDeletedWithTheirUser(t *testing.T) {
	reset(t)
	ctx := context.Background()
	u := newUser(t, "ana@example.com")
	require.NoError(t, NewDevices(testPool).Upsert(ctx, dev(u.ID, phone, "tok"), now))
	require.NoError(t, NewUsers(testPool).Delete(ctx, u.ID))
	assert.Empty(t, deviceRows(t), "ON DELETE CASCADE")
}

func TestDevicesErrorsPassThrough(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	repo := NewDevices(testPool)
	assert.Error(t, repo.Upsert(ctx, dev(uuid.New(), phone, "t"), now))
	assert.Error(t, repo.Delete(ctx, uuid.New(), phone))
}
