-- +goose Up 
CREATE TABLE pharmacies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(), 
    name TEXT NOT NULL, 
    npi TEXT NOT NULL UNIQUE,
    address TEXT, 
    created_at TIMESTAMP NOT NULL DEFAULT now()
);


-- +goose Down 
DROP TABLE IF EXISTS pharmacies;