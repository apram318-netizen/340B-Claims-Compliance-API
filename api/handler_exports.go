package main

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"claims-system/internal/database"
)

// POST /v1/exports
func (apiCfg *apiConfig) handlerCreateExport(w http.ResponseWriter, r *http.Request) {
	type request struct {
		ReportType     string `json:"report_type"`
		FromDate       string `json:"from_date"`
		ToDate         string `json:"to_date"`
		OrgID          string `json:"org_id,omitempty"`
		ManufacturerID string `json:"manufacturer_id,omitempty"`
		BatchID        string `json:"batch_id,omitempty"`
	}

	var req request
	if issues := decodeJSONStrict(r, &req); len(issues) > 0 {
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}

	validTypes := map[string]bool{
		"manufacturer_compliance":      true,
		"duplicate_findings":           true,
		"submission_completeness":      true,
		"exceptions":                   true,
		"batch_reconciliation_results": true,
	}
	if !validTypes[req.ReportType] {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
			{Field: "report_type", Message: "must be one of manufacturer_compliance, duplicate_findings, submission_completeness, exceptions, batch_reconciliation_results"},
		})
		return
	}
	if req.FromDate == "" || req.ToDate == "" {
		issues := make([]ValidationIssue, 0, 2)
		if req.FromDate == "" {
			issues = append(issues, ValidationIssue{Field: "from_date", Message: "is required"})
		}
		if req.ToDate == "" {
			issues = append(issues, ValidationIssue{Field: "to_date", Message: "is required"})
		}
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}

	authUser, err := getAuthUser(r)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	isAdmin := isAdminRole(authUser.Role)
	if !isAdmin {
		// Members are restricted to exporting their own organization only.
		if req.OrgID != "" && req.OrgID != authUser.OrgID.String() {
			respondWithError(w, http.StatusForbidden, "members may only export their own organization data")
			return
		}
		req.OrgID = authUser.OrgID.String()
	}

	params := map[string]string{
		"from_date": req.FromDate,
		"to_date":   req.ToDate,
	}
	if req.OrgID != "" {
		params["org_id"] = req.OrgID
	}
	if req.ManufacturerID != "" {
		params["manufacturer_id"] = req.ManufacturerID
	}
	if req.BatchID != "" {
		params["batch_id"] = req.BatchID
	}
	paramsJSON, _ := json.Marshal(params)

	var orgID pgtype.UUID
	if req.OrgID != "" {
		id, parseErr := uuid.Parse(req.OrgID)
		if parseErr != nil {
			respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
				{Field: "org_id", Message: "must be a valid UUID"},
			})
			return
		}
		orgID = pgtype.UUID{Bytes: id, Valid: true}
	}
	var mfgID pgtype.UUID
	if req.ManufacturerID != "" {
		id, parseErr := uuid.Parse(req.ManufacturerID)
		if parseErr != nil {
			respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
				{Field: "manufacturer_id", Message: "must be a valid UUID"},
			})
			return
		}
		mfgID = pgtype.UUID{Bytes: id, Valid: true}
	}
	if req.BatchID != "" {
		if _, parseErr := uuid.Parse(req.BatchID); parseErr != nil {
			respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
				{Field: "batch_id", Message: "must be a valid UUID"},
			})
			return
		}
	}

	run, err := apiCfg.DB.CreateExportRun(r.Context(), database.CreateExportRunParams{
		OrgID:          orgID,
		ManufacturerID: mfgID,
		ReportType:     req.ReportType,
		Status:         "pending",
		RequestedBy:    pgtype.UUID{Bytes: authUser.ID, Valid: true},
		Params:         paramsJSON,
	})
	if err != nil {
		slog.Error("create export run failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Enqueue for async processing
	if err := apiCfg.Queue.PublishWithContext(r.Context(), "", "report_generation", false, false,
		publishMsg(run.ID.String()),
	); err != nil {
		slog.Error("enqueue export run failed", "run_id", run.ID, "error", err)
		// Run was created; return it even if publish fails (worker can be retriggered)
	}
	appMetrics.exportRequested.Add(1)

	respondWithJSON(w, http.StatusCreated, run)
}

// GET /v1/exports/{id}
func (apiCfg *apiConfig) handlerGetExport(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid export id")
		return
	}

	run, err := apiCfg.DB.GetExportRunByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "export not found")
			return
		}
		slog.Error("get export run failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	authUser, ok := requireAuthUser(w, r)
	if !ok {
		return
	}
	if ok := authorizeExportRead(w, run, authUser); !ok {
		return
	}

	respondWithJSON(w, http.StatusOK, run)
}

// GET /v1/exports/{id}/download
func (apiCfg *apiConfig) handlerDownloadExport(w http.ResponseWriter, r *http.Request) {
	run, ok := apiCfg.loadExportForRequest(w, r)
	if !ok {
		return
	}
	authUser, ok := requireAuthUser(w, r)
	if !ok {
		return
	}
	if ok := authorizeExportRead(w, run, authUser); !ok {
		return
	}
	if run.Status != "completed" {
		respondWithError(w, http.StatusConflict, "export is not ready for download")
		return
	}
	if !run.FilePath.Valid || run.FilePath.String == "" {
		respondWithError(w, http.StatusNotFound, "export file is missing")
		return
	}

	ref := run.FilePath.String
	filename := filepath.Base(ref)

	appMetrics.exportDownloaded.Add(1)
	apiCfg.recordAuditEventBestEffort(r, "export_downloaded", "export_run", run.ID.String(), authUser.ID, map[string]string{
		"ref": ref,
	})

	if err := apiCfg.Storage.ServeDownload(r.Context(), ref, filename, w, r); err != nil {
		slog.Error("serve export download failed", "run_id", run.ID, "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
	}
}

// POST /v1/exports/{id}/retry
func (apiCfg *apiConfig) handlerRetryExport(w http.ResponseWriter, r *http.Request) {
	run, ok := apiCfg.loadExportForRequest(w, r)
	if !ok {
		return
	}
	authUser, ok := requireAuthUser(w, r)
	if !ok {
		return
	}
	if ok := authorizeExportRead(w, run, authUser); !ok {
		return
	}
	if run.Status == "completed" {
		respondWithError(w, http.StatusConflict, "completed export does not need retry")
		return
	}

	if err := apiCfg.Queue.PublishWithContext(r.Context(), "", "report_generation", false, false, publishMsg(run.ID.String())); err != nil {
		slog.Error("retry export enqueue failed", "run_id", run.ID, "error", err)
		respondWithError(w, http.StatusInternalServerError, "failed to enqueue retry")
		return
	}

	appMetrics.exportRetried.Add(1)
	apiCfg.recordAuditEventBestEffort(r, "export_requeued", "export_run", run.ID.String(), authUser.ID, map[string]string{
		"previous_status": run.Status,
	})
	respondWithJSON(w, http.StatusAccepted, map[string]string{
		"status": "requeued",
		"id":     run.ID.String(),
	})
}

// POST /v1/claims/{id}/override
func (apiCfg *apiConfig) handlerOverrideClaim(w http.ResponseWriter, r *http.Request) {
	claimID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithValidationIssues(w, r, "invalid path params", []ValidationIssue{
			{Field: "id", Message: "must be a valid claim UUID"},
		})
		return
	}

	type request struct {
		NewStatus      string `json:"new_status"`
		Reason         string `json:"reason"`
		RebateRecordID string `json:"rebate_record_id,omitempty"`
	}

	var req request
	if issues := decodeJSONStrict(r, &req); len(issues) > 0 {
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}
	if req.NewStatus == "" || req.Reason == "" {
		issues := make([]ValidationIssue, 0, 2)
		if req.NewStatus == "" {
			issues = append(issues, ValidationIssue{Field: "new_status", Message: "is required"})
		}
		if req.Reason == "" {
			issues = append(issues, ValidationIssue{Field: "reason", Message: "is required"})
		}
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}

	// Tenant isolation
	claim, err := apiCfg.DB.GetClaimByID(r.Context(), claimID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "claim not found")
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
	if !isAdminRole(authUser.Role) && authUser.OrgID != claim.OrgID {
		respondWithError(w, http.StatusForbidden, "access denied")
		return
	}

	// Fetch the current decision to record previous_status
	decision, err := apiCfg.DB.GetMatchDecisionByClaim(r.Context(), claimID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "no match decision found for this claim")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var rebateRecordID pgtype.UUID
	if req.RebateRecordID != "" {
		id, parseErr := uuid.Parse(req.RebateRecordID)
		if parseErr != nil {
			respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
				{Field: "rebate_record_id", Message: "must be a valid UUID"},
			})
			return
		}
		rebateRecordID = pgtype.UUID{Bytes: id, Valid: true}
	}

	override, err := apiCfg.DB.CreateManualOverride(r.Context(), database.CreateManualOverrideParams{
		ClaimID:        claimID,
		PreviousStatus: decision.Status,
		NewStatus:      req.NewStatus,
		Reason:         req.Reason,
		RebateRecordID: rebateRecordID,
		OverriddenBy:   authUser.ID,
	})
	if err != nil {
		slog.Error("create manual override failed", "claim_id", claimID, "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	appMetrics.manualOverrides.Add(1)
	apiCfg.recordAuditEventBestEffort(r, "manual_override_applied", "claim", claimID.String(), authUser.ID, map[string]string{
		"new_status": req.NewStatus,
	})

	if err := apiCfg.DB.UpdateMatchDecisionOverride(r.Context(), database.UpdateMatchDecisionOverrideParams{
		ClaimID:    claimID,
		Status:     req.NewStatus,
		OverrideID: pgtype.UUID{Bytes: override.ID, Valid: true},
	}); err != nil {
		slog.Error("update match decision override failed", "claim_id", claimID, "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	respondWithJSON(w, http.StatusOK, override)
}

func (apiCfg *apiConfig) loadExportForRequest(w http.ResponseWriter, r *http.Request) (database.ExportRun, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid export id")
		return database.ExportRun{}, false
	}

	run, err := apiCfg.DB.GetExportRunByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "export not found")
			return database.ExportRun{}, false
		}
		slog.Error("get export run failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return database.ExportRun{}, false
	}
	return run, true
}

func requireAuthUser(w http.ResponseWriter, r *http.Request) (authUser, bool) {
	au, err := getAuthUser(r)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return authUser{}, false
	}
	return au, true
}

func authorizeExportRead(w http.ResponseWriter, run database.ExportRun, authUser authUser) bool {
	if !isAdminRole(authUser.Role) {
		if !run.OrgID.Valid || run.OrgID.Bytes != authUser.OrgID {
			respondWithError(w, http.StatusForbidden, "access denied")
			return false
		}
	}
	return true
}

func (apiCfg *apiConfig) recordAuditEventBestEffort(
	r *http.Request,
	eventType, entityType, entityID string,
	actorID uuid.UUID,
	payload map[string]string,
) {
	payloadJSON, _ := json.Marshal(payload)
	_, err := apiCfg.DB.CreateAuditEvent(r.Context(), database.CreateAuditEventParams{
		EventType:  eventType,
		EntityType: entityType,
		EntityID:   entityID,
		ActorID:    pgtype.UUID{Bytes: actorID, Valid: true},
		Payload:    payloadJSON,
	})
	if err != nil {
		slog.Error("audit event failed", "event_type", eventType, "entity_id", entityID, "error", err)
	}
}
