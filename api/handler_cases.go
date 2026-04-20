package main

import (
	"claims-system/internal/database"
	"claims-system/internal/feature"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// POST /v1/organizations/{id}/cases
func (apiCfg *apiConfig) handlerCreateCase(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid organization id")
		return
	}
	if !requireOrgAccess(w, r, orgID) {
		return
	}
	if apiCfg.Features == nil || !apiCfg.Features.Enabled(r.Context(), orgID, feature.ExceptionCases) {
		respondWithError(w, http.StatusNotFound, "feature not enabled")
		return
	}
	uid, err := getUserId(r)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body struct {
		Title    string     `json:"title"`
		Status   string     `json:"status"`
		Priority string     `json:"priority"`
		ClaimID  *uuid.UUID `json:"claim_id"`
		BatchID  *uuid.UUID `json:"batch_id"`
	}
	if issues := decodeJSONStrict(r, &body); len(issues) > 0 {
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}
	if strings.TrimSpace(body.Title) == "" {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{{Field: "title", Message: "is required"}})
		return
	}
	status := strings.TrimSpace(body.Status)
	if status == "" {
		status = "open"
	}
	priority := strings.TrimSpace(body.Priority)
	if priority == "" {
		priority = "normal"
	}
	var claimID, batchID pgtype.UUID
	if body.ClaimID != nil {
		claimID = pgtype.UUID{Bytes: *body.ClaimID, Valid: true}
	}
	if body.BatchID != nil {
		batchID = pgtype.UUID{Bytes: *body.BatchID, Valid: true}
	}
	c, err := apiCfg.DB.CreateExceptionCase(r.Context(), database.CreateExceptionCaseParams{
		OrgID:          orgID,
		ClaimID:        claimID,
		BatchID:        batchID,
		Title:          body.Title,
		Status:         status,
		Priority:       priority,
		AssigneeUserID: pgtype.UUID{},
		CreatedBy:      uid,
	})
	if err != nil {
		slog.Error("create case", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	respondWithJSON(w, http.StatusCreated, exceptionCaseToJSON(c))
}

// GET /v1/organizations/{id}/cases
func (apiCfg *apiConfig) handlerListCases(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid organization id")
		return
	}
	if !requireOrgAccess(w, r, orgID) {
		return
	}
	limit, offset, ok := parsePagination(r)
	if !ok {
		respondWithValidationIssues(w, r, "invalid pagination params", []ValidationIssue{{Field: "limit/offset", Message: "invalid"}})
		return
	}
	total, err := apiCfg.DB.CountExceptionCasesByOrg(r.Context(), orgID)
	if err != nil {
		slog.Error("count cases", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	rows, err := apiCfg.DB.ListExceptionCasesByOrg(r.Context(), database.ListExceptionCasesByOrgParams{
		OrgID:  orgID,
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		slog.Error("list cases", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, c := range rows {
		out = append(out, exceptionCaseToJSON(c))
	}
	applyPaginationHeaders(w, total, limit, offset)
	respondWithJSON(w, http.StatusOK, map[string]any{"cases": out})
}

// GET /v1/organizations/{id}/cases/{caseId}
func (apiCfg *apiConfig) handlerGetExceptionCase(w http.ResponseWriter, r *http.Request) {
	orgID, _ := uuid.Parse(chi.URLParam(r, "id"))
	cid, err := uuid.Parse(chi.URLParam(r, "caseId"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid case id")
		return
	}
	if !requireOrgAccess(w, r, orgID) {
		return
	}
	c, err := apiCfg.DB.GetExceptionCase(r.Context(), database.GetExceptionCaseParams{ID: cid, OrgID: orgID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "not found")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	respondWithJSON(w, http.StatusOK, exceptionCaseToJSON(c))
}

// PATCH /v1/organizations/{id}/cases/{caseId}
func (apiCfg *apiConfig) handlerPatchCase(w http.ResponseWriter, r *http.Request) {
	orgID, _ := uuid.Parse(chi.URLParam(r, "id"))
	cid, err := uuid.Parse(chi.URLParam(r, "caseId"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid case id")
		return
	}
	if !requireOrgAccess(w, r, orgID) {
		return
	}
	existing, err := apiCfg.DB.GetExceptionCase(r.Context(), database.GetExceptionCaseParams{ID: cid, OrgID: orgID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "not found")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	var body struct {
		Title          *string     `json:"title"`
		Status         *string     `json:"status"`
		Priority       *string     `json:"priority"`
		AssigneeUserID *uuid.UUID  `json:"assignee_user_id"`
	}
	if issues := decodeJSONStrict(r, &body); len(issues) > 0 {
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}
	title := existing.Title
	if body.Title != nil {
		title = *body.Title
	}
	status := existing.Status
	if body.Status != nil {
		status = *body.Status
	}
	priority := existing.Priority
	if body.Priority != nil {
		priority = *body.Priority
	}
	assignee := existing.AssigneeUserID
	if body.AssigneeUserID != nil {
		assignee = pgtype.UUID{Bytes: *body.AssigneeUserID, Valid: true}
	}
	c, err := apiCfg.DB.UpdateExceptionCase(r.Context(), database.UpdateExceptionCaseParams{
		ID:             cid,
		OrgID:          orgID,
		Title:          title,
		Status:         status,
		Priority:       priority,
		AssigneeUserID: assignee,
	})
	if err != nil {
		slog.Error("update case", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	respondWithJSON(w, http.StatusOK, exceptionCaseToJSON(c))
}

// POST /v1/organizations/{id}/cases/{caseId}/comments
func (apiCfg *apiConfig) handlerAddCaseComment(w http.ResponseWriter, r *http.Request) {
	orgID, _ := uuid.Parse(chi.URLParam(r, "id"))
	cid, err := uuid.Parse(chi.URLParam(r, "caseId"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid case id")
		return
	}
	if !requireOrgAccess(w, r, orgID) {
		return
	}
	_, err = apiCfg.DB.GetExceptionCase(r.Context(), database.GetExceptionCaseParams{ID: cid, OrgID: orgID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "not found")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	uid, err := getUserId(r)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body struct {
		Body string `json:"body"`
	}
	if issues := decodeJSONStrict(r, &body); len(issues) > 0 {
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}
	if strings.TrimSpace(body.Body) == "" {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{{Field: "body", Message: "is required"}})
		return
	}
	cm, err := apiCfg.DB.CreateCaseComment(r.Context(), database.CreateCaseCommentParams{
		CaseID:   cid,
		AuthorID: uid,
		Body:     body.Body,
	})
	if err != nil {
		slog.Error("create comment", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	respondWithJSON(w, http.StatusCreated, map[string]any{
		"id": cm.ID, "case_id": cm.CaseID, "author_id": cm.AuthorID, "body": cm.Body, "created_at": cm.CreatedAt,
	})
}

// GET /v1/organizations/{id}/cases/{caseId}/comments
func (apiCfg *apiConfig) handlerListCaseComments(w http.ResponseWriter, r *http.Request) {
	orgID, _ := uuid.Parse(chi.URLParam(r, "id"))
	cid, err := uuid.Parse(chi.URLParam(r, "caseId"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid case id")
		return
	}
	if !requireOrgAccess(w, r, orgID) {
		return
	}
	if _, err := apiCfg.DB.GetExceptionCase(r.Context(), database.GetExceptionCaseParams{ID: cid, OrgID: orgID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "not found")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	rows, err := apiCfg.DB.ListCaseComments(r.Context(), cid)
	if err != nil {
		slog.Error("list comments", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	respondWithJSON(w, http.StatusOK, map[string]any{"comments": rows})
}

func exceptionCaseToJSON(c database.ExceptionCase) map[string]any {
	m := map[string]any{
		"id":         c.ID,
		"org_id":     c.OrgID,
		"title":      c.Title,
		"status":     c.Status,
		"priority":   c.Priority,
		"created_by": c.CreatedBy,
		"created_at": c.CreatedAt,
		"updated_at": c.UpdatedAt,
	}
	if c.ClaimID.Valid {
		m["claim_id"] = uuid.UUID(c.ClaimID.Bytes)
	}
	if c.BatchID.Valid {
		m["batch_id"] = uuid.UUID(c.BatchID.Bytes)
	}
	if c.AssigneeUserID.Valid {
		m["assignee_user_id"] = uuid.UUID(c.AssigneeUserID.Bytes)
	}
	return m
}
