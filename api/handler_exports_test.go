package main

import (
	"bytes"
	"claims-system/internal/database"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestCreateExportMemberCannotSpecifyOtherOrg(t *testing.T) {
	userID := uuid.New()
	memberOrg := uuid.New()
	otherOrg := uuid.New()

	store := &mockStore{
		GetUserByIDFn: func(_ context.Context, id uuid.UUID) (database.User, error) {
			return newTestUser(id, memberOrg, "member"), nil
		},
	}

	apiCfg := apiConfig{
		DB:        store,
		Queue:     &mockQueue{},
		JwtSecret: "test-secret-test-secret-test-secret",
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/exports",
		bytes.NewBufferString(`{"report_type":"exceptions","from_date":"2026-01-01","to_date":"2026-01-31","org_id":"`+otherOrg.String()+`"}`))
	token, _ := createJWT(userID, apiCfg.JwtSecret)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	testRouter(apiCfg).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateExportMemberForcedToOwnOrg(t *testing.T) {
	userID := uuid.New()
	memberOrg := uuid.New()
	runID := uuid.New()
	var seenOrg pgtype.UUID

	store := &mockStore{
		GetUserByIDFn: func(_ context.Context, id uuid.UUID) (database.User, error) {
			return newTestUser(id, memberOrg, "member"), nil
		},
		CreateExportRunFn: func(_ context.Context, arg database.CreateExportRunParams) (database.ExportRun, error) {
			seenOrg = arg.OrgID
			return database.ExportRun{
				ID:         runID,
				OrgID:      arg.OrgID,
				ReportType: arg.ReportType,
				Status:     arg.Status,
				CreatedAt:  time.Now(),
			}, nil
		},
	}
	apiCfg := apiConfig{
		DB:        store,
		Queue:     &mockQueue{},
		JwtSecret: "test-secret-test-secret-test-secret",
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/exports",
		bytes.NewBufferString(`{"report_type":"exceptions","from_date":"2026-01-01","to_date":"2026-01-31"}`))
	token, _ := createJWT(userID, apiCfg.JwtSecret)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	testRouter(apiCfg).ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !seenOrg.Valid || seenOrg.Bytes != memberOrg {
		t.Fatalf("expected export org to be member org")
	}
}

func TestGetExportMemberForbiddenDifferentOrg(t *testing.T) {
	userID := uuid.New()
	memberOrg := uuid.New()
	otherOrg := uuid.New()
	runID := uuid.New()

	store := &mockStore{
		GetUserByIDFn: func(_ context.Context, id uuid.UUID) (database.User, error) {
			return newTestUser(id, memberOrg, "member"), nil
		},
		GetExportRunByIDFn: func(_ context.Context, id uuid.UUID) (database.ExportRun, error) {
			return database.ExportRun{
				ID:        id,
				OrgID:     pgUUID(otherOrg),
				Status:    "completed",
				CreatedAt: time.Now(),
			}, nil
		},
	}
	apiCfg := apiConfig{
		DB:        store,
		Queue:     &mockQueue{},
		JwtSecret: "test-secret-test-secret-test-secret",
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/exports/"+runID.String(), nil)
	token, _ := createJWT(userID, apiCfg.JwtSecret)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	testRouter(apiCfg).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestOverrideClaimAdminAllowedCrossOrg(t *testing.T) {
	adminID := uuid.New()
	adminOrg := uuid.New()
	claimOrg := uuid.New()
	claimID := uuid.New()
	overrideID := uuid.New()

	store := &mockStore{
		GetUserByIDFn: func(_ context.Context, id uuid.UUID) (database.User, error) {
			return newTestUser(id, adminOrg, "admin"), nil
		},
		GetClaimByIDFn: func(_ context.Context, id uuid.UUID) (database.Claim, error) {
			return database.Claim{ID: id, OrgID: claimOrg}, nil
		},
		GetMatchDecisionByClaimFn: func(_ context.Context, id uuid.UUID) (database.MatchDecision, error) {
			return database.MatchDecision{ClaimID: id, Status: "unmatched"}, nil
		},
		CreateManualOverrideFn: func(_ context.Context, arg database.CreateManualOverrideParams) (database.ManualOverrideEvent, error) {
			return database.ManualOverrideEvent{
				ID:             overrideID,
				ClaimID:        arg.ClaimID,
				PreviousStatus: arg.PreviousStatus,
				NewStatus:      arg.NewStatus,
				Reason:         arg.Reason,
				OverriddenBy:   arg.OverriddenBy,
				CreatedAt:      time.Now(),
			}, nil
		},
		UpdateMatchDecisionOverrideFn: func(_ context.Context, _ database.UpdateMatchDecisionOverrideParams) error {
			return nil
		},
	}
	apiCfg := apiConfig{
		DB:        store,
		Queue:     &mockQueue{},
		JwtSecret: "test-secret-test-secret-test-secret",
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/claims/"+claimID.String()+"/override",
		bytes.NewBufferString(`{"new_status":"matched","reason":"manual correction"}`))
	token, _ := createJWT(adminID, apiCfg.JwtSecret)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	testRouter(apiCfg).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}
