-- +goose Up
-- Single-use password reset tokens.
-- Only the SHA-256 hash of the token is stored; the raw token is sent to the
-- user (out of band — email in production) and never persisted.
CREATE TABLE password_reset_tokens (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL UNIQUE,   -- SHA-256(raw_token) hex-encoded
    expires_at  TIMESTAMPTZ NOT NULL,
    used_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_reset_tokens_user_id    ON password_reset_tokens (user_id);
CREATE INDEX idx_reset_tokens_expires_at ON password_reset_tokens (expires_at);

-- +goose Down
DROP TABLE IF EXISTS password_reset_tokens;
