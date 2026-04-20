# Platform operator guide

## Environment variables

| Variable | Purpose |
|----------|---------|
| `FEATURE_FLAGS` | Comma-separated `key=value` toggles: `webhooks`, `exception_cases`, `scim`, `oidc_sso`, `saml_sso` (defaults: webhooks/cases/oidc on; scim and saml off). |
| `OIDC_REDIRECT_URL` | Full HTTPS URL registered with the IdP for `GET /v1/auth/oidc/callback` (required for OIDC SSO). |
| `SAML_SP_KEY_FILE` | Path to PEM-encoded RSA private key for the SAML SP (required for SAML). |
| `SAML_SP_CERT_FILE` | Path to PEM-encoded SP certificate (required for SAML). |
| `SAML_PUBLIC_BASE_URL` | Public base URL of the API (e.g. `https://api.example.com`); used to build ACS and metadata URLs. |
| `SAML_SP_ENTITY_ID` | Optional SP entity ID (defaults to `{SAML_PUBLIC_BASE_URL}/v1/auth/saml/metadata`). |
| `SCIM_BEARER_TOKEN` | Bearer token for `/scim/v2/*` (optional). |
| `SCIM_ORG_ID` | UUID of the organization SCIM provisions users into. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OpenTelemetry OTLP endpoint (HTTP); enables tracing when set. Alternatively `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`. |
| `OTEL_SERVICE_NAME` | Service name in traces (default `claims-system-api`). |

## Background workers

- **Webhook delivery** runs inside the API process (every ~5s): pending deliveries are POSTed to subscriber URLs with `X-Webhook-Signature`. Metrics: `webhook_deliveries_succeeded_total`, `webhook_deliveries_failed_total`, `webhook_retries_total`.

## Database

- Apply migrations through `027_platform.sql` and `028_saml_scim.sql` (and earlier) before deploying this version.

## Security notes

- `org_sso_config.oidc_client_secret` and IdP SAML certificate PEM are stored in plaintext in this revision; use KMS or secret injection in production.
- SCIM and SAML are disabled by default (`FEATURE_FLAGS` / per-org override).
