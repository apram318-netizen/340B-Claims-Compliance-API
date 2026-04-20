package main

import (
	"claims-system/internal/database"
	"claims-system/internal/ratelimit"
	"claims-system/internal/storage"
	"context"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	amqp "github.com/rabbitmq/amqp091-go"
)

type mockStore struct {
	database.Store

	CreateUserFn                  func(context.Context, database.CreateUserParams) (database.User, error)
	GetUserByEmailFn              func(context.Context, string) (database.User, error)
	GetUserByIDFn                 func(context.Context, uuid.UUID) (database.User, error)
	CreateOrganizationFn          func(context.Context, database.CreateOrganizationParams) (database.Organization, error)
	GetOrganizationFn             func(context.Context, uuid.UUID) (database.Organization, error)
	CreateUploadBatchesFn         func(context.Context, database.CreateUploadBatchesParams) (database.UploadBatch, error)
	GetUploadBatchesFn            func(context.Context, uuid.UUID) (database.UploadBatch, error)
	CreateUploadRowFn             func(context.Context, database.CreateUploadRowParams) (database.UploadRow, error)
	CreateAuditEventFn            func(context.Context, database.CreateAuditEventParams) (database.AuditEvent, error)
	CountClaimsByBatchFn          func(context.Context, uuid.UUID) (int64, error)
	GetClaimsByBatchFn            func(context.Context, uuid.UUID) ([]database.Claim, error)
	GetClaimsByBatchPaginatedFn   func(context.Context, database.GetClaimsByBatchPaginatedParams) ([]database.Claim, error)
	GetReconciliationJobByBatchFn func(context.Context, uuid.UUID) (database.ReconciliationJob, error)
	GetClaimByIDFn                func(context.Context, uuid.UUID) (database.Claim, error)
	CreateManufacturerFn          func(context.Context, database.CreateManufacturerParams) (database.Manufacturer, error)
	CountManufacturersFn          func(context.Context) (int64, error)
	ListManufacturersFn           func(context.Context) ([]database.Manufacturer, error)
	ListManufacturersPaginatedFn  func(context.Context, database.ListManufacturersPaginatedParams) ([]database.Manufacturer, error)
	GetManufacturerByIDFn         func(context.Context, uuid.UUID) (database.Manufacturer, error)
	CreateManufacturerProductFn   func(context.Context, database.CreateManufacturerProductParams) (database.ManufacturerProduct, error)
	CountProductsByManufacturerFn func(context.Context, uuid.UUID) (int64, error)
	ListProductsByManufacturerFn  func(context.Context, uuid.UUID) ([]database.ManufacturerProduct, error)
	ListProductsPaginatedFn       func(context.Context, database.ListProductsByManufacturerPaginatedParams) ([]database.ManufacturerProduct, error)
	CreatePolicyFn                func(context.Context, database.CreatePolicyParams) (database.Policy, error)
	CountPoliciesByManufacturerFn func(context.Context, uuid.UUID) (int64, error)
	ListPoliciesByManufacturerFn  func(context.Context, uuid.UUID) ([]database.Policy, error)
	ListPoliciesPaginatedFn       func(context.Context, database.ListPoliciesByManufacturerPaginatedParams) ([]database.Policy, error)
	CreatePolicyVersionFn         func(context.Context, database.CreatePolicyVersionParams) (database.PolicyVersion, error)
	GetPolicyVersionByIDFn        func(context.Context, uuid.UUID) (database.PolicyVersion, error)
	CreatePolicyRuleFn            func(context.Context, database.CreatePolicyRuleParams) (database.PolicyRule, error)
	CountRulesByPolicyVersionFn   func(context.Context, uuid.UUID) (int64, error)
	GetRulesByPolicyVersionFn     func(context.Context, uuid.UUID) ([]database.PolicyRule, error)
	GetRulesPaginatedFn           func(context.Context, database.GetRulesByPolicyVersionPaginatedParams) ([]database.PolicyRule, error)
	CreateRebateRecordFn          func(context.Context, database.CreateRebateRecordParams) (database.RebateRecord, error)
	GetReconciliationJobByIDFn    func(context.Context, uuid.UUID) (database.ReconciliationJob, error)
	CountMatchDecisionsByJobFn    func(context.Context, uuid.UUID) (int64, error)
	GetMatchDecisionsByJobFn      func(context.Context, uuid.UUID) ([]database.MatchDecision, error)
	GetMatchDecisionsPaginatedFn  func(context.Context, database.GetMatchDecisionsByJobPaginatedParams) ([]database.MatchDecision, error)
	CreateExportRunFn             func(context.Context, database.CreateExportRunParams) (database.ExportRun, error)
	GetExportRunByIDFn            func(context.Context, uuid.UUID) (database.ExportRun, error)
	GetMatchDecisionByClaimFn     func(context.Context, uuid.UUID) (database.MatchDecision, error)
	CreateManualOverrideFn        func(context.Context, database.CreateManualOverrideParams) (database.ManualOverrideEvent, error)
	UpdateMatchDecisionOverrideFn func(context.Context, database.UpdateMatchDecisionOverrideParams) error
	CountAuditEventsFn            func(context.Context, database.CountAuditEventsByEntityParams) (int64, error)
	GetAuditEventsByEntityFn      func(context.Context, database.GetAuditEventsByEntityParams) ([]database.AuditEvent, error)
	GetAuditEventsPaginatedFn     func(context.Context, database.GetAuditEventsByEntityPaginatedParams) ([]database.AuditEvent, error)
}

func (m *mockStore) CreateUser(ctx context.Context, arg database.CreateUserParams) (database.User, error) {
	return m.CreateUserFn(ctx, arg)
}

func (m *mockStore) GetUserByEmail(ctx context.Context, email string) (database.User, error) {
	return m.GetUserByEmailFn(ctx, email)
}

func (m *mockStore) GetUserByID(ctx context.Context, id uuid.UUID) (database.User, error) {
	return m.GetUserByIDFn(ctx, id)
}

func (m *mockStore) CreateOrganization(ctx context.Context, arg database.CreateOrganizationParams) (database.Organization, error) {
	return m.CreateOrganizationFn(ctx, arg)
}

func (m *mockStore) GetOrganization(ctx context.Context, id uuid.UUID) (database.Organization, error) {
	return m.GetOrganizationFn(ctx, id)
}

func (m *mockStore) CreateUploadBatches(ctx context.Context, arg database.CreateUploadBatchesParams) (database.UploadBatch, error) {
	return m.CreateUploadBatchesFn(ctx, arg)
}

func (m *mockStore) GetUploadBatches(ctx context.Context, id uuid.UUID) (database.UploadBatch, error) {
	return m.GetUploadBatchesFn(ctx, id)
}

func (m *mockStore) CreateUploadRow(ctx context.Context, arg database.CreateUploadRowParams) (database.UploadRow, error) {
	return m.CreateUploadRowFn(ctx, arg)
}

func (m *mockStore) CreateAuditEvent(ctx context.Context, arg database.CreateAuditEventParams) (database.AuditEvent, error) {
	if m.CreateAuditEventFn == nil {
		return database.AuditEvent{}, nil
	}
	return m.CreateAuditEventFn(ctx, arg)
}

func (m *mockStore) GetClaimsByBatch(ctx context.Context, batchID uuid.UUID) ([]database.Claim, error) {
	return m.GetClaimsByBatchFn(ctx, batchID)
}

func (m *mockStore) CountClaimsByBatch(ctx context.Context, batchID uuid.UUID) (int64, error) {
	if m.CountClaimsByBatchFn != nil {
		return m.CountClaimsByBatchFn(ctx, batchID)
	}
	items, err := m.GetClaimsByBatchFn(ctx, batchID)
	if err != nil {
		return 0, err
	}
	return int64(len(items)), nil
}

func (m *mockStore) GetClaimsByBatchPaginated(ctx context.Context, arg database.GetClaimsByBatchPaginatedParams) ([]database.Claim, error) {
	if m.GetClaimsByBatchPaginatedFn != nil {
		return m.GetClaimsByBatchPaginatedFn(ctx, arg)
	}
	items, err := m.GetClaimsByBatchFn(ctx, arg.BatchID)
	if err != nil {
		return nil, err
	}
	return paginate(items, int(arg.Limit), int(arg.Offset)), nil
}

func (m *mockStore) GetReconciliationJobByBatch(ctx context.Context, batchID uuid.UUID) (database.ReconciliationJob, error) {
	return m.GetReconciliationJobByBatchFn(ctx, batchID)
}

func (m *mockStore) CreateManufacturer(ctx context.Context, arg database.CreateManufacturerParams) (database.Manufacturer, error) {
	return m.CreateManufacturerFn(ctx, arg)
}

func (m *mockStore) ListManufacturers(ctx context.Context) ([]database.Manufacturer, error) {
	return m.ListManufacturersFn(ctx)
}

func (m *mockStore) CountManufacturers(ctx context.Context) (int64, error) {
	if m.CountManufacturersFn != nil {
		return m.CountManufacturersFn(ctx)
	}
	items, err := m.ListManufacturersFn(ctx)
	if err != nil {
		return 0, err
	}
	return int64(len(items)), nil
}

func (m *mockStore) ListManufacturersPaginated(ctx context.Context, arg database.ListManufacturersPaginatedParams) ([]database.Manufacturer, error) {
	if m.ListManufacturersPaginatedFn != nil {
		return m.ListManufacturersPaginatedFn(ctx, arg)
	}
	items, err := m.ListManufacturersFn(ctx)
	if err != nil {
		return nil, err
	}
	return paginate(items, int(arg.Limit), int(arg.Offset)), nil
}

func (m *mockStore) GetManufacturerByID(ctx context.Context, id uuid.UUID) (database.Manufacturer, error) {
	return m.GetManufacturerByIDFn(ctx, id)
}

func (m *mockStore) CreateManufacturerProduct(ctx context.Context, arg database.CreateManufacturerProductParams) (database.ManufacturerProduct, error) {
	return m.CreateManufacturerProductFn(ctx, arg)
}

func (m *mockStore) ListProductsByManufacturer(ctx context.Context, manufacturerID uuid.UUID) ([]database.ManufacturerProduct, error) {
	return m.ListProductsByManufacturerFn(ctx, manufacturerID)
}

func (m *mockStore) CountProductsByManufacturer(ctx context.Context, manufacturerID uuid.UUID) (int64, error) {
	if m.CountProductsByManufacturerFn != nil {
		return m.CountProductsByManufacturerFn(ctx, manufacturerID)
	}
	items, err := m.ListProductsByManufacturerFn(ctx, manufacturerID)
	if err != nil {
		return 0, err
	}
	return int64(len(items)), nil
}

func (m *mockStore) ListProductsByManufacturerPaginated(ctx context.Context, arg database.ListProductsByManufacturerPaginatedParams) ([]database.ManufacturerProduct, error) {
	if m.ListProductsPaginatedFn != nil {
		return m.ListProductsPaginatedFn(ctx, arg)
	}
	items, err := m.ListProductsByManufacturerFn(ctx, arg.ManufacturerID)
	if err != nil {
		return nil, err
	}
	return paginate(items, int(arg.Limit), int(arg.Offset)), nil
}

func (m *mockStore) CreatePolicy(ctx context.Context, arg database.CreatePolicyParams) (database.Policy, error) {
	return m.CreatePolicyFn(ctx, arg)
}

func (m *mockStore) ListPoliciesByManufacturer(ctx context.Context, manufacturerID uuid.UUID) ([]database.Policy, error) {
	return m.ListPoliciesByManufacturerFn(ctx, manufacturerID)
}

func (m *mockStore) CountPoliciesByManufacturer(ctx context.Context, manufacturerID uuid.UUID) (int64, error) {
	if m.CountPoliciesByManufacturerFn != nil {
		return m.CountPoliciesByManufacturerFn(ctx, manufacturerID)
	}
	items, err := m.ListPoliciesByManufacturerFn(ctx, manufacturerID)
	if err != nil {
		return 0, err
	}
	return int64(len(items)), nil
}

func (m *mockStore) ListPoliciesByManufacturerPaginated(ctx context.Context, arg database.ListPoliciesByManufacturerPaginatedParams) ([]database.Policy, error) {
	if m.ListPoliciesPaginatedFn != nil {
		return m.ListPoliciesPaginatedFn(ctx, arg)
	}
	items, err := m.ListPoliciesByManufacturerFn(ctx, arg.ManufacturerID)
	if err != nil {
		return nil, err
	}
	return paginate(items, int(arg.Limit), int(arg.Offset)), nil
}

func (m *mockStore) CreatePolicyVersion(ctx context.Context, arg database.CreatePolicyVersionParams) (database.PolicyVersion, error) {
	return m.CreatePolicyVersionFn(ctx, arg)
}

func (m *mockStore) GetPolicyVersionByID(ctx context.Context, id uuid.UUID) (database.PolicyVersion, error) {
	return m.GetPolicyVersionByIDFn(ctx, id)
}

func (m *mockStore) CreatePolicyRule(ctx context.Context, arg database.CreatePolicyRuleParams) (database.PolicyRule, error) {
	return m.CreatePolicyRuleFn(ctx, arg)
}

func (m *mockStore) GetRulesByPolicyVersion(ctx context.Context, policyVersionID uuid.UUID) ([]database.PolicyRule, error) {
	return m.GetRulesByPolicyVersionFn(ctx, policyVersionID)
}

func (m *mockStore) CountRulesByPolicyVersion(ctx context.Context, policyVersionID uuid.UUID) (int64, error) {
	if m.CountRulesByPolicyVersionFn != nil {
		return m.CountRulesByPolicyVersionFn(ctx, policyVersionID)
	}
	items, err := m.GetRulesByPolicyVersionFn(ctx, policyVersionID)
	if err != nil {
		return 0, err
	}
	return int64(len(items)), nil
}

func (m *mockStore) GetRulesByPolicyVersionPaginated(ctx context.Context, arg database.GetRulesByPolicyVersionPaginatedParams) ([]database.PolicyRule, error) {
	if m.GetRulesPaginatedFn != nil {
		return m.GetRulesPaginatedFn(ctx, arg)
	}
	items, err := m.GetRulesByPolicyVersionFn(ctx, arg.PolicyVersionID)
	if err != nil {
		return nil, err
	}
	return paginate(items, int(arg.Limit), int(arg.Offset)), nil
}

func (m *mockStore) CreateRebateRecord(ctx context.Context, arg database.CreateRebateRecordParams) (database.RebateRecord, error) {
	return m.CreateRebateRecordFn(ctx, arg)
}

func (m *mockStore) GetReconciliationJobByID(ctx context.Context, id uuid.UUID) (database.ReconciliationJob, error) {
	return m.GetReconciliationJobByIDFn(ctx, id)
}

func (m *mockStore) GetMatchDecisionsByJob(ctx context.Context, id uuid.UUID) ([]database.MatchDecision, error) {
	return m.GetMatchDecisionsByJobFn(ctx, id)
}

func (m *mockStore) CountMatchDecisionsByJob(ctx context.Context, id uuid.UUID) (int64, error) {
	if m.CountMatchDecisionsByJobFn != nil {
		return m.CountMatchDecisionsByJobFn(ctx, id)
	}
	items, err := m.GetMatchDecisionsByJobFn(ctx, id)
	if err != nil {
		return 0, err
	}
	return int64(len(items)), nil
}

func (m *mockStore) GetMatchDecisionsByJobPaginated(ctx context.Context, arg database.GetMatchDecisionsByJobPaginatedParams) ([]database.MatchDecision, error) {
	if m.GetMatchDecisionsPaginatedFn != nil {
		return m.GetMatchDecisionsPaginatedFn(ctx, arg)
	}
	items, err := m.GetMatchDecisionsByJobFn(ctx, arg.JobID)
	if err != nil {
		return nil, err
	}
	return paginate(items, int(arg.Limit), int(arg.Offset)), nil
}

func (m *mockStore) CreateExportRun(ctx context.Context, arg database.CreateExportRunParams) (database.ExportRun, error) {
	return m.CreateExportRunFn(ctx, arg)
}

func (m *mockStore) GetExportRunByID(ctx context.Context, id uuid.UUID) (database.ExportRun, error) {
	return m.GetExportRunByIDFn(ctx, id)
}

func (m *mockStore) GetClaimByID(ctx context.Context, id uuid.UUID) (database.Claim, error) {
	return m.GetClaimByIDFn(ctx, id)
}

func (m *mockStore) GetMatchDecisionByClaim(ctx context.Context, claimID uuid.UUID) (database.MatchDecision, error) {
	return m.GetMatchDecisionByClaimFn(ctx, claimID)
}

func (m *mockStore) CreateManualOverride(ctx context.Context, arg database.CreateManualOverrideParams) (database.ManualOverrideEvent, error) {
	return m.CreateManualOverrideFn(ctx, arg)
}

func (m *mockStore) UpdateMatchDecisionOverride(ctx context.Context, arg database.UpdateMatchDecisionOverrideParams) error {
	return m.UpdateMatchDecisionOverrideFn(ctx, arg)
}

func (m *mockStore) GetAuditEventsByEntity(ctx context.Context, arg database.GetAuditEventsByEntityParams) ([]database.AuditEvent, error) {
	return m.GetAuditEventsByEntityFn(ctx, arg)
}

func (m *mockStore) CountAuditEventsByEntity(ctx context.Context, arg database.CountAuditEventsByEntityParams) (int64, error) {
	if m.CountAuditEventsFn != nil {
		return m.CountAuditEventsFn(ctx, arg)
	}
	items, err := m.GetAuditEventsByEntityFn(ctx, database.GetAuditEventsByEntityParams{
		EntityType: arg.EntityType,
		EntityID:   arg.EntityID,
	})
	if err != nil {
		return 0, err
	}
	return int64(len(items)), nil
}

func (m *mockStore) GetAuditEventsByEntityPaginated(ctx context.Context, arg database.GetAuditEventsByEntityPaginatedParams) ([]database.AuditEvent, error) {
	if m.GetAuditEventsPaginatedFn != nil {
		return m.GetAuditEventsPaginatedFn(ctx, arg)
	}
	items, err := m.GetAuditEventsByEntityFn(ctx, database.GetAuditEventsByEntityParams{
		EntityType: arg.EntityType,
		EntityID:   arg.EntityID,
	})
	if err != nil {
		return nil, err
	}
	return paginate(items, int(arg.Limit), int(arg.Offset)), nil
}

// Stubs for Store methods added after mockStore was introduced; tests that need
// behavior should override via embedding or dedicated fakes.
var errMockStoreNotStubbed = errors.New("mockStore: not stubbed for this test")

func (m *mockStore) CreateContractPharmacy(ctx context.Context, arg database.CreateContractPharmacyParams) (database.ContractPharmacy, error) {
	return database.ContractPharmacy{}, errMockStoreNotStubbed
}

func (m *mockStore) GetContractPharmacyByID(ctx context.Context, id uuid.UUID) (database.ContractPharmacy, error) {
	return database.ContractPharmacy{}, pgx.ErrNoRows
}

func (m *mockStore) ListContractPharmaciesByOrg(ctx context.Context, orgID uuid.UUID) ([]database.ContractPharmacy, error) {
	return nil, nil
}

func (m *mockStore) UpdateContractPharmacyStatus(ctx context.Context, arg database.UpdateContractPharmacyStatusParams) error {
	return errMockStoreNotStubbed
}

func (m *mockStore) CreateContractPharmacyAuth(ctx context.Context, arg database.CreateContractPharmacyAuthParams) (database.ManufacturerContractPharmacyAuth, error) {
	return database.ManufacturerContractPharmacyAuth{}, errMockStoreNotStubbed
}

func (m *mockStore) ListContractPharmacyAuthsByManufacturer(ctx context.Context, manufacturerID uuid.UUID) ([]database.ManufacturerContractPharmacyAuth, error) {
	return nil, nil
}

func (m *mockStore) CreateDispute(ctx context.Context, arg database.CreateDisputeParams) (database.Dispute, error) {
	return database.Dispute{}, errMockStoreNotStubbed
}

func (m *mockStore) GetDisputeByID(ctx context.Context, id uuid.UUID) (database.Dispute, error) {
	return database.Dispute{}, pgx.ErrNoRows
}

func (m *mockStore) ListDisputesByClaim(ctx context.Context, claimID uuid.UUID) ([]database.Dispute, error) {
	return nil, nil
}

func (m *mockStore) ListDisputesByOrg(ctx context.Context, orgID uuid.UUID) ([]database.Dispute, error) {
	return nil, nil
}

func (m *mockStore) UpdateDisputeStatus(ctx context.Context, arg database.UpdateDisputeStatusParams) (database.Dispute, error) {
	return database.Dispute{}, errMockStoreNotStubbed
}

func (m *mockStore) CreatePasswordResetToken(ctx context.Context, arg database.CreatePasswordResetTokenParams) (database.PasswordResetToken, error) {
	return database.PasswordResetToken{}, errMockStoreNotStubbed
}

func (m *mockStore) GetPasswordResetToken(ctx context.Context, tokenHash string) (database.PasswordResetToken, error) {
	return database.PasswordResetToken{}, pgx.ErrNoRows
}

func (m *mockStore) MarkPasswordResetTokenUsed(ctx context.Context, id uuid.UUID) error {
	return nil
}

func (m *mockStore) UpdateUserPassword(ctx context.Context, arg database.UpdateUserPasswordParams) error {
	return errMockStoreNotStubbed
}

func (m *mockStore) SetUserMFA(ctx context.Context, arg database.SetUserMFAParams) (database.User, error) {
	return database.User{}, errMockStoreNotStubbed
}

func (m *mockStore) DisableUserMFA(ctx context.Context, id uuid.UUID) (database.User, error) {
	return database.User{}, errMockStoreNotStubbed
}

func (m *mockStore) InsertMfaBackupCode(ctx context.Context, arg database.InsertMfaBackupCodeParams) error {
	return errMockStoreNotStubbed
}

func (m *mockStore) DeleteMfaBackupCodesForUser(ctx context.Context, userID uuid.UUID) error {
	return errMockStoreNotStubbed
}

func (m *mockStore) ConsumeMfaBackupCode(ctx context.Context, arg database.ConsumeMfaBackupCodeParams) (uuid.UUID, error) {
	return uuid.Nil, errMockStoreNotStubbed
}

func (m *mockStore) PurgeExpiredAuditEvents(ctx context.Context) (int64, error) {
	return 0, nil
}

func (m *mockStore) ListClaimsForRxRehash(ctx context.Context) ([]database.ListClaimsForRxRehashRow, error) {
	return nil, errMockStoreNotStubbed
}

func (m *mockStore) UpdateClaimHashedRxKey(ctx context.Context, arg database.UpdateClaimHashedRxKeyParams) error {
	return errMockStoreNotStubbed
}

type mockQueue struct {
	published []amqp.Publishing
}

func (m *mockQueue) PublishWithContext(
	_ context.Context,
	_ string,
	_ string,
	_ bool,
	_ bool,
	msg amqp.Publishing,
) error {
	m.published = append(m.published, msg)
	return nil
}

func testRouter(apiCfg apiConfig) http.Handler {
	cfg := apiCfg
	if cfg.Limiter == nil {
		cfg.Limiter = ratelimit.NewInMemoryLimiter(10, time.Minute)
	}
	if cfg.Storage == nil {
		dir, err := os.MkdirTemp("", "claims-api-test-export")
		if err != nil {
			panic(err)
		}
		ls, err := storage.NewLocalStorage(dir)
		if err != nil {
			panic(err)
		}
		cfg.Storage = ls
	}

	router := chi.NewRouter()

	router.Use(middleware.RequestID)
	router.Use(middlewareTrace)
	router.Use(middleware.RealIP)
	router.Use(securityHeadersMiddleware)
	router.Use(requestLogger)
	router.Use(middleware.Recoverer)
	router.Use(corsMiddleware)
	router.Use(middleware.RequestSize(5 * 1024 * 1024))

	router.Get("/health", cfg.handlerHealth)
	router.Get("/metrics", handlerMetrics)

	router.Post("/v1/register", cfg.handlerRegister)
	router.With(newLoginRateLimiter(cfg.Limiter)).Post("/v1/login", cfg.handlerLogin)
	router.Post("/v1/password-reset/request", cfg.handlerPasswordResetRequest)
	router.Post("/v1/password-reset/confirm", cfg.handlerPasswordResetConfirm)

	router.Group(func(r chi.Router) {
		r.Use(cfg.middlewareAuth)

		r.Get("/v1/organizations/{id}", cfg.handlerGetOrganization)
		r.With(cfg.middlewareRequireAdmin, cfg.middlewareRequireMFAStepUp).Post("/v1/organizations", cfg.handlerCreateOrganization)

		r.Post("/v1/batches", cfg.handlerCreateBatch)
		r.Post("/v1/batches/upload", cfg.handlerUploadBatch)
		r.Get("/v1/batches/{id}", cfg.handlerGetBatch)
		r.Get("/v1/batches/{id}/claims", cfg.handlerGetBatchClaims)
		r.Get("/v1/batches/{id}/reconciliation-job", cfg.handlerGetBatchReconciliationJob)
		r.Get("/v1/claims/{id}", cfg.handlerGetClaim)
		r.Get("/v1/claims/{id}/decision", cfg.handlerGetClaimDecision)

		r.Get("/v1/manufacturers", cfg.handlerListManufacturers)
		r.Get("/v1/manufacturers/{id}", cfg.handlerGetManufacturer)
		r.Get("/v1/manufacturers/{id}/products", cfg.handlerListManufacturerProducts)
		r.With(cfg.middlewareRequireAdmin, cfg.middlewareRequireMFAStepUp).Post("/v1/manufacturers", cfg.handlerCreateManufacturer)
		r.With(cfg.middlewareRequireAdmin, cfg.middlewareRequireMFAStepUp).Post("/v1/manufacturers/{id}/products", cfg.handlerCreateManufacturerProduct)

		r.Get("/v1/manufacturers/{id}/policies", cfg.handlerListPolicies)
		r.Get("/v1/policy-versions/{id}/rules", cfg.handlerListPolicyRules)
		r.With(cfg.middlewareRequireAdmin, cfg.middlewareRequireMFAStepUp).Post("/v1/manufacturers/{id}/policies", cfg.handlerCreatePolicy)
		r.With(cfg.middlewareRequireAdmin, cfg.middlewareRequireMFAStepUp).Post("/v1/policies/{id}/versions", cfg.handlerCreatePolicyVersion)
		r.With(cfg.middlewareRequireAdmin, cfg.middlewareRequireMFAStepUp).Post("/v1/policy-versions/{id}/rules", cfg.handlerCreatePolicyRule)

		r.With(cfg.middlewareRequireAdmin, cfg.middlewareRequireMFAStepUp).Post("/v1/rebate-records", cfg.handlerCreateRebateRecord)

		r.Get("/v1/reconciliation-jobs/{id}", cfg.handlerGetReconciliationJob)
		r.Get("/v1/reconciliation-jobs/{id}/decisions", cfg.handlerGetJobDecisions)

		r.Post("/v1/exports", cfg.handlerCreateExport)
		r.Get("/v1/exports/{id}", cfg.handlerGetExport)
		r.Get("/v1/exports/{id}/download", cfg.handlerDownloadExport)
		r.Post("/v1/exports/{id}/retry", cfg.handlerRetryExport)

		r.Post("/v1/claims/{id}/override", cfg.handlerOverrideClaim)

		r.Get("/v1/audit-events", cfg.handlerGetAuditEvents)

		r.Post("/v1/logout", cfg.handlerLogout)
		r.Post("/v1/mfa/setup", cfg.handlerMFASetup)
		r.Post("/v1/mfa/enable", cfg.handlerMFAEnable)
		r.Post("/v1/mfa/disable", cfg.handlerMFADisable)
		r.Post("/v1/mfa/backup-codes/regenerate", cfg.handlerMFABackupCodesRegenerate)
		r.Post("/v1/mfa/step-up", cfg.handlerMFAStepUp)

		r.Get("/v1/contract-pharmacies", cfg.handlerListContractPharmacies)
		r.Get("/v1/contract-pharmacies/{id}", cfg.handlerGetContractPharmacy)
		r.With(cfg.middlewareRequireAdmin, cfg.middlewareRequireMFAStepUp).Post("/v1/contract-pharmacies", cfg.handlerCreateContractPharmacy)
		r.With(cfg.middlewareRequireAdmin, cfg.middlewareRequireMFAStepUp).Patch("/v1/contract-pharmacies/{id}/status", cfg.handlerUpdateContractPharmacyStatus)
		r.With(cfg.middlewareRequireAdmin, cfg.middlewareRequireMFAStepUp).Post("/v1/manufacturers/{id}/contract-pharmacy-auths", cfg.handlerCreateContractPharmacyAuth)

		r.Post("/v1/disputes", cfg.handlerCreateDispute)
		r.Get("/v1/disputes/{id}", cfg.handlerGetDispute)
		r.Get("/v1/claims/{id}/disputes", cfg.handlerListClaimDisputes)
		r.Post("/v1/disputes/{id}/submit", cfg.handlerSubmitDispute)
		r.With(cfg.middlewareRequireAdmin, cfg.middlewareRequireMFAStepUp).Post("/v1/disputes/{id}/resolve", cfg.handlerResolveDispute)
		r.Post("/v1/disputes/{id}/withdraw", cfg.handlerWithdrawDispute)
	})

	return router
}

func newTestUser(userID, orgID uuid.UUID, role string) database.User {
	return database.User{
		ID:     userID,
		OrgID:  orgID,
		Role:   role,
		Active: true,
	}
}

func pgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}
