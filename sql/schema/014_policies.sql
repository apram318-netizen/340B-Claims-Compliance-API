-- +goose Up

CREATE TABLE policies (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    manufacturer_id UUID NOT NULL REFERENCES manufacturers(id),
    name            TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'active', -- active | archived
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A policy can have multiple versions over time; only one version is active
-- for a given date range.
CREATE TABLE policy_versions (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    policy_id      UUID NOT NULL REFERENCES policies(id),
    version_number INTEGER NOT NULL,
    effective_from DATE NOT NULL,
    effective_to   DATE,               -- NULL means currently active
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (policy_id, version_number)
);

-- Each rule is a typed JSONB config evaluated by the policy engine.
-- rule_type values: ndc_scope | payer_channel | date_window | pharmacy_limit
CREATE TABLE policy_rules (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    policy_version_id UUID NOT NULL REFERENCES policy_versions(id),
    rule_type         TEXT NOT NULL,
    rule_config       JSONB NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_policies_manufacturer ON policies(manufacturer_id);
CREATE INDEX idx_policy_versions_policy ON policy_versions(policy_id);
CREATE INDEX idx_policy_rules_version ON policy_rules(policy_version_id);

-- +goose Down
DROP INDEX IF EXISTS idx_policy_rules_version;
DROP INDEX IF EXISTS idx_policy_versions_policy;
DROP INDEX IF EXISTS idx_policies_manufacturer;
DROP TABLE IF EXISTS policy_rules;
DROP TABLE IF EXISTS policy_versions;
DROP TABLE IF EXISTS policies;
