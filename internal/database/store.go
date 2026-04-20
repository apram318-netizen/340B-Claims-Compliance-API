package database

import (
	"context"

	"github.com/google/uuid"
)

// Store is the interface that all components depend on instead of *Queries
// directly, allowing test doubles to be injected.
// *Queries satisfies this interface automatically.
type Store interface {
	// ── Users ──────────────────────────────────────────────────────────────
	CreateUser(ctx context.Context, arg CreateUserParams) (User, error)
	GetUserByEmail(ctx context.Context, email string) (User, error)
	GetUserByID(ctx context.Context, id uuid.UUID) (User, error)
	UpdateUserActive(ctx context.Context, arg UpdateUserActiveParams) (User, error)
	UpdateUserName(ctx context.Context, arg UpdateUserNameParams) (User, error)
	SetUserMFA(ctx context.Context, arg SetUserMFAParams) (User, error)
	DisableUserMFA(ctx context.Context, id uuid.UUID) (User, error)
	InsertMfaBackupCode(ctx context.Context, arg InsertMfaBackupCodeParams) error
	DeleteMfaBackupCodesForUser(ctx context.Context, userID uuid.UUID) error
	ConsumeMfaBackupCode(ctx context.Context, arg ConsumeMfaBackupCodeParams) (uuid.UUID, error)

	// ── Organizations ───────────────────────────────────────────────────────
	CreateOrganization(ctx context.Context, arg CreateOrganizationParams) (Organization, error)
	GetOrganization(ctx context.Context, id uuid.UUID) (Organization, error)

	// ── Upload batches ──────────────────────────────────────────────────────
	CreateUploadBatches(ctx context.Context, arg CreateUploadBatchesParams) (UploadBatch, error)
	GetUploadBatches(ctx context.Context, id uuid.UUID) (UploadBatch, error)
	UpdateBatchStatus(ctx context.Context, arg UpdateBatchStatusParams) error
	UpdateBatchStatusWithError(ctx context.Context, arg UpdateBatchStatusWithErrorParams) error

	// ── Upload rows ─────────────────────────────────────────────────────────
	CreateUploadRow(ctx context.Context, arg CreateUploadRowParams) (UploadRow, error)
	GetUploadRowsByBatch(ctx context.Context, batchID uuid.UUID) ([]UploadRow, error)
	GetUploadRowsByBatchAndValidation(ctx context.Context, arg GetUploadRowsByBatchAndValidationParams) ([]UploadRow, error)

	// ── Audit events ────────────────────────────────────────────────────────
	CountAuditEventsByEntity(ctx context.Context, arg CountAuditEventsByEntityParams) (int64, error)
	CreateAuditEvent(ctx context.Context, arg CreateAuditEventParams) (AuditEvent, error)
	GetAuditEventsByEntity(ctx context.Context, arg GetAuditEventsByEntityParams) ([]AuditEvent, error)
	GetAuditEventsByEntityPaginated(ctx context.Context, arg GetAuditEventsByEntityPaginatedParams) ([]AuditEvent, error)

	// ── Claims ──────────────────────────────────────────────────────────────
	CountClaimsByBatch(ctx context.Context, batchID uuid.UUID) (int64, error)
	CreateClaim(ctx context.Context, arg CreateClaimParams) (Claim, error)
	GetClaimByID(ctx context.Context, id uuid.UUID) (Claim, error)
	GetClaimsByBatch(ctx context.Context, batchID uuid.UUID) ([]Claim, error)
	GetClaimsByBatchPaginated(ctx context.Context, arg GetClaimsByBatchPaginatedParams) ([]Claim, error)
	GetPendingClaimsByBatch(ctx context.Context, batchID uuid.UUID) ([]Claim, error)
	UpdateClaimReconciliationStatus(ctx context.Context, arg UpdateClaimReconciliationStatusParams) error
	ListClaimsForRxRehash(ctx context.Context) ([]ListClaimsForRxRehashRow, error)
	UpdateClaimHashedRxKey(ctx context.Context, arg UpdateClaimHashedRxKeyParams) error

	// ── Manufacturers ────────────────────────────────────────────────────────
	CountManufacturers(ctx context.Context) (int64, error)
	CountProductsByManufacturer(ctx context.Context, manufacturerID uuid.UUID) (int64, error)
	CreateManufacturer(ctx context.Context, arg CreateManufacturerParams) (Manufacturer, error)
	GetManufacturerByID(ctx context.Context, id uuid.UUID) (Manufacturer, error)
	GetManufacturerByNDC(ctx context.Context, ndc string) (Manufacturer, error)
	ListManufacturers(ctx context.Context) ([]Manufacturer, error)
	ListManufacturersPaginated(ctx context.Context, arg ListManufacturersPaginatedParams) ([]Manufacturer, error)
	CreateManufacturerProduct(ctx context.Context, arg CreateManufacturerProductParams) (ManufacturerProduct, error)
	ListProductsByManufacturer(ctx context.Context, manufacturerID uuid.UUID) ([]ManufacturerProduct, error)
	ListProductsByManufacturerPaginated(ctx context.Context, arg ListProductsByManufacturerPaginatedParams) ([]ManufacturerProduct, error)

	// ── Policies ────────────────────────────────────────────────────────────
	CountPoliciesByManufacturer(ctx context.Context, manufacturerID uuid.UUID) (int64, error)
	CountRulesByPolicyVersion(ctx context.Context, policyVersionID uuid.UUID) (int64, error)
	CreatePolicy(ctx context.Context, arg CreatePolicyParams) (Policy, error)
	GetPolicyByID(ctx context.Context, id uuid.UUID) (Policy, error)
	ListPoliciesByManufacturer(ctx context.Context, manufacturerID uuid.UUID) ([]Policy, error)
	ListPoliciesByManufacturerPaginated(ctx context.Context, arg ListPoliciesByManufacturerPaginatedParams) ([]Policy, error)
	CreatePolicyVersion(ctx context.Context, arg CreatePolicyVersionParams) (PolicyVersion, error)
	GetPolicyVersionByID(ctx context.Context, id uuid.UUID) (PolicyVersion, error)
	GetActivePolicyVersion(ctx context.Context, arg GetActivePolicyVersionParams) (PolicyVersion, error)
	CreatePolicyRule(ctx context.Context, arg CreatePolicyRuleParams) (PolicyRule, error)
	GetRulesByPolicyVersion(ctx context.Context, policyVersionID uuid.UUID) ([]PolicyRule, error)
	GetRulesByPolicyVersionPaginated(ctx context.Context, arg GetRulesByPolicyVersionPaginatedParams) ([]PolicyRule, error)

	// ── Rebate records ───────────────────────────────────────────────────────
	CreateRebateRecord(ctx context.Context, arg CreateRebateRecordParams) (RebateRecord, error)
	GetRebateRecordByID(ctx context.Context, id uuid.UUID) (RebateRecord, error)
	GetCandidateRebateRecords(ctx context.Context, arg GetCandidateRebateRecordsParams) ([]RebateRecord, error)

	// ── Reconciliation ───────────────────────────────────────────────────────
	CountMatchDecisionsByJob(ctx context.Context, jobID uuid.UUID) (int64, error)
	CreateReconciliationJob(ctx context.Context, arg CreateReconciliationJobParams) (ReconciliationJob, error)
	GetReconciliationJobByID(ctx context.Context, id uuid.UUID) (ReconciliationJob, error)
	GetReconciliationJobByBatch(ctx context.Context, batchID uuid.UUID) (ReconciliationJob, error)
	MarkJobStarted(ctx context.Context, id uuid.UUID) error
	UpdateJobStatus(ctx context.Context, arg UpdateJobStatusParams) error
	UpdateJobCounts(ctx context.Context, arg UpdateJobCountsParams) error
	CreateCandidateMatch(ctx context.Context, arg CreateCandidateMatchParams) (CandidateMatch, error)
	CreateMatchDecision(ctx context.Context, arg CreateMatchDecisionParams) (MatchDecision, error)
	GetMatchDecisionByClaim(ctx context.Context, claimID uuid.UUID) (MatchDecision, error)
	GetMatchDecisionsByJob(ctx context.Context, jobID uuid.UUID) ([]MatchDecision, error)
	GetMatchDecisionsByJobPaginated(ctx context.Context, arg GetMatchDecisionsByJobPaginatedParams) ([]MatchDecision, error)

	// ── Export runs ──────────────────────────────────────────────────────────
	CreateExportRun(ctx context.Context, arg CreateExportRunParams) (ExportRun, error)
	GetExportRunByID(ctx context.Context, id uuid.UUID) (ExportRun, error)
	UpdateExportRunStarted(ctx context.Context, id uuid.UUID) error
	UpdateExportRunCompleted(ctx context.Context, arg UpdateExportRunCompletedParams) error
	UpdateExportRunFailed(ctx context.Context, arg UpdateExportRunFailedParams) error

	// ── Manual overrides ─────────────────────────────────────────────────────
	CreateManualOverride(ctx context.Context, arg CreateManualOverrideParams) (ManualOverrideEvent, error)
	GetManualOverridesByClaim(ctx context.Context, claimID uuid.UUID) ([]ManualOverrideEvent, error)
	UpdateMatchDecisionOverride(ctx context.Context, arg UpdateMatchDecisionOverrideParams) error

	// ── Reporting ────────────────────────────────────────────────────────────
	GetManufacturerComplianceData(ctx context.Context, arg GetManufacturerComplianceDataParams) ([]GetManufacturerComplianceDataRow, error)
	GetDuplicateDiscountFindings(ctx context.Context, arg GetDuplicateDiscountFindingsParams) ([]GetDuplicateDiscountFindingsRow, error)
	GetSubmissionCompleteness(ctx context.Context, arg GetSubmissionCompletenessParams) ([]GetSubmissionCompletenessRow, error)
	GetUnresolvedExceptions(ctx context.Context, arg GetUnresolvedExceptionsParams) ([]GetUnresolvedExceptionsRow, error)
	GetStuckBatches(ctx context.Context) ([]UploadBatch, error)

	// ── Validation results ───────────────────────────────────────────────────
	CreateValidationResult(ctx context.Context, arg CreateValidationResultParams) (ValidationResult, error)

	// ── Contract pharmacies ──────────────────────────────────────────────────
	CreateContractPharmacy(ctx context.Context, arg CreateContractPharmacyParams) (ContractPharmacy, error)
	GetContractPharmacyByID(ctx context.Context, id uuid.UUID) (ContractPharmacy, error)
	ListContractPharmaciesByOrg(ctx context.Context, orgID uuid.UUID) ([]ContractPharmacy, error)
	UpdateContractPharmacyStatus(ctx context.Context, arg UpdateContractPharmacyStatusParams) error
	CreateContractPharmacyAuth(ctx context.Context, arg CreateContractPharmacyAuthParams) (ManufacturerContractPharmacyAuth, error)
	ListContractPharmacyAuthsByManufacturer(ctx context.Context, manufacturerID uuid.UUID) ([]ManufacturerContractPharmacyAuth, error)

	// ── Disputes ─────────────────────────────────────────────────────────────
	CreateDispute(ctx context.Context, arg CreateDisputeParams) (Dispute, error)
	GetDisputeByID(ctx context.Context, id uuid.UUID) (Dispute, error)
	ListDisputesByClaim(ctx context.Context, claimID uuid.UUID) ([]Dispute, error)
	ListDisputesByOrg(ctx context.Context, orgID uuid.UUID) ([]Dispute, error)
	UpdateDisputeStatus(ctx context.Context, arg UpdateDisputeStatusParams) (Dispute, error)

	// ── Password reset ───────────────────────────────────────────────────────
	CreatePasswordResetToken(ctx context.Context, arg CreatePasswordResetTokenParams) (PasswordResetToken, error)
	GetPasswordResetToken(ctx context.Context, tokenHash string) (PasswordResetToken, error)
	MarkPasswordResetTokenUsed(ctx context.Context, id uuid.UUID) error
	UpdateUserPassword(ctx context.Context, arg UpdateUserPasswordParams) error

	// ── Audit retention ──────────────────────────────────────────────────────
	PurgeExpiredAuditEvents(ctx context.Context) (int64, error)

	// ── Platform (tenant admin, webhooks, cases, SSO, flags) ────────────────
	GetOrganizationSettings(ctx context.Context, orgID uuid.UUID) (OrganizationSetting, error)
	UpsertOrganizationSettings(ctx context.Context, arg UpsertOrganizationSettingsParams) (OrganizationSetting, error)
	ListUsersByOrg(ctx context.Context, orgID uuid.UUID) ([]User, error)
	ListUsersByOrgPaginated(ctx context.Context, arg ListUsersByOrgPaginatedParams) ([]User, error)
	CountUsersByOrg(ctx context.Context, orgID uuid.UUID) (int64, error)
	GetUserByIDForOrg(ctx context.Context, arg GetUserByIDForOrgParams) (User, error)
	UpdateUserRole(ctx context.Context, arg UpdateUserRoleParams) (User, error)
	CreateWebhookSubscription(ctx context.Context, arg CreateWebhookSubscriptionParams) (WebhookSubscription, error)
	ListWebhookSubscriptionsByOrg(ctx context.Context, orgID uuid.UUID) ([]WebhookSubscription, error)
	GetWebhookSubscription(ctx context.Context, arg GetWebhookSubscriptionParams) (WebhookSubscription, error)
	GetWebhookSubscriptionByID(ctx context.Context, id uuid.UUID) (WebhookSubscription, error)
	UpdateWebhookSubscription(ctx context.Context, arg UpdateWebhookSubscriptionParams) (WebhookSubscription, error)
	DeleteWebhookSubscription(ctx context.Context, arg DeleteWebhookSubscriptionParams) error
	CreateWebhookDelivery(ctx context.Context, arg CreateWebhookDeliveryParams) (WebhookDelivery, error)
	ListWebhookDeliveriesPending(ctx context.Context, limit int32) ([]WebhookDelivery, error)
	ListWebhookDeliveriesBySubscription(ctx context.Context, arg ListWebhookDeliveriesBySubscriptionParams) ([]WebhookDelivery, error)
	UpdateWebhookDeliveryStatus(ctx context.Context, arg UpdateWebhookDeliveryStatusParams) error
	CreateExceptionCase(ctx context.Context, arg CreateExceptionCaseParams) (ExceptionCase, error)
	GetExceptionCase(ctx context.Context, arg GetExceptionCaseParams) (ExceptionCase, error)
	ListExceptionCasesByOrg(ctx context.Context, arg ListExceptionCasesByOrgParams) ([]ExceptionCase, error)
	CountExceptionCasesByOrg(ctx context.Context, orgID uuid.UUID) (int64, error)
	UpdateExceptionCase(ctx context.Context, arg UpdateExceptionCaseParams) (ExceptionCase, error)
	CreateCaseComment(ctx context.Context, arg CreateCaseCommentParams) (CaseComment, error)
	ListCaseComments(ctx context.Context, caseID uuid.UUID) ([]CaseComment, error)
	InsertUserExternalIdentity(ctx context.Context, arg InsertUserExternalIdentityParams) (UserExternalIdentity, error)
	GetUserExternalIdentityByIssuerSubject(ctx context.Context, arg GetUserExternalIdentityByIssuerSubjectParams) (UserExternalIdentity, error)
	GetOrgSSOConfig(ctx context.Context, orgID uuid.UUID) (OrgSsoConfig, error)
	UpsertOrgSSOConfig(ctx context.Context, arg UpsertOrgSSOConfigParams) (OrgSsoConfig, error)
	GetFeatureFlagOverride(ctx context.Context, arg GetFeatureFlagOverrideParams) (FeatureFlagOverride, error)
	UpsertFeatureFlagOverride(ctx context.Context, arg UpsertFeatureFlagOverrideParams) (FeatureFlagOverride, error)
	ListFeatureFlagOverridesByOrg(ctx context.Context, orgID uuid.UUID) ([]FeatureFlagOverride, error)
}

// compile-time proof that *Queries satisfies Store.
var _ Store = (*Queries)(nil)
