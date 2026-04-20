-- name: CreateManualOverride :one
INSERT INTO manual_override_events (claim_id, previous_status, new_status, reason, rebate_record_id, overridden_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetManualOverridesByClaim :many
SELECT * FROM manual_override_events
WHERE claim_id = $1
ORDER BY created_at DESC;

-- name: UpdateMatchDecisionOverride :exec
UPDATE match_decisions
SET status      = $2,
    override_id = $3
WHERE claim_id = $1;
