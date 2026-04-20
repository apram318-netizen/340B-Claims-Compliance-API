# Integrator guide

## API versioning

- Base path: `/v1/...`.
- `GET /api/version` returns the supported API major version.
- Prefer `Accept: application/json` and send `X-Trace-Id` or W3C `traceparent` for cross-service correlation (responses include `X-Trace-Id`, aligned with OpenTelemetry when tracing is enabled).

## Webhooks

1. Enable the feature (`FEATURE_FLAGS` or org override `webhooks=true`).
2. `POST /v1/organizations/{org_id}/webhooks` with `{ "url": "https://...", "event_types": ["export.completed"] }` (empty `event_types` = all subscribed types).
3. Store the returned `secret` once; each POST includes `X-Webhook-Signature: sha256=<hex>` = HMAC-SHA256(secret, raw body).
4. Inspect delivery history: `GET .../webhooks/{id}/deliveries`.

## OIDC SSO

1. Set `OIDC_REDIRECT_URL` on the API to match the IdP app registration.
2. `PUT /v1/organizations/{id}/sso` with `oidc_issuer`, `oidc_client_id`, `oidc_client_secret`, `enabled: true`.
3. Users open `GET /v1/auth/oidc/login?org_id={uuid}`; after IdP login, `GET /v1/auth/oidc/callback` returns `{ "token", "user" }`.
4. Optional JIT: set org settings JSON `{ "sso_jit_provision": true }` so users with an email from the IdP are created on first login.

## SAML SSO

1. Provision an RSA key pair for the SP and set `SAML_SP_KEY_FILE`, `SAML_SP_CERT_FILE` on the API host.
2. Set `SAML_PUBLIC_BASE_URL` to the public origin of this API (e.g. `https://api.example.com`). Optionally set `SAML_SP_ENTITY_ID` (defaults to `{SAML_PUBLIC_BASE_URL}/v1/auth/saml/metadata`).
3. Register the SP with your IdP using `GET /v1/auth/saml/metadata` (ACS URL `{SAML_PUBLIC_BASE_URL}/v1/auth/saml/acs`).
4. `PUT /v1/organizations/{id}/sso` with `saml_idp_entity_id`, `saml_idp_sso_url`, `saml_idp_cert_pem` (PEM of IdP signing cert), `enabled: true`. Enable feature flag `saml_sso` for the org if using env defaults.
5. Users open `GET /v1/auth/saml/login?org_id={uuid}`; the API returns `{ "token", "user" }` after `POST` to `/v1/auth/saml/acs`.

## SCIM 2.0

- Configure `SCIM_BEARER_TOKEN` and `SCIM_ORG_ID` on the API.
- Enable `scim` via `FEATURE_FLAGS` or per-org override.
- Endpoints include `GET /scim/v2/ServiceProviderConfig`, `GET /scim/v2/Users` (supports `filter`, `startIndex`, `count`), `POST /scim/v2/Users`, `GET|PATCH|DELETE /scim/v2/Users/{id}`, `GET /scim/v2/ResourceTypes`, `GET /scim/v2/Schemas`, `GET /scim/v2/Schemas/{id}`.
- Deactivate users with `DELETE /scim/v2/Users/{id}` (sets `active: false` in the directory; maps to `users.active` in this service).

## Feature flags (per org)

- `GET /v1/organizations/{id}/feature-flags` — effective flags.
- `PUT /v1/organizations/{id}/feature-flags/{key}` — `{ "value": true|false }` for keys: `webhooks`, `exception_cases`, `scim`, `oidc_sso`, `saml_sso`.
