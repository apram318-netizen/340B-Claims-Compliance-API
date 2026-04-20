package main

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

type metricsRegistry struct {
	requestsTotal     atomic.Int64
	requestsInFlight  atomic.Int64
	requestDurationMS atomic.Int64
	exportRequested   atomic.Int64
	exportDownloaded  atomic.Int64
	exportRetried     atomic.Int64
	manualOverrides   atomic.Int64

	passwordResetRequested  atomic.Int64
	passwordResetEmailed    atomic.Int64
	passwordResetEmailFailed atomic.Int64
	mfaFailures             atomic.Int64
	webhookDeliveriesSucceeded atomic.Int64
	webhookDeliveriesFailed    atomic.Int64
	webhookRetries             atomic.Int64

	mu           sync.Mutex
	statusCounts map[int]int64
}

func newMetricsRegistry() *metricsRegistry {
	return &metricsRegistry{
		statusCounts: map[int]int64{},
	}
}

var appMetrics = newMetricsRegistry()

func (m *metricsRegistry) observeRequest(status int, durationMS int64) {
	m.requestsTotal.Add(1)
	m.requestDurationMS.Add(durationMS)

	m.mu.Lock()
	m.statusCounts[status]++
	m.mu.Unlock()
}

func (m *metricsRegistry) setInFlight(v int64) {
	m.requestsInFlight.Store(v)
}

func (m *metricsRegistry) metricsText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "http_requests_total %d\n", m.requestsTotal.Load())
	fmt.Fprintf(&b, "http_requests_in_flight %d\n", m.requestsInFlight.Load())
	fmt.Fprintf(&b, "http_request_duration_ms_total %d\n", m.requestDurationMS.Load())
	fmt.Fprintf(&b, "export_runs_requested_total %d\n", m.exportRequested.Load())
	fmt.Fprintf(&b, "export_runs_downloaded_total %d\n", m.exportDownloaded.Load())
	fmt.Fprintf(&b, "export_runs_retried_total %d\n", m.exportRetried.Load())
	fmt.Fprintf(&b, "manual_overrides_total %d\n", m.manualOverrides.Load())
	fmt.Fprintf(&b, "password_reset_requested_total %d\n", m.passwordResetRequested.Load())
	fmt.Fprintf(&b, "password_reset_emailed_total %d\n", m.passwordResetEmailed.Load())
	fmt.Fprintf(&b, "password_reset_email_failed_total %d\n", m.passwordResetEmailFailed.Load())
	fmt.Fprintf(&b, "mfa_failures_total %d\n", m.mfaFailures.Load())
	fmt.Fprintf(&b, "webhook_deliveries_succeeded_total %d\n", m.webhookDeliveriesSucceeded.Load())
	fmt.Fprintf(&b, "webhook_deliveries_failed_total %d\n", m.webhookDeliveriesFailed.Load())
	fmt.Fprintf(&b, "webhook_retries_total %d\n", m.webhookRetries.Load())

	m.mu.Lock()
	statuses := make([]int, 0, len(m.statusCounts))
	for s := range m.statusCounts {
		statuses = append(statuses, s)
	}
	sort.Ints(statuses)
	for _, s := range statuses {
		fmt.Fprintf(&b, "http_responses_total{status=\"%d\"} %d\n", s, m.statusCounts[s])
	}
	m.mu.Unlock()

	return b.String()
}

func handlerMetrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(appMetrics.metricsText()))
}
