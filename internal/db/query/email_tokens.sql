-- name: InvalidateUnusedEmailTokens :exec
UPDATE email_token SET consumed_at = sqlc.arg('now')::timestamptz
WHERE user_id = $1 AND purpose = $2 AND consumed_at IS NULL;

-- name: InsertEmailToken :exec
INSERT INTO email_token (user_id, purpose, token_hash, expires_at, created_at)
VALUES ($1, $2, $3, $4, $5);

-- name: LastEmailTokenCreatedAt :one
SELECT created_at FROM email_token
WHERE user_id = $1 AND purpose = $2
ORDER BY created_at DESC LIMIT 1;

-- name: ConsumeEmailToken :one
UPDATE email_token SET consumed_at = sqlc.arg('now')::timestamptz
WHERE token_hash = $1 AND purpose = $2 AND consumed_at IS NULL AND expires_at > sqlc.arg('now')::timestamptz
RETURNING user_id;
