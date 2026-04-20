-- name: CreateUploadRow :one
INSERT INTO upload_rows (batch_id, row_number, data)
VALUES ($1,$2,$3)
RETURNING *;

-- name: GetUploadRowsByBatch :many 
SELECT * FROM upload_rows
WHERE batch_id = $1 
ORDER BY row_number;

-- name: GetUploadRowsByBatchAndValidation :many
SELECT ur.* FROM upload_rows ur
JOIN validation_results vr ON vr.row_id = ur.id
WHERE ur.batch_id = $1 AND vr.is_valid = $2 
ORDER BY ur.row_number;


