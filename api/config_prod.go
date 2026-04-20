package main

import (
	"log/slog"
	"os"
	"strings"

	"claims-system/internal/envx"
)

// validateAPIStartup enforces production guardrails before serving traffic.
func validateAPIStartup() {
	if p := strings.TrimSpace(os.Getenv("JWT_SECRET_PREVIOUS")); p != "" && len(p) < 32 {
		slog.Error("JWT_SECRET_PREVIOUS must be at least 32 characters when set")
		os.Exit(1)
	}
	if !envx.IsProduction() {
		return
	}
	dbURL := os.Getenv("DATABASE_URL")
	if !strings.Contains(dbURL, "sslmode=require") {
		slog.Error("production: DATABASE_URL must include sslmode=require")
		os.Exit(1)
	}
	if os.Getenv("REDIS_URL") == "" {
		slog.Warn("production: REDIS_URL not set — distributed rate limits and JWT revocation lists are disabled")
	}
	if !passwordResetExposeToken() {
		if strings.TrimSpace(os.Getenv("EMAIL_SMTP_HOST")) == "" {
			slog.Error("production: configure EMAIL_SMTP_HOST (and related vars) or set PASSWORD_RESET_EXPOSE_TOKEN for non-production testing only")
			os.Exit(1)
		}
		if strings.TrimSpace(os.Getenv("EMAIL_FROM")) == "" {
			slog.Error("production: EMAIL_FROM is required when SMTP is configured")
			os.Exit(1)
		}
	}
}
