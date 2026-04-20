// Package policy evaluates manufacturer-specific 340B policy rules against
// a normalized claim.  Rules are data-driven (stored in policy_rules) so no
// code changes are needed when a manufacturer updates their policy.
package policy

import (
	"claims-system/internal/database"
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

// RuleType identifies the kind of policy rule being evaluated.
type RuleType string

const (
	RuleNDCScope      RuleType = "ndc_scope"
	RulePayerChannel  RuleType = "payer_channel"
	RuleDateWindow    RuleType = "date_window"
	RulePharmacyLimit RuleType = "pharmacy_limit"
)

// RuleResult records whether a single rule passed and why.
type RuleResult struct {
	Passed   bool   `json:"passed"`
	RuleType string `json:"rule_type"`
	Reason   string `json:"reason"`
}

// Decision is the output of evaluating all rules for one claim.
type Decision struct {
	Eligible        bool
	ExcludedByRule  string               // set when Eligible == false
	Reasoning       map[string]RuleResult
	PolicyVersionID *uuid.UUID
}

// Engine loads and evaluates manufacturer policies.
type Engine struct {
	db database.Store
}

func NewEngine(db database.Store) *Engine {
	return &Engine{db: db}
}

// EvaluateClaim returns the policy decision for a claim on its service_date.
// If no manufacturer or policy is found the claim is considered eligible
// (no rules = no exclusions).
func (e *Engine) EvaluateClaim(ctx context.Context, claim database.Claim) (*Decision, error) {
	// Step 1 — resolve the manufacturer from the NDC.
	manufacturer, err := e.db.GetManufacturerByNDC(ctx, claim.Ndc)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return eligible("no manufacturer registered for this NDC"), nil
		}
		return nil, fmt.Errorf("manufacturer lookup: %w", err)
	}

	// Step 2 — find the policy version active on the claim's service_date.
	serviceDate := claim.ServiceDate.Time
	pv, err := e.db.GetActivePolicyVersion(ctx, database.GetActivePolicyVersionParams{
		ManufacturerID: manufacturer.ID,
		EffectiveFrom:  pgtype.Date{Time: serviceDate, Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return eligible("no active policy for this manufacturer on service_date"), nil
		}
		return nil, fmt.Errorf("policy version lookup: %w", err)
	}

	// Step 3 — load all rules for this version.
	rules, err := e.db.GetRulesByPolicyVersion(ctx, pv.ID)
	if err != nil {
		return nil, fmt.Errorf("load policy rules: %w", err)
	}

	// Step 4 — evaluate every rule; first failure marks the claim excluded.
	decision := &Decision{
		Eligible:        true,
		Reasoning:       make(map[string]RuleResult),
		PolicyVersionID: &pv.ID,
	}

	for _, rule := range rules {
		result := e.evalRule(claim, rule)
		decision.Reasoning[rule.RuleType] = result
		if !result.Passed && decision.Eligible {
			decision.Eligible = false
			decision.ExcludedByRule = rule.RuleType
		}
	}

	return decision, nil
}

func (e *Engine) evalRule(claim database.Claim, rule database.PolicyRule) RuleResult {
	switch RuleType(rule.RuleType) {
	case RuleNDCScope:
		return evalNDCScope(claim, rule.RuleConfig)
	case RulePayerChannel:
		return evalPayerChannel(claim, rule.RuleConfig)
	case RuleDateWindow:
		return evalDateWindow(claim, rule.RuleConfig)
	default:
		slog.Warn("unknown policy rule type — skipping", "rule_type", rule.RuleType)
		return RuleResult{Passed: true, RuleType: rule.RuleType, Reason: "unrecognised rule type, skipped"}
	}
}

// ─── Individual rule evaluators ──────────────────────────────────────────────

// ndc_scope config: {"ndcs": ["1234567890"], "ndc_prefixes": ["12345"]}
func evalNDCScope(claim database.Claim, config []byte) RuleResult {
	var cfg struct {
		NDCs        []string `json:"ndcs"`
		NDCPrefixes []string `json:"ndc_prefixes"`
	}
	if err := json.Unmarshal(config, &cfg); err != nil {
		return fail(RuleNDCScope, "invalid rule config: "+err.Error())
	}

	for _, ndc := range cfg.NDCs {
		if ndc == claim.Ndc {
			return pass(RuleNDCScope, "NDC in scope list")
		}
	}
	for _, prefix := range cfg.NDCPrefixes {
		if strings.HasPrefix(claim.Ndc, prefix) {
			return pass(RuleNDCScope, "NDC matches prefix "+prefix)
		}
	}
	if len(cfg.NDCs) == 0 && len(cfg.NDCPrefixes) == 0 {
		return pass(RuleNDCScope, "no NDC restrictions defined")
	}
	return fail(RuleNDCScope, "NDC "+claim.Ndc+" is not in manufacturer scope")
}

// payer_channel config: {"excluded_payer_types": ["medicaid"]}
func evalPayerChannel(claim database.Claim, config []byte) RuleResult {
	var cfg struct {
		ExcludedPayerTypes []string `json:"excluded_payer_types"`
	}
	if err := json.Unmarshal(config, &cfg); err != nil {
		return fail(RulePayerChannel, "invalid rule config: "+err.Error())
	}

	claimPayer := strings.ToLower(claim.PayerType.String)
	for _, excl := range cfg.ExcludedPayerTypes {
		if strings.ToLower(excl) == claimPayer {
			return fail(RulePayerChannel, "payer type '"+claimPayer+"' is excluded by manufacturer policy")
		}
	}
	return pass(RulePayerChannel, "payer channel is eligible")
}

// date_window config: {"lookback_days": 90}
func evalDateWindow(claim database.Claim, config []byte) RuleResult {
	var cfg struct {
		LookbackDays int `json:"lookback_days"`
	}
	if err := json.Unmarshal(config, &cfg); err != nil {
		return fail(RuleDateWindow, "invalid rule config: "+err.Error())
	}
	if cfg.LookbackDays <= 0 {
		return pass(RuleDateWindow, "no date window restriction")
	}

	cutoff := time.Now().AddDate(0, 0, -cfg.LookbackDays)
	if claim.ServiceDate.Time.Before(cutoff) {
		return fail(RuleDateWindow, fmt.Sprintf(
			"service_date %s is outside the %d-day lookback window",
			claim.ServiceDate.Time.Format("2006-01-02"),
			cfg.LookbackDays,
		))
	}
	return pass(RuleDateWindow, "service_date within lookback window")
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func pass(rt RuleType, reason string) RuleResult {
	return RuleResult{Passed: true, RuleType: string(rt), Reason: reason}
}

func fail(rt RuleType, reason string) RuleResult {
	return RuleResult{Passed: false, RuleType: string(rt), Reason: reason}
}

func eligible(reason string) *Decision {
	return &Decision{
		Eligible:  true,
		Reasoning: map[string]RuleResult{"system": {Passed: true, RuleType: "system", Reason: reason}},
	}
}
