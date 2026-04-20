package database

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// ── Params ────────────────────────────────────────────────────────────────────

type CreateDisputeParams struct {
	ClaimID        uuid.UUID
	OrgID          uuid.UUID
	ManufacturerID uuid.UUID
	ReasonCode     string
	Description    string
	OpenedBy       uuid.UUID
}

type UpdateDisputeStatusParams struct {
	ID         uuid.UUID
	Status     string
	Resolution pgtype.Text
	ResolvedBy pgtype.UUID
}

// ── Queries ───────────────────────────────────────────────────────────────────

const createDispute = `
INSERT INTO manufacturer_disputes
    (claim_id, org_id, manufacturer_id, reason_code, description, opened_by)
VALUES ($1,$2,$3,$4,$5,$6)
RETURNING id, claim_id, org_id, manufacturer_id, status, reason_code, description,
          evidence_ref, resolution, opened_by, resolved_by, opened_at, resolved_at,
          created_at, updated_at
`

func (q *Queries) CreateDispute(ctx context.Context, arg CreateDisputeParams) (Dispute, error) {
	row := q.db.QueryRow(ctx, createDispute,
		arg.ClaimID, arg.OrgID, arg.ManufacturerID,
		arg.ReasonCode, arg.Description, arg.OpenedBy,
	)
	return scanDispute(row)
}

const getDisputeByID = `
SELECT id, claim_id, org_id, manufacturer_id, status, reason_code, description,
       evidence_ref, resolution, opened_by, resolved_by, opened_at, resolved_at,
       created_at, updated_at
FROM manufacturer_disputes WHERE id = $1
`

func (q *Queries) GetDisputeByID(ctx context.Context, id uuid.UUID) (Dispute, error) {
	row := q.db.QueryRow(ctx, getDisputeByID, id)
	return scanDispute(row)
}

const listDisputesByClaim = `
SELECT id, claim_id, org_id, manufacturer_id, status, reason_code, description,
       evidence_ref, resolution, opened_by, resolved_by, opened_at, resolved_at,
       created_at, updated_at
FROM manufacturer_disputes WHERE claim_id = $1 ORDER BY opened_at DESC
`

func (q *Queries) ListDisputesByClaim(ctx context.Context, claimID uuid.UUID) ([]Dispute, error) {
	return queryDisputes(ctx, q, listDisputesByClaim, claimID)
}

const listDisputesByOrg = `
SELECT id, claim_id, org_id, manufacturer_id, status, reason_code, description,
       evidence_ref, resolution, opened_by, resolved_by, opened_at, resolved_at,
       created_at, updated_at
FROM manufacturer_disputes WHERE org_id = $1 ORDER BY opened_at DESC
`

func (q *Queries) ListDisputesByOrg(ctx context.Context, orgID uuid.UUID) ([]Dispute, error) {
	return queryDisputes(ctx, q, listDisputesByOrg, orgID)
}

const updateDisputeStatus = `
UPDATE manufacturer_disputes
SET status      = $2,
    resolution  = CASE WHEN $3::text IS NOT NULL THEN $3 ELSE resolution END,
    resolved_by = CASE WHEN $4::uuid IS NOT NULL THEN $4 ELSE resolved_by END,
    resolved_at = CASE WHEN $2 IN ('resolved_accepted','resolved_rejected') THEN NOW() ELSE resolved_at END,
    updated_at  = NOW()
WHERE id = $1
RETURNING id, claim_id, org_id, manufacturer_id, status, reason_code, description,
          evidence_ref, resolution, opened_by, resolved_by, opened_at, resolved_at,
          created_at, updated_at
`

func (q *Queries) UpdateDisputeStatus(ctx context.Context, arg UpdateDisputeStatusParams) (Dispute, error) {
	row := q.db.QueryRow(ctx, updateDisputeStatus, arg.ID, arg.Status, arg.Resolution, arg.ResolvedBy)
	return scanDispute(row)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

type scanner interface {
	Scan(dest ...any) error
}

func scanDispute(row scanner) (Dispute, error) {
	var i Dispute
	err := row.Scan(
		&i.ID, &i.ClaimID, &i.OrgID, &i.ManufacturerID,
		&i.Status, &i.ReasonCode, &i.Description,
		&i.EvidenceRef, &i.Resolution,
		&i.OpenedBy, &i.ResolvedBy, &i.OpenedAt, &i.ResolvedAt,
		&i.CreatedAt, &i.UpdatedAt,
	)
	return i, err
}

func queryDisputes(ctx context.Context, q *Queries, sql string, arg any) ([]Dispute, error) {
	rows, err := q.db.Query(ctx, sql, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Dispute
	for rows.Next() {
		d, err := scanDispute(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, d)
	}
	return items, rows.Err()
}
