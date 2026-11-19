-- name: InsertRefreshToken :one
INSERT INTO refresh_token (user_id, device_id, token_hash, lifetime_seconds, expires_at, created_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetRefreshTokenByHash :one
SELECT * FROM refresh_token WHERE token_hash = $1;

-- name: GetRefreshTokenByID :one
SELECT * FROM refresh_token WHERE id = $1;

-- name: RevokeActiveDeviceTokens :exec
UPDATE refresh_token SET revoked_at = $3
WHERE user_id = $1 AND device_id = $2 AND revoked_at IS NULL;

-- name: RevokeTokenIfActive :execrows
UPDATE refresh_token SET revoked_at = $2 WHERE id = $1 AND revoked_at IS NULL;

-- name: SetTokenReplacedBy :exec
UPDATE refresh_token SET replaced_by = $2 WHERE id = $1;

-- name: RevokeAllUserTokens :exec
UPDATE refresh_token SET revoked_at = $2 WHERE user_id = $1 AND revoked_at IS NULL;
