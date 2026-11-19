-- +goose Up
-- citext gives case-insensitive unique emails (users.email, M1).
CREATE EXTENSION IF NOT EXISTS citext;

-- +goose Down
DROP EXTENSION IF EXISTS citext;
