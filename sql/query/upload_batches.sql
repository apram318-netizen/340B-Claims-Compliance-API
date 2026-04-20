-- name: CreateUploadBatches :one
INSERT INTO upload_batches (org_id, uploaded_by, file_name, row_count, status, error_message)
VALUES ($1,$2,$3,$4,$5,$6)
RETURNING *; 


-- name: GetUploadBatches :one
SELECT * FROM upload_batches 
WHERE id = $1;

-- name: UpdateBatchStatus :exec
UPDATE upload_batches
SET status = $2, updated_at = now()
WHERE id = $1; 


-- name: UpdateBatchStatusWithError :exec 
UPDATE upload_batches
SET status = $2, error_message = $3 , updated_at = now()
WHERE id = $1; 


