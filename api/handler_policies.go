package main

import (
	"claims-system/internal/database"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// POST /v1/manufacturers/{id}/policies
func (apiCfg *apiConfig) handlerCreatePolicy(w http.ResponseWriter, r *http.Request) {
	manufacturerID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithValidationIssues(w, r, "invalid path params", []ValidationIssue{
			{Field: "id", Message: "must be a valid manufacturer UUID"},
		})
		return
	}

	type requestBody struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}

	var body requestBody
	if issues := decodeJSONStrict(r, &body); len(issues) > 0 {
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}
	if body.Name == "" {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
			{Field: "name", Message: "is required"},
		})
		return
	}
	if body.Status == "" {
		body.Status = "active"
	}

	p, err := apiCfg.DB.CreatePolicy(r.Context(), database.CreatePolicyParams{
		ManufacturerID: manufacturerID,
		Name:           body.Name,
		Status:         body.Status,
	})
	if err != nil {
		slog.Error("create policy failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "failed to create policy")
		return
	}

	respondWithJSON(w, http.StatusCreated, p)
}

// GET /v1/manufacturers/{id}/policies
func (apiCfg *apiConfig) handlerListPolicies(w http.ResponseWriter, r *http.Request) {
	manufacturerID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid manufacturer id")
		return
	}

	limit, offset, ok := parsePagination(r)
	if !ok {
		respondWithValidationIssues(w, r, "invalid pagination params", []ValidationIssue{
			{Field: "limit/offset", Message: "must be positive integers; offset must be >= 0"},
		})
		return
	}

	total, err := apiCfg.DB.CountPoliciesByManufacturer(r.Context(), manufacturerID)
	if err != nil {
		slog.Error("count policies failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	policies, err := apiCfg.DB.ListPoliciesByManufacturerPaginated(r.Context(), database.ListPoliciesByManufacturerPaginatedParams{
		ManufacturerID: manufacturerID,
		Limit:          int32(limit),
		Offset:         int32(offset),
	})
	if err != nil {
		slog.Error("list policies failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	applyPaginationHeaders(w, total, limit, offset)
	respondWithJSON(w, http.StatusOK, policies)
}

// POST /v1/policies/{id}/versions
func (apiCfg *apiConfig) handlerCreatePolicyVersion(w http.ResponseWriter, r *http.Request) {
	policyID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithValidationIssues(w, r, "invalid path params", []ValidationIssue{
			{Field: "id", Message: "must be a valid policy UUID"},
		})
		return
	}

	type requestBody struct {
		VersionNumber int32  `json:"version_number"`
		EffectiveFrom string `json:"effective_from"` // YYYY-MM-DD
		EffectiveTo   string `json:"effective_to"`   // YYYY-MM-DD, optional
	}

	var body requestBody
	if issues := decodeJSONStrict(r, &body); len(issues) > 0 {
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}
	if body.EffectiveFrom == "" {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
			{Field: "effective_from", Message: "is required and must be YYYY-MM-DD"},
		})
		return
	}

	effectiveFrom, err := time.Parse("2006-01-02", body.EffectiveFrom)
	if err != nil {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
			{Field: "effective_from", Message: "must be YYYY-MM-DD"},
		})
		return
	}

	var effectiveTo pgtype.Date
	if body.EffectiveTo != "" {
		t, err := time.Parse("2006-01-02", body.EffectiveTo)
		if err != nil {
			respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
				{Field: "effective_to", Message: "must be YYYY-MM-DD"},
			})
			return
		}
		effectiveTo = pgtype.Date{Time: t, Valid: true}
	}

	pv, err := apiCfg.DB.CreatePolicyVersion(r.Context(), database.CreatePolicyVersionParams{
		PolicyID:      policyID,
		VersionNumber: body.VersionNumber,
		EffectiveFrom: pgtype.Date{Time: effectiveFrom, Valid: true},
		EffectiveTo:   effectiveTo,
	})
	if err != nil {
		slog.Error("create policy version failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "failed to create policy version")
		return
	}

	respondWithJSON(w, http.StatusCreated, pv)
}

// POST /v1/policy-versions/{id}/rules
func (apiCfg *apiConfig) handlerCreatePolicyRule(w http.ResponseWriter, r *http.Request) {
	versionID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithValidationIssues(w, r, "invalid path params", []ValidationIssue{
			{Field: "id", Message: "must be a valid policy version UUID"},
		})
		return
	}

	// Validate the version exists
	if _, err := apiCfg.DB.GetPolicyVersionByID(r.Context(), versionID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "policy version not found")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	type requestBody struct {
		RuleType   string          `json:"rule_type"`
		RuleConfig json.RawMessage `json:"rule_config"`
	}

	var body requestBody
	if issues := decodeJSONStrict(r, &body); len(issues) > 0 {
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}
	if body.RuleType == "" {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
			{Field: "rule_type", Message: "is required"},
		})
		return
	}
	if len(body.RuleConfig) == 0 {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
			{Field: "rule_config", Message: "is required"},
		})
		return
	}

	rule, err := apiCfg.DB.CreatePolicyRule(r.Context(), database.CreatePolicyRuleParams{
		PolicyVersionID: versionID,
		RuleType:        body.RuleType,
		RuleConfig:      []byte(body.RuleConfig),
	})
	if err != nil {
		slog.Error("create policy rule failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "failed to create policy rule")
		return
	}

	respondWithJSON(w, http.StatusCreated, rule)
}

// GET /v1/policy-versions/{id}/rules
func (apiCfg *apiConfig) handlerListPolicyRules(w http.ResponseWriter, r *http.Request) {
	versionID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid version id")
		return
	}

	limit, offset, ok := parsePagination(r)
	if !ok {
		respondWithValidationIssues(w, r, "invalid pagination params", []ValidationIssue{
			{Field: "limit/offset", Message: "must be positive integers; offset must be >= 0"},
		})
		return
	}

	total, err := apiCfg.DB.CountRulesByPolicyVersion(r.Context(), versionID)
	if err != nil {
		slog.Error("count policy rules failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	rules, err := apiCfg.DB.GetRulesByPolicyVersionPaginated(r.Context(), database.GetRulesByPolicyVersionPaginatedParams{
		PolicyVersionID: versionID,
		Limit:           int32(limit),
		Offset:          int32(offset),
	})
	if err != nil {
		slog.Error("list policy rules failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	applyPaginationHeaders(w, total, limit, offset)
	respondWithJSON(w, http.StatusOK, rules)
}
