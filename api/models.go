package main

import (
	"claims-system/internal/database"
	"time"

	"github.com/google/uuid"
)

type Claims struct {
	ID              uuid.UUID `json:"id"` // internal ID
	ExternalClaimID string    `json:"external_claim_id"`
	OrganizationID  uuid.UUID `json:"organization_id"`
	NDC             string    `json:"ndc"`
	PharmacyID      string    `json:"pharmacy_id"`
	ServiceDate     time.Time `json:"service_date"`
	Quantity        int32     `json:"quantity"`
	HashedRxKey     string    `json:"hashed_rx_key"`
	PayerType       string    `json:"payer_type"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type Organization struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	EntityID  string    `json:"entity_id"`
	CreatedAt time.Time `json:"created_at"`
}

type UploadBatch struct {
	ID         uuid.UUID `json:"id"`
	OrgID      uuid.UUID `json:"org_id"`
	UploadedBy uuid.UUID `json:"uploaded_by"`
	FileName   string    `json:"file_name"`
	RowCount   int32     `json:"row_count"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

type User struct {
	ID         uuid.UUID `json:"id"`
	OrgID      uuid.UUID `json:"org_id"`
	Email      string    `json:"email"`
	Name       string    `json:"name"`
	Role       string    `json:"role"`
	CreatedAt  time.Time `json:"created_at"`
	MfaEnabled bool      `json:"mfa_enabled"`
	Active     bool      `json:"active"`
}

func databaseOrgToOrganization(dbOrg database.Organization) Organization {
	return Organization{
		ID:        dbOrg.ID,
		Name:      dbOrg.Name,
		EntityID:  dbOrg.EntityID,
		CreatedAt: dbOrg.CreatedAt,
	}
}

func databaseUserToUser(dbUser database.User) User {
	return User{
		ID:         dbUser.ID,
		OrgID:      dbUser.OrgID,
		Email:      dbUser.Email,
		Name:       dbUser.Name,
		Role:       dbUser.Role,
		CreatedAt:  dbUser.CreatedAt,
		MfaEnabled: dbUser.MfaEnabled,
		Active:     dbUser.Active,
	}
}

func databaseBatchToBatch(dbBatch database.UploadBatch) UploadBatch {
	return UploadBatch{
		ID:         dbBatch.ID,
		OrgID:      dbBatch.OrgID,
		UploadedBy: dbBatch.UploadedBy,
		FileName:   dbBatch.FileName,
		RowCount:   dbBatch.RowCount,
		Status:     dbBatch.Status,
		CreatedAt:  dbBatch.CreatedAt,
	}
}
