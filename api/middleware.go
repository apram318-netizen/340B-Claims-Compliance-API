package main

import (
	"claims-system/internal/ratelimit"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// corsMiddleware sets permissive CORS headers for development.
// In production, set ALLOWED_ORIGIN to the specific frontend domain.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := os.Getenv("ALLOWED_ORIGIN")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-Id, X-Trace-Id, X-API-Version, X-MFA-Step-Up")
			w.Header().Set("Access-Control-Max-Age", "86400")
		}

		if r.Method == http.MethodOptions {
			if origin == "" {
				respondWithError(w, http.StatusForbidden, "cors preflight disabled until ALLOWED_ORIGIN is configured")
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		w.Header().Set("Cache-Control", "no-store")
		// HSTS: tell browsers to only contact this service over HTTPS for 1 year.
		// Include subdomains so no sub-domain can be used as a downgrade vector.
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")
		next.ServeHTTP(w, r)
	})
}

// requestLogger emits a structured log line for every request.
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		inFlight := appMetrics.requestsInFlight.Add(1)
		appMetrics.setInFlight(inFlight)
		defer func() {
			now := appMetrics.requestsInFlight.Add(-1)
			appMetrics.setInFlight(now)
		}()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		next.ServeHTTP(ww, r)
		durationMS := time.Since(start).Milliseconds()
		appMetrics.observeRequest(ww.Status(), durationMS)

		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"duration_ms", durationMS,
			"request_id", middleware.GetReqID(r.Context()),
			"trace_id", traceIDFromContext(r.Context()),
		)
	})
}

// clientIP extracts just the host part of the remote address.
// chi's RealIP middleware already rewrites r.RemoteAddr from X-Forwarded-For /
// X-Real-IP, so this gives us the true client address without the port.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr // fallback: already a bare IP or unparseable
	}
	return host
}

// newLoginRateLimiter returns a per-IP rate-limit middleware backed by the
// provided ratelimit.Limiter (Redis in production, in-memory for tests).
func newLoginRateLimiter(lim ratelimit.Limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !lim.Allow(r.Context(), "login:"+clientIP(r)) {
				respondWithError(w, http.StatusTooManyRequests, "too many login attempts, please try again later")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
