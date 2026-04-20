package database

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// ── Params ────────────────────────────────────────────────────────────────────

type CreateContractPharmacyParams struct {
	OrgID         uuid.UUID
	PharmacyName  string
	PharmacyNpi   string
	DeaNumber     pgtype.Text
	Address       pgtype.Text
	City          pgtype.Text
	State         pgtype.Text
	Zip           pgtype.Text
	EffectiveFrom pgtype.Date
	EffectiveTo   pgtype.Date
}

type UpdateContractPharmacyStatusParams struct {
	ID     uuid.UUID
	Status string
}

type CreateContractPharmacyAuthParams struct {
	ManufacturerID     uuid.UUID
	ContractPharmacyID uuid.UUID
	EffectiveFrom      pgtype.Date
	EffectiveTo        pgtype.Date
}

// ── Queries ───────────────────────────────────────────────────────────────────

const createContractPharmacy = `
INSERT INTO contract_pharmacies
    (org_id, pharmacy_name, pharmacy_npi, dea_number, address, city, state, zip, effective_from, effective_to)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
RETURNING id, org_id, pharmacy_name, pharmacy_npi, dea_number, address, city, state, zip,
          status, effective_from, effective_to, created_at, updated_at
`

func (q *Queries) CreateContractPharmacy(ctx context.Context, arg CreateContractPharmacyParams) (ContractPharmacy, error) {
	row := q.db.QueryRow(ctx, createContractPharmacy,
		arg.OrgID, arg.PharmacyName, arg.PharmacyNpi,
		arg.DeaNumber, arg.Address, arg.City, arg.State, arg.Zip,
		arg.EffectiveFrom, arg.EffectiveTo,
	)
	var i ContractPharmacy
	err := row.Scan(
		&i.ID, &i.OrgID, &i.PharmacyName, &i.PharmacyNpi,
		&i.DeaNumber, &i.Address, &i.City, &i.State, &i.Zip,
		&i.Status, &i.EffectiveFrom, &i.EffectiveTo, &i.CreatedAt, &i.UpdatedAt,
	)
	return i, err
}

const getContractPharmacyByID = `
SELECT id, org_id, pharmacy_name, pharmacy_npi, dea_number, address, city, state, zip,
       status, effective_from, effective_to, created_at, updated_at
FROM contract_pharmacies WHERE id = $1
`

func (q *Queries) GetContractPharmacyByID(ctx context.Context, id uuid.UUID) (ContractPharmacy, error) {
	row := q.db.QueryRow(ctx, getContractPharmacyByID, id)
	var i ContractPharmacy
	err := row.Scan(
		&i.ID, &i.OrgID, &i.PharmacyName, &i.PharmacyNpi,
		&i.DeaNumber, &i.Address, &i.City, &i.State, &i.Zip,
		&i.Status, &i.EffectiveFrom, &i.EffectiveTo, &i.CreatedAt, &i.UpdatedAt,
	)
	return i, err
}

const listContractPharmaciesByOrg = `
SELECT id, org_id, pharmacy_name, pharmacy_npi, dea_number, address, city, state, zip,
       status, effective_from, effective_to, created_at, updated_at
FROM contract_pharmacies WHERE org_id = $1 ORDER BY pharmacy_name
`

func (q *Queries) ListContractPharmaciesByOrg(ctx context.Context, orgID uuid.UUID) ([]ContractPharmacy, error) {
	rows, err := q.db.Query(ctx, listContractPharmaciesByOrg, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []ContractPharmacy
	for rows.Next() {
		var i ContractPharmacy
		if err := rows.Scan(
			&i.ID, &i.OrgID, &i.PharmacyName, &i.PharmacyNpi,
			&i.DeaNumber, &i.Address, &i.City, &i.State, &i.Zip,
			&i.Status, &i.EffectiveFrom, &i.EffectiveTo, &i.CreatedAt, &i.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}

const updateContractPharmacyStatus = `
UPDATE contract_pharmacies SET status = $2, updated_at = NOW() WHERE id = $1
`

func (q *Queries) UpdateContractPharmacyStatus(ctx context.Context, arg UpdateContractPharmacyStatusParams) error {
	_, err := q.db.Exec(ctx, updateContractPharmacyStatus, arg.ID, arg.Status)
	return err
}

const createContractPharmacyAuth = `
INSERT INTO manufacturer_contract_pharmacy_auths
    (manufacturer_id, contract_pharmacy_id, effective_from, effective_to)
VALUES ($1,$2,$3,$4)
RETURNING id, manufacturer_id, contract_pharmacy_id, authorized, effective_from, effective_to, created_at
`

func (q *Queries) CreateContractPharmacyAuth(ctx context.Context, arg CreateContractPharmacyAuthParams) (ManufacturerContractPharmacyAuth, error) {
	row := q.db.QueryRow(ctx, createContractPharmacyAuth,
		arg.ManufacturerID, arg.ContractPharmacyID, arg.EffectiveFrom, arg.EffectiveTo,
	)
	var i ManufacturerContractPharmacyAuth
	err := row.Scan(
		&i.ID, &i.ManufacturerID, &i.ContractPharmacyID,
		&i.Authorized, &i.EffectiveFrom, &i.EffectiveTo, &i.CreatedAt,
	)
	return i, err
}

const listContractPharmacyAuthsByManufacturer = `
SELECT id, manufacturer_id, contract_pharmacy_id, authorized, effective_from, effective_to, created_at
FROM manufacturer_contract_pharmacy_auths
WHERE manufacturer_id = $1
ORDER BY created_at DESC
`

func (q *Queries) ListContractPharmacyAuthsByManufacturer(ctx context.Context, manufacturerID uuid.UUID) ([]ManufacturerContractPharmacyAuth, error) {
	rows, err := q.db.Query(ctx, listContractPharmacyAuthsByManufacturer, manufacturerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []ManufacturerContractPharmacyAuth
	for rows.Next() {
		var i ManufacturerContractPharmacyAuth
		if err := rows.Scan(
			&i.ID, &i.ManufacturerID, &i.ContractPharmacyID,
			&i.Authorized, &i.EffectiveFrom, &i.EffectiveTo, &i.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}

// ensure time import is used
var _ = time.Now
