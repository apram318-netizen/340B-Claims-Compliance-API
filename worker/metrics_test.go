package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWorkerMetricsRenderIncludesFailureReasons(t *testing.T) {
	workerMetrics = newWorkerMetricsRegistry()
	workerInFlight.Store(2)

	workerMetrics.incReceived("validation")
	workerMetrics.incSuccess("validation")
	workerMetrics.incFailure("validation", "invalid_message")
	workerMetrics.addProcessingMS("validation", 17)

	out := workerMetrics.render()
	if !strings.Contains(out, "worker_jobs_failure_reason_total{stage=\"validation\",reason=\"invalid_message\"} 1") {
		t.Fatalf("missing failure reason metric: %s", out)
	}
	if !strings.Contains(out, "worker_jobs_in_flight 2") {
		t.Fatalf("missing in-flight metric: %s", out)
	}
}

func TestWorkerHealthAndReadinessEndpoints(t *testing.T) {
	mux := workerMetricsMux()

	healthReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthRec := httptest.NewRecorder()
	mux.ServeHTTP(healthRec, healthReq)
	if healthRec.Code != http.StatusOK {
		t.Fatalf("expected healthz 200, got %d", healthRec.Code)
	}

	workerReady.Store(false)
	notReadyReq := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	notReadyRec := httptest.NewRecorder()
	mux.ServeHTTP(notReadyRec, notReadyReq)
	if notReadyRec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected readyz 503, got %d", notReadyRec.Code)
	}

	workerReady.Store(true)
	readyReq := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	readyRec := httptest.NewRecorder()
	mux.ServeHTTP(readyRec, readyReq)
	if readyRec.Code != http.StatusOK {
		t.Fatalf("expected readyz 200, got %d", readyRec.Code)
	}
}
