-- +goose Up

-- Fix: effective_date was TEXT, should be DATE
ALTER TABLE designations
  ALTER COLUMN effective_date TYPE DATE
  USING effective_date::DATE;

-- Fix: remove the insecure empty-string default from password_hash
ALTER TABLE designations
  ALTER COLUMN effective_date DROP DEFAULT;

ALTER TABLE users
  ALTER COLUMN password_hash DROP DEFAULT;

-- Add missing claim fields needed for the reconciliation engine (Phase 2)
ALTER TABLE claims
  ADD COLUMN IF NOT EXISTS adjudication_date   DATE,
  ADD COLUMN IF NOT EXISTS fill_date           DATE,
  ADD COLUMN IF NOT EXISTS reconciliation_status TEXT NOT NULL DEFAULT 'pending',
  ADD COLUMN IF NOT EXISTS updated_at          TIMESTAMPTZ NOT NULL DEFAULT now();

-- Composite indexes for reconciliation queries (ndc + org, ndc + date window)
CREATE INDEX IF NOT EXISTS idx_claims_org_ndc
  ON claims(org_id, ndc);

CREATE INDEX IF NOT EXISTS idx_claims_ndc_service_date
  ON claims(ndc, service_date);

CREATE INDEX IF NOT EXISTS idx_claims_reconciliation_status
  ON claims(reconciliation_status)
  WHERE reconciliation_status = 'pending';

-- Index for time-range queries on audit log
CREATE INDEX IF NOT EXISTS idx_audit_events_created_at
  ON audit_events(created_at DESC);

-- +goose Down

DROP INDEX IF EXISTS idx_audit_events_created_at;
DROP INDEX IF EXISTS idx_claims_reconciliation_status;
DROP INDEX IF EXISTS idx_claims_ndc_service_date;
DROP INDEX IF EXISTS idx_claims_org_ndc;

ALTER TABLE claims
  DROP COLUMN IF EXISTS updated_at,
  DROP COLUMN IF EXISTS reconciliation_status,
  DROP COLUMN IF EXISTS fill_date,
  DROP COLUMN IF EXISTS adjudication_date;

ALTER TABLE users
  ALTER COLUMN password_hash SET DEFAULT '';

ALTER TABLE designations
  ALTER COLUMN effective_date SET DEFAULT CURRENT_DATE,
  ALTER COLUMN effective_date TYPE TEXT USING effective_date::TEXT;
