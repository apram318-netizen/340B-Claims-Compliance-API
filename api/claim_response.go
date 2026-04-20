package main

import (
	"claims-system/internal/database"
	"claims-system/internal/sensitivity"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// claimJSON is the stable public shape for GET /v1/claims/{id} with optional redaction.
type claimJSON struct {
	ID                   uuid.UUID   `json:"id"`
	BatchID              uuid.UUID   `json:"batch_id"`
	OrgID                uuid.UUID   `json:"org_id"`
	RowID                uuid.UUID   `json:"row_id"`
	Ndc                  string      `json:"ndc"`
	PharmacyNpi          string      `json:"pharmacy_npi"`
	ServiceDate          pgtype.Date `json:"service_date"`
	Quantity             int32       `json:"quantity"`
	HashedRxKey          pgtype.Text `json:"hashed_rx_key"`
	PayerType            pgtype.Text `json:"payer_type"`
	Status               string      `json:"status"`
	CreatedAt            time.Time   `json:"created_at"`
	AdjudicationDate     pgtype.Date `json:"adjudication_date"`
	FillDate             pgtype.Date `json:"fill_date"`
	ReconciliationStatus string      `json:"reconciliation_status"`
	UpdatedAt            time.Time   `json:"updated_at"`
}

func claimToJSON(c database.Claim) claimJSON {
	out := claimJSON{
		ID:                   c.ID,
		BatchID:              c.BatchID,
		OrgID:                c.OrgID,
		RowID:                c.RowID,
		Ndc:                  c.Ndc,
		PharmacyNpi:          c.PharmacyNpi,
		ServiceDate:          c.ServiceDate,
		Quantity:             c.Quantity,
		HashedRxKey:          c.HashedRxKey,
		PayerType:            c.PayerType,
		Status:               c.Status,
		CreatedAt:            c.CreatedAt,
		AdjudicationDate:     c.AdjudicationDate,
		FillDate:             c.FillDate,
		ReconciliationStatus: c.ReconciliationStatus,
		UpdatedAt:            c.UpdatedAt,
	}
	if !sensitivity.RedactAPIFields() {
		return out
	}
	out.PharmacyNpi = sensitivity.MaskNPI(c.PharmacyNpi)
	if c.HashedRxKey.Valid {
		out.HashedRxKey = pgtype.Text{String: sensitivity.RedactHashPlaceholder(c.HashedRxKey.String), Valid: true}
	}
	return out
}
