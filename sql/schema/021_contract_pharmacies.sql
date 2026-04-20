-- +goose Up
-- Tracks contract pharmacies linked to a 340B covered entity (org).
CREATE TABLE contract_pharmacies (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    pharmacy_name   TEXT NOT NULL,
    pharmacy_npi    TEXT NOT NULL,
    dea_number      TEXT,
    address         TEXT,
    city            TEXT,
    state           CHAR(2),
    zip             TEXT,
    status          TEXT NOT NULL DEFAULT 'active'
                        CHECK (status IN ('active', 'inactive', 'suspended')),
    effective_from  DATE NOT NULL,
    effective_to    DATE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (org_id, pharmacy_npi)
);

CREATE INDEX idx_contract_pharmacies_org_id ON contract_pharmacies (org_id);
CREATE INDEX idx_contract_pharmacies_npi    ON contract_pharmacies (pharmacy_npi);

-- Tracks which manufacturers have authorised a given contract pharmacy.
-- A manufacturer can revoke access by setting authorized = false rather than
-- deleting the row, preserving the audit trail.
CREATE TABLE manufacturer_contract_pharmacy_auths (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    manufacturer_id      UUID NOT NULL REFERENCES manufacturers(id) ON DELETE CASCADE,
    contract_pharmacy_id UUID NOT NULL REFERENCES contract_pharmacies(id) ON DELETE CASCADE,
    authorized           BOOLEAN NOT NULL DEFAULT TRUE,
    effective_from       DATE NOT NULL,
    effective_to         DATE,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (manufacturer_id, contract_pharmacy_id)
);

CREATE INDEX idx_mcp_auths_manufacturer ON manufacturer_contract_pharmacy_auths (manufacturer_id);
CREATE INDEX idx_mcp_auths_pharmacy     ON manufacturer_contract_pharmacy_auths (contract_pharmacy_id);

-- +goose Down
DROP TABLE IF EXISTS manufacturer_contract_pharmacy_auths;
DROP TABLE IF EXISTS contract_pharmacies;
