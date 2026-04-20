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

func (apiCfg *apiConfig) handlerCreateOrganization(w http.ResponseWriter, r *http.Request) {
	type requestBody struct {
		Name     string `json:"name"`
		EntityID string `json:"entity_id"`
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
	if body.EntityID == "" {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
			{Field: "entity_id", Message: "is required"},
		})
		return
	}

	org, err := apiCfg.DB.CreateOrganization(r.Context(), database.CreateOrganizationParams{
		Name:     body.Name,
		EntityID: body.EntityID,
	})
	if err != nil {
		slog.Error("create organization failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "failed to create organization")
		return
	}

	respondWithJSON(w, http.StatusCreated, databaseOrgToOrganization(org))
}

func (apiCfg *apiConfig) handlerGetOrganization(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")

	id, err := uuid.Parse(idStr)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid organization id")
		return
	}

	// Tenant isolation: users may only view their own organization
	userID, err := getUserId(r)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	user, err := apiCfg.DB.GetUserByID(r.Context(), userID)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if user.OrgID != id {
		respondWithError(w, http.StatusForbidden, "access denied")
		return
	}

	org, err := apiCfg.DB.GetOrganization(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "organization not found")
			return
		}
		slog.Error("get organization failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	respondWithJSON(w, http.StatusOK, databaseOrgToOrganization(org))
}
