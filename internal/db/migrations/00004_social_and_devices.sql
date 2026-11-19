-- +goose Up
CREATE TABLE oauth_account (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    provider   text        NOT NULL CHECK (provider IN ('apple', 'google')),
    subject    text        NOT NULL,           -- the provider's stable id for the person
    email      text,                           -- what the provider reported when the identity was linked (informational)
    created_at timestamptz NOT NULL,
    UNIQUE (provider, subject)
);
CREATE INDEX oauth_account_user_id ON oauth_account (user_id);

CREATE TABLE device (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    device_id  uuid        NOT NULL,           -- the client-generated id, same as refresh_token.device_id
    fcm_token  text        NOT NULL UNIQUE,    -- one token, one owner: registering it again moves it
    platform   text        NOT NULL CHECK (platform IN ('ios', 'android')),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (user_id, device_id)
);

-- +goose Down
DROP TABLE device;
DROP TABLE oauth_account;
