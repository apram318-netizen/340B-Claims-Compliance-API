-- name: CreateOrganization :one 
INSERT INTO organizations (name ,entity_id)
VALUES ($1,$2)
RETURNING *;

-- name: GetOrganization :one 
SELECT * FROM organizations
WHERE id = $1;