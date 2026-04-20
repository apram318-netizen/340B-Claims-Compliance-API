-- +goose Up

CREATE TABLE export_runs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID REFERENCES organizations(id),          -- NULL = platform-wide report
    manufacturer_id UUID REFERENCES manufacturers(id),          -- NULL = all manufacturers
    report_type     TEXT NOT NULL,   -- manufacturer_compliance | duplicate_findings |
                                     -- submission_completeness | exceptions
    status          TEXT NOT NULL DEFAULT 'pending',            -- pending | running | completed | failed
    file_path       TEXT,            -- relative path under EXPORT_DIR once written
    row_count       INTEGER,
    requested_by    UUID REFERENCES users(id),
    params          JSONB NOT NULL DEFAULT '{}', -- {from_date, to_date, manufacturer_id, org_id}
    error_message   TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at    TIMESTAMPTZ
);

CREATE INDEX idx_export_runs_org ON export_runs(org_id) WHERE org_id IS NOT NULL;
CREATE INDEX idx_export_runs_status ON export_runs(status);

-- +goose Down
DROP INDEX IF EXISTS idx_export_runs_status;
DROP INDEX IF EXISTS idx_export_runs_org;
DROP TABLE IF EXISTS export_runs;
