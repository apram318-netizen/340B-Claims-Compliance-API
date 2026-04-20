package reporting

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"claims-system/internal/database"
	"claims-system/internal/feature"
	"claims-system/internal/sensitivity"
	"claims-system/internal/storage"
	"claims-system/internal/webhook"
)

const (
	ReportManufacturerCompliance = "manufacturer_compliance"
	ReportDuplicateFindings      = "duplicate_findings"
	ReportSubmissionCompleteness = "submission_completeness"
	ReportExceptions             = "exceptions"
	ReportBatchReconciliation    = "batch_reconciliation_results"
)

// ReportParams mirrors the JSONB params column in export_runs.
type ReportParams struct {
	FromDate       string `json:"from_date"`
	ToDate         string `json:"to_date"`
	ManufacturerID string `json:"manufacturer_id,omitempty"`
	OrgID          string `json:"org_id,omitempty"`
	BatchID        string `json:"batch_id,omitempty"`
}

// Generator handles report generation and export_run lifecycle updates.
type Generator struct {
	db       database.Store
	storage  storage.ExportStorage
	features *feature.Resolver
}

func NewGenerator(db database.Store, st storage.ExportStorage, fr *feature.Resolver) *Generator {
	return &Generator{db: db, storage: st, features: fr}
}

// GenerateReport executes the full generate → write → update cycle for one export run.
func (g *Generator) GenerateReport(ctx context.Context, run database.ExportRun) error {
	if err := g.db.UpdateExportRunStarted(ctx, run.ID); err != nil {
		return fmt.Errorf("mark running: %w", err)
	}

	var params ReportParams
	if err := json.Unmarshal(run.Params, &params); err != nil {
		return g.fail(ctx, run.ID, fmt.Sprintf("invalid params JSON: %v", err))
	}

	fromDate, err := parseDate(params.FromDate)
	if err != nil {
		return g.fail(ctx, run.ID, fmt.Sprintf("invalid from_date: %v", err))
	}
	toDate, err := parseDate(params.ToDate)
	if err != nil {
		return g.fail(ctx, run.ID, fmt.Sprintf("invalid to_date: %v", err))
	}

	fileName := fmt.Sprintf("%s_%s.csv", run.ReportType, run.ID)

	ew, err := g.storage.Create(fileName)
	if err != nil {
		return g.fail(ctx, run.ID, fmt.Sprintf("create export file: %v", err))
	}

	w := csv.NewWriter(ew)
	var rowCount int

	switch run.ReportType {
	case ReportManufacturerCompliance:
		rowCount, err = g.writeManufacturerCompliance(ctx, w, fromDate, toDate)
	case ReportDuplicateFindings:
		rowCount, err = g.writeDuplicateFindings(ctx, w, fromDate, toDate)
	case ReportSubmissionCompleteness:
		rowCount, err = g.writeSubmissionCompleteness(ctx, w, fromDate, toDate)
	case ReportExceptions:
		orgID := parseOptionalUUID(params.OrgID)
		rowCount, err = g.writeExceptions(ctx, w, fromDate, toDate, orgID)
	case ReportBatchReconciliation:
		batchID := parseOptionalUUID(params.BatchID)
		if batchID == uuid.Nil {
			return g.fail(ctx, run.ID, "batch_id is required for batch_reconciliation_results")
		}
		rowCount, err = g.writeBatchReconciliationResults(ctx, w, batchID)
	default:
		return g.fail(ctx, run.ID, fmt.Sprintf("unknown report type: %s", run.ReportType))
	}

	if err != nil {
		ew.Abort()
		return g.fail(ctx, run.ID, err.Error())
	}

	w.Flush()
	if err := w.Error(); err != nil {
		ew.Abort()
		return g.fail(ctx, run.ID, fmt.Sprintf("csv flush: %v", err))
	}

	ref, err := ew.Commit()
	if err != nil {
		return g.fail(ctx, run.ID, fmt.Sprintf("commit export file: %v", err))
	}

	slog.Info("report generated", "run_id", run.ID, "type", run.ReportType, "rows", rowCount, "ref", ref)

	if err := g.db.UpdateExportRunCompleted(ctx, database.UpdateExportRunCompletedParams{
		ID:       run.ID,
		FilePath: pgtype.Text{String: ref, Valid: true},
		RowCount: pgtype.Int4{Int32: int32(rowCount), Valid: true},
	}); err != nil {
		return err
	}

	if g.features != nil && run.OrgID.Valid {
		orgID := uuid.UUID(run.OrgID.Bytes)
		if g.features.Enabled(ctx, orgID, feature.Webhooks) {
			payload := webhook.ExportPayload{
				ExportID:   run.ID,
				ReportType: run.ReportType,
				Status:     "completed",
			}
			if run.OrgID.Valid {
				payload.OrgID = uuid.UUID(run.OrgID.Bytes).String()
			}
			webhook.EnqueueForOrg(ctx, g.db, orgID, webhook.EventExportCompleted, payload)
		}
	}
	return nil
}

// --- report writers ---

func (g *Generator) writeManufacturerCompliance(ctx context.Context, w *csv.Writer, from, to time.Time) (int, error) {
	rows, err := g.db.GetManufacturerComplianceData(ctx, database.GetManufacturerComplianceDataParams{
		ServiceDate:   pgtype.Date{Time: from, Valid: true},
		ServiceDate_2: pgtype.Date{Time: to, Valid: true},
	})
	if err != nil {
		return 0, fmt.Errorf("query manufacturer compliance: %w", err)
	}

	_ = w.Write([]string{
		"manufacturer_id", "manufacturer_name", "labeler_code",
		"total_claims", "matched", "probable_match",
		"unmatched", "duplicate_risk", "excluded", "invalid",
	})

	for _, r := range rows {
		_ = w.Write([]string{
			r.ManufacturerID.String(),
			r.ManufacturerName,
			r.LabelerCode,
			strconv.FormatInt(r.TotalClaims, 10),
			strconv.FormatInt(r.MatchedCount, 10),
			strconv.FormatInt(r.ProbableMatchCount, 10),
			strconv.FormatInt(r.UnmatchedCount, 10),
			strconv.FormatInt(r.DuplicateRiskCount, 10),
			strconv.FormatInt(r.ExcludedCount, 10),
			strconv.FormatInt(r.InvalidCount, 10),
		})
	}
	return len(rows), nil
}

func (g *Generator) writeDuplicateFindings(ctx context.Context, w *csv.Writer, from, to time.Time) (int, error) {
	rows, err := g.db.GetDuplicateDiscountFindings(ctx, database.GetDuplicateDiscountFindingsParams{
		ServiceDate:   pgtype.Date{Time: from, Valid: true},
		ServiceDate_2: pgtype.Date{Time: to, Valid: true},
	})
	if err != nil {
		return 0, fmt.Errorf("query duplicate findings: %w", err)
	}

	_ = w.Write([]string{
		"claim_id", "org_id", "ndc", "pharmacy_npi",
		"service_date", "quantity", "payer_type",
		"rebate_amount", "rebate_source", "reasoning",
	})

	for _, r := range rows {
		_ = w.Write([]string{
			r.ClaimID.String(),
			r.OrgID.String(),
			r.Ndc,
			exportNPI(r.PharmacyNpi),
			formatDate(r.ServiceDate),
			strconv.Itoa(int(r.Quantity)),
			r.PayerType.String,
			numericString(r.RebateAmount),
			r.RebateSource.String,
			string(r.Reasoning),
		})
	}
	return len(rows), nil
}

func (g *Generator) writeSubmissionCompleteness(ctx context.Context, w *csv.Writer, from, to time.Time) (int, error) {
	rows, err := g.db.GetSubmissionCompleteness(ctx, database.GetSubmissionCompletenessParams{
		CreatedAt:   from,
		CreatedAt_2: to,
	})
	if err != nil {
		return 0, fmt.Errorf("query submission completeness: %w", err)
	}

	_ = w.Write([]string{"org_id", "org_name", "entity_id", "batch_count", "total_rows"})

	for _, r := range rows {
		_ = w.Write([]string{
			r.OrgID.String(),
			r.OrgName,
			r.EntityID,
			strconv.FormatInt(r.BatchCount, 10),
			fmt.Sprintf("%v", r.TotalRows),
		})
	}
	return len(rows), nil
}

func (g *Generator) writeExceptions(ctx context.Context, w *csv.Writer, from, to time.Time, orgID uuid.UUID) (int, error) {
	rows, err := g.db.GetUnresolvedExceptions(ctx, database.GetUnresolvedExceptionsParams{
		Column1:       orgID,
		ServiceDate:   pgtype.Date{Time: from, Valid: true},
		ServiceDate_2: pgtype.Date{Time: to, Valid: true},
	})
	if err != nil {
		return 0, fmt.Errorf("query exceptions: %w", err)
	}

	_ = w.Write([]string{
		"claim_id", "org_id", "ndc", "pharmacy_npi",
		"service_date", "quantity", "decision_status", "reasoning",
	})

	for _, r := range rows {
		_ = w.Write([]string{
			r.ClaimID.String(),
			r.OrgID.String(),
			r.Ndc,
			exportNPI(r.PharmacyNpi),
			formatDate(r.ServiceDate),
			strconv.Itoa(int(r.Quantity)),
			r.DecisionStatus,
			string(r.Reasoning),
		})
	}
	return len(rows), nil
}

func (g *Generator) writeBatchReconciliationResults(ctx context.Context, w *csv.Writer, batchID uuid.UUID) (int, error) {
	job, err := g.db.GetReconciliationJobByBatch(ctx, batchID)
	if err != nil {
		return 0, fmt.Errorf("query reconciliation job: %w", err)
	}
	decisions, err := g.db.GetMatchDecisionsByJob(ctx, job.ID)
	if err != nil {
		return 0, fmt.Errorf("query job decisions: %w", err)
	}

	_ = w.Write([]string{
		"job_id", "batch_id", "decision_id", "claim_id",
		"status", "rebate_record_id", "policy_version_id", "created_at",
	})
	for _, d := range decisions {
		_ = w.Write([]string{
			job.ID.String(),
			batchID.String(),
			d.ID.String(),
			d.ClaimID.String(),
			d.Status,
			uuidFromPG(d.RebateRecordID),
			uuidFromPG(d.PolicyVersionID),
			d.CreatedAt.Format(time.RFC3339),
		})
	}
	return len(decisions), nil
}

// --- helpers ---

func (g *Generator) fail(ctx context.Context, id uuid.UUID, msg string) error {
	slog.Error("report generation failed", "run_id", id, "error", msg)
	_ = g.db.UpdateExportRunFailed(ctx, database.UpdateExportRunFailedParams{
		ID:           id,
		ErrorMessage: pgtype.Text{String: msg, Valid: true},
	})
	return fmt.Errorf("report %s failed: %s", id, msg)
}

func parseDate(s string) (time.Time, error) {
	return time.Parse("2006-01-02", s)
}

func parseOptionalUUID(s string) uuid.UUID {
	if s == "" {
		return uuid.Nil
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil
	}
	return id
}

func formatDate(d pgtype.Date) string {
	if !d.Valid {
		return ""
	}
	return d.Time.Format("2006-01-02")
}

func numericString(n pgtype.Numeric) string {
	if !n.Valid {
		return ""
	}
	f, _ := n.Float64Value()
	if !f.Valid {
		return n.Int.String()
	}
	return strconv.FormatFloat(f.Float64, 'f', 2, 64)
}

func uuidFromPG(v pgtype.UUID) string {
	if !v.Valid {
		return ""
	}
	return uuid.UUID(v.Bytes).String()
}

func exportNPI(n string) string {
	if sensitivity.RedactExportFields() {
		return sensitivity.MaskNPI(n)
	}
	return n
}
