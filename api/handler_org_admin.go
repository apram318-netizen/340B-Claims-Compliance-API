package main

import (
	"claims-system/internal/database"
	"claims-system/internal/feature"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// GET /v1/organizations/{id}/settings
func (apiCfg *apiConfig) handlerGetOrganizationSettings(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid organization id")
		return
	}
	if !requireOrgAdmin(w, r, orgID) {
		return
	}
	row, err := apiCfg.DB.GetOrganizationSettings(r.Context(), orgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondWithJSON(w, http.StatusOK, map[string]json.RawMessage{"settings": json.RawMessage(`{}`)})
			return
		}
		slog.Error("get org settings", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	respondWithJSON(w, http.StatusOK, map[string]json.RawMessage{"settings": json.RawMessage(row.Settings)})
}

// PATCH /v1/organizations/{id}/settings — body { "settings": { ... } } merged server-side (replace document)
func (apiCfg *apiConfig) handlerPatchOrganizationSettings(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid organization id")
		return
	}
	if !requireOrgAdmin(w, r, orgID) {
		return
	}
	var body struct {
		Settings json.RawMessage `json:"settings"`
	}
	if issues := decodeJSONStrict(r, &body); len(issues) > 0 {
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}
	if len(body.Settings) == 0 {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{{Field: "settings", Message: "is required"}})
		return
	}
	row, err := apiCfg.DB.UpsertOrganizationSettings(r.Context(), database.UpsertOrganizationSettingsParams{
		OrgID:    orgID,
		Settings: []byte(body.Settings),
	})
	if err != nil {
		slog.Error("upsert org settings", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	actorID, _ := getUserId(r)
	apiCfg.recordAuditEventBestEffort(r, "organization_settings_updated", "organization", orgID.String(), actorID, map[string]string{
		"ip": clientIP(r),
	})
	respondWithJSON(w, http.StatusOK, map[string]json.RawMessage{"settings": row.Settings})
}

// GET /v1/organizations/{id}/users
func (apiCfg *apiConfig) handlerListOrgUsers(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid organization id")
		return
	}
	if !requireOrgAdmin(w, r, orgID) {
		return
	}
	users, err := apiCfg.DB.ListUsersByOrg(r.Context(), orgID)
	if err != nil {
		slog.Error("list org users", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]User, 0, len(users))
	for _, u := range users {
		out = append(out, databaseUserToUser(u))
	}
	respondWithJSON(w, http.StatusOK, map[string]any{"users": out})
}

// PATCH /v1/organizations/{id}/users/{userId} — { "role": "member"|"admin"|"viewer" }
func (apiCfg *apiConfig) handlerPatchOrgUser(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid organization id")
		return
	}
	userID, err := uuid.Parse(chi.URLParam(r, "userId"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	if !requireOrgAdmin(w, r, orgID) {
		return
	}
	var body struct {
		Role string `json:"role"`
	}
	if issues := decodeJSONStrict(r, &body); len(issues) > 0 {
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}
	role := strings.ToLower(strings.TrimSpace(body.Role))
	switch role {
	case "member", "admin", "viewer":
	default:
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{{Field: "role", Message: "must be member, admin, or viewer"}})
		return
	}
	updated, err := apiCfg.DB.UpdateUserRole(r.Context(), database.UpdateUserRoleParams{
		ID:    userID,
		Role:  role,
		OrgID: orgID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "user not found")
			return
		}
		slog.Error("update user role", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	apiCfg.recordAuditEventBestEffort(r, "user_role_updated", "user", userID.String(), userID, map[string]string{
		"ip": clientIP(r), "role": role,
	})
	respondWithJSON(w, http.StatusOK, databaseUserToUser(updated))
}

// GET /v1/organizations/{id}/feature-flags
func (apiCfg *apiConfig) handlerListOrgFeatureFlags(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid organization id")
		return
	}
	if !requireOrgAdmin(w, r, orgID) {
		return
	}
	if apiCfg.Features == nil {
		respondWithJSON(w, http.StatusOK, map[string]any{"flags": map[string]bool{}, "overrides": []any{}})
		return
	}
	rows, err := apiCfg.DB.ListFeatureFlagOverridesByOrg(r.Context(), orgID)
	if err != nil {
		slog.Error("list feature flags", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	res := apiCfg.Features.GlobalSnapshot()
	for _, row := range rows {
		res[row.FlagKey] = row.Value
	}
	respondWithJSON(w, http.StatusOK, map[string]any{"flags": res, "overrides": rows})
}

// PUT /v1/organizations/{id}/feature-flags/{key} — { "value": true|false }
func (apiCfg *apiConfig) handlerPutOrgFeatureFlag(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid organization id")
		return
	}
	key := strings.TrimSpace(chi.URLParam(r, "key"))
	if key == "" {
		respondWithError(w, http.StatusBadRequest, "invalid flag key")
		return
	}
	if !requireOrgAdmin(w, r, orgID) {
		return
	}
	var body struct {
		Value bool `json:"value"`
	}
	if issues := decodeJSONStrict(r, &body); len(issues) > 0 {
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}
	switch key {
	case feature.Webhooks, feature.ExceptionCases, feature.SCIM, feature.OIDCSSO, feature.SAMLSSO:
	default:
		respondWithError(w, http.StatusBadRequest, "unknown feature flag key")
		return
	}
	row, err := apiCfg.DB.UpsertFeatureFlagOverride(r.Context(), database.UpsertFeatureFlagOverrideParams{
		OrgID:   orgID,
		FlagKey: key,
		Value:   body.Value,
	})
	if err != nil {
		slog.Error("upsert feature flag", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	respondWithJSON(w, http.StatusOK, row)
}
