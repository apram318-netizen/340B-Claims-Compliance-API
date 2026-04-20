# Backend Ops Runbook

## Audit event retention purge

- Schema defines `expires_at` on `audit_events` (HIPAA-style retention window).
- Run the purge job on a schedule (weekly is a reasonable default):
  - Binary: `go run ./cmd/purge-audit` with `DATABASE_URL` set (exits 0 and logs rows deleted).
  - Kubernetes: apply [`k8s/cronjob-audit-purge.yaml`](../k8s/cronjob-audit-purge.yaml) after building `Dockerfile.purge-audit` and pushing the image.
- Monitor logs for `audit_purge_completed` (JSON field `rows_deleted`) and alert on non-zero exit.

## PHI `hashed_rx_key` re-hash (key rotation)

- After changing `PHI_ENCRYPTION_KEY`, run `go run ./cmd/rehash-rx-keys -dry-run` against a staging snapshot first, then without `-dry-run` during a maintenance window with `DATABASE_URL` set.
- The job updates `claims.hashed_rx_key` from the CSV-derived value in `upload_rows.data` → `hashed_rx_key`. It does not update `rebate_records`.

## MFA step-up for admins

- Set `REQUIRE_MFA_STEP_UP_FOR_ADMIN=true` when you want MFA-enrolled admins to prove possession of a second factor again before sensitive mutations. Clients call `POST /v1/mfa/step-up`, then pass `X-MFA-Step-Up: <step_up_token>` together with `Authorization: Bearer <jwt>` on admin POST/PATCH routes that require it.

## Export Recovery

- Inspect run state: `GET /v1/exports/{id}`
- If `status` is `failed` or `pending`, requeue: `POST /v1/exports/{id}/retry`
- Download completed artifact: `GET /v1/exports/{id}/download`

## Runtime Metrics

- Fetch API metrics: `GET /metrics`
- Fetch worker metrics: `GET http://<worker-host>:9091/metrics`
- Worker probes:
  - `GET http://<worker-host>:9091/healthz`
  - `GET http://<worker-host>:9091/readyz`
- Key counters:
  - `http_requests_total`
  - `http_responses_total{status="..."}`
  - `export_runs_requested_total`
  - `export_runs_downloaded_total`
  - `export_runs_retried_total`
  - `manual_overrides_total`
  - `password_reset_requested_total`, `password_reset_emailed_total`, `password_reset_email_failed_total`
  - `mfa_failures_total`
  - `worker_jobs_received_total{stage="..."}`
  - `worker_jobs_success_total{stage="..."}`
  - `worker_jobs_failure_total{stage="..."}`
  - `worker_jobs_failure_reason_total{stage="...",reason="..."}`
  - `worker_jobs_processing_ms_total{stage="..."}`

## Common Failure Modes

- **Export not ready**: API returns `409` on download until run is `completed`.
- **File missing**: API returns `404` if DB row points to a non-existent artifact.
- **Unauthorized tenant access**: non-admin users are restricted to their own org exports.
- **Broker interruption**: worker reconnect loop should re-establish AMQP sessions with backoff.

## RBAC Baseline

- `admin` required for:
  - `POST /v1/organizations`
  - `POST /v1/manufacturers`
  - `POST /v1/manufacturers/{id}/products`
  - `POST /v1/manufacturers/{id}/policies`
  - `POST /v1/policies/{id}/versions`
  - `POST /v1/policy-versions/{id}/rules`
  - `POST /v1/rebate-records`
- `member` can:
  - upload and read own-org batches/claims/decisions
  - create and read own-org exports
  - submit overrides within org scope

## Deployment Guardrails

- **AMQP TLS:** When `ENVIRONMENT=production` or `REQUIRE_AMQPS=true`, API and worker refuse to start unless `AMQP_URL` uses the `amqps://` scheme.
- CI quality gate:
  - `go vet ./...`
  - `go test ./... -race -count=1`
  - `sqlc generate` + no generated diff
  - migration ordering check (`./scripts/check-migrations.sh`)
  - integration test (`go test -tags=integration ./integration/...`)
- Always deploy with a reversible migration strategy (expand -> migrate app -> contract).

## Rollback Plan

1. Detect elevated 5xx, queue backlog growth, or worker failure spikes.
2. Halt rollout and route traffic to previous stable API/worker versions.
3. If schema changed, execute only pre-approved reversible migration rollback steps.
4. Validate:
   - `/health` and `/metrics` stabilize
   - worker `/readyz` returns 200
   - queue depth returns to baseline
5. Record timeline and RCA notes.

## Incident Drill Checklist

- [ ] Simulate RabbitMQ restart and verify worker recovery.
- [ ] Simulate API restart and verify readiness gating.
- [ ] Validate export retry flow for failed report generation.
- [ ] Run backup restore verification in staging.
