package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

type workerMetricsRegistry struct {
	mu sync.Mutex

	received      map[string]int64
	success       map[string]int64
	failure       map[string]int64
	failureReason map[string]int64
	processingMS  map[string]int64
}

func newWorkerMetricsRegistry() *workerMetricsRegistry {
	return &workerMetricsRegistry{
		received:      map[string]int64{},
		success:       map[string]int64{},
		failure:       map[string]int64{},
		failureReason: map[string]int64{},
		processingMS:  map[string]int64{},
	}
}

var workerMetrics = newWorkerMetricsRegistry()
var workerInFlight atomic.Int64
var workerReady atomic.Bool

func (m *workerMetricsRegistry) incReceived(stage string) {
	m.mu.Lock()
	m.received[stage]++
	m.mu.Unlock()
}

func (m *workerMetricsRegistry) incSuccess(stage string) {
	m.mu.Lock()
	m.success[stage]++
	m.mu.Unlock()
}

func (m *workerMetricsRegistry) incFailure(stage, reason string) {
	m.mu.Lock()
	m.failure[stage]++
	if reason == "" {
		reason = "unknown"
	}
	m.failureReason[failureReasonKey(stage, reason)]++
	m.mu.Unlock()
}

func (m *workerMetricsRegistry) addProcessingMS(stage string, ms int64) {
	m.mu.Lock()
	m.processingMS[stage] += ms
	m.mu.Unlock()
}

func (m *workerMetricsRegistry) render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "worker_jobs_in_flight %d\n", workerInFlight.Load())

	emitLabeled := func(metric string, data map[string]int64) {
		keys := make([]string, 0, len(data))
		for k := range data {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, stage := range keys {
			fmt.Fprintf(&b, "%s{stage=\"%s\"} %d\n", metric, stage, data[stage])
		}
	}

	m.mu.Lock()
	emitLabeled("worker_jobs_received_total", m.received)
	emitLabeled("worker_jobs_success_total", m.success)
	emitLabeled("worker_jobs_failure_total", m.failure)
	emitFailureReasons(&b, m.failureReason)
	emitLabeled("worker_jobs_processing_ms_total", m.processingMS)
	m.mu.Unlock()

	return b.String()
}

func failureReasonKey(stage, reason string) string {
	return stage + "|" + reason
}

func splitFailureReasonKey(k string) (string, string) {
	parts := strings.SplitN(k, "|", 2)
	if len(parts) < 2 {
		return parts[0], "unknown"
	}
	return parts[0], parts[1]
}

func emitFailureReasons(b *strings.Builder, data map[string]int64) {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		stage, reason := splitFailureReasonKey(k)
		fmt.Fprintf(b, "worker_jobs_failure_reason_total{stage=\"%s\",reason=\"%s\"} %d\n", stage, reason, data[k])
	}
}

func startWorkerMetricsServer() {
	port := os.Getenv("WORKER_METRICS_PORT")
	if port == "" {
		port = "9091"
	}

	mux := workerMetricsMux()

	go func() {
		addr := ":" + port
		slog.Info("worker metrics server started", "addr", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			slog.Error("worker metrics server failed", "error", err)
		}
	}()
}

func workerMetricsMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(workerMetrics.render()))
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if !workerReady.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("not ready"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})
	return mux
}
