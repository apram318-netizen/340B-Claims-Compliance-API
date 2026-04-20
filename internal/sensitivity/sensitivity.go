// Package sensitivity centralizes redaction policy for API responses and CSV exports.
//
// Environment:
//   - API_REDACT_SENSITIVE_FIELDS: default true; set false/0/no for local debugging only.
//   - EXPORT_REDACT_SENSITIVE_FIELDS: default true; set false to include full NPI etc. in CSV exports.
package sensitivity

import (
	"os"
	"strings"
)

func redactEnv(key string, defaultRedact bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if v == "" {
		return defaultRedact
	}
	switch v {
	case "false", "0", "no", "off":
		return false
	default:
		return true
	}
}

// RedactAPIFields controls JSON field masking for claims and related handlers.
func RedactAPIFields() bool {
	return redactEnv("API_REDACT_SENSITIVE_FIELDS", true)
}

// RedactExportFields controls CSV/report column masking.
func RedactExportFields() bool {
	return redactEnv("EXPORT_REDACT_SENSITIVE_FIELDS", true)
}

// MaskNPI keeps only the last 4 characters for display/export when redacting.
func MaskNPI(npi string) string {
	npi = strings.TrimSpace(npi)
	if len(npi) <= 4 {
		if npi == "" {
			return ""
		}
		return "****"
	}
	return "****" + npi[len(npi)-4:]
}

// RedactHashPlaceholder replaces stored keyed-hash or token-like values in API/CSV.
func RedactHashPlaceholder(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return "[redacted]"
}
