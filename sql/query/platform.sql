-- name: GetOrganizationSettings :one
SELECT org_id, settings, updated_at FROM organization_settings WHERE org_id = $1;

-- name: UpsertOrganizationSettings :one
INSERT INTO organization_settings (org_id, settings, updated_at)
VALUES ($1, $2, now())
ON CONFLICT (org_id) DO UPDATE SET settings = EXCLUDED.settings, updated_at = now()
RETURNING org_id, settings, updated_at;

-- name: ListUsersByOrg :many
SELECT id, org_id, email, name, role, created_at, password_hash, failed_login_attempts, locked_until, mfa_enabled, mfa_secret, active
FROM users WHERE org_id = $1 ORDER BY email ASC;

-- name: ListUsersByOrgPaginated :many
SELECT id, org_id, email, name, role, created_at, password_hash, failed_login_attempts, locked_until, mfa_enabled, mfa_secret, active
FROM users WHERE org_id = $1 ORDER BY email ASC LIMIT $2 OFFSET $3;

-- name: CountUsersByOrg :one
SELECT COUNT(*) FROM users WHERE org_id = $1;

-- name: GetUserByIDForOrg :one
SELECT id, org_id, email, name, role, created_at, password_hash, failed_login_attempts, locked_until, mfa_enabled, mfa_secret, active
FROM users WHERE id = $1 AND org_id = $2;

-- name: UpdateUserRole :one
UPDATE users SET role = $2 WHERE id = $1 AND org_id = $3
RETURNING id, org_id, email, name, role, created_at, password_hash, failed_login_attempts, locked_until, mfa_enabled, mfa_secret, active;

-- name: CreateWebhookSubscription :one
INSERT INTO webhook_subscriptions (org_id, url, secret_hmac, event_types, active)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListWebhookSubscriptionsByOrg :many
SELECT * FROM webhook_subscriptions WHERE org_id = $1 ORDER BY created_at DESC;

-- name: GetWebhookSubscription :one
SELECT * FROM webhook_subscriptions WHERE id = $1 AND org_id = $2;

-- name: UpdateWebhookSubscription :one
UPDATE webhook_subscriptions SET url = $2, event_types = $3, active = $4, updated_at = now()
WHERE id = $1 AND org_id = $5
RETURNING *;

-- name: DeleteWebhookSubscription :exec
DELETE FROM webhook_subscriptions WHERE id = $1 AND org_id = $2;

-- name: CreateWebhookDelivery :one
INSERT INTO webhook_deliveries (subscription_id, event_type, payload, status, next_retry_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListWebhookDeliveriesPending :many
SELECT * FROM webhook_deliveries
WHERE status IN ('pending', 'retrying')
  AND (next_retry_at IS NULL OR next_retry_at <= now())
ORDER BY created_at ASC
LIMIT $1;

-- name: GetWebhookSubscriptionByID :one
SELECT * FROM webhook_subscriptions WHERE id = $1;

-- name: UpdateWebhookDeliveryStatus :exec
UPDATE webhook_deliveries SET status = $2, attempt_count = $3, last_error = $4, next_retry_at = $5, updated_at = now()
WHERE id = $1;

-- name: ListWebhookDeliveriesBySubscription :many
SELECT * FROM webhook_deliveries WHERE subscription_id = $1 ORDER BY created_at DESC LIMIT $2;

-- name: CreateExceptionCase :one
INSERT INTO exception_cases (org_id, claim_id, batch_id, title, status, priority, assignee_user_id, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetExceptionCase :one
SELECT * FROM exception_cases WHERE id = $1 AND org_id = $2;

-- name: ListExceptionCasesByOrg :many
SELECT * FROM exception_cases WHERE org_id = $1 ORDER BY updated_at DESC LIMIT $2 OFFSET $3;

-- name: CountExceptionCasesByOrg :one
SELECT COUNT(*) FROM exception_cases WHERE org_id = $1;

-- name: UpdateExceptionCase :one
UPDATE exception_cases
SET title = $3,
    status = $4,
    priority = $5,
    assignee_user_id = $6,
    updated_at = now()
WHERE id = $1 AND org_id = $2
RETURNING *;

-- name: CreateCaseComment :one
INSERT INTO case_comments (case_id, author_id, body)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListCaseComments :many
SELECT * FROM case_comments WHERE case_id = $1 ORDER BY created_at ASC;

-- name: InsertUserExternalIdentity :one
INSERT INTO user_external_identities (user_id, provider, issuer, subject, email)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetUserExternalIdentityByIssuerSubject :one
SELECT * FROM user_external_identities WHERE issuer = $1 AND subject = $2;

-- name: GetOrgSSOConfig :one
SELECT * FROM org_sso_config WHERE org_id = $1;

-- name: UpsertOrgSSOConfig :one
INSERT INTO org_sso_config (
  org_id, oidc_issuer, oidc_client_id, oidc_client_secret, enabled,
  saml_idp_entity_id, saml_idp_sso_url, saml_idp_cert_pem, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now())
ON CONFLICT (org_id) DO UPDATE SET
  oidc_issuer = EXCLUDED.oidc_issuer,
  oidc_client_id = EXCLUDED.oidc_client_id,
  oidc_client_secret = EXCLUDED.oidc_client_secret,
  enabled = EXCLUDED.enabled,
  saml_idp_entity_id = EXCLUDED.saml_idp_entity_id,
  saml_idp_sso_url = EXCLUDED.saml_idp_sso_url,
  saml_idp_cert_pem = EXCLUDED.saml_idp_cert_pem,
  updated_at = now()
RETURNING *;

-- name: GetFeatureFlagOverride :one
SELECT org_id, flag_key, value, updated_at FROM feature_flag_overrides WHERE org_id = $1 AND flag_key = $2;

-- name: UpsertFeatureFlagOverride :one
INSERT INTO feature_flag_overrides (org_id, flag_key, value, updated_at)
VALUES ($1, $2, $3, now())
ON CONFLICT (org_id, flag_key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()
RETURNING org_id, flag_key, value, updated_at;

-- name: ListFeatureFlagOverridesByOrg :many
SELECT org_id, flag_key, value, updated_at FROM feature_flag_overrides WHERE org_id = $1;
