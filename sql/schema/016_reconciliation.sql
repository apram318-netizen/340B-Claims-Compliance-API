-- +goose Up

-- One job per batch; tracks aggregate reconciliation progress.
CREATE TABLE reconciliation_jobs (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    batch_id             UUID NOT NULL REFERENCES upload_batches(id) UNIQUE,
    status               TEXT NOT NULL DEFAULT 'pending', -- pending | running | completed | failed
    total_claims         INTEGER NOT NULL DEFAULT 0,
    matched_count        INTEGER NOT NULL DEFAULT 0,
    unmatched_count      INTEGER NOT NULL DEFAULT 0,
    duplicate_risk_count INTEGER NOT NULL DEFAULT 0,
    excluded_count       INTEGER NOT NULL DEFAULT 0,
    error_count          INTEGER NOT NULL DEFAULT 0,
    started_at           TIMESTAMPTZ,
    completed_at         TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Intermediate: every rebate record that was considered a candidate for a claim.
-- Retained for auditability even when ultimately not chosen.
CREATE TABLE candidate_matches (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id           UUID NOT NULL REFERENCES reconciliation_jobs(id),
    claim_id         UUID NOT NULL REFERENCES claims(id),
    rebate_record_id UUID NOT NULL REFERENCES rebate_records(id),
    score            INTEGER NOT NULL DEFAULT 0, -- higher = stronger match
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Final decision for each claim.  The reasoning column stores the full
-- rule-by-rule evaluation so decisions are reproducible and defensible.
CREATE TABLE match_decisions (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id            UUID NOT NULL REFERENCES reconciliation_jobs(id),
    claim_id          UUID NOT NULL REFERENCES claims(id) UNIQUE,
    rebate_record_id  UUID REFERENCES rebate_records(id), -- NULL when unmatched
    policy_version_id UUID REFERENCES policy_versions(id),
    status            TEXT NOT NULL,   -- matched | probable_match | unmatched |
                                       -- duplicate_discount_risk | invalid |
                                       -- excluded_by_policy | pending_external_data
    reasoning         JSONB NOT NULL,  -- full rule-by-rule trace
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_reconciliation_jobs_batch ON reconciliation_jobs(batch_id);
CREATE INDEX idx_candidate_matches_claim ON candidate_matches(claim_id);
CREATE INDEX idx_candidate_matches_job ON candidate_matches(job_id);
CREATE INDEX idx_match_decisions_job ON match_decisions(job_id);
CREATE INDEX idx_match_decisions_status ON match_decisions(status);

-- +goose Down
DROP INDEX IF EXISTS idx_match_decisions_status;
DROP INDEX IF EXISTS idx_match_decisions_job;
DROP INDEX IF EXISTS idx_candidate_matches_job;
DROP INDEX IF EXISTS idx_candidate_matches_claim;
DROP INDEX IF EXISTS idx_reconciliation_jobs_batch;
DROP TABLE IF EXISTS match_decisions;
DROP TABLE IF EXISTS candidate_matches;
DROP TABLE IF EXISTS reconciliation_jobs;
