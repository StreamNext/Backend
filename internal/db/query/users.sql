-- name: CreateUser :one
INSERT INTO users (email, password_hash, email_verified_at, display_name, timezone)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: UpdateUserProfile :one
UPDATE users
SET display_name = COALESCE(sqlc.narg('display_name'), display_name),
    timezone     = COALESCE(sqlc.narg('timezone'), timezone),
    updated_at   = sqlc.arg('now')
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: SetUserPassword :execrows
UPDATE users SET password_hash = sqlc.arg('password_hash')::text, updated_at = sqlc.arg('now')::timestamptz
WHERE id = $1;

-- name: MarkUserEmailVerified :execrows
UPDATE users SET email_verified_at = COALESCE(email_verified_at, sqlc.arg('now')::timestamptz), updated_at = sqlc.arg('now')::timestamptz
WHERE id = $1;

-- name: DeleteUser :execrows
DELETE FROM users WHERE id = $1;

-- name: ClearUserPassword :execrows
UPDATE users SET password_hash = NULL, updated_at = sqlc.arg('now')::timestamptz
WHERE id = $1;
