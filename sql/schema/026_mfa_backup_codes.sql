-- +goose Up
CREATE TABLE mfa_backup_codes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash TEXT NOT NULL,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, code_hash)
);

CREATE INDEX idx_mfa_backup_codes_user_id ON mfa_backup_codes (user_id) WHERE used_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS mfa_backup_codes;
