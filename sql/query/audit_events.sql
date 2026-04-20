-- name: CreateAuditEvent :one
INSERT INTO audit_events( event_type, entity_type, entity_id, actor_id, payload)
VALUES ($1,$2,$3,$4,$5)
RETURNING *; 

-- name: GetAuditEventsByEntity :many
SELECT * FROM audit_events 
WHERE entity_type = $1 AND entity_id = $2 
ORDER BY created_at; 

-- name: GetAuditEventsByEntityPaginated :many
SELECT * FROM audit_events
WHERE entity_type = $1 AND entity_id = $2
ORDER BY created_at
LIMIT $3 OFFSET $4;

-- name: CountAuditEventsByEntity :one
SELECT COUNT(*) FROM audit_events
WHERE entity_type = $1 AND entity_id = $2;