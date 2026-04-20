# Performance and SLO Validation Guide

## Goals

- Ensure API latency remains stable under sustained load.
- Verify worker pipeline keeps up with queue throughput.
- Detect DB or RabbitMQ saturation before production rollout.

## Initial SLO Targets (backend-only)

- API availability: `>= 99.9%`
- API p95 latency (authenticated reads): `< 800ms`
- API p99 latency (authenticated reads): `< 1500ms`
- Error rate (5xx): `< 0.5%`
- Queue lag for hot queues (`batch_validation`, `batch_reconciliation`): `< 2 minutes` under nominal load
- Export generation p95 completion: `< 5 minutes` for standard org-scoped report windows

## Tooling

- Script: `load/k6-pipeline-smoke.js`
- Metrics endpoints:
  - API: `GET /metrics`
  - Worker: `GET http://<worker-host>:9091/metrics`

## Smoke Run

```bash
k6 run \
  -e BASE_URL=http://localhost:8080 \
  -e AUTH_TOKEN="<jwt>" \
  -e BATCH_ID="<batch_uuid>" \
  -e EXPORT_ID="<export_uuid>" \
  load/k6-pipeline-smoke.js
```

## Saturation Validation Plan

1. Run baseline test at 5-20 VUs and record latency + failure rates.
2. Increase to 50/100 VUs in steps and monitor:
   - `http_requests_total`, `http_responses_total`
   - `worker_jobs_in_flight`
   - `worker_jobs_failure_total`
   - PostgreSQL CPU/IO, connection count
   - RabbitMQ queue depth and consumer ack rates
3. Identify first bottleneck and document capacity limit.
4. Re-run after optimization and compare deltas.

## Exit Criteria Before Go-Live

- All SLO targets above are met for at least 3 consecutive runs.
- No sustained queue growth after the warm-up period.
- No data integrity errors (missing jobs, duplicate processing, dropped exports).
