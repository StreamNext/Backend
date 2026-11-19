-- name: CreateUser :one
INSERT INTO users (email, password_hash, display_name, timezone)
VALUES ($1, $2, $3, $4)
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
