-- +goose Up 

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE users (
    id     UUID PRIMARY KEY DEFAULT gen_random_uuid(), 
    org_id UUID NOT NULL REFERENCES organizations(id),
    email  TEXT NOT NULL  UNIQUE,
    name   TEXT NOT NULL ,
    role   TEXT NOT NULL DEFAULT 'viewer', 
    created_at TIMESTAMP NOT NULL DEFAULT now()
);


-- +goose Down
DROP TABLE IF EXISTS users;

