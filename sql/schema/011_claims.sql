-- +goose Up 

CREATE TABLE claims (
    id        UUID   PRIMARY KEY DEFAULT gen_random_uuid(), 
    batch_id  UUID   NOT NULL REFERENCES upload_batches(id), 
    org_id UUID NOT NULL REFERENCES organizations(id),
    row_id UUID NOT NULL REFERENCES upload_rows(id), 
    ndc TEXT NOT NULL, 
    pharmacy_npi TEXT NOT NULL, 
    service_date DATE NOT NULL, 
    quantity INTEGER NOT NULL, 
    hashed_rx_key TEXT, 
    payer_type TEXT, 
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
); 

CREATE INDEX idx_claims_batch_id ON claims(batch_id); 
CREATE INDEX idx_claims_org_id ON claims(org_id); 
CREATE INDEX idx_claims_ndc ON claims(ndc);


-- +goose Down 

DROP INDEX idx_claims_batch_id; 
DROP INDEX idx_claims_ndc; 
DROP INDEX idx_claims_org_id;
DROP TABLE IF EXISTS claims; 
