package reconciliation

import (
	"claims-system/internal/database"
	"claims-system/internal/policy"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type mockStore struct {
	database.Store
	getManufacturerByNDCFn       func(context.Context, string) (database.Manufacturer, error)
	getCandidateRebateRecordsFn  func(context.Context, database.GetCandidateRebateRecordsParams) ([]database.RebateRecord, error)
	createCandidateMatchFn       func(context.Context, database.CreateCandidateMatchParams) (database.CandidateMatch, error)
	createMatchDecisionFn        func(context.Context, database.CreateMatchDecisionParams) (database.MatchDecision, error)
	updateClaimReconcileStatusFn func(context.Context, database.UpdateClaimReconciliationStatusParams) error
}

func (m *mockStore) GetManufacturerByNDC(ctx context.Context, ndc string) (database.Manufacturer, error) {
	return m.getManufacturerByNDCFn(ctx, ndc)
}

func (m *mockStore) GetCandidateRebateRecords(ctx context.Context, arg database.GetCandidateRebateRecordsParams) ([]database.RebateRecord, error) {
	return m.getCandidateRebateRecordsFn(ctx, arg)
}

func (m *mockStore) CreateCandidateMatch(ctx context.Context, arg database.CreateCandidateMatchParams) (database.CandidateMatch, error) {
	return m.createCandidateMatchFn(ctx, arg)
}

func (m *mockStore) CreateMatchDecision(ctx context.Context, arg database.CreateMatchDecisionParams) (database.MatchDecision, error) {
	return m.createMatchDecisionFn(ctx, arg)
}

func (m *mockStore) UpdateClaimReconciliationStatus(ctx context.Context, arg database.UpdateClaimReconciliationStatusParams) error {
	return m.updateClaimReconcileStatusFn(ctx, arg)
}

func TestScoreCandidatesPrefersExactRxKey(t *testing.T) {
	claim := database.Claim{
		ServiceDate: pgtype.Date{Time: time.Now(), Valid: true},
		HashedRxKey: pgtype.Text{String: "abc", Valid: true},
	}
	c1 := database.RebateRecord{
		ID:          uuid.New(),
		ServiceDate: claim.ServiceDate,
		HashedRxKey: pgtype.Text{String: "abc", Valid: true},
	}
	c2 := database.RebateRecord{
		ID:          uuid.New(),
		ServiceDate: claim.ServiceDate,
		HashedRxKey: pgtype.Text{String: "xyz", Valid: true},
	}

	best, score := scoreCandidates(claim, []database.RebateRecord{c2, c1})
	if best.ID != c1.ID {
		t.Fatalf("expected exact rx key candidate to win")
	}
	if score < 100 {
		t.Fatalf("expected strong score, got %d", score)
	}
}

func TestReconcileClaimDuplicateDiscountRisk(t *testing.T) {
	claimID := uuid.New()
	jobID := uuid.New()
	rebateID := uuid.New()
	var wroteStatus string

	store := &mockStore{
		getManufacturerByNDCFn: func(_ context.Context, _ string) (database.Manufacturer, error) {
			// no manufacturer means policy engine defaults to eligible
			return database.Manufacturer{}, pgx.ErrNoRows
		},
		getCandidateRebateRecordsFn: func(_ context.Context, _ database.GetCandidateRebateRecordsParams) ([]database.RebateRecord, error) {
			return []database.RebateRecord{
				{ID: rebateID, ServiceDate: pgtype.Date{Time: time.Now(), Valid: true}},
			}, nil
		},
		createCandidateMatchFn: func(_ context.Context, _ database.CreateCandidateMatchParams) (database.CandidateMatch, error) {
			return database.CandidateMatch{ID: uuid.New()}, nil
		},
		createMatchDecisionFn: func(_ context.Context, arg database.CreateMatchDecisionParams) (database.MatchDecision, error) {
			wroteStatus = arg.Status
			return database.MatchDecision{ID: uuid.New(), Status: arg.Status}, nil
		},
		updateClaimReconcileStatusFn: func(_ context.Context, _ database.UpdateClaimReconciliationStatusParams) error {
			return nil
		},
	}

	policyEngine := policy.NewEngine(store)
	engine := NewEngine(store, policyEngine)

	claim := database.Claim{
		ID:          claimID,
		OrgID:       uuid.New(),
		Ndc:         "12345",
		PharmacyNpi: "9999999999",
		ServiceDate: pgtype.Date{Time: time.Now(), Valid: true},
		PayerType:   pgtype.Text{String: "medicaid", Valid: true},
	}

	status, err := engine.reconcileClaim(context.Background(), jobID, claim)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != StatusDuplicateDiscount {
		t.Fatalf("expected duplicate discount, got %s", status)
	}
	if wroteStatus != string(StatusDuplicateDiscount) {
		t.Fatalf("expected written status to be duplicate discount, got %s", wroteStatus)
	}
}
