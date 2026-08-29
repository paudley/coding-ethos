// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

// Package tokeneconomy records and reports provider-native token evidence for
// controlled Coding Ethos comparisons.
//
//nolint:govet // Public JSON evidence fields are grouped by contract semantics.
package tokeneconomy

import "time"

const (
	// SchemaVersion is the token-economy DuckDB schema version.
	SchemaVersion = 1
	// ReportKind identifies the stable JSON report contract.
	ReportKind = "coding_ethos.token_economy.v1"
)

// Provider identifies the agent ledger format used by a run.
type Provider string

const (
	// ProviderCodex identifies an OpenAI Codex native session ledger.
	ProviderCodex Provider = "codex"
	// ProviderClaude identifies an Anthropic Claude Code native session ledger.
	ProviderClaude Provider = "claude"
)

// Arm identifies one benchmark treatment.
type Arm string

const (
	// ArmFull enables static and dynamic Coding Ethos behavior.
	ArmFull Arm = "full"
	// ArmStatic enables only the shared static Coding Ethos context.
	ArmStatic Arm = "static"
	// ArmOff removes only Coding Ethos-provided context and runtime behavior.
	ArmOff Arm = "off"
)

// Conclusion is the strongest claim supported by a report.
type Conclusion string

const (
	// ConclusionCausalSavings means token savings and quality gates both passed.
	ConclusionCausalSavings Conclusion = "causal_savings"
	// ConclusionNoDifference means a precise comparison did not detect savings.
	ConclusionNoDifference Conclusion = "no_detectable_difference"
	// ConclusionTokenRegression means the enabled arm used significantly more tokens.
	ConclusionTokenRegression Conclusion = "token_regression"
	// ConclusionQualityTradeoff means lower token use came with unacceptable quality.
	ConclusionQualityTradeoff Conclusion = "quality_tradeoff"
	// ConclusionInconclusive means the evidence is incomplete or imprecise.
	ConclusionInconclusive Conclusion = "inconclusive"
	// ConclusionObservational means only gross within-run reduction is available.
	ConclusionObservational Conclusion = "observational_gross_reduction"
)

// TokenUsage is a provider-neutral ledger total without tokenizer conversion.
type TokenUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	CachedInputTokens        int64 `json:"cached_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	ReasoningOutputTokens    int64 `json:"reasoning_output_tokens"`
	TotalTokens              int64 `json:"total_tokens"`
}

// UsageEvent is one sanitized provider usage record. It contains no message
// or prompt content.
type UsageEvent struct {
	ProviderMessageID string     `json:"provider_message_id"`
	RecordedAtUTC     string     `json:"recorded_at_utc,omitempty"`
	UsageKind         string     `json:"usage_kind"`
	Ordinal           int        `json:"ordinal"`
	Usage             TokenUsage `json:"usage"`
}

// Ledger is the sanitized result of parsing one provider-native session file.
type Ledger struct {
	Provider     Provider     `json:"provider"`
	SessionID    string       `json:"session_id"`
	Model        string       `json:"model,omitempty"`
	SourcePath   string       `json:"source_path"`
	SourceSHA256 string       `json:"source_sha256"`
	Usage        TokenUsage   `json:"usage"`
	Events       []UsageEvent `json:"events"`
}

// Experiment is immutable protocol-level benchmark provenance.
type Experiment struct {
	ExperimentID             string   `json:"experiment_id"`
	ManifestSHA256           string   `json:"manifest_sha256"`
	ProtocolSHA256           string   `json:"protocol_sha256"`
	Provider                 Provider `json:"provider"`
	Model                    string   `json:"model"`
	RuntimeVersion           string   `json:"runtime_version"`
	CreatedAtUTC             string   `json:"created_at_utc"`
	RandomizationSeed        string   `json:"randomization_seed"`
	Status                   string   `json:"status"`
	AnalysisBlockCheckpoints []int    `json:"analysis_block_checkpoints"`
	Randomized               bool     `json:"randomized"`
	ArmIsolationVerified     bool     `json:"arm_isolation_verified"`
}

// Task is immutable task and acceptance provenance within an experiment.
type Task struct {
	ExperimentID    string `json:"experiment_id"`
	TaskID          string `json:"task_id"`
	Kind            string `json:"kind"`
	SourceSHA256    string `json:"source_sha256"`
	PromptSHA256    string `json:"prompt_sha256"`
	ValidatorSHA256 string `json:"validator_sha256"`
}

// MechanismMetrics records Coding Ethos context changes without treating them
// as provider-billed savings.
type MechanismMetrics struct {
	RawContextTokens       int64 `json:"raw_context_tokens"`
	DeliveredContextTokens int64 `json:"delivered_context_tokens"`
	AvoidedContextTokens   int64 `json:"avoided_context_tokens"`
	InjectedGuidanceTokens int64 `json:"injected_guidance_tokens"`
	RepeatedAdviceCount    int   `json:"repeated_advice_count"`
	TransformEventCount    int   `json:"transform_event_count"`
}

// Run is one immutable task, arm, and replicate outcome.
type Run struct {
	RunID                     string           `json:"run_id"`
	ExperimentID              string           `json:"experiment_id"`
	TaskID                    string           `json:"task_id"`
	Arm                       Arm              `json:"arm"`
	Provider                  Provider         `json:"provider"`
	Model                     string           `json:"model"`
	ProviderSessionID         string           `json:"provider_session_id"`
	LedgerSHA256              string           `json:"ledger_sha256"`
	ValidationReceiptSHA256   string           `json:"validation_receipt_sha256"`
	StartedAtUTC              string           `json:"started_at_utc"`
	CompletedAtUTC            string           `json:"completed_at_utc"`
	Status                    string           `json:"status"`
	FailureReason             string           `json:"failure_reason,omitempty"`
	Replicate                 int              `json:"replicate"`
	DurationMilliseconds      int64            `json:"duration_ms"`
	Accepted                  bool             `json:"accepted"`
	SevereGovernanceViolation bool             `json:"severe_governance_violation"`
	Usage                     TokenUsage       `json:"usage"`
	UsageEvents               []UsageEvent     `json:"usage_events"`
	Mechanisms                MechanismMetrics `json:"mechanisms"`
}

// HistoricalReportOptions selects immutable code-intel sources and an explicit
// half-open UTC reporting window.
type HistoricalReportOptions struct {
	DatabasePaths []string
	FromUTC       string
	ToUTC         string
}

// HistoricalSource records one verified read-only source used by a historical
// report.
type HistoricalSource struct {
	Path            string `json:"path"`
	SHA256Before    string `json:"sha256_before"`
	SHA256After     string `json:"sha256_after"`
	SourceUnchanged bool   `json:"source_unchanged"`
}

// HistoricalMetrics summarizes gross context changes across enabled
// code-intel stores. They are observational and never a causal comparison.
type HistoricalMetrics struct {
	FromUTC                string             `json:"from_utc"`
	ToUTC                  string             `json:"to_utc"`
	Sources                []HistoricalSource `json:"sources"`
	RawContextTokens       int64              `json:"raw_context_tokens"`
	DeliveredContextTokens int64              `json:"delivered_context_tokens"`
	AvoidedContextTokens   int64              `json:"avoided_context_tokens"`
	GrossReductionPercent  float64            `json:"gross_reduction_percent"`
	TransformedEvents      int                `json:"transformed_events"`
	ReducedEvents          int                `json:"reduced_events"`
	ExpandedEvents         int                `json:"expanded_events"`
	ProxySessions          int                `json:"proxy_sessions"`
}

// Interval is a percentile confidence interval.
type Interval struct {
	Lower float64 `json:"lower"`
	Upper float64 `json:"upper"`
}

// Comparison summarizes one enabled arm against one control arm.
type Comparison struct {
	TreatmentArm                 Arm      `json:"treatment_arm"`
	ControlArm                   Arm      `json:"control_arm"`
	TaskCount                    int      `json:"task_count"`
	AssignedRuns                 int      `json:"assigned_runs"`
	TreatmentAccepted            int      `json:"treatment_accepted"`
	ControlAccepted              int      `json:"control_accepted"`
	TreatmentTokensPerAccepted   float64  `json:"treatment_tokens_per_accepted"`
	ControlTokensPerAccepted     float64  `json:"control_tokens_per_accepted"`
	SavingsPercent               float64  `json:"savings_percent"`
	SavingsPercentInterval       Interval `json:"savings_percent_interval"`
	AcceptanceDifferencePoints   float64  `json:"acceptance_difference_points"`
	AcceptanceDifferenceInterval Interval `json:"acceptance_difference_interval"`
	ConfidenceLevelPercent       float64  `json:"confidence_level_percent"`
	PrecisionMet                 bool     `json:"precision_met"`
	QualityNonInferior           bool     `json:"quality_noninferior"`
	AdditionalSevereViolations   int      `json:"additional_severe_violations"`
}

// Coverage describes evidence completeness and claim blockers.
type Coverage struct {
	Providers             []Provider `json:"providers"`
	Arms                  []Arm      `json:"arms"`
	TaskCount             int        `json:"task_count"`
	CompleteTaskCount     int        `json:"complete_task_count"`
	CompleteBlockCount    int        `json:"complete_block_count"`
	PartialBlockCount     int        `json:"partial_block_count"`
	RunCount              int        `json:"run_count"`
	AcceptedRunCount      int        `json:"accepted_run_count"`
	MissingLedgerRuns     int        `json:"missing_ledger_runs"`
	MissingValidationRuns int        `json:"missing_validation_runs"`
	Reasons               []string   `json:"reasons,omitempty"`
}

// Report is the stable JSON and Markdown token-economy artifact.
type Report struct {
	Kind           string             `json:"kind"`
	Cohort         string             `json:"cohort"`
	GeneratedAtUTC string             `json:"generated_at_utc"`
	Conclusion     Conclusion         `json:"conclusion"`
	SchemaVersion  int                `json:"schema_version"`
	Causal         bool               `json:"causal"`
	Coverage       Coverage           `json:"coverage"`
	Historical     *HistoricalMetrics `json:"historical,omitempty"`
	Comparisons    []Comparison       `json:"comparisons,omitempty"`
	Provenance     map[string]string  `json:"provenance"`
}

func reportTimestamp(now time.Time) string {
	if now.IsZero() {
		now = time.Now().UTC()
	}

	return now.UTC().Format(time.RFC3339Nano)
}
