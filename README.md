# Claims System

A production-grade, multi-tenant SaaS platform for **340B drug pricing compliance** — covering pharmacy claim ingestion, policy-driven reconciliation, dispute management, and compliance reporting.

Built in Go with PostgreSQL, RabbitMQ, and Redis. Kubernetes-ready with full observability (OpenTelemetry + Prometheus).

---

## Table of Contents

- [Overview](#overview)
- [Features](#features)
  - [Batch Claims Pipeline](#batch-claims-pipeline)
  - [Policy Engine](#policy-engine)
  - [Reconciliation Engine](#reconciliation-engine)
  - [Authentication & Authorization](#authentication--authorization)
  - [Multi-Factor Authentication](#multi-factor-authentication)
  - [SSO — OIDC & SAML 2.0](#sso--oidc--saml-20)
  - [SCIM 2.0 User Provisioning](#scim-20-user-provisioning)
  - [Organizations & Multi-Tenancy](#organizations--multi-tenancy)
  - [Webhooks](#webhooks)
  - [Exception Cases Workflow](#exception-cases-workflow)
  - [Manufacturers, Products & Policies](#manufacturers-products--policies)
  - [Disputes](#disputes)
  - [Contract Pharmacies](#contract-pharmacies)
  - [Exports & Reports](#exports--reports)
  - [Manual Overrides](#manual-overrides)
  - [Audit Logging](#audit-logging)
  - [Feature Flags](#feature-flags)
  - [Rate Limiting](#rate-limiting)
  - [Observability](#observability)
- [Architecture](#architecture)
- [Tech Stack](#tech-stack)
- [API Reference](#api-reference)
- [Getting Started](#getting-started)
  - [Prerequisites](#prerequisites)
  - [Environment Variables](#environment-variables)
  - [Running Locally](#running-locally)
  - [Database Migrations](#database-migrations)
- [Kubernetes Deployment](#kubernetes-deployment)
- [Security](#security)
- [Project Structure](#project-structure)

---

## Overview

The Claims System helps covered entities (hospitals, health systems, federally qualified health centers) manage their 340B drug pricing program compliance:

1. **Upload** pharmacy claim files (CSV or XLSX)
2. **Validate & normalize** claims against business rules
3. **Reconcile** claims against rebate records using manufacturer policy rules
4. **Generate** compliance reports (Excel/CSV)
5. **Manage** disputes, exceptions, and audit trails

The platform is multi-tenant: each organization's data is fully isolated. Users authenticate with passwords, MFA, or SSO (OIDC/SAML), and can be provisioned automatically via SCIM 2.0.

---

## Features

### Batch Claims Pipeline

Claims are processed asynchronously through a four-stage pipeline backed by RabbitMQ:

| Stage | Queue | Description |
|-------|-------|-------------|
| 1. Upload | — | CSV/XLSX file parsed, batch record created, rows enqueued |
| 2. Validation | `batch_validation` | Schema checks: required fields, NDC length ≥ 10, quantity > 0, valid dates |
| 3. Normalization | `batch_normalization` | Date parsing, NPI/NDC trimming, payer-type mapping, optional Rx key hashing |
| 4. Reconciliation | `batch_reconciliation` | Claims matched against rebate records with manufacturer policy rules |

**Accepted file formats:** CSV, XLSX (max 10 MB)

**Required columns:** `ndc`, `pharmacy_npi`, `service_date`, `quantity`

Each stage uses a dead-letter exchange (DLQ) so failed messages are captured without blocking the pipeline. The worker process exposes `/health` and `/ready` probes and reconnects automatically on RabbitMQ failures.

---

### Policy Engine

Manufacturer-specific rules are evaluated per claim on the `service_date`. Rules are data-driven — no code changes are needed when policies change. Supported rule types:

- **NDC scope** — restrict which National Drug Codes are covered
- **Payer channel** — include or exclude specific payer types (e.g. Medicaid)
- **Date window** — policy effective-from and expiry dates
- **Pharmacy limits** — restrict to specific contract pharmacy NPIs

Policies are versioned. A new policy version can be activated without deleting the old one.

---

### Reconciliation Engine

Claim decisions are one of seven statuses:

| Status | Meaning |
|--------|---------|
| `matched` | Exact rebate record found |
| `probable_match` | High-confidence candidate found |
| `unmatched` | No candidate rebate record |
| `duplicate_discount_risk` | Medicaid overlap detected |
| `invalid` | Malformed or missing required fields |
| `excluded_by_policy` | Manufacturer policy excludes this claim |
| `pending_external_data` | Waiting for reference data |

Decisions are queryable per reconciliation job with full pagination. Admins can apply **manual overrides** to any decision post-reconciliation.

---

### Authentication & Authorization

- **JWT** (HS256) signed with `JWT_SECRET`; optional rotation via `JWT_SECRET_PREVIOUS`
- **Password hashing** with bcrypt
- **Account lockout** after repeated failed login attempts
- **Role-based access control** — three roles per organization: `member`, `admin`, `viewer`
- Org-scoped middleware ensures users can only access their own organization's data

---

### Multi-Factor Authentication

TOTP-based MFA (RFC 6238) with QR code enrollment and backup codes:

| Endpoint | Action |
|----------|--------|
| `POST /v1/mfa/setup` | Returns TOTP secret + QR code URI |
| `POST /v1/mfa/enable` | Verifies first TOTP code and enables MFA |
| `POST /v1/mfa/disable` | Disables MFA (requires TOTP) |
| `POST /v1/mfa/backup-codes/regenerate` | Issues new one-time backup codes |
| `POST /v1/mfa/step-up` | Issues a short-lived step-up token for sensitive operations |

When `REQUIRE_MFA_FOR_ADMIN=true`, all admin accounts must complete MFA enrollment. When `REQUIRE_MFA_STEP_UP_FOR_ADMIN=true`, admin write operations require a valid step-up token.

---

### SSO — OIDC & SAML 2.0

**OIDC (OpenID Connect)**

- Org-level configuration (issuer, client ID, client secret, scopes)
- Optional Just-in-Time (JIT) user provisioning (`sso_jit_provision` org setting)
- Stateful callback with short-lived state tokens (Redis-backed)
- Feature flag: `oidc_sso` (default: `true`)

**SAML 2.0 Service Provider**

- SP-initiated SSO with IdP redirect
- Assertion Consumer Service (`POST /v1/auth/saml/acs`)
- SP metadata endpoint (`GET /v1/auth/saml/metadata`) for IdP registration
- IdP metadata, certificate, and entity ID stored per org
- SP private key/cert loaded from `SAML_SP_KEY_FILE` / `SAML_SP_CERT_FILE`
- Feature flag: `saml_sso` (default: `false`)

Configure SSO per organization via `GET/PUT /v1/organizations/{id}/sso`.

---

### SCIM 2.0 User Provisioning

Full SCIM 2.0 implementation for automated user lifecycle management from an Identity Provider:

- `GET /scim/v2/Users` — list with `filter` (userName, id) and pagination
- `POST /scim/v2/Users` — create user
- `GET /scim/v2/Users/{id}` — get user
- `PATCH /scim/v2/Users/{id}` — modify attributes
- `DELETE /scim/v2/Users/{id}` — soft-deactivate (sets `active=false`, blocks login)
- Schema and capability discovery endpoints
- Bearer token authentication (`SCIM_BEARER_TOKEN`)
- Feature flag: `scim` (default: `false`)

---

### Organizations & Multi-Tenancy

- Each organization is a fully isolated tenant
- Org-level settings (JSON blob) for SSO, JIT provisioning, etc.
- Feature flag overrides per org via `PUT /v1/organizations/{id}/feature-flags/{key}`
- Org admin endpoints for user role management and settings updates
- Admin write operations protected by MFA step-up when configured

---

### Webhooks

Organizations can subscribe to platform events via webhooks:

- **Event types:** `export.completed` (extensible)
- **Signed delivery:** `X-Webhook-Signature: sha256=<HMAC-SHA256>` using the webhook's secret
- **Delivery log:** full history of attempts per webhook
- **Automatic retries:** failed deliveries re-enqueued via the background worker
- **CRUD endpoints:** `POST/GET/PATCH/DELETE /v1/organizations/{id}/webhooks`
- **Delivery history:** `GET /v1/organizations/{id}/webhooks/{id}/deliveries`
- Feature flag: `webhooks` (default: `true`)

**Verifying a webhook delivery (example):**

```go
mac := hmac.New(sha256.New, []byte(webhookSecret))
mac.Write(body)
expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
// compare expected with X-Webhook-Signature header
```

---

### Exception Cases Workflow

Track and resolve data exceptions with a structured workflow:

- **States:** `open` → `in_progress` → `waiting` → `resolved` → `closed`
- Assignable to org members
- Threaded comments with author and timestamp
- Scoped per organization
- Feature flag: `exception_cases` (default: `true`)

---

### Manufacturers, Products & Policies

- Master manufacturer catalog with NDC-to-manufacturer mapping
- Product catalog per manufacturer
- Policy versioning with `effective_from` dates
- Policy rules: NDC scope, payer channel, date window, pharmacy whitelisting

---

### Disputes

Users can dispute reconciliation decisions:

- `POST /v1/disputes` — open a dispute on a claim decision
- `POST /v1/disputes/{id}/submit` — submit for admin review
- `POST /v1/disputes/{id}/withdraw` — withdraw before review
- `POST /v1/disputes/{id}/resolve` — admin resolution (requires MFA step-up)

---

### Contract Pharmacies

- Whitelist/blacklist network pharmacies by NPI
- Activate or deactivate pharmacy status
- Grant or revoke per-manufacturer authorization
- Admin-only write operations (with MFA step-up)

---

### Exports & Reports

On-demand report generation with async processing:

- Formats: Excel (XLSX) and CSV
- Optional PII redaction (`EXPORT_REDACT_SENSITIVE_FIELDS=true`)
- Status tracking: `pending` → `in_progress` → `completed` / `failed`
- Download via redirect (local filesystem or AWS S3)
- Retry failed exports: `POST /v1/exports/{id}/retry`
- Webhook notification on completion (when `webhooks` feature is enabled)

---

### Manual Overrides

Admins can manually change a claim's reconciliation decision after the fact. All overrides are:

- Protected by admin role + MFA step-up
- Recorded in the audit log with the overriding user and timestamp

---

### Audit Logging

All sensitive operations produce audit log entries:

- Authentication events (login, logout, failed attempts, lockout)
- Claim decisions and overrides
- Policy and manufacturer changes
- User role changes
- Dispute and case actions

Configurable retention (default 365 days). Queryable via `GET /v1/audit-events` with pagination. Old entries are pruned by a scheduled Kubernetes CronJob (`cmd/purge-audit`).

---

### Feature Flags

Feature availability is controlled at two levels:

1. **Global defaults** via `FEATURE_FLAGS` environment variable:
   ```
   FEATURE_FLAGS=webhooks=true,exception_cases=true,oidc_sso=true,scim=false,saml_sso=false
   ```

2. **Per-organization overrides** via `PUT /v1/organizations/{id}/feature-flags/{key}` (admin only)

| Flag | Default | Controls |
|------|---------|---------|
| `webhooks` | `true` | Webhook CRUD and delivery |
| `exception_cases` | `true` | Exception cases workflow |
| `oidc_sso` | `true` | OIDC SSO login flow |
| `scim` | `false` | SCIM 2.0 provisioning endpoints |
| `saml_sso` | `false` | SAML 2.0 SP login flow |

---

### Rate Limiting

- 10 requests/minute per IP by default
- Redis-backed in production, in-memory fallback when Redis is unavailable
- Applied to all API routes via middleware

---

### Observability

**Metrics (Prometheus)**

- Exposed at `GET /metrics` (API) and port `9091` (worker)
- In-flight request counts, processing latency, pipeline success/failure rates

**Tracing (OpenTelemetry)**

- OTLP/HTTP export when `OTEL_EXPORTER_OTLP_ENDPOINT` is set
- HTTP instrumentation via `otelhttp` (skips `/metrics`)
- `X-Trace-Id` response header aligned with active trace

**Structured Logging**

- JSON-formatted via Go's `log/slog`
- All log lines include trace ID, org ID, and request context

**Health Probes**

- `GET /health` — API liveness (checks database connectivity)
- Worker: `/health` and `/ready` on `WORKER_METRICS_PORT` (default `9091`)

---

## Architecture

```
                        ┌─────────────┐
  Browser / Client ────▶│  API Server │ :8080
                        │  (chi v5)   │
                        └──────┬──────┘
                               │ SQL (pgx)
                    ┌──────────▼──────────┐
                    │    PostgreSQL 16     │
                    └──────────▲──────────┘
                               │ SQL (pgx)
                        ┌──────┴──────┐
          RabbitMQ ────▶│   Worker    │ :9091 (metrics)
         (4 queues)     │  (pipeline) │
                        └─────────────┘
                               │
                    ┌──────────▼──────────┐
                    │     AWS S3 / Local  │
                    │    (Export Files)   │
                    └─────────────────────┘
```

**Async pipeline stages:**

```
Upload ──▶ batch_validation ──▶ batch_normalization ──▶ batch_reconciliation
                                                               │
                                                    report_generation queue
                                                               │
                                                         exports/ (S3 or local)
```

Each queue has a dead-letter exchange. The worker reconnects automatically on RabbitMQ failures with exponential backoff.

---

## Tech Stack

| Component | Technology |
|-----------|-----------|
| Language | Go 1.25 |
| HTTP Router | [chi v5](https://github.com/go-chi/chi) |
| Database | PostgreSQL 16 ([pgx v5](https://github.com/jackc/pgx)) |
| DB Code Generation | [sqlc](https://sqlc.dev) |
| Migrations | [goose v3](https://github.com/pressly/goose) |
| Message Queue | RabbitMQ ([amqp091-go](https://github.com/rabbitmq/amqp091-go)) |
| Cache / Rate Limit | Redis ([go-redis v9](https://github.com/redis/go-redis)) |
| JWT | [golang-jwt v4](https://github.com/golang-jwt/jwt) |
| Password Hashing | bcrypt (`golang.org/x/crypto`) |
| MFA (TOTP) | [pquerna/otp v1.4](https://github.com/pquerna/otp) |
| OIDC | [coreos/go-oidc v3](https://github.com/coreos/go-oidc) |
| SAML 2.0 | [crewjam/saml v0.4](https://github.com/crewjam/saml) |
| Excel Parsing / Export | [excelize v2](https://github.com/xuri/excelize) |
| AWS S3 | [aws-sdk-go-v2](https://github.com/aws/aws-sdk-go-v2) |
| Tracing | [OpenTelemetry](https://opentelemetry.io) (OTLP/HTTP) |
| Containers | Docker (distroless images) |
| Orchestration | Kubernetes |

---

## API Reference

The full OpenAPI 3.0 specification is at [`api/openapi.yaml`](api/openapi.yaml) (version 0.5.0).

### Quick endpoint summary

<details>
<summary>Authentication</summary>

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/register` | Create account |
| `POST` | `/v1/login` | Login (returns JWT) |
| `POST` | `/v1/logout` | Invalidate session |
| `POST` | `/v1/password-reset/request` | Initiate password reset |
| `POST` | `/v1/password-reset/confirm` | Complete password reset |
| `GET` | `/v1/auth/oidc/login` | Redirect to OIDC IdP |
| `GET` | `/v1/auth/oidc/callback` | OIDC callback |
| `GET` | `/v1/auth/saml/login` | Redirect to SAML IdP |
| `POST` | `/v1/auth/saml/acs` | SAML assertion consumer |
| `GET` | `/v1/auth/saml/metadata` | SP metadata XML |

</details>

<details>
<summary>MFA</summary>

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/mfa/setup` | Begin TOTP enrollment |
| `POST` | `/v1/mfa/enable` | Activate MFA |
| `POST` | `/v1/mfa/disable` | Deactivate MFA |
| `POST` | `/v1/mfa/backup-codes/regenerate` | Regenerate backup codes |
| `POST` | `/v1/mfa/step-up` | Issue step-up token |

</details>

<details>
<summary>Organizations</summary>

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/organizations` | Create organization |
| `GET` | `/v1/organizations/{id}` | Get organization |
| `GET` | `/v1/organizations/{id}/settings` | Org settings |
| `PATCH` | `/v1/organizations/{id}/settings` | Update org settings |
| `GET` | `/v1/organizations/{id}/users` | List org users |
| `PATCH` | `/v1/organizations/{id}/users/{userId}` | Update user role/status |
| `GET` | `/v1/organizations/{id}/feature-flags` | Effective feature flags |
| `PUT` | `/v1/organizations/{id}/feature-flags/{key}` | Override feature flag |
| `GET` | `/v1/organizations/{id}/sso` | SSO configuration |
| `PUT` | `/v1/organizations/{id}/sso` | Update SSO configuration |

</details>

<details>
<summary>Claims & Batches</summary>

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/batches` | Create batch |
| `POST` | `/v1/batches/upload` | Upload CSV/XLSX file |
| `GET` | `/v1/batches/{id}` | Batch status |
| `GET` | `/v1/batches/{id}/claims` | Claims in batch (paginated) |
| `GET` | `/v1/batches/{id}/reconciliation-job` | Associated recon job |
| `GET` | `/v1/claims/{id}` | Single claim |
| `GET` | `/v1/claims/{id}/decision` | Reconciliation decision |
| `GET` | `/v1/claims/{id}/disputes` | Disputes on claim |
| `POST` | `/v1/claims/{id}/override` | Manual override (admin) |

</details>

<details>
<summary>Reconciliation & Exports</summary>

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/reconciliation-jobs/{id}` | Job status and summary |
| `GET` | `/v1/reconciliation-jobs/{id}/decisions` | Decisions (paginated) |
| `POST` | `/v1/exports` | Request report generation |
| `GET` | `/v1/exports/{id}` | Export status |
| `GET` | `/v1/exports/{id}/download` | Download file |
| `POST` | `/v1/exports/{id}/retry` | Retry failed export |

</details>

<details>
<summary>Manufacturers & Policies</summary>

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/manufacturers` | List manufacturers |
| `POST` | `/v1/manufacturers` | Create manufacturer |
| `GET` | `/v1/manufacturers/{id}` | Manufacturer details |
| `GET` | `/v1/manufacturers/{id}/products` | Product catalog |
| `POST` | `/v1/manufacturers/{id}/products` | Add product |
| `GET` | `/v1/manufacturers/{id}/policies` | List policies |
| `POST` | `/v1/manufacturers/{id}/policies` | Create policy |
| `POST` | `/v1/policies/{id}/versions` | New policy version |
| `GET` | `/v1/policy-versions/{id}/rules` | Policy rules |
| `POST` | `/v1/policy-versions/{id}/rules` | Add rule |

</details>

<details>
<summary>Disputes, Cases, Webhooks & Pharmacies</summary>

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/disputes` | Open dispute |
| `POST` | `/v1/disputes/{id}/submit` | Submit dispute |
| `POST` | `/v1/disputes/{id}/withdraw` | Withdraw dispute |
| `POST` | `/v1/disputes/{id}/resolve` | Resolve dispute (admin) |
| `POST` | `/v1/organizations/{id}/cases` | Create exception case |
| `GET` | `/v1/organizations/{id}/cases` | List cases |
| `PATCH` | `/v1/organizations/{id}/cases/{caseId}` | Update case |
| `POST` | `/v1/organizations/{id}/cases/{caseId}/comments` | Add comment |
| `POST` | `/v1/organizations/{id}/webhooks` | Create webhook |
| `GET` | `/v1/organizations/{id}/webhooks` | List webhooks |
| `DELETE` | `/v1/organizations/{id}/webhooks/{webhookId}` | Delete webhook |
| `GET` | `/v1/organizations/{id}/webhooks/{webhookId}/deliveries` | Delivery history |
| `GET` | `/v1/contract-pharmacies` | List pharmacies |
| `POST` | `/v1/contract-pharmacies` | Add pharmacy |
| `PATCH` | `/v1/contract-pharmacies/{id}/status` | Update status |

</details>

<details>
<summary>SCIM 2.0</summary>

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/scim/v2/Users` | List users |
| `POST` | `/scim/v2/Users` | Create user |
| `GET` | `/scim/v2/Users/{id}` | Get user |
| `PATCH` | `/scim/v2/Users/{id}` | Modify user |
| `DELETE` | `/scim/v2/Users/{id}` | Deactivate user |
| `GET` | `/scim/v2/ServiceProviderConfig` | SCIM capabilities |
| `GET` | `/scim/v2/Schemas` | Schema list |
| `GET` | `/scim/v2/ResourceTypes` | Resource types |

</details>

<details>
<summary>Health & Metadata</summary>

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health` | Liveness probe |
| `GET` | `/metrics` | Prometheus metrics |
| `GET` | `/api/version` | API version info |

</details>

---

## Getting Started

### Prerequisites

- Go 1.25+
- Docker & Docker Compose
- `make`

### Environment Variables

Copy `.env.example` to `.env` and fill in the values:

```bash
cp .env.example .env
```

| Variable | Required | Description |
|----------|----------|-------------|
| `DATABASE_URL` | Yes | PostgreSQL connection string |
| `AMQP_URL` | Yes | RabbitMQ connection string |
| `JWT_SECRET` | Yes | ≥ 32-character random string — `openssl rand -hex 32` |
| `PORT` | No | API listen port (default `8080`) |
| `REDIS_URL` | No | Redis URL for production rate limiting |
| `ALLOWED_ORIGIN` | No | CORS allowed origin (e.g. `https://app.example.com`) |
| `WORKER_METRICS_PORT` | No | Worker metrics port (default `9091`) |
| `EXPORT_DIR` | No | Local export directory (default `./exports`) |
| `S3_EXPORT_BUCKET` | No | AWS S3 bucket for exports |
| `S3_EXPORT_PREFIX` | No | S3 object key prefix (default `reports/`) |
| `FEATURE_FLAGS` | No | Feature toggles (see [Feature Flags](#feature-flags)) |
| `ENVIRONMENT` | No | `production`, `staging`, or `development` |
| `REQUIRE_MFA_FOR_ADMIN` | No | Force MFA enrollment for admin accounts |
| `REQUIRE_MFA_STEP_UP_FOR_ADMIN` | No | Require MFA step-up token for admin writes |
| `API_REDACT_SENSITIVE_FIELDS` | No | Mask NPI/Rx in API responses (default `true`) |
| `EXPORT_REDACT_SENSITIVE_FIELDS` | No | Mask NPI/Rx in exports (default `true`) |
| `SMTP_HOST` | No | SMTP server host |
| `SMTP_PORT` | No | SMTP server port |
| `SMTP_USER` | No | SMTP username |
| `SMTP_PASSWORD` | No | SMTP password |
| `SMTP_FROM` | No | From address for system emails |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | No | OpenTelemetry collector endpoint |
| `OTEL_SERVICE_NAME` | No | Service name in traces (default `claims-system-api`) |
| `OIDC_REDIRECT_URL` | No | OIDC callback URL (must match IdP registration) |
| `SAML_SP_KEY_FILE` | No | Path to SAML SP private key (PEM) |
| `SAML_SP_CERT_FILE` | No | Path to SAML SP certificate (PEM) |
| `SAML_PUBLIC_BASE_URL` | No | Public API base URL for SAML ACS and metadata |
| `SCIM_BEARER_TOKEN` | No | Bearer token for SCIM 2.0 endpoints |
| `SCIM_ORG_ID` | No | Target organization UUID for SCIM provisioning |
| `CRYPTO_KEY` | No | Key for HMAC hashing PHI fields (Rx keys) |
| `JWT_SECRET_PREVIOUS` | No | Previous JWT secret for key rotation |

### Running Locally

```bash
# Start PostgreSQL, RabbitMQ, and Redis
make up

# Run database migrations
make migrate

# Start the API server (port 8080)
make api

# Start the worker process (separate terminal)
make worker
```

### Database Migrations

Migrations use [goose](https://github.com/pressly/goose). The schema files are in `sql/schema/` (001–028).

```bash
make migrate          # Apply all pending migrations
make migrate-down     # Roll back the last migration
make migrate-status   # Show migration status
```

### All Make Targets

```bash
make up               # Start local infrastructure (docker-compose)
make down             # Stop local infrastructure
make api              # Run API server
make worker           # Run worker process
make migrate          # Apply database migrations
make migrate-down     # Rollback last migration
make migrate-status   # Show migration status
make sqlc             # Regenerate database code from SQL queries
make build            # Compile binaries to bin/
make lint             # go vet ./...
make test             # Run tests with race detector
make integration-test # Run end-to-end tests against live infrastructure
make tidy             # go mod tidy
make check-migrations # Validate migration file numbering
make ci-check         # lint + test + sqlc + check-migrations
```

---

## Kubernetes Deployment

All manifests are in `k8s/`. The stack includes:

- **Deployments**: API server (2 replicas) and worker (2 replicas)
- **HPA**: Horizontal Pod Autoscaler for the API
- **PDB**: Pod Disruption Budget for availability guarantees
- **NetworkPolicy**: Restrict egress/ingress to required services
- **CronJob**: Scheduled audit log pruning
- **PVC**: Persistent volume for local export storage

**Setup:**

```bash
# Create namespace
kubectl apply -f k8s/namespace.yaml

# Apply ConfigMap (review and edit first)
kubectl apply -f k8s/configmap.yaml

# Populate and apply secrets (never commit k8s/secret.yaml)
cp k8s/secret.yaml.template k8s/secret.yaml
# Edit k8s/secret.yaml with your base64-encoded values
kubectl apply -f k8s/secret.yaml

# Apply remaining manifests
kubectl apply -f k8s/
```

The API is exposed via an Ingress with TLS termination. Set `ENVIRONMENT=production` to enforce HTTPS and AMQPS.

---

## Security

### Implemented Controls

- **Password hashing** — bcrypt with cost factor
- **JWT signing** — HMAC-SHA256 with configurable key rotation
- **MFA** — TOTP (RFC 6238) with backup codes
- **Account lockout** — blocks login after repeated failures
- **Rate limiting** — 10 req/min per IP (Redis or in-memory)
- **Tenant isolation** — org ID enforced on every request via middleware
- **CORS** — strict origin allowlist via `ALLOWED_ORIGIN`
- **Security headers** — applied by tracing middleware
- **Request size limit** — 5 MB max body
- **SQL injection prevention** — all queries use SQLC-generated parameterized statements
- **Webhook signatures** — HMAC-SHA256 delivery verification
- **CSV formula injection prevention** — cell sanitization on export
- **PHI field hashing** — Rx keys hashed via HMAC when `CRYPTO_KEY` is set
- **PII redaction** — NPI and Rx key masking in API responses and exports
- **Audit logging** — comprehensive trail for all sensitive operations
- **Graceful shutdown** — in-flight requests drained before process exit
- **Distroless containers** — minimal attack surface in Docker images
- **Kubernetes NetworkPolicy** — egress/ingress restrictions

### Production Checklist

- Generate `JWT_SECRET` with `openssl rand -hex 32` (minimum 32 characters)
- Set `ENVIRONMENT=production` (enforces HTTPS, AMQPS, disables dev-only endpoints)
- Set `REQUIRE_MFA_FOR_ADMIN=true` for admin accounts
- Configure `ALLOWED_ORIGIN` to your exact frontend origin
- Use a secrets manager (AWS Secrets Manager, Vault) for database credentials and SMTP passwords
- Enable TLS on PostgreSQL and RabbitMQ connections
- Store SAML SP keys as Kubernetes Secrets (not in the image)
- Review and restrict `k8s/networkpolicy.yaml` for your environment

---

## Project Structure

```
claims-system/
├── api/                    # REST API server (chi router, 50+ handlers)
│   ├── main.go             # Entry point, router setup, graceful shutdown
│   ├── openapi.yaml        # OpenAPI 3.0 spec (v0.5.0)
│   ├── handler_*.go        # Route handlers
│   ├── middleware*.go      # Auth, CORS, tracing, org isolation middleware
│   ├── auth.go             # JWT helpers, password hashing
│   └── ...
├── worker/                 # Async batch processing worker
│   └── main.go             # 4-stage pipeline: validate→normalize→reconcile→report
├── cmd/
│   ├── purge-audit/        # Audit log retention cleanup
│   └── rehash-rx-keys/     # PHI key rotation utility
├── internal/
│   ├── database/           # SQLC-generated database layer
│   ├── reconciliation/     # 340B claim matching engine
│   ├── policy/             # Manufacturer policy evaluation engine
│   ├── reporting/          # Excel/CSV report generation
│   ├── webhook/            # Webhook enqueue and signed delivery
│   ├── crypto/             # PHI field HMAC hashing
│   ├── storage/            # Local and S3 file storage
│   ├── sensitivity/        # PII redaction (NPI/Rx masking)
│   ├── feature/            # Feature flag resolver
│   ├── ratelimit/          # Redis/in-memory rate limiter
│   ├── email/              # SMTP mailer
│   ├── telemetry/          # OpenTelemetry tracer setup
│   └── scheduler/          # Background scheduler
├── sql/
│   ├── schema/             # 28 goose migration files (001–028)
│   └── query/              # SQLC query definitions
├── k8s/                    # Kubernetes manifests
├── docs/                   # Operator and integrator documentation
├── integration/            # End-to-end tests
├── scripts/                # Utility scripts
├── docker-compose.yml      # Local development stack
├── Makefile                # Development commands
├── .env.example            # Environment variable template
└── go.mod                  # Go module (go 1.25)
```

---

## License

This project is proprietary. All rights reserved.
