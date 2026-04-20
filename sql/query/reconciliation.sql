-- name: CreateReconciliationJob :one
INSERT INTO reconciliation_jobs (batch_id, status, total_claims)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetReconciliationJobByID :one
SELECT * FROM reconciliation_jobs WHERE id = $1;

-- name: GetReconciliationJobByBatch :one
SELECT * FROM reconciliation_jobs WHERE batch_id = $1;

-- name: UpdateJobStatus :exec
UPDATE reconciliation_jobs
SET status = $2
WHERE id = $1;

-- name: UpdateJobCounts :exec
UPDATE reconciliation_jobs
SET matched_count        = $2,
    unmatched_count      = $3,
    duplicate_risk_count = $4,
    excluded_count       = $5,
    error_count          = $6,
    completed_at         = now()
WHERE id = $1;

-- name: MarkJobStarted :exec
UPDATE reconciliation_jobs
SET status = 'running', started_at = now()
WHERE id = $1;

-- name: CreateCandidateMatch :one
INSERT INTO candidate_matches (job_id, claim_id, rebate_record_id, score)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: CreateMatchDecision :one
INSERT INTO match_decisions (
    job_id, claim_id, rebate_record_id,
    policy_version_id, status, reasoning
)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetMatchDecisionByClaim :one
SELECT * FROM match_decisions WHERE claim_id = $1;

-- name: GetMatchDecisionsByJob :many
SELECT * FROM match_decisions
WHERE job_id = $1
ORDER BY created_at;

-- name: GetMatchDecisionsByJobPaginated :many
SELECT * FROM match_decisions
WHERE job_id = $1
ORDER BY created_at
LIMIT $2 OFFSET $3;

-- name: CountMatchDecisionsByJob :one
SELECT COUNT(*) FROM match_decisions
WHERE job_id = $1;

-- name: GetPendingClaimsByBatch :many
SELECT * FROM claims
WHERE batch_id = $1
  AND reconciliation_status = 'pending'
ORDER BY created_at;

-- name: UpdateClaimReconciliationStatus :exec
UPDATE claims
SET reconciliation_status = $2,
    updated_at            = now()
WHERE id = $1;
