package main

import (
	"claims-system/internal/database"
	"claims-system/internal/sensitivity"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (apiCfg *apiConfig) handlerGetBatchClaims(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid batch id")
		return
	}

	// Verify batch belongs to the caller's org
	batch, err := apiCfg.DB.GetUploadBatches(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "batch not found")
			return
		}
		slog.Error("get batch failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	userID, _ := getUserId(r)
	user, err := apiCfg.DB.GetUserByID(r.Context(), userID)
	if err != nil || user.OrgID != batch.OrgID {
		respondWithError(w, http.StatusForbidden, "access denied")
		return
	}

	limit, offset, ok := parsePagination(r)
	if !ok {
		respondWithValidationIssues(w, r, "invalid pagination params", []ValidationIssue{
			{Field: "limit/offset", Message: "must be positive integers; offset must be >= 0"},
		})
		return
	}

	total, err := apiCfg.DB.CountClaimsByBatch(r.Context(), id)
	if err != nil {
		slog.Error("count claims failed", "batch_id", id, "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	claims, err := apiCfg.DB.GetClaimsByBatchPaginated(r.Context(), database.GetClaimsByBatchPaginatedParams{
		BatchID: id,
		Limit:   int32(limit),
		Offset:  int32(offset),
	})
	if err != nil {
		slog.Error("get claims failed", "batch_id", id, "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	type claimResponse struct {
		ID          uuid.UUID `json:"id"`
		BatchID     uuid.UUID `json:"batch_id"`
		OrgID       uuid.UUID `json:"org_id"`
		NDC         string    `json:"ndc"`
		PharmacyNPI string    `json:"pharmacy_npi"`
		Quantity    int32     `json:"quantity"`
		Status      string    `json:"status"`
	}

	out := make([]claimResponse, 0, len(claims))
	for _, c := range claims {
		npi := c.PharmacyNpi
		if sensitivity.RedactAPIFields() {
			npi = sensitivity.MaskNPI(npi)
		}
		out = append(out, claimResponse{
			ID:          c.ID,
			BatchID:     c.BatchID,
			OrgID:       c.OrgID,
			NDC:         c.Ndc,
			PharmacyNPI: npi,
			Quantity:    c.Quantity,
			Status:      c.Status,
		})
	}

	applyPaginationHeaders(w, total, limit, offset)
	respondWithJSON(w, http.StatusOK, out)
}

func (apiCfg *apiConfig) handlerGetClaim(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid claim id")
		return
	}

	claim, err := apiCfg.DB.GetClaimByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "claim not found")
			return
		}
		slog.Error("get claim failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Tenant isolation
	userID, _ := getUserId(r)
	user, err := apiCfg.DB.GetUserByID(r.Context(), userID)
	if err != nil || user.OrgID != claim.OrgID {
		respondWithError(w, http.StatusForbidden, "access denied")
		return
	}

	respondWithJSON(w, http.StatusOK, claimToJSON(claim))
}

func (apiCfg *apiConfig) handlerGetAuditEvents(w http.ResponseWriter, r *http.Request) {
	entityType := r.URL.Query().Get("entity_type")
	entityID := r.URL.Query().Get("entity_id")

	if entityType == "" || entityID == "" {
		issues := make([]ValidationIssue, 0, 2)
		if entityType == "" {
			issues = append(issues, ValidationIssue{Field: "entity_type", Message: "is required"})
		}
		if entityID == "" {
			issues = append(issues, ValidationIssue{Field: "entity_id", Message: "is required"})
		}
		respondWithValidationIssues(w, r, "missing required query params", issues)
		return
	}

	limit, offset, ok := parsePagination(r)
	if !ok {
		respondWithValidationIssues(w, r, "invalid pagination params", []ValidationIssue{
			{Field: "limit/offset", Message: "must be positive integers; offset must be >= 0"},
		})
		return
	}
	total, err := apiCfg.DB.CountAuditEventsByEntity(r.Context(), database.CountAuditEventsByEntityParams{
		EntityType: entityType,
		EntityID:   entityID,
	})
	if err != nil {
		slog.Error("count audit events failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	events, err := apiCfg.DB.GetAuditEventsByEntityPaginated(r.Context(), database.GetAuditEventsByEntityPaginatedParams{
		EntityType: entityType,
		EntityID:   entityID,
		Limit:      int32(limit),
		Offset:     int32(offset),
	})
	if err != nil {
		slog.Error("get audit events failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	applyPaginationHeaders(w, total, limit, offset)
	respondWithJSON(w, http.StatusOK, events)
}
