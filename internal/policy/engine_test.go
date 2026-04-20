package policy

import (
	"claims-system/internal/database"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type mockStore struct {
	database.Store
	getManufacturerByNDCFn func(context.Context, string) (database.Manufacturer, error)
	getActiveVersionFn     func(context.Context, database.GetActivePolicyVersionParams) (database.PolicyVersion, error)
	getRulesFn             func(context.Context, uuid.UUID) ([]database.PolicyRule, error)
}

func (m *mockStore) GetManufacturerByNDC(ctx context.Context, ndc string) (database.Manufacturer, error) {
	return m.getManufacturerByNDCFn(ctx, ndc)
}

func (m *mockStore) GetActivePolicyVersion(ctx context.Context, arg database.GetActivePolicyVersionParams) (database.PolicyVersion, error) {
	return m.getActiveVersionFn(ctx, arg)
}

func (m *mockStore) GetRulesByPolicyVersion(ctx context.Context, id uuid.UUID) ([]database.PolicyRule, error) {
	return m.getRulesFn(ctx, id)
}

func TestEvaluateClaim_NoManufacturerDefaultsEligible(t *testing.T) {
	engine := NewEngine(&mockStore{
		getManufacturerByNDCFn: func(_ context.Context, _ string) (database.Manufacturer, error) {
			return database.Manufacturer{}, pgx.ErrNoRows
		},
	})

	claim := database.Claim{Ndc: "12345"}
	decision, err := engine.EvaluateClaim(context.Background(), claim)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !decision.Eligible {
		t.Fatalf("expected eligible decision")
	}
}

func TestEvaluateClaim_ExcludedByPayerRule(t *testing.T) {
	policyVersionID := uuid.New()
	store := &mockStore{
		getManufacturerByNDCFn: func(_ context.Context, _ string) (database.Manufacturer, error) {
			return database.Manufacturer{ID: uuid.New()}, nil
		},
		getActiveVersionFn: func(_ context.Context, _ database.GetActivePolicyVersionParams) (database.PolicyVersion, error) {
			return database.PolicyVersion{ID: policyVersionID}, nil
		},
		getRulesFn: func(_ context.Context, _ uuid.UUID) ([]database.PolicyRule, error) {
			return []database.PolicyRule{
				{
					RuleType:   "payer_channel",
					RuleConfig: []byte(`{"excluded_payer_types":["medicaid"]}`),
				},
			}, nil
		},
	}

	engine := NewEngine(store)
	claim := database.Claim{
		Ndc:       "12345",
		PayerType: pgtype.Text{String: "medicaid", Valid: true},
	}
	decision, err := engine.EvaluateClaim(context.Background(), claim)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Eligible {
		t.Fatalf("expected ineligible decision")
	}
	if decision.ExcludedByRule != "payer_channel" {
		t.Fatalf("expected exclusion by payer_channel, got %s", decision.ExcludedByRule)
	}
}

func TestEvalDateWindow(t *testing.T) {
	claim := database.Claim{
		ServiceDate: pgtype.Date{Time: time.Now().AddDate(0, 0, -2), Valid: true},
	}
	res := evalDateWindow(claim, []byte(`{"lookback_days":30}`))
	if !res.Passed {
		t.Fatalf("expected date window to pass, got %s", res.Reason)
	}
}
