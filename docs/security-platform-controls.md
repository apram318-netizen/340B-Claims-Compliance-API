# Security Platform Controls (Production)

This document captures controls that complement application-level hardening for sensitive-data environments.

## TLS Termination and Transport Security

- Terminate TLS at an ingress/load balancer with modern ciphers only.
- Enforce TLS 1.2+ (prefer TLS 1.3).
- Redirect all plaintext HTTP traffic to HTTPS.
- Enable HSTS with preload where appropriate.
- Use mTLS for internal service-to-service traffic where feasible.

## Secret Management and Rotation

- Store runtime secrets in managed secret storage (not `.env` in production).
- Rotate `JWT_SECRET`, DB credentials, and broker credentials on a fixed schedule (e.g., 60-90 days).
- **JWT rotation (implemented):** set optional `JWT_SECRET_PREVIOUS` to the previous signing secret. The API verifies bearer tokens with `JWT_SECRET` first, then `JWT_SECRET_PREVIOUS`, so existing sessions remain valid during overlap. New tokens are always signed with `JWT_SECRET` only. After all clients refresh, clear `JWT_SECRET_PREVIOUS`.
- Alert on failed secret retrieval and unauthorized access attempts.

## Password reset (production)

- Do **not** expose the raw reset token in JSON. Set `PASSWORD_RESET_EXPOSE_TOKEN=false` (default). Tokens are never included in the response when unset or false.
- Configure SMTP: `EMAIL_SMTP_HOST`, `EMAIL_SMTP_PORT` (default 587), `EMAIL_SMTP_USER`, `EMAIL_SMTP_PASSWORD`, `EMAIL_FROM`. The API sends a plain-text message containing the token (never logged server-side in success path).
- Optional `PASSWORD_RESET_PUBLIC_BASE_URL` is included in the email instructions (defaults to `http://localhost:$PORT`).
- For local development and automated tests, set `PASSWORD_RESET_EXPOSE_TOKEN=true` or `1` or `yes` to return the token in the response body (no email service required).

## Multi-factor authentication (TOTP)

- Users can enroll via `POST /v1/mfa/setup` (returns `secret` and `otpauth_url`), then `POST /v1/mfa/enable` with `{secret, code}`. The response includes `backup_codes` (10 one-time codes; store safely).
- `POST /v1/mfa/backup-codes/regenerate` with `{code}` (current TOTP) issues new backup codes and invalidates unused old codes.
- `POST /v1/mfa/disable` requires the account password and clears backup codes.
- When MFA is enabled, `POST /v1/login` must include `totp_code` **or** `backup_code` after the password is validated; otherwise the API returns HTTP 403 with `code: MFA_REQUIRED`.
- Optional `REQUIRE_MFA_FOR_ADMIN=true`: users with an admin role cannot log in until MFA is enabled (HTTP 403, `code: MFA_SETUP_REQUIRED`).
- Optional **`REQUIRE_MFA_STEP_UP_FOR_ADMIN=true`**: MFA-enabled admins must obtain a short-lived token via `POST /v1/mfa/step-up` (body: `totp_code` or `backup_code`) and send it on the **`X-MFA-Step-Up`** header for mutating admin routes (create org/manufacturer/policy/rebate/contract pharmacy, resolve dispute, etc.). Token lifetime is about 10 minutes. Admins without MFA are unchanged.
- Store TOTP secrets only over TLS; the API never returns the stored secret after enrollment.

## Backup and Restore

- Enable encrypted automated PostgreSQL backups.
- Define RPO/RTO targets and document ownership.
- Run restore drills on a schedule (monthly recommended).
- Validate both schema and data integrity after restore.

## Encryption at Rest

- Require encryption at rest for:
  - PostgreSQL storage volumes/snapshots
  - RabbitMQ persistent storage
  - Export artifact storage
  - Backup archives
- Use provider-managed KMS keys with audit logs enabled.

### Application-layer PHI key (`PHI_ENCRYPTION_KEY`)

- When set (32-byte secret, base64-encoded), the worker applies **HMAC-SHA256** to `hashed_rx_key` at claim ingest so the raw value is not stored in plaintext. Matching logic compares the HMAC output. If the key is unset, ingest stores the field as provided (development only).
- **Rotation:** changing the key invalidates equality for existing stored HMAC values. Plan a maintenance window: run [`cmd/rehash-rx-keys`](../cmd/rehash-rx-keys/main.go) after updating `PHI_ENCRYPTION_KEY` — it re-hashes from the original `hashed_rx_key` value still stored in `upload_rows.data` JSON for each claim. Use `-dry-run` first. Rows that no longer have source JSON or an empty `hashed_rx_key` in the upload payload are skipped; **rebate_records** and other tables are not modified by this tool (handle separately or re-import).

## Audit Log Retention and Immutability

- Forward audit events to centralized log storage with WORM/immutability controls.
- Set retention windows to meet policy/regulatory obligations.
- Restrict audit log deletion permissions to a break-glass process.
- Verify request IDs are retained end-to-end for traceability.
- **Operational purge (implemented):** run [`cmd/purge-audit`](../cmd/purge-audit/main.go) on a schedule (see [`k8s/cronjob-audit-purge.yaml`](../k8s/cronjob-audit-purge.yaml)) with `DATABASE_URL` set. It deletes rows in `audit_events` past `expires_at` (see `PurgeExpiredAuditEvents`).

## Access Control and Segmentation

- Restrict production network access to least privilege.
- Separate environments (dev/staging/prod) by account/project/VPC when possible.
- Enforce MFA and short-lived credentials for administrative access.

## Observability (OpenTelemetry)

- When `OTEL_EXPORTER_OTLP_ENDPOINT` is set, traces leave the process over OTLP/HTTP. Use a collector or SaaS backend with access controls; do not expose collectors without authentication on untrusted networks.
- Trace context is propagated via standard W3C `traceparent` headers; API responses also echo `X-Trace-Id` for operators.

## SAML SP key material

- `SAML_SP_KEY_FILE` / `SAML_SP_CERT_FILE` must be readable only by the API OS user. Rotate on the same schedule as TLS certificates for the SP.
- IdP signing certificates stored in `org_sso_config.saml_idp_cert_pem` are sensitive; restrict DB access and prefer automated metadata refresh from a trusted URL in hardened deployments.

## Security Validation Checklist

- [ ] External TLS policy validated
- [ ] Secret rotation runbook tested
- [ ] Restore drill completed and documented
- [ ] Encryption-at-rest verified for all stores
- [ ] Audit retention + immutability policy enforced
- [ ] IAM least-privilege review signed off
