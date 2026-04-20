// Package feature resolves feature flags from environment defaults and per-org DB overrides.
package feature

import (
	"context"
	"os"
	"strings"

	"claims-system/internal/database"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Known flags (document in CHANGELOG / docs).
const (
	Webhooks       = "webhooks"
	ExceptionCases = "exception_cases"
	SCIM           = "scim"
	OIDCSSO        = "oidc_sso"
	SAMLSSO        = "saml_sso"
)

// Resolver merges FEATURE_FLAGS env (key=value comma-separated) with feature_flag_overrides.
type Resolver struct {
	DB     database.Store
	Global map[string]bool // from env; defaults true when key not set
}

func NewResolver(db database.Store) *Resolver {
	return &Resolver{DB: db, Global: parseFeatureEnv()}
}

func parseFeatureEnv() map[string]bool {
	m := map[string]bool{
		Webhooks:       true,
		ExceptionCases: true,
		SCIM:           false,
		OIDCSSO:        true,
		SAMLSSO:        false,
	}
	raw := strings.TrimSpace(os.Getenv("FEATURE_FLAGS"))
	if raw == "" {
		return m
	}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "yes", "on":
			m[k] = true
		case "false", "0", "no", "off":
			m[k] = false
		}
	}
	return m
}

// GlobalSnapshot returns a copy of env-based defaults for API responses.
func (r *Resolver) GlobalSnapshot() map[string]bool {
	out := make(map[string]bool, len(r.Global))
	for k, v := range r.Global {
		out[k] = v
	}
	return out
}

// Enabled returns true if the flag is on for this org (DB override wins over global env default).
func (r *Resolver) Enabled(ctx context.Context, orgID uuid.UUID, key string) bool {
	base := r.Global[key]
	if r.DB == nil {
		return base
	}
	row, err := r.DB.GetFeatureFlagOverride(ctx, database.GetFeatureFlagOverrideParams{
		OrgID:   orgID,
		FlagKey: key,
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			return base
		}
		return base
	}
	return row.Value
}
