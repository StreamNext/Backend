-- name: DeleteDevicesWithTokenExcept :exec
DELETE FROM device
WHERE fcm_token = $1 AND NOT (user_id = $2 AND device_id = $3);

-- name: UpsertDevice :exec
INSERT INTO device (user_id, device_id, fcm_token, platform, created_at, updated_at)
VALUES ($1, $2, $3, $4, sqlc.arg('now')::timestamptz, sqlc.arg('now')::timestamptz)
ON CONFLICT (user_id, device_id)
DO UPDATE SET fcm_token = EXCLUDED.fcm_token, platform = EXCLUDED.platform, updated_at = EXCLUDED.updated_at;

-- name: DeleteDevice :exec
DELETE FROM device WHERE user_id = $1 AND device_id = $2;
