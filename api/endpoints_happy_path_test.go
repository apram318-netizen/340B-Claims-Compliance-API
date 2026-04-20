package main

import (
	"bytes"
	"claims-system/internal/database"
	"claims-system/internal/ratelimit"
	"claims-system/internal/storage"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/bcrypt"
)

func TestPublicEndpointsHappyPath(t *testing.T) {
	ids := testIDs()
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("Password1!"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	store := newHappyPathStore(ids, string(passwordHash), "")
	apiCfg := apiConfig{
		DB:        store,
		Queue:     &mockQueue{},
		JwtSecret: "test-secret-test-secret-test-secret",
		Limiter:   ratelimit.NewInMemoryLimiter(10, time.Minute),
	}
	router := testRouter(apiCfg)

	t.Run("register", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/register", bytes.NewBufferString(
			fmt.Sprintf(`{"org_id":"%s","email":"user@example.com","name":"Test User","password":"Password1!"}`, ids.orgID),
		))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("login", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/login", bytes.NewBufferString(
			`{"email":"user@example.com","password":"Password1!"}`,
		))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
		}
	})
}

func TestProtectedEndpointsHappyPath(t *testing.T) {
	ids := testIDs()
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("Password1!"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	tmpDir := t.TempDir()
	exportPath := filepath.Join(tmpDir, "export.csv")
	if err := os.WriteFile(exportPath, []byte("a,b\n1,2\n"), 0o644); err != nil {
		t.Fatalf("write export file: %v", err)
	}

	store := newHappyPathStore(ids, string(passwordHash), exportPath)
	queue := &mockQueue{}
	localSt, err := storage.NewLocalStorage(tmpDir)
	if err != nil {
		t.Fatalf("local storage: %v", err)
	}
	apiCfg := apiConfig{
		DB:        store,
		Queue:     queue,
		JwtSecret: "test-secret-test-secret-test-secret",
		Storage:   localSt,
		Limiter:   ratelimit.NewInMemoryLimiter(10, time.Minute),
	}
	router := testRouter(apiCfg)
	token, err := createJWT(ids.userID, apiCfg.JwtSecret)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	cases := []struct {
		name       string
		method     string
		path       string
		body       string
		multipart  bool
		wantStatus int
	}{
		{"create organization", http.MethodPost, "/v1/organizations", `{"name":"Org","entity_id":"E123"}`, false, http.StatusCreated},
		{"get organization", http.MethodGet, "/v1/organizations/" + ids.orgID.String(), "", false, http.StatusOK},
		{"create batch", http.MethodPost, "/v1/batches", `{"file_name":"claims.json","rows":[{"ndc":"12345","pharmacy_npi":"999","service_date":"2026-01-01","quantity":"1"}]}`, false, http.StatusCreated},
		{"upload batch", http.MethodPost, "/v1/batches/upload", "", true, http.StatusCreated},
		{"get batch", http.MethodGet, "/v1/batches/" + ids.batchID.String(), "", false, http.StatusOK},
		{"get batch claims", http.MethodGet, "/v1/batches/" + ids.batchID.String() + "/claims", "", false, http.StatusOK},
		{"get batch reconciliation job", http.MethodGet, "/v1/batches/" + ids.batchID.String() + "/reconciliation-job", "", false, http.StatusOK},
		{"get claim", http.MethodGet, "/v1/claims/" + ids.claimID.String(), "", false, http.StatusOK},
		{"get claim decision", http.MethodGet, "/v1/claims/" + ids.claimID.String() + "/decision", "", false, http.StatusOK},
		{"create manufacturer", http.MethodPost, "/v1/manufacturers", `{"name":"Mfg","labeler_code":"12345"}`, false, http.StatusCreated},
		{"list manufacturers", http.MethodGet, "/v1/manufacturers", "", false, http.StatusOK},
		{"get manufacturer", http.MethodGet, "/v1/manufacturers/" + ids.manufacturerID.String(), "", false, http.StatusOK},
		{"create manufacturer product", http.MethodPost, "/v1/manufacturers/" + ids.manufacturerID.String() + "/products", `{"ndc":"12345","product_name":"Drug"}`, false, http.StatusCreated},
		{"list manufacturer products", http.MethodGet, "/v1/manufacturers/" + ids.manufacturerID.String() + "/products", "", false, http.StatusOK},
		{"create policy", http.MethodPost, "/v1/manufacturers/" + ids.manufacturerID.String() + "/policies", `{"name":"Policy","status":"active"}`, false, http.StatusCreated},
		{"list policies", http.MethodGet, "/v1/manufacturers/" + ids.manufacturerID.String() + "/policies", "", false, http.StatusOK},
		{"create policy version", http.MethodPost, "/v1/policies/" + ids.policyID.String() + "/versions", `{"version_number":1,"effective_from":"2026-01-01"}`, false, http.StatusCreated},
		{"create policy rule", http.MethodPost, "/v1/policy-versions/" + ids.policyVersionID.String() + "/rules", `{"rule_type":"ndc_scope","rule_config":{"ndcs":["12345"]}}`, false, http.StatusCreated},
		{"list policy rules", http.MethodGet, "/v1/policy-versions/" + ids.policyVersionID.String() + "/rules", "", false, http.StatusOK},
		{"create rebate record", http.MethodPost, "/v1/rebate-records", fmt.Sprintf(`{"manufacturer_id":"%s","org_id":"%s","ndc":"12345","pharmacy_npi":"9999999999","service_date":"2026-01-01","quantity":1,"rebate_amount":12.34}`, ids.manufacturerID, ids.orgID), false, http.StatusCreated},
		{"get reconciliation job", http.MethodGet, "/v1/reconciliation-jobs/" + ids.jobID.String(), "", false, http.StatusOK},
		{"get job decisions", http.MethodGet, "/v1/reconciliation-jobs/" + ids.jobID.String() + "/decisions", "", false, http.StatusOK},
		{"create export", http.MethodPost, "/v1/exports", `{"report_type":"exceptions","from_date":"2026-01-01","to_date":"2026-01-31"}`, false, http.StatusCreated},
		{"get export", http.MethodGet, "/v1/exports/" + ids.exportID.String(), "", false, http.StatusOK},
		{"download export", http.MethodGet, "/v1/exports/" + ids.exportID.String() + "/download", "", false, http.StatusOK},
		{"retry export", http.MethodPost, "/v1/exports/" + ids.failedExportID.String() + "/retry", "", false, http.StatusAccepted},
		{"override claim", http.MethodPost, "/v1/claims/" + ids.claimID.String() + "/override", `{"new_status":"matched","reason":"manual correction"}`, false, http.StatusOK},
		{"get audit events", http.MethodGet, "/v1/audit-events?entity_type=batch&entity_id=" + ids.batchID.String(), "", false, http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var req *http.Request
			if tc.multipart {
				body, ct := makeMultipartFile(t, "claims.csv", []byte(
					"ndc,pharmacy_npi,service_date,quantity\n12345,9999999999,2026-01-01,1\n",
				))
				req = httptest.NewRequest(tc.method, tc.path, body)
				req.Header.Set("Content-Type", ct)
			} else {
				req = httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
				if tc.body != "" {
					req.Header.Set("Content-Type", "application/json")
				}
			}
			req.Header.Set("Authorization", "Bearer "+token)

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("expected %d, got %d body=%s", tc.wantStatus, rec.Code, rec.Body.String())
			}
		})
	}
}

type fixtureIDs struct {
	userID          uuid.UUID
	orgID           uuid.UUID
	batchID         uuid.UUID
	claimID         uuid.UUID
	jobID           uuid.UUID
	manufacturerID  uuid.UUID
	policyID        uuid.UUID
	policyVersionID uuid.UUID
	rebateID        uuid.UUID
	exportID        uuid.UUID
	failedExportID  uuid.UUID
	overrideID      uuid.UUID
}

func testIDs() fixtureIDs {
	return fixtureIDs{
		userID:          uuid.New(),
		orgID:           uuid.New(),
		batchID:         uuid.New(),
		claimID:         uuid.New(),
		jobID:           uuid.New(),
		manufacturerID:  uuid.New(),
		policyID:        uuid.New(),
		policyVersionID: uuid.New(),
		rebateID:        uuid.New(),
		exportID:        uuid.New(),
		failedExportID:  uuid.New(),
		overrideID:      uuid.New(),
	}
}

func newHappyPathStore(ids fixtureIDs, passwordHash, exportPath string) *mockStore {
	return &mockStore{
		CreateUserFn: func(_ context.Context, arg database.CreateUserParams) (database.User, error) {
			return database.User{ID: ids.userID, OrgID: arg.OrgID, Email: arg.Email, Name: arg.Name, Role: arg.Role, PasswordHash: arg.PasswordHash, Active: true}, nil
		},
		GetUserByEmailFn: func(_ context.Context, email string) (database.User, error) {
			return database.User{ID: ids.userID, OrgID: ids.orgID, Email: email, Name: "User", Role: "member", PasswordHash: passwordHash, Active: true}, nil
		},
		GetUserByIDFn: func(_ context.Context, id uuid.UUID) (database.User, error) {
			return newTestUser(id, ids.orgID, "admin"), nil
		},
		CreateOrganizationFn: func(_ context.Context, arg database.CreateOrganizationParams) (database.Organization, error) {
			return database.Organization{ID: ids.orgID, Name: arg.Name, EntityID: arg.EntityID}, nil
		},
		GetOrganizationFn: func(_ context.Context, id uuid.UUID) (database.Organization, error) {
			return database.Organization{ID: id, Name: "Org", EntityID: "E123"}, nil
		},
		CreateUploadBatchesFn: func(_ context.Context, arg database.CreateUploadBatchesParams) (database.UploadBatch, error) {
			return database.UploadBatch{ID: ids.batchID, OrgID: arg.OrgID, UploadedBy: arg.UploadedBy, FileName: arg.FileName, RowCount: arg.RowCount, Status: arg.Status}, nil
		},
		GetUploadBatchesFn: func(_ context.Context, id uuid.UUID) (database.UploadBatch, error) {
			return database.UploadBatch{ID: id, OrgID: ids.orgID, UploadedBy: ids.userID, FileName: "claims.csv", RowCount: 1, Status: "uploaded"}, nil
		},
		CreateUploadRowFn: func(_ context.Context, arg database.CreateUploadRowParams) (database.UploadRow, error) {
			return database.UploadRow{ID: uuid.New(), BatchID: arg.BatchID, RowNumber: arg.RowNumber, Data: arg.Data}, nil
		},
		CreateAuditEventFn: func(_ context.Context, arg database.CreateAuditEventParams) (database.AuditEvent, error) {
			return database.AuditEvent{ID: uuid.New(), EventType: arg.EventType, EntityType: arg.EntityType, EntityID: arg.EntityID, ActorID: arg.ActorID}, nil
		},
		GetClaimsByBatchFn: func(_ context.Context, _ uuid.UUID) ([]database.Claim, error) {
			return []database.Claim{{ID: ids.claimID, BatchID: ids.batchID, OrgID: ids.orgID, Ndc: "12345", PharmacyNpi: "9999999999", Quantity: 1, Status: "normalized"}}, nil
		},
		GetReconciliationJobByBatchFn: func(_ context.Context, _ uuid.UUID) (database.ReconciliationJob, error) {
			return database.ReconciliationJob{ID: ids.jobID, BatchID: ids.batchID, Status: "completed", TotalClaims: 1}, nil
		},
		GetClaimByIDFn: func(_ context.Context, id uuid.UUID) (database.Claim, error) {
			return database.Claim{ID: id, BatchID: ids.batchID, OrgID: ids.orgID, Ndc: "12345", PharmacyNpi: "9999999999", ServiceDate: pgtype.Date{Time: time.Now(), Valid: true}}, nil
		},
		CreateManufacturerFn: func(_ context.Context, arg database.CreateManufacturerParams) (database.Manufacturer, error) {
			return database.Manufacturer{ID: ids.manufacturerID, Name: arg.Name, LabelerCode: arg.LabelerCode}, nil
		},
		ListManufacturersFn: func(_ context.Context) ([]database.Manufacturer, error) {
			return []database.Manufacturer{{ID: ids.manufacturerID, Name: "Mfg", LabelerCode: "12345"}}, nil
		},
		GetManufacturerByIDFn: func(_ context.Context, id uuid.UUID) (database.Manufacturer, error) {
			return database.Manufacturer{ID: id, Name: "Mfg", LabelerCode: "12345"}, nil
		},
		CreateManufacturerProductFn: func(_ context.Context, arg database.CreateManufacturerProductParams) (database.ManufacturerProduct, error) {
			return database.ManufacturerProduct{ID: uuid.New(), ManufacturerID: arg.ManufacturerID, Ndc: arg.Ndc, ProductName: arg.ProductName}, nil
		},
		ListProductsByManufacturerFn: func(_ context.Context, manufacturerID uuid.UUID) ([]database.ManufacturerProduct, error) {
			return []database.ManufacturerProduct{{ID: uuid.New(), ManufacturerID: manufacturerID, Ndc: "12345"}}, nil
		},
		CreatePolicyFn: func(_ context.Context, arg database.CreatePolicyParams) (database.Policy, error) {
			return database.Policy{ID: ids.policyID, ManufacturerID: arg.ManufacturerID, Name: arg.Name, Status: arg.Status}, nil
		},
		ListPoliciesByManufacturerFn: func(_ context.Context, manufacturerID uuid.UUID) ([]database.Policy, error) {
			return []database.Policy{{ID: ids.policyID, ManufacturerID: manufacturerID, Name: "Policy", Status: "active"}}, nil
		},
		CreatePolicyVersionFn: func(_ context.Context, arg database.CreatePolicyVersionParams) (database.PolicyVersion, error) {
			return database.PolicyVersion{ID: ids.policyVersionID, PolicyID: arg.PolicyID, VersionNumber: arg.VersionNumber, EffectiveFrom: arg.EffectiveFrom, EffectiveTo: arg.EffectiveTo}, nil
		},
		GetPolicyVersionByIDFn: func(_ context.Context, id uuid.UUID) (database.PolicyVersion, error) {
			return database.PolicyVersion{ID: id, PolicyID: ids.policyID, VersionNumber: 1, EffectiveFrom: pgtype.Date{Time: time.Now(), Valid: true}}, nil
		},
		CreatePolicyRuleFn: func(_ context.Context, arg database.CreatePolicyRuleParams) (database.PolicyRule, error) {
			return database.PolicyRule{ID: uuid.New(), PolicyVersionID: arg.PolicyVersionID, RuleType: arg.RuleType, RuleConfig: arg.RuleConfig}, nil
		},
		GetRulesByPolicyVersionFn: func(_ context.Context, policyVersionID uuid.UUID) ([]database.PolicyRule, error) {
			return []database.PolicyRule{{ID: uuid.New(), PolicyVersionID: policyVersionID, RuleType: "ndc_scope", RuleConfig: []byte(`{"ndcs":["12345"]}`)}}, nil
		},
		CreateRebateRecordFn: func(_ context.Context, arg database.CreateRebateRecordParams) (database.RebateRecord, error) {
			return database.RebateRecord{
				ID:             ids.rebateID,
				ManufacturerID: arg.ManufacturerID,
				OrgID:          arg.OrgID,
				Ndc:            arg.Ndc,
				PharmacyNpi:    arg.PharmacyNpi,
				ServiceDate:    arg.ServiceDate,
				Quantity:       arg.Quantity,
				HashedRxKey:    arg.HashedRxKey,
				PayerType:      arg.PayerType,
				RebateAmount:   arg.RebateAmount,
				Source:         arg.Source,
				Status:         "active",
			}, nil
		},
		GetReconciliationJobByIDFn: func(_ context.Context, id uuid.UUID) (database.ReconciliationJob, error) {
			return database.ReconciliationJob{ID: id, BatchID: ids.batchID, Status: "completed", TotalClaims: 1}, nil
		},
		GetMatchDecisionsByJobFn: func(_ context.Context, jobID uuid.UUID) ([]database.MatchDecision, error) {
			return []database.MatchDecision{{ID: uuid.New(), JobID: jobID, ClaimID: ids.claimID, Status: "matched"}}, nil
		},
		CreateExportRunFn: func(_ context.Context, arg database.CreateExportRunParams) (database.ExportRun, error) {
			return database.ExportRun{ID: ids.exportID, OrgID: arg.OrgID, ManufacturerID: arg.ManufacturerID, ReportType: arg.ReportType, Status: arg.Status, RequestedBy: arg.RequestedBy, Params: arg.Params}, nil
		},
		GetExportRunByIDFn: func(_ context.Context, id uuid.UUID) (database.ExportRun, error) {
			if id == ids.failedExportID {
				return database.ExportRun{ID: id, OrgID: pgUUID(ids.orgID), ReportType: "exceptions", Status: "failed"}, nil
			}
			return database.ExportRun{
				ID:         id,
				OrgID:      pgUUID(ids.orgID),
				ReportType: "exceptions",
				Status:     "completed",
				FilePath:   pgtype.Text{String: exportPath, Valid: exportPath != ""},
			}, nil
		},
		GetMatchDecisionByClaimFn: func(_ context.Context, claimID uuid.UUID) (database.MatchDecision, error) {
			return database.MatchDecision{ID: uuid.New(), ClaimID: claimID, Status: "unmatched"}, nil
		},
		CreateManualOverrideFn: func(_ context.Context, arg database.CreateManualOverrideParams) (database.ManualOverrideEvent, error) {
			return database.ManualOverrideEvent{
				ID:             ids.overrideID,
				ClaimID:        arg.ClaimID,
				PreviousStatus: arg.PreviousStatus,
				NewStatus:      arg.NewStatus,
				Reason:         arg.Reason,
				RebateRecordID: arg.RebateRecordID,
				OverriddenBy:   arg.OverriddenBy,
			}, nil
		},
		UpdateMatchDecisionOverrideFn: func(_ context.Context, _ database.UpdateMatchDecisionOverrideParams) error {
			return nil
		},
		GetAuditEventsByEntityFn: func(_ context.Context, arg database.GetAuditEventsByEntityParams) ([]database.AuditEvent, error) {
			return []database.AuditEvent{{ID: uuid.New(), EventType: "batch_uploaded", EntityType: arg.EntityType, EntityID: arg.EntityID}}, nil
		},
	}
}
