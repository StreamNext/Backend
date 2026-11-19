package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/StreamNext/Backend/internal/service"
	"github.com/StreamNext/Backend/internal/sqlc"
)

// upsertAttempts bounds the retries when two people register the same token at the same instant.
const upsertAttempts = 3

type Devices struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
}

func NewDevices(pool *pgxpool.Pool) *Devices { return &Devices{pool: pool, q: sqlc.New(pool)} }

// Upsert stores the device's token and removes that token from any other owner, in one transaction.
// The FCM token is unique, so a concurrent registration of the same token can still lose the race at
// the insert; that is retried, and by then the winner's row is there to be moved.
func (r *Devices) Upsert(ctx context.Context, d service.Device, now time.Time) error {
	var err error
	for attempt := 0; attempt < upsertAttempts; attempt++ {
		err = inTx(ctx, r.pool, r.q, func(q *sqlc.Queries) error {
			if err := q.DeleteDevicesWithTokenExcept(ctx, sqlc.DeleteDevicesWithTokenExceptParams{FcmToken: d.FCMToken, UserID: d.UserID, DeviceID: d.DeviceID}); err != nil {
				return err
			}
			return q.UpsertDevice(ctx, sqlc.UpsertDeviceParams{
				UserID: d.UserID, DeviceID: d.DeviceID, FcmToken: d.FCMToken, Platform: string(d.Platform), Now: now,
			})
		})
		if !isUniqueViolation(err, "device_fcm_token_key") {
			break
		}
	}
	if isForeignKeyViolation(err) { // the user was deleted
		return service.ErrNotFound
	}
	return err
}

func (r *Devices) Delete(ctx context.Context, userID, deviceID uuid.UUID) error {
	return r.q.DeleteDevice(ctx, sqlc.DeleteDeviceParams{UserID: userID, DeviceID: deviceID})
}
