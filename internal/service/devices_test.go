package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/StreamNext/Backend/internal/apperr"
)

func newDevices() (*Devices, *fakeDevices) {
	f := newFakeDevices()
	return NewDevices(f, (&clock{t: epoch}).Now), f
}

var (
	ana = uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	bob = uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
)

func TestRegisterDevice(t *testing.T) {
	d, f := newDevices()
	require.NoError(t, d.Register(context.Background(), ana, device, "  fcm-token-1  ", PlatformIOS))
	assert.Equal(t, []Device{{UserID: ana, DeviceID: device, FCMToken: "fcm-token-1", Platform: PlatformIOS}}, f.rows, "the token is trimmed")
}

func TestRegisteringAgainUpdatesTheTokenInPlace(t *testing.T) {
	d, f := newDevices()
	require.NoError(t, d.Register(context.Background(), ana, device, "old", PlatformAndroid))
	require.NoError(t, d.Register(context.Background(), ana, device, "new", PlatformAndroid))
	assert.Equal(t, []Device{{UserID: ana, DeviceID: device, FCMToken: "new", Platform: PlatformAndroid}}, f.rows)
}

func TestAnotherUsersLoginOnTheSamePhoneTakesTheTokenOver(t *testing.T) {
	d, f := newDevices()
	require.NoError(t, d.Register(context.Background(), ana, device, "shared-phone-token", PlatformIOS))
	require.NoError(t, d.Register(context.Background(), bob, device, "shared-phone-token", PlatformIOS))

	require.Len(t, f.rows, 1, "one token, one owner")
	assert.Equal(t, bob, f.rows[0].UserID, "so Ana's notifications can no longer reach Bob's phone")
}

func TestOneUserCanHaveSeveralDevices(t *testing.T) {
	d, f := newDevices()
	require.NoError(t, d.Register(context.Background(), ana, device, "phone", PlatformIOS))
	require.NoError(t, d.Register(context.Background(), ana, otherDevice, "tablet", PlatformAndroid))
	assert.Len(t, f.rows, 2)
}

func TestRegisterDeviceValidation(t *testing.T) {
	d, f := newDevices()
	cases := map[string]struct {
		device   uuid.UUID
		token    string
		platform Platform
	}{
		"nil device":       {uuid.Nil, "t", PlatformIOS},
		"empty token":      {device, "", PlatformIOS},
		"blank token":      {device, "   ", PlatformIOS},
		"token too long":   {device, strings.Repeat("x", 4097), PlatformIOS},
		"unknown platform": {device, "t", "windows"},
		"empty platform":   {device, "t", ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			requireKind(t, d.Register(context.Background(), ana, c.device, c.token, c.platform), apperr.InvalidArgument)
		})
	}
	assert.Empty(t, f.rows)

	require.NoError(t, d.Register(context.Background(), ana, device, strings.Repeat("x", 4096), PlatformIOS), "4096 is allowed")
}

func TestRegisterDeviceErrors(t *testing.T) {
	d, f := newDevices()
	f.gone = true
	requireKind(t, d.Register(context.Background(), ana, device, "t", PlatformIOS), apperr.Unauthenticated, "the user no longer exists")

	f.gone = false
	f.fail["Upsert"] = errors.New("db down")
	requireKind(t, d.Register(context.Background(), ana, device, "t", PlatformIOS), apperr.Internal)
}

func TestRemoveDevice(t *testing.T) {
	d, f := newDevices()
	require.NoError(t, d.Register(context.Background(), ana, device, "t1", PlatformIOS))
	require.NoError(t, d.Register(context.Background(), bob, device, "t2", PlatformIOS))

	require.NoError(t, d.Remove(context.Background(), ana, device))
	require.Len(t, f.rows, 1)
	assert.Equal(t, bob, f.rows[0].UserID, "only the caller's row goes")

	assert.NoError(t, d.Remove(context.Background(), ana, device), "removing again is fine")
	assert.NoError(t, d.Remove(context.Background(), ana, uuid.New()), "so is removing one that never existed")

	f.fail["Delete"] = errors.New("db down")
	requireKind(t, d.Remove(context.Background(), ana, device), apperr.Internal)
}
