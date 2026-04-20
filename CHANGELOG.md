# Changelog

All notable changes to this project are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/).

## [Unreleased]

### Added

- **OpenAPI** `0.5.0`: paths for platform routes (org settings/users/feature flags/SSO/webhooks/cases), OIDC/SAML auth, SCIM 2.0 resources, and `GET /api/version`; `ErrorResponse` includes optional `trace_id`.
- **OpenTelemetry**: OTLP/HTTP tracing when `OTEL_EXPORTER_OTLP_ENDPOINT` (or `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`) is set; `OTEL_SERVICE_NAME` defaults to `claims-system-api`. HTTP instrumentation via `otelhttp` (skips `/metrics`). `X-Trace-Id` aligns with the active trace when present.
- **SAML 2.0 SP**: `GET /v1/auth/saml/login`, `POST /v1/auth/saml/acs`, `GET /v1/auth/saml/metadata`; IdP metadata stored per org (`saml_idp_*` columns); SP key/cert from `SAML_SP_KEY_FILE` / `SAML_SP_CERT_FILE`; public URLs from `SAML_PUBLIC_BASE_URL` / `SAML_SP_ENTITY_ID`. Feature flag `saml_sso`.
- **SCIM 2.0 (expanded)**: `GET /Users` (pagination + `filter` for `userName` / `id`), `GET|PATCH|DELETE /Users/{id}`, `GET /ResourceTypes`, `GET /Schemas`, `GET /Schemas/{id}`; soft-delete maps to `users.active=false`.
- **Schema** `028_saml_scim.sql`: SAML columns on `org_sso_config`; `users.active` (default true) for SCIM deactivation and auth enforcement.

- **API version** discovery: `GET /api/version` returns `{ "version": "1", "service": "claims-system-api" }`; responses include `X-API-Version: 1` and optional `X-Trace-Id` (mirrors `X-Trace-Id` request header, OpenTelemetry trace id, or request ID).
- **Tenant admin**: `GET/PATCH /v1/organizations/{id}/settings`, `GET /v1/organizations/{id}/users`, `PATCH /v1/organizations/{id}/users/{userId}` (roles: `member`, `admin`, `viewer`), feature flag overrides per org.
- **Feature flags**: env `FEATURE_FLAGS=webhooks=true,exception_cases=true,scim=false,oidc_sso=true,saml_sso=false` plus DB overrides via `PUT /v1/organizations/{id}/feature-flags/{key}`.
- **Webhooks**: CRUD under `/v1/organizations/{id}/webhooks`, signed deliveries (`X-Webhook-Signature: sha256=...`), delivery log, background worker with retries; `export.completed` events after report generation (when `webhooks` flag enabled).
- **Exception cases**: workflow under `/v1/organizations/{id}/cases` with comments; states `open|in_progress|waiting|resolved|closed`.
- **OIDC SSO**: `GET /v1/auth/oidc/login?org_id=...`, `GET /v1/auth/oidc/callback`; org config `GET/PUT /v1/organizations/{id}/sso`; optional JIT via `sso_jit_provision` in org settings JSON.
- **SCIM 2.0**: Bearer `SCIM_BEARER_TOKEN`, org `SCIM_ORG_ID`; full user lifecycle and discovery endpoints (see OpenAPI).
- **Schema** `027_platform.sql`: `organization_settings`, `webhook_*`, `exception_cases`, `case_comments`, `user_external_identities`, `org_sso_config`, `feature_flag_overrides`. See `028_saml_scim.sql` for SAML columns and `users.active`.

### Documentation

- `docs/operator-platform.md` — env vars and jobs.
- `docs/integrator-guide.md` — webhooks, SSO, SCIM, feature flags.

### Tooling

- `scripts/gen-openapi-client.sh` — optional OpenAPI generator (requires Docker + OpenAPI Generator).
