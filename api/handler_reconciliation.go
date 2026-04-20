package main

import (
	"claims-system/internal/database"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// GET /v1/reconciliation-jobs/{id}
func (apiCfg *apiConfig) handlerGetReconciliationJob(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid job id")
		return
	}

	job, err := apiCfg.DB.GetReconciliationJobByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "reconciliation job not found")
			return
		}
		slog.Error("get reconciliation job failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	authUser, err := getAuthUser(r)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if !isAdminRole(authUser.Role) {
		batch, err := apiCfg.DB.GetUploadBatches(r.Context(), job.BatchID)
		if err != nil {
			respondWithError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if batch.OrgID != authUser.OrgID {
			respondWithError(w, http.StatusForbidden, "access denied")
			return
		}
	}

	respondWithJSON(w, http.StatusOK, job)
}

// GET /v1/batches/{id}/reconciliation-job
func (apiCfg *apiConfig) handlerGetBatchReconciliationJob(w http.ResponseWriter, r *http.Request) {
	batchID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid batch id")
		return
	}

	// Tenant isolation
	batch, err := apiCfg.DB.GetUploadBatches(r.Context(), batchID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "batch not found")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	userID, _ := getUserId(r)
	user, err := apiCfg.DB.GetUserByID(r.Context(), userID)
	if err != nil || user.OrgID != batch.OrgID {
		respondWithError(w, http.StatusForbidden, "access denied")
		return
	}

	job, err := apiCfg.DB.GetReconciliationJobByBatch(r.Context(), batchID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "no reconciliation job for this batch yet")
			return
		}
		slog.Error("get reconciliation job failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	respondWithJSON(w, http.StatusOK, job)
}

// GET /v1/claims/{id}/decision
func (apiCfg *apiConfig) handlerGetClaimDecision(w http.ResponseWriter, r *http.Request) {
	claimID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid claim id")
		return
	}

	// Tenant isolation via claim → org
	claim, err := apiCfg.DB.GetClaimByID(r.Context(), claimID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "claim not found")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	userID, _ := getUserId(r)
	user, err := apiCfg.DB.GetUserByID(r.Context(), userID)
	if err != nil || user.OrgID != claim.OrgID {
		respondWithError(w, http.StatusForbidden, "access denied")
		return
	}

	decision, err := apiCfg.DB.GetMatchDecisionByClaim(r.Context(), claimID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "no decision recorded for this claim yet")
			return
		}
		slog.Error("get match decision failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	respondWithJSON(w, http.StatusOK, decision)
}

// GET /v1/reconciliation-jobs/{id}/decisions
func (apiCfg *apiConfig) handlerGetJobDecisions(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid job id")
		return
	}

	job, err := apiCfg.DB.GetReconciliationJobByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "reconciliation job not found")
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
	if !isAdminRole(authUser.Role) {
		batch, err := apiCfg.DB.GetUploadBatches(r.Context(), job.BatchID)
		if err != nil {
			respondWithError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if batch.OrgID != authUser.OrgID {
			respondWithError(w, http.StatusForbidden, "access denied")
			return
		}
	}

	limit, offset, ok := parsePagination(r)
	if !ok {
		respondWithValidationIssues(w, r, "invalid pagination params", []ValidationIssue{
			{Field: "limit/offset", Message: "must be positive integers; offset must be >= 0"},
		})
		return
	}

	total, err := apiCfg.DB.CountMatchDecisionsByJob(r.Context(), id)
	if err != nil {
		slog.Error("count decisions failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	decisions, err := apiCfg.DB.GetMatchDecisionsByJobPaginated(r.Context(), database.GetMatchDecisionsByJobPaginatedParams{
		JobID:  id,
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		slog.Error("get decisions failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	applyPaginationHeaders(w, total, limit, offset)
	respondWithJSON(w, http.StatusOK, decisions)
}
