-- +goose Up
CREATE TABLE email_token (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    purpose     text        NOT NULL CHECK (purpose IN ('verify', 'reset')),
    token_hash  text        NOT NULL UNIQUE,   -- SHA-256 hex; the token itself only ever exists in the email
    expires_at  timestamptz NOT NULL,
    created_at  timestamptz NOT NULL,
    consumed_at timestamptz                    -- set when used, or when a newer token of the same purpose supersedes it
);

-- Serves the per-address cooldown (latest token per user and purpose).
CREATE INDEX email_token_user_purpose ON email_token (user_id, purpose, created_at DESC);

-- +goose Down
DROP TABLE email_token;
