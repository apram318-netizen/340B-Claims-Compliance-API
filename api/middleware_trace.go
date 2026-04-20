package main

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel/trace"
)

// middlewareTrace ensures X-Trace-Id (prefers W3C trace from OpenTelemetry when present), Request-ID, or a new id.
func middlewareTrace(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tid := strings.TrimSpace(r.Header.Get("X-Trace-Id"))
		if sc := trace.SpanContextFromContext(r.Context()); sc.IsValid() {
			tid = sc.TraceID().String()
		}
		if tid == "" {
			tid = middleware.GetReqID(r.Context())
		}
		if tid == "" {
			tid = newTraceID()
		}
		ctx := WithTraceID(r.Context(), tid)
		w.Header().Set("X-Trace-Id", tid)
		w.Header().Set("X-API-Version", "1")
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
