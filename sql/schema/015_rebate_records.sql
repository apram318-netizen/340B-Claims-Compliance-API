-- +goose Up

-- Manufacturer-side rebate data ingested from external sources (e.g. IntegriChain).
-- These are the records the reconciliation engine matches against claims.
CREATE TABLE rebate_records (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    manufacturer_id UUID NOT NULL REFERENCES manufacturers(id),
    org_id          UUID NOT NULL REFERENCES organizations(id),
    ndc             TEXT NOT NULL,
    pharmacy_npi    TEXT NOT NULL,
    service_date    DATE NOT NULL,
    quantity        INTEGER NOT NULL,
    hashed_rx_key   TEXT,
    payer_type      TEXT,
    rebate_amount   NUMERIC(12, 4),
    source          TEXT NOT NULL DEFAULT 'manual', -- manual | integrichain | api
    status          TEXT NOT NULL DEFAULT 'active',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_rebate_records_ndc_org ON rebate_records(ndc, org_id);
CREATE INDEX idx_rebate_records_service_date ON rebate_records(service_date);
CREATE INDEX idx_rebate_records_pharmacy ON rebate_records(pharmacy_npi);
CREATE INDEX idx_rebate_records_manufacturer ON rebate_records(manufacturer_id);

-- +goose Down
DROP INDEX IF EXISTS idx_rebate_records_manufacturer;
DROP INDEX IF EXISTS idx_rebate_records_pharmacy;
DROP INDEX IF EXISTS idx_rebate_records_service_date;
DROP INDEX IF EXISTS idx_rebate_records_ndc_org;
DROP TABLE IF EXISTS rebate_records;
