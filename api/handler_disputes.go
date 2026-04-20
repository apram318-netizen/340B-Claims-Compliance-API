package main

import (
	"claims-system/internal/database"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// POST /v1/disputes
func (apiCfg *apiConfig) handlerCreateDispute(w http.ResponseWriter, r *http.Request) {
	type request struct {
		ClaimID        string `json:"claim_id"`
		ManufacturerID string `json:"manufacturer_id"`
		ReasonCode     string `json:"reason_code"`
		Description    string `json:"description"`
	}

	var req request
	if issues := decodeJSONStrict(r, &req); len(issues) > 0 {
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}

	var issues []ValidationIssue
	if req.ClaimID == "" {
		issues = append(issues, ValidationIssue{Field: "claim_id", Message: "is required"})
	}
	if req.ManufacturerID == "" {
		issues = append(issues, ValidationIssue{Field: "manufacturer_id", Message: "is required"})
	}
	if req.ReasonCode == "" {
		issues = append(issues, ValidationIssue{Field: "reason_code", Message: "is required"})
	}
	if req.Description == "" {
		issues = append(issues, ValidationIssue{Field: "description", Message: "is required"})
	}
	if len(issues) > 0 {
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}

	validReasonCodes := map[string]bool{
		"duplicate_discount": true, "eligibility": true,
		"pricing": true, "incorrect_ndc": true, "other": true,
	}
	if !validReasonCodes[req.ReasonCode] {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
			{Field: "reason_code", Message: "must be one of duplicate_discount, eligibility, pricing, incorrect_ndc, other"},
		})
		return
	}

	claimID, err := uuid.Parse(req.ClaimID)
	if err != nil {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
			{Field: "claim_id", Message: "must be a valid UUID"},
		})
		return
	}
	mfgID, err := uuid.Parse(req.ManufacturerID)
	if err != nil {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
			{Field: "manufacturer_id", Message: "must be a valid UUID"},
		})
		return
	}

	authUser, err := getAuthUser(r)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Verify claim belongs to user's org
	claim, err := apiCfg.DB.GetClaimByID(r.Context(), claimID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "claim not found")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !isAdminRole(authUser.Role) && authUser.OrgID != claim.OrgID {
		respondWithError(w, http.StatusForbidden, "access denied")
		return
	}

	dispute, err := apiCfg.DB.CreateDispute(r.Context(), database.CreateDisputeParams{
		ClaimID:        claimID,
		OrgID:          claim.OrgID,
		ManufacturerID: mfgID,
		ReasonCode:     req.ReasonCode,
		Description:    req.Description,
		OpenedBy:       authUser.ID,
	})
	if err != nil {
		slog.Error("create dispute failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	apiCfg.recordAuditEventBestEffort(r, "dispute_opened", "dispute", dispute.ID.String(), authUser.ID, map[string]string{
		"claim_id":    claimID.String(),
		"reason_code": req.ReasonCode,
	})

	respondWithJSON(w, http.StatusCreated, dispute)
}

// GET /v1/disputes/{id}
func (apiCfg *apiConfig) handlerGetDispute(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid dispute id")
		return
	}

	dispute, err := apiCfg.DB.GetDisputeByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "dispute not found")
			return
		}
		slog.Error("get dispute failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	authUser, err := getAuthUser(r)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if !isAdminRole(authUser.Role) && authUser.OrgID != dispute.OrgID {
		respondWithError(w, http.StatusForbidden, "access denied")
		return
	}

	respondWithJSON(w, http.StatusOK, dispute)
}

// GET /v1/claims/{id}/disputes
func (apiCfg *apiConfig) handlerListClaimDisputes(w http.ResponseWriter, r *http.Request) {
	claimID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid claim id")
		return
	}

	authUser, err := getAuthUser(r)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Verify claim org access
	claim, err := apiCfg.DB.GetClaimByID(r.Context(), claimID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "claim not found")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !isAdminRole(authUser.Role) && authUser.OrgID != claim.OrgID {
		respondWithError(w, http.StatusForbidden, "access denied")
		return
	}

	disputes, err := apiCfg.DB.ListDisputesByClaim(r.Context(), claimID)
	if err != nil {
		slog.Error("list disputes by claim failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	respondWithJSON(w, http.StatusOK, disputes)
}

// POST /v1/disputes/{id}/submit
func (apiCfg *apiConfig) handlerSubmitDispute(w http.ResponseWriter, r *http.Request) {
	apiCfg.transitionDispute(w, r, "open", "submitted", false)
}

// POST /v1/disputes/{id}/resolve  (admin only)
func (apiCfg *apiConfig) handlerResolveDispute(w http.ResponseWriter, r *http.Request) {
	type request struct {
		Resolution string `json:"resolution"`
		Accepted   bool   `json:"accepted"`
	}
	var req request
	if issues := decodeJSONStrict(r, &req); len(issues) > 0 {
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}
	if req.Resolution == "" {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
			{Field: "resolution", Message: "is required"},
		})
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid dispute id")
		return
	}

	dispute, err := apiCfg.DB.GetDisputeByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "dispute not found")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if dispute.Status != "under_review" && dispute.Status != "submitted" {
		respondWithError(w, http.StatusConflict, "dispute must be in submitted or under_review status to resolve")
		return
	}

	authUser, err := getAuthUser(r)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	newStatus := "resolved_rejected"
	if req.Accepted {
		newStatus = "resolved_accepted"
	}

	updated, err := apiCfg.DB.UpdateDisputeStatus(r.Context(), database.UpdateDisputeStatusParams{
		ID:         id,
		Status:     newStatus,
		Resolution: pgtype.Text{String: req.Resolution, Valid: true},
		ResolvedBy: pgtype.UUID{Bytes: authUser.ID, Valid: true},
	})
	if err != nil {
		slog.Error("resolve dispute failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	apiCfg.recordAuditEventBestEffort(r, "dispute_resolved", "dispute", id.String(), authUser.ID, map[string]string{
		"new_status": newStatus,
	})
	respondWithJSON(w, http.StatusOK, updated)
}

// POST /v1/disputes/{id}/withdraw
func (apiCfg *apiConfig) handlerWithdrawDispute(w http.ResponseWriter, r *http.Request) {
	apiCfg.transitionDispute(w, r, "", "withdrawn", false)
}

// transitionDispute is a shared helper for simple one-way state transitions.
// If fromStatus is non-empty, the dispute must currently be in that status.
// If requireAdmin is true, the caller must be an admin.
func (apiCfg *apiConfig) transitionDispute(w http.ResponseWriter, r *http.Request, fromStatus, toStatus string, requireAdmin bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid dispute id")
		return
	}

	dispute, err := apiCfg.DB.GetDisputeByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "dispute not found")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	authUser, err := getAuthUser(r)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if requireAdmin && !isAdminRole(authUser.Role) {
		respondWithError(w, http.StatusForbidden, "admin role required")
		return
	}
	if !isAdminRole(authUser.Role) && authUser.OrgID != dispute.OrgID {
		respondWithError(w, http.StatusForbidden, "access denied")
		return
	}
	if fromStatus != "" && dispute.Status != fromStatus {
		respondWithError(w, http.StatusConflict, "dispute is not in the expected status for this transition")
		return
	}

	updated, err := apiCfg.DB.UpdateDisputeStatus(r.Context(), database.UpdateDisputeStatusParams{
		ID:     id,
		Status: toStatus,
	})
	if err != nil {
		slog.Error("dispute status transition failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	apiCfg.recordAuditEventBestEffort(r, "dispute_status_changed", "dispute", id.String(), authUser.ID, map[string]string{
		"new_status": toStatus,
	})
	respondWithJSON(w, http.StatusOK, updated)
}
