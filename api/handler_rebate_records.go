package main

import (
	"claims-system/internal/database"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// POST /v1/rebate-records
// Ingests manufacturer-side rebate data for use by the reconciliation engine.
func (apiCfg *apiConfig) handlerCreateRebateRecord(w http.ResponseWriter, r *http.Request) {
	type requestBody struct {
		ManufacturerID string  `json:"manufacturer_id"`
		OrgID          string  `json:"org_id"`
		NDC            string  `json:"ndc"`
		PharmacyNPI    string  `json:"pharmacy_npi"`
		ServiceDate    string  `json:"service_date"` // YYYY-MM-DD
		Quantity       int32   `json:"quantity"`
		HashedRxKey    string  `json:"hashed_rx_key"`
		PayerType      string  `json:"payer_type"`
		RebateAmount   float64 `json:"rebate_amount"`
		Source         string  `json:"source"`
	}

	var body requestBody
	if issues := decodeJSONStrict(r, &body); len(issues) > 0 {
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}

	// Validate required fields
	if body.ManufacturerID == "" || body.OrgID == "" || body.NDC == "" ||
		body.PharmacyNPI == "" || body.ServiceDate == "" || body.Quantity <= 0 {
		issues := make([]ValidationIssue, 0, 6)
		if body.ManufacturerID == "" {
			issues = append(issues, ValidationIssue{Field: "manufacturer_id", Message: "is required"})
		}
		if body.OrgID == "" {
			issues = append(issues, ValidationIssue{Field: "org_id", Message: "is required"})
		}
		if body.NDC == "" {
			issues = append(issues, ValidationIssue{Field: "ndc", Message: "is required"})
		}
		if body.PharmacyNPI == "" {
			issues = append(issues, ValidationIssue{Field: "pharmacy_npi", Message: "is required"})
		}
		if body.ServiceDate == "" {
			issues = append(issues, ValidationIssue{Field: "service_date", Message: "is required"})
		}
		if body.Quantity <= 0 {
			issues = append(issues, ValidationIssue{Field: "quantity", Message: "must be greater than 0"})
		}
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}

	manufacturerID, err := uuid.Parse(body.ManufacturerID)
	if err != nil {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
			{Field: "manufacturer_id", Message: "must be a valid UUID"},
		})
		return
	}

	orgID, err := uuid.Parse(body.OrgID)
	if err != nil {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
			{Field: "org_id", Message: "must be a valid UUID"},
		})
		return
	}

	serviceDate, err := time.Parse("2006-01-02", body.ServiceDate)
	if err != nil {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
			{Field: "service_date", Message: "must be YYYY-MM-DD"},
		})
		return
	}

	if body.Source == "" {
		body.Source = "api"
	}

	record, err := apiCfg.DB.CreateRebateRecord(r.Context(), database.CreateRebateRecordParams{
		ManufacturerID: manufacturerID,
		OrgID:          orgID,
		Ndc:            body.NDC,
		PharmacyNpi:    body.PharmacyNPI,
		ServiceDate:    pgtype.Date{Time: serviceDate, Valid: true},
		Quantity:       body.Quantity,
		HashedRxKey:    pgTextFrom(body.HashedRxKey),
		PayerType:      pgTextFrom(body.PayerType),
		RebateAmount:   pgNumericFrom(body.RebateAmount),
		Source:         body.Source,
	})
	if err != nil {
		slog.Error("create rebate record failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "failed to create rebate record")
		return
	}

	respondWithJSON(w, http.StatusCreated, record)
}
