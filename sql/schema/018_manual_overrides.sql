-- +goose Up

-- Append-only record of every manual decision override.
-- The reasoning column records what the admin stated; the full audit
-- trail is completed by the audit_events entry written alongside it.
CREATE TABLE manual_override_events (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    claim_id         UUID NOT NULL REFERENCES claims(id),
    previous_status  TEXT NOT NULL,
    new_status       TEXT NOT NULL,
    reason           TEXT NOT NULL,
    rebate_record_id UUID REFERENCES rebate_records(id), -- may be set when overriding to matched
    overridden_by    UUID NOT NULL REFERENCES users(id),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_manual_overrides_claim ON manual_override_events(claim_id);

-- Allow the match_decisions status to be updated by an override.
-- (The original status is preserved in manual_override_events.)
ALTER TABLE match_decisions ADD COLUMN IF NOT EXISTS override_id UUID REFERENCES manual_override_events(id);

-- +goose Down
ALTER TABLE match_decisions DROP COLUMN IF EXISTS override_id;
DROP INDEX IF EXISTS idx_manual_overrides_claim;
DROP TABLE IF EXISTS manual_override_events;
