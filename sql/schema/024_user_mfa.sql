-- +goose Up
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS mfa_enabled BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS mfa_secret TEXT;

-- +goose Down
ALTER TABLE users
    DROP COLUMN IF EXISTS mfa_secret,
    DROP COLUMN IF EXISTS mfa_enabled;
