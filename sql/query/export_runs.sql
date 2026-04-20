-- name: CreateExportRun :one
INSERT INTO export_runs (org_id, manufacturer_id, report_type, status, requested_by, params)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetExportRunByID :one
SELECT * FROM export_runs WHERE id = $1;

-- name: UpdateExportRunStarted :exec
UPDATE export_runs
SET status = 'running'
WHERE id = $1;

-- name: UpdateExportRunCompleted :exec
UPDATE export_runs
SET status      = 'completed',
    file_path   = $2,
    row_count   = $3,
    completed_at = now()
WHERE id = $1;

-- name: UpdateExportRunFailed :exec
UPDATE export_runs
SET status        = 'failed',
    error_message = $2,
    completed_at  = now()
WHERE id = $1;
