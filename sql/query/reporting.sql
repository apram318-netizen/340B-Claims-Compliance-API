-- name: GetManufacturerComplianceData :many
SELECT
    m.id                                                                         AS manufacturer_id,
    m.name                                                                       AS manufacturer_name,
    m.labeler_code,
    COUNT(md.id)                                                                 AS total_claims,
    COUNT(md.id) FILTER (WHERE md.status = 'matched')                           AS matched_count,
    COUNT(md.id) FILTER (WHERE md.status = 'probable_match')                    AS probable_match_count,
    COUNT(md.id) FILTER (WHERE md.status = 'unmatched')                         AS unmatched_count,
    COUNT(md.id) FILTER (WHERE md.status = 'duplicate_discount_risk')           AS duplicate_risk_count,
    COUNT(md.id) FILTER (WHERE md.status = 'excluded_by_policy')                AS excluded_count,
    COUNT(md.id) FILTER (WHERE md.status = 'invalid')                           AS invalid_count
FROM claims c
JOIN match_decisions md ON md.claim_id = c.id
JOIN manufacturer_products mp ON mp.ndc = c.ndc
JOIN manufacturers m ON m.id = mp.manufacturer_id
WHERE c.service_date BETWEEN $1 AND $2
GROUP BY m.id, m.name, m.labeler_code
ORDER BY m.name;

-- name: GetDuplicateDiscountFindings :many
SELECT
    c.id          AS claim_id,
    c.org_id,
    c.ndc,
    c.pharmacy_npi,
    c.service_date,
    c.quantity,
    c.payer_type,
    md.reasoning,
    rr.rebate_amount,
    rr.source     AS rebate_source
FROM claims c
JOIN match_decisions md ON md.claim_id = c.id
LEFT JOIN rebate_records rr ON rr.id = md.rebate_record_id
WHERE md.status = 'duplicate_discount_risk'
  AND c.service_date BETWEEN $1 AND $2
ORDER BY c.service_date;

-- name: GetUnresolvedExceptions :many
SELECT
    c.id              AS claim_id,
    c.org_id,
    c.ndc,
    c.pharmacy_npi,
    c.service_date,
    c.quantity,
    md.status         AS decision_status,
    md.reasoning
FROM claims c
JOIN match_decisions md ON md.claim_id = c.id
WHERE md.status IN ('unmatched', 'pending_external_data')
  AND ($1::uuid IS NULL OR c.org_id = $1)
  AND c.service_date BETWEEN $2 AND $3
ORDER BY c.service_date;

-- name: GetSubmissionCompleteness :many
SELECT
    o.id                               AS org_id,
    o.name                             AS org_name,
    o.entity_id,
    COUNT(ub.id)                       AS batch_count,
    COALESCE(SUM(ub.row_count), 0)     AS total_rows
FROM organizations o
LEFT JOIN upload_batches ub
       ON ub.org_id = o.id
      AND ub.created_at BETWEEN $1 AND $2
GROUP BY o.id, o.name, o.entity_id
ORDER BY o.name;

-- name: GetStuckBatches :many
SELECT * FROM upload_batches
WHERE status IN ('uploaded', 'normalized')
  AND updated_at < now() - interval '1 hour'
ORDER BY updated_at;
