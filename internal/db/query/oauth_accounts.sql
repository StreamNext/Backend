-- name: GetOAuthUserID :one
SELECT user_id FROM oauth_account WHERE provider = $1 AND subject = $2;

-- name: InsertOAuthAccount :exec
INSERT INTO oauth_account (user_id, provider, subject, email, created_at)
VALUES ($1, $2, $3, $4, $5);
