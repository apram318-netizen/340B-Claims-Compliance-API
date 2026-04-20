-- +goose Up 

CREATE TABLE designations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(), 
    org_id UUID NOT NULL REFERENCES organizations(id), 
    pharmacy_id UUID NOT NULL REFERENCES pharmacies(id), 
    manufacturer_id TEXT NOT NULL, 
    effective_date TEXT NOT NULL, 
    end_date DATE, 
    status TEXT NOT NULL DEFAULT 'active', 
    created_by UUID REFERENCES users(id),
    created_at TIMESTAMP NOT NULL DEFAULT now()
);

-- +goose Down 
DROP TABLE IF EXISTS designations; 