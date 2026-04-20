-- +goose Up
-- Tracks formal manufacturer disputes raised by covered entities.
-- Status machine:
--   open → submitted → under_review → resolved_accepted | resolved_rejected
--   Any non-resolved state → withdrawn
CREATE TABLE manufacturer_disputes (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    claim_id        UUID NOT NULL REFERENCES claims(id) ON DELETE RESTRICT,
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    manufacturer_id UUID NOT NULL REFERENCES manufacturers(id) ON DELETE RESTRICT,
    status          TEXT NOT NULL DEFAULT 'open'
                        CHECK (status IN (
                            'open', 'submitted', 'under_review',
                            'resolved_accepted', 'resolved_rejected', 'withdrawn'
                        )),
    reason_code     TEXT NOT NULL
                        CHECK (reason_code IN (
                            'duplicate_discount', 'eligibility', 'pricing',
                            'incorrect_ndc', 'other'
                        )),
    description     TEXT NOT NULL,
    evidence_ref    TEXT,           -- storage reference (file path or S3 key)
    resolution      TEXT,
    opened_by       UUID NOT NULL REFERENCES users(id),
    resolved_by     UUID REFERENCES users(id),
    opened_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_disputes_claim_id        ON manufacturer_disputes (claim_id);
CREATE INDEX idx_disputes_org_id          ON manufacturer_disputes (org_id);
CREATE INDEX idx_disputes_manufacturer_id ON manufacturer_disputes (manufacturer_id);
CREATE INDEX idx_disputes_status          ON manufacturer_disputes (status);

-- +goose Down
DROP TABLE IF EXISTS manufacturer_disputes;
