-- +goose Up
CREATE TABLE users (
    id                uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    email             citext      NOT NULL UNIQUE,
    password_hash     text,                       -- null for social-only accounts (slice 3)
    email_verified_at timestamptz,
    display_name      text        NOT NULL,
    timezone          text        NOT NULL DEFAULT 'UTC',
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE refresh_token (
    id               uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    device_id        uuid        NOT NULL,
    token_hash       text        NOT NULL UNIQUE,  -- SHA-256 hex of the opaque token; the token itself is never stored
    lifetime_seconds integer     NOT NULL,         -- carried forward on rotation so the window slides
    expires_at       timestamptz NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    revoked_at       timestamptz,
    replaced_by      uuid        REFERENCES refresh_token (id) ON DELETE SET NULL
);

-- One live token per user+device; rotation must revoke the old row before inserting the new one.
CREATE UNIQUE INDEX refresh_token_one_active_per_device
    ON refresh_token (user_id, device_id) WHERE revoked_at IS NULL;
CREATE INDEX refresh_token_user_id ON refresh_token (user_id);

-- +goose Down
DROP TABLE refresh_token;
DROP TABLE users;
