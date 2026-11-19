package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const maxFCMTokenLen = 4096

// Devices keeps the push (FCM) tokens of a user's devices.
type Devices struct {
	repo DeviceRepository
	now  func() time.Time
}

func NewDevices(repo DeviceRepository, now func() time.Time) *Devices {
	return &Devices{repo: repo, now: now}
}

// Register stores the token for the user's device. If the same token was registered by someone else
// (another person signed in on that phone), it moves to this user.
func (d *Devices) Register(ctx context.Context, userID, deviceID uuid.UUID, fcmToken string, platform Platform) error {
	token := strings.TrimSpace(fcmToken)
	switch {
	case deviceID == uuid.Nil:
		return invalid("deviceId is required")
	case token == "" || utf8.RuneCountInString(token) > maxFCMTokenLen:
		return invalid("fcmToken must be 1 to 4096 characters")
	case platform != PlatformIOS && platform != PlatformAndroid:
		return invalid("platform must be ios or android")
	}
	err := d.repo.Upsert(ctx, Device{UserID: userID, DeviceID: deviceID, FCMToken: token, Platform: platform}, d.now())
	if errors.Is(err, ErrNotFound) { // a valid access token for a user that has been deleted
		return errInvalidCredentials
	}
	if err != nil {
		return fmt.Errorf("register device: %w", err)
	}
	return nil
}

// Remove forgets the device's token. Removing a device that was never registered is fine.
func (d *Devices) Remove(ctx context.Context, userID, deviceID uuid.UUID) error {
	if err := d.repo.Delete(ctx, userID, deviceID); err != nil {
		return fmt.Errorf("remove device: %w", err)
	}
	return nil
}
