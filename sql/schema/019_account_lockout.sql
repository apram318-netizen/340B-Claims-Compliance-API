-- +goose Up
ALTER TABLE users
    ADD COLUMN failed_login_attempts INT NOT NULL DEFAULT 0,
    ADD COLUMN locked_until          TIMESTAMPTZ;

-- +goose Down
ALTER TABLE users
    DROP COLUMN failed_login_attempts,
    DROP COLUMN locked_until;
