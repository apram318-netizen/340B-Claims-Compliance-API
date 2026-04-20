// Package reconciliation implements the 340B claim matching engine.
//
// Pipeline per claim:
//   A. Candidate retrieval  — fetch rebate records that could match
//   B. Policy check         — apply manufacturer rules via policy.Engine
//   C. Deterministic match  — score candidates on exact-match criteria
//   D. Duplicate detection  — flag Medicaid + 340B overlap risk
//   E. Classification       — assign one of 7 statuses with full reasoning
package reconciliation

import (
	"claims-system/internal/database"
	"claims-system/internal/policy"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// MatchStatus mirrors the values stored in match_decisions.status.
type MatchStatus string

const (
	StatusMatched             MatchStatus = "matched"
	StatusProbableMatch       MatchStatus = "probable_match"
	StatusUnmatched           MatchStatus = "unmatched"
	StatusDuplicateDiscount   MatchStatus = "duplicate_discount_risk"
	StatusInvalid             MatchStatus = "invalid"
	StatusExcludedByPolicy    MatchStatus = "excluded_by_policy"
	StatusPendingExternalData MatchStatus = "pending_external_data"
)

// candidateLookbackDays is the default date window for candidate retrieval
// when no policy date_window rule overrides it.
const candidateLookbackDays = 90

// Engine runs the reconciliation pipeline for a batch.
type Engine struct {
	db     database.Store
	policy *policy.Engine
}

func NewEngine(db database.Store, policy *policy.Engine) *Engine {
	return &Engine{db: db, policy: policy}
}

// ReconcileBatch processes every pending claim in a batch: creates a
// reconciliation job, evaluates each claim, and records match decisions.
func (e *Engine) ReconcileBatch(ctx context.Context, batchID uuid.UUID) error {
	claims, err := e.db.GetPendingClaimsByBatch(ctx, batchID)
	if err != nil {
		return fmt.Errorf("fetch pending claims: %w", err)
	}
	if len(claims) == 0 {
		slog.Info("no pending claims to reconcile", "batch_id", batchID)
		return nil
	}

	// Create job record.
	job, err := e.db.CreateReconciliationJob(ctx, database.CreateReconciliationJobParams{
		BatchID:     batchID,
		Status:      "pending",
		TotalClaims: int32(len(claims)),
	})
	if err != nil {
		return fmt.Errorf("create reconciliation job: %w", err)
	}

	if err := e.db.MarkJobStarted(ctx, job.ID); err != nil {
		return fmt.Errorf("mark job started: %w", err)
	}

	// Counters for the job summary.
	var matched, unmatched, dupRisk, excluded, errCount int32

	for _, claim := range claims {
		status, err := e.reconcileClaim(ctx, job.ID, claim)
		if err != nil {
			slog.Error("claim reconciliation failed", "claim_id", claim.ID, "error", err)
			errCount++
			// Mark claim so it won't be retried endlessly without intervention.
			e.db.UpdateClaimReconciliationStatus(ctx, database.UpdateClaimReconciliationStatusParams{
				ID:                   claim.ID,
				ReconciliationStatus: string(StatusPendingExternalData),
			})
			continue
		}

		switch status {
		case StatusMatched, StatusProbableMatch:
			matched++
		case StatusUnmatched, StatusPendingExternalData:
			unmatched++
		case StatusDuplicateDiscount:
			dupRisk++
		case StatusExcludedByPolicy, StatusInvalid:
			excluded++
		}
	}

	finalStatus := "completed"
	if errCount == int32(len(claims)) {
		finalStatus = "failed"
	}

	if err := e.db.UpdateJobStatus(ctx, database.UpdateJobStatusParams{
		ID:     job.ID,
		Status: finalStatus,
	}); err != nil {
		slog.Error("update job status failed", "job_id", job.ID, "error", err)
	}

	if err := e.db.UpdateJobCounts(ctx, database.UpdateJobCountsParams{
		ID:                 job.ID,
		MatchedCount:       matched,
		UnmatchedCount:     unmatched,
		DuplicateRiskCount: dupRisk,
		ExcludedCount:      excluded,
		ErrorCount:         errCount,
	}); err != nil {
		slog.Error("update job counts failed", "job_id", job.ID, "error", err)
	}

	slog.Info("reconciliation complete",
		"batch_id", batchID,
		"job_id", job.ID,
		"total", len(claims),
		"matched", matched,
		"unmatched", unmatched,
		"dup_risk", dupRisk,
		"excluded", excluded,
		"errors", errCount,
	)
	return nil
}

// reconcileClaim runs the full A→E pipeline for a single claim.
func (e *Engine) reconcileClaim(ctx context.Context, jobID uuid.UUID, claim database.Claim) (MatchStatus, error) {
	reasoning := make(map[string]interface{})

	// ── Step A: Basic validity check ─────────────────────────────────────────
	if claim.Ndc == "" || claim.PharmacyNpi == "" || !claim.ServiceDate.Valid {
		return e.writeDecision(ctx, jobID, claim, StatusInvalid, nil, nil,
			addReason(reasoning, "validity", "claim is missing required fields (ndc, pharmacy_npi, or service_date)"))
	}

	// ── Step B: Policy evaluation ─────────────────────────────────────────────
	policyDecision, err := e.policy.EvaluateClaim(ctx, claim)
	if err != nil {
		return "", fmt.Errorf("policy evaluation: %w", err)
	}

	reasoning["policy"] = policyDecision.Reasoning

	if !policyDecision.Eligible {
		return e.writeDecision(ctx, jobID, claim, StatusExcludedByPolicy, nil, policyDecision.PolicyVersionID,
			addReason(reasoning, "policy_exclusion", "excluded by rule: "+policyDecision.ExcludedByRule))
	}

	// ── Step C: Candidate retrieval ───────────────────────────────────────────
	windowStart := claim.ServiceDate.Time.AddDate(0, 0, -candidateLookbackDays)
	candidates, err := e.db.GetCandidateRebateRecords(ctx, database.GetCandidateRebateRecordsParams{
		Ndc:         claim.Ndc,
		OrgID:       claim.OrgID,
		PharmacyNpi: claim.PharmacyNpi,
		ServiceDate: pgtype.Date{Time: windowStart, Valid: true},
		ServiceDate_2: pgtype.Date{Time: claim.ServiceDate.Time.Add(24 * time.Hour), Valid: true},
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("candidate retrieval: %w", err)
	}

	reasoning["candidates_found"] = len(candidates)

	if len(candidates) == 0 {
		return e.writeDecision(ctx, jobID, claim, StatusUnmatched, nil, policyDecision.PolicyVersionID,
			addReason(reasoning, "match", "no rebate records found for this ndc/org/pharmacy/date window"))
	}

	// ── Step D: Duplicate-discount detection ─────────────────────────────────
	// HRSA rule: a drug cannot receive both a 340B discount and a Medicaid rebate.
	claimPayer := strings.ToLower(claim.PayerType.String)
	if claimPayer == "medicaid" || claimPayer == "medicaid_managed_care" {
		// A Medicaid claim with matching rebate records = duplicate discount risk.
		reasoning["duplicate_discount"] = "payer is Medicaid and matching rebate records exist"
		best := &candidates[0]
		e.saveCandidates(ctx, jobID, claim.ID, candidates)
		return e.writeDecision(ctx, jobID, claim, StatusDuplicateDiscount, &best.ID, policyDecision.PolicyVersionID, reasoning)
	}

	// ── Step E: Deterministic scoring ────────────────────────────────────────
	best, score := scoreCandidates(claim, candidates)
	e.saveCandidates(ctx, jobID, claim.ID, candidates)

	reasoning["match_score"] = score
	reasoning["best_candidate"] = best.ID

	var status MatchStatus
	switch {
	case score >= 100:
		status = StatusMatched
		reasoning["match_detail"] = "exact match on ndc, org, pharmacy, date, and hashed_rx_key"
	case score >= 60:
		status = StatusProbableMatch
		reasoning["match_detail"] = "probable match — hashed_rx_key absent or mismatched"
	default:
		status = StatusUnmatched
		reasoning["match_detail"] = fmt.Sprintf("best candidate score %d is below threshold", score)
		best = nil
	}

	var rebateID *uuid.UUID
	if best != nil {
		rebateID = &best.ID
	}
	return e.writeDecision(ctx, jobID, claim, status, rebateID, policyDecision.PolicyVersionID, reasoning)
}

// scoreCandidates ranks rebate records against the claim and returns the best
// candidate and its score.  Scoring is deterministic:
//
//	+40  NDC exact match          (always true — candidates are pre-filtered)
//	+20  org_id match             (always true — pre-filtered)
//	+20  pharmacy_npi match       (always true — pre-filtered)
//	+20  hashed_rx_key match
//	+10  same service_date (within 1 day)
//	 −5  per day off service_date
func scoreCandidates(claim database.Claim, candidates []database.RebateRecord) (*database.RebateRecord, int) {
	best := &candidates[0]
	bestScore := -1

	for i := range candidates {
		c := &candidates[i]
		score := 80 // ndc + org + pharmacy pre-filtered

		// Hashed rx key match
		if claim.HashedRxKey.Valid && c.HashedRxKey.Valid &&
			claim.HashedRxKey.String == c.HashedRxKey.String {
			score += 20
		}

		// Date proximity
		daysDiff := int(abs(claim.ServiceDate.Time.Sub(c.ServiceDate.Time).Hours() / 24))
		if daysDiff == 0 {
			score += 10
		} else {
			score -= daysDiff * 5
		}

		if score > bestScore {
			bestScore = score
			best = c
		}
	}

	return best, bestScore
}

func (e *Engine) saveCandidates(ctx context.Context, jobID, claimID uuid.UUID, candidates []database.RebateRecord) {
	for _, c := range candidates {
		if _, err := e.db.CreateCandidateMatch(ctx, database.CreateCandidateMatchParams{
			JobID:          jobID,
			ClaimID:        claimID,
			RebateRecordID: c.ID,
			Score:          0, // score attached to match_decision, not candidate record
		}); err != nil {
			slog.Error("save candidate match failed", "claim_id", claimID, "rebate_id", c.ID, "error", err)
		}
	}
}

func (e *Engine) writeDecision(
	ctx context.Context,
	jobID uuid.UUID,
	claim database.Claim,
	status MatchStatus,
	rebateRecordID *uuid.UUID,
	policyVersionID *uuid.UUID,
	reasoning map[string]interface{},
) (MatchStatus, error) {
	reasoningJSON, err := json.Marshal(reasoning)
	if err != nil {
		return "", fmt.Errorf("marshal reasoning: %w", err)
	}

	var rebateUUID pgtype.UUID
	if rebateRecordID != nil {
		rebateUUID = pgtype.UUID{Bytes: *rebateRecordID, Valid: true}
	}

	var pvUUID pgtype.UUID
	if policyVersionID != nil {
		pvUUID = pgtype.UUID{Bytes: *policyVersionID, Valid: true}
	}

	if _, err := e.db.CreateMatchDecision(ctx, database.CreateMatchDecisionParams{
		JobID:           jobID,
		ClaimID:         claim.ID,
		RebateRecordID:  rebateUUID,
		PolicyVersionID: pvUUID,
		Status:          string(status),
		Reasoning:       reasoningJSON,
	}); err != nil {
		return "", fmt.Errorf("write match decision: %w", err)
	}

	if err := e.db.UpdateClaimReconciliationStatus(ctx, database.UpdateClaimReconciliationStatusParams{
		ID:                   claim.ID,
		ReconciliationStatus: string(status),
	}); err != nil {
		slog.Error("update claim status failed", "claim_id", claim.ID, "error", err)
	}

	return status, nil
}

func addReason(m map[string]interface{}, key, value string) map[string]interface{} {
	m[key] = value
	return m
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
