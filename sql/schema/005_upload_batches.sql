-- +goose Up

CREATE TABLE upload_batches (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        UUID NOT NULL REFERENCES organizations(id),
    uploaded_by   UUID NOT NULL REFERENCES users(id),
    file_name     TEXT NOT NULL,
    row_count     INTEGER NOT NULL DEFAULT 0,
    status        TEXT NOT NULL DEFAULT 'uploaded',
    error_message TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down

DROP TABLE IF EXISTS upload_batches;