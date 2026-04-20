-- +goose Up
-- HIPAA §164.312(b): retain audit logs for 6 years.
-- Use a trigger (not GENERATED) so all supported Postgres versions accept the
-- expression (some reject timestamptz + interval as non-immutable for GENERATED).
ALTER TABLE audit_events ADD COLUMN expires_at TIMESTAMPTZ;

UPDATE audit_events SET expires_at = created_at + INTERVAL '6 years' WHERE expires_at IS NULL;

CREATE OR REPLACE FUNCTION audit_events_set_expires_at()
RETURNS trigger AS $$
BEGIN
    NEW.expires_at := NEW.created_at + INTERVAL '6 years';
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_audit_events_expires_at
    BEFORE INSERT OR UPDATE OF created_at ON audit_events
    FOR EACH ROW
    EXECUTE PROCEDURE audit_events_set_expires_at();

CREATE INDEX idx_audit_events_expires_at ON audit_events (expires_at);

-- +goose Down
DROP TRIGGER IF EXISTS trg_audit_events_expires_at ON audit_events;
DROP FUNCTION IF EXISTS audit_events_set_expires_at();
DROP INDEX IF EXISTS idx_audit_events_expires_at;
ALTER TABLE audit_events DROP COLUMN IF EXISTS expires_at;
