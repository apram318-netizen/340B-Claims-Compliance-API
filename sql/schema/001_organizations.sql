-- +goose Up
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE organizations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL, 
    entity_id TEXT NOT NULL UNIQUE, 
    created_at TIMESTAMP NOT NULL DEFAULT now()
);


-- +goose Down
DROP TABLE IF EXISTS organizations;
