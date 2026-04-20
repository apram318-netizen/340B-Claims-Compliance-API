-- name: CreatePolicy :one
INSERT INTO policies (manufacturer_id, name, status)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetPolicyByID :one
SELECT * FROM policies WHERE id = $1;

-- name: ListPoliciesByManufacturer :many
SELECT * FROM policies
WHERE manufacturer_id = $1
ORDER BY created_at DESC;

-- name: ListPoliciesByManufacturerPaginated :many
SELECT * FROM policies
WHERE manufacturer_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: CountPoliciesByManufacturer :one
SELECT COUNT(*) FROM policies
WHERE manufacturer_id = $1;

-- name: CreatePolicyVersion :one
INSERT INTO policy_versions (policy_id, version_number, effective_from, effective_to)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetActivePolicyVersion :one
SELECT pv.* FROM policy_versions pv
JOIN policies p ON p.id = pv.policy_id
WHERE p.manufacturer_id = $1
  AND pv.effective_from <= $2
  AND (pv.effective_to IS NULL OR pv.effective_to >= $2)
  AND p.status = 'active'
ORDER BY pv.version_number DESC
LIMIT 1;

-- name: GetPolicyVersionByID :one
SELECT * FROM policy_versions WHERE id = $1;

-- name: CreatePolicyRule :one
INSERT INTO policy_rules (policy_version_id, rule_type, rule_config)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetRulesByPolicyVersion :many
SELECT * FROM policy_rules
WHERE policy_version_id = $1
ORDER BY created_at;

-- name: GetRulesByPolicyVersionPaginated :many
SELECT * FROM policy_rules
WHERE policy_version_id = $1
ORDER BY created_at
LIMIT $2 OFFSET $3;

-- name: CountRulesByPolicyVersion :one
SELECT COUNT(*) FROM policy_rules
WHERE policy_version_id = $1;
