-- name: CreateClaim :one
INSERT INTO claims (batch_id, row_id, org_id, ndc, pharmacy_npi, service_date, quantity, hashed_rx_key, payer_type)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetClaimsByBatch :many
SELECT * FROM claims
WHERE batch_id = $1
ORDER BY created_at;

-- name: GetClaimsByBatchPaginated :many
SELECT * FROM claims
WHERE batch_id = $1
ORDER BY created_at
LIMIT $2 OFFSET $3;

-- name: CountClaimsByBatch :one
SELECT COUNT(*) FROM claims
WHERE batch_id = $1;

-- name: GetClaimByID :one
SELECT * FROM claims WHERE id = $1;

-- name: ListClaimsForRxRehash :many
SELECT c.id, c.hashed_rx_key, ur.data
FROM claims c
INNER JOIN upload_rows ur ON ur.id = c.row_id
WHERE c.hashed_rx_key IS NOT NULL AND btrim(c.hashed_rx_key) <> '';

-- name: UpdateClaimHashedRxKey :exec
UPDATE claims SET hashed_rx_key = $2, updated_at = now() WHERE id = $1;