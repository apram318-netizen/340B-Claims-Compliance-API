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

// POST /v1/contract-pharmacies
func (apiCfg *apiConfig) handlerCreateContractPharmacy(w http.ResponseWriter, r *http.Request) {
	type request struct {
		OrgID         string `json:"org_id"`
		PharmacyName  string `json:"pharmacy_name"`
		PharmacyNPI   string `json:"pharmacy_npi"`
		DEANumber     string `json:"dea_number,omitempty"`
		Address       string `json:"address,omitempty"`
		City          string `json:"city,omitempty"`
		State         string `json:"state,omitempty"`
		Zip           string `json:"zip,omitempty"`
		EffectiveFrom string `json:"effective_from"`
		EffectiveTo   string `json:"effective_to,omitempty"`
	}

	var req request
	if issues := decodeJSONStrict(r, &req); len(issues) > 0 {
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}

	var issues []ValidationIssue
	if req.OrgID == "" {
		issues = append(issues, ValidationIssue{Field: "org_id", Message: "is required"})
	}
	if req.PharmacyName == "" {
		issues = append(issues, ValidationIssue{Field: "pharmacy_name", Message: "is required"})
	}
	if req.PharmacyNPI == "" {
		issues = append(issues, ValidationIssue{Field: "pharmacy_npi", Message: "is required"})
	}
	if req.EffectiveFrom == "" {
		issues = append(issues, ValidationIssue{Field: "effective_from", Message: "is required"})
	}
	if len(issues) > 0 {
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}

	orgID, err := uuid.Parse(req.OrgID)
	if err != nil {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
			{Field: "org_id", Message: "must be a valid UUID"},
		})
		return
	}

	from, err := parseDateParam(req.EffectiveFrom)
	if err != nil {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
			{Field: "effective_from", Message: "must be YYYY-MM-DD"},
		})
		return
	}
	var to pgtype.Date
	if req.EffectiveTo != "" {
		t, err := parseDateParam(req.EffectiveTo)
		if err != nil {
			respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
				{Field: "effective_to", Message: "must be YYYY-MM-DD"},
			})
			return
		}
		to = t
	}

	cp, err := apiCfg.DB.CreateContractPharmacy(r.Context(), database.CreateContractPharmacyParams{
		OrgID:         orgID,
		PharmacyName:  req.PharmacyName,
		PharmacyNpi:   req.PharmacyNPI,
		DeaNumber:     pgtype.Text{String: req.DEANumber, Valid: req.DEANumber != ""},
		Address:       pgtype.Text{String: req.Address, Valid: req.Address != ""},
		City:          pgtype.Text{String: req.City, Valid: req.City != ""},
		State:         pgtype.Text{String: req.State, Valid: req.State != ""},
		Zip:           pgtype.Text{String: req.Zip, Valid: req.Zip != ""},
		EffectiveFrom: from,
		EffectiveTo:   to,
	})
	if err != nil {
		slog.Error("create contract pharmacy failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	respondWithJSON(w, http.StatusCreated, cp)
}

// GET /v1/contract-pharmacies
func (apiCfg *apiConfig) handlerListContractPharmacies(w http.ResponseWriter, r *http.Request) {
	authUser, err := getAuthUser(r)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	orgID := authUser.OrgID
	if isAdminRole(authUser.Role) {
		if q := r.URL.Query().Get("org_id"); q != "" {
			id, parseErr := uuid.Parse(q)
			if parseErr != nil {
				respondWithValidationIssues(w, r, "invalid query", []ValidationIssue{
					{Field: "org_id", Message: "must be a valid UUID"},
				})
				return
			}
			orgID = id
		}
	}

	list, err := apiCfg.DB.ListContractPharmaciesByOrg(r.Context(), orgID)
	if err != nil {
		slog.Error("list contract pharmacies failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	respondWithJSON(w, http.StatusOK, list)
}

// GET /v1/contract-pharmacies/{id}
func (apiCfg *apiConfig) handlerGetContractPharmacy(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid contract pharmacy id")
		return
	}

	cp, err := apiCfg.DB.GetContractPharmacyByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "contract pharmacy not found")
			return
		}
		slog.Error("get contract pharmacy failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	authUser, err := getAuthUser(r)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if !isAdminRole(authUser.Role) && authUser.OrgID != cp.OrgID {
		respondWithError(w, http.StatusForbidden, "access denied")
		return
	}

	respondWithJSON(w, http.StatusOK, cp)
}

// PATCH /v1/contract-pharmacies/{id}/status  (admin only)
func (apiCfg *apiConfig) handlerUpdateContractPharmacyStatus(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid contract pharmacy id")
		return
	}

	type request struct {
		Status string `json:"status"`
	}
	var req request
	if issues := decodeJSONStrict(r, &req); len(issues) > 0 {
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}

	validStatuses := map[string]bool{"active": true, "inactive": true, "suspended": true}
	if !validStatuses[req.Status] {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
			{Field: "status", Message: "must be one of active, inactive, suspended"},
		})
		return
	}

	if err := apiCfg.DB.UpdateContractPharmacyStatus(r.Context(), database.UpdateContractPharmacyStatusParams{
		ID:     id,
		Status: req.Status,
	}); err != nil {
		slog.Error("update contract pharmacy status failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	respondWithJSON(w, http.StatusOK, map[string]string{"status": req.Status})
}

// POST /v1/manufacturers/{id}/contract-pharmacy-auths  (admin only)
func (apiCfg *apiConfig) handlerCreateContractPharmacyAuth(w http.ResponseWriter, r *http.Request) {
	manufacturerID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid manufacturer id")
		return
	}

	type request struct {
		ContractPharmacyID string `json:"contract_pharmacy_id"`
		EffectiveFrom      string `json:"effective_from"`
		EffectiveTo        string `json:"effective_to,omitempty"`
	}
	var req request
	if issues := decodeJSONStrict(r, &req); len(issues) > 0 {
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}

	cpID, err := uuid.Parse(req.ContractPharmacyID)
	if err != nil {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
			{Field: "contract_pharmacy_id", Message: "must be a valid UUID"},
		})
		return
	}
	from, err := parseDateParam(req.EffectiveFrom)
	if err != nil {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
			{Field: "effective_from", Message: "must be YYYY-MM-DD"},
		})
		return
	}
	var to pgtype.Date
	if req.EffectiveTo != "" {
		t, err := parseDateParam(req.EffectiveTo)
		if err != nil {
			respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
				{Field: "effective_to", Message: "must be YYYY-MM-DD"},
			})
			return
		}
		to = t
	}

	auth, err := apiCfg.DB.CreateContractPharmacyAuth(r.Context(), database.CreateContractPharmacyAuthParams{
		ManufacturerID:     manufacturerID,
		ContractPharmacyID: cpID,
		EffectiveFrom:      from,
		EffectiveTo:        to,
	})
	if err != nil {
		slog.Error("create contract pharmacy auth failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	respondWithJSON(w, http.StatusCreated, auth)
}
