-- name: CreateRebateRecord :one
INSERT INTO rebate_records (
    manufacturer_id, org_id, ndc, pharmacy_npi,
    service_date, quantity, hashed_rx_key, payer_type,
    rebate_amount, source
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: GetRebateRecordByID :one
SELECT * FROM rebate_records WHERE id = $1;

-- Candidate retrieval: fetch records that could match a given claim.
-- Called by the reconciliation engine during candidate retrieval (Step A).
-- name: GetCandidateRebateRecords :many
SELECT * FROM rebate_records
WHERE ndc          = $1
  AND org_id       = $2
  AND pharmacy_npi = $3
  AND service_date BETWEEN $4 AND $5
  AND status       = 'active'
ORDER BY service_date;
