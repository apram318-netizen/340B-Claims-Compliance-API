-- +goose Up 

CREATE TABLE audit_events(
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(), 
    event_type TEXT NOT NULL, 
    entity_type TEXT NOT NULL, 
    entity_id TEXT NOT NULL, 
    actor_id    UUID,
    payload JSONB, 
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
); 

CREATE INDEX idx_audit_events_entity ON audit_events(entity_type,entity_id); 
CREATE INDEX idx_audit_events_type ON audit_events(event_type);

-- +goose Down

DROP INDEX IF EXISTS idx_audit_events_type;
DROP INDEX IF EXISTS idx_audit_events_entity;
DROP TABLE IF EXISTS audit_events;