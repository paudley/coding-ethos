// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/agentmsg"
	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/evidence"
	"blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/shellparse"
)

type TraceIngester struct {
	store *Store
}

func NewTraceIngester(store *Store) TraceIngester {
	return TraceIngester{store: store}
}

func (ingester TraceIngester) IngestLintTrace(
	ctx context.Context,
	payload []byte,
) error {
	trace, err := DecodeLintTrace("", payload)
	if err != nil {
		return err
	}

	return ingester.store.IngestTrace(ctx, trace)
}

func (ingester TraceIngester) IngestHookTrace(
	ctx context.Context,
	payload []byte,
) error {
	return ingester.IngestHookTraceSource(ctx, "", payload)
}

func (ingester TraceIngester) IngestHookTraceSource(
	ctx context.Context,
	path string,
	payload []byte,
) error {
	trace, err := DecodeHookTrace(path, payload)
	if err != nil {
		return err
	}

	return ingester.store.IngestTrace(ctx, trace)
}

func (ingester TraceIngester) IngestTraceDirs(
	ctx context.Context,
	root string,
) (IngestSummary, error) {
	summary := IngestSummary{}

	err := ingester.ingestTraceDir(
		ctx,
		filepath.Join(root, ".coding-ethos", "lint-runs"),
		"lint",
		&summary,
	)
	if err != nil {
		return summary, err
	}

	err = ingester.ingestTraceDir(
		ctx,
		filepath.Join(root, ".coding-ethos", "hook-runs"),
		"hook",
		&summary,
	)
	if err != nil {
		return summary, err
	}

	return summary, nil
}

func (ingester TraceIngester) ingestTraceDir(
	ctx context.Context,
	dir string,
	kind string,
	summary *IngestSummary,
) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return fmt.Errorf("open trace dir %q: %w", dir, err)
	}
	defer root.Close()

	err = filepath.WalkDir(dir, ingester.traceWalkFunc(ctx, dir, kind, root, summary))
	if err != nil {
		return fmt.Errorf("walk trace directory %s: %w", dir, err)
	}

	return nil
}

func (ingester TraceIngester) traceWalkFunc(
	ctx context.Context,
	dir string,
	kind string,
	root *os.Root,
	summary *IngestSummary,
) fs.WalkDirFunc {
	return func(path string, entry os.DirEntry, err error) error {
		return ingester.ingestTraceEntry(ctx, traceEntryInput{
			dir:     dir,
			kind:    kind,
			root:    root,
			summary: summary,
			path:    path,
			entry:   entry,
			err:     err,
		})
	}
}

type traceEntryInput struct {
	root    *os.Root
	summary *IngestSummary
	entry   os.DirEntry
	err     error
	dir     string
	kind    string
	path    string
}

func (ingester TraceIngester) ingestTraceEntry(
	ctx context.Context,
	input traceEntryInput,
) error {
	if input.err != nil {
		if os.IsNotExist(input.err) {
			return filepath.SkipDir
		}

		return fmt.Errorf("walk trace dir %q: %w", input.dir, input.err)
	}

	if skipTraceEntry(input.kind, input.path, input.entry) {
		return nil
	}

	input.summary.FilesScanned++

	rel, relErr := filepath.Rel(input.dir, input.path)
	if relErr != nil {
		return fmt.Errorf("relativize trace %q: %w", input.path, relErr)
	}

	payload, readErr := input.root.ReadFile(rel)
	if readErr != nil {
		return fmt.Errorf("read trace %q: %w", input.path, readErr)
	}

	unchanged, unchangedErr := ingester.traceSourceUnchanged(ctx, input.path, payload)
	if unchangedErr != nil {
		return unchangedErr
	}

	if unchanged {
		return nil
	}

	ingestErr := ingester.ingestTracePayload(ctx, input.kind, input.path, payload)
	if ingestErr != nil {
		return fmt.Errorf("ingest trace %q: %w", input.path, ingestErr)
	}

	input.summary.FilesIngested++

	return nil
}

func (ingester TraceIngester) traceSourceUnchanged(
	ctx context.Context,
	path string,
	payload []byte,
) (bool, error) {
	row := ingester.store.database.QueryRowContext(
		ctx,
		"SELECT raw_json FROM traces WHERE source_path = ? LIMIT 1",
		path,
	)

	var raw string

	err := row.Scan(&raw)
	if err == nil {
		return raw == string(payload), nil
	}

	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}

	return false, fmt.Errorf("lookup ingested trace source %q: %w", path, err)
}

func skipTraceEntry(kind, path string, entry os.DirEntry) bool {
	if entry.IsDir() || filepath.Ext(path) != ".json" {
		return true
	}

	return kind == "hook" && filepath.Base(path) != "event.json"
}

func (ingester TraceIngester) ingestTracePayload(
	ctx context.Context,
	kind string,
	path string,
	payload []byte,
) error {
	var (
		trace Trace
		err   error
	)

	switch kind {
	case "lint":
		trace, err = DecodeLintTrace(path, payload)
	case "hook":
		trace, err = DecodeHookTrace(path, payload)
	default:
		return apperror.Wrapf(
			apperror.StaticError("unsupported trace kind %q"),
			"unsupported trace kind %q",
			kind,
		)
	}

	if err != nil {
		return err
	}

	return ingester.store.IngestTrace(ctx, trace)
}

func DecodeLintTrace(path string, payload []byte) (Trace, error) {
	var record lint.TraceRecord

	err := json.Unmarshal(payload, &record)
	if err != nil {
		return Trace{}, fmt.Errorf("decode lint trace %q: %w", path, err)
	}

	traceID := traceIDOrSourceFallback(record.TraceID, path)

	return Trace{
		ID:                traceID,
		Kind:              "lint",
		RecordedAtUTC:     record.RecordedAtUTC,
		RepoRoot:          record.RepoRoot,
		Status:            record.Result.Status,
		SourcePath:        path,
		Raw:               payload,
		Findings:          record.Findings,
		AgentRemediation:  record.AgentRemediation,
		RemediationEvents: record.RemediationEvents,
	}, nil
}

func DecodeHookTrace(path string, payload []byte) (Trace, error) {
	var record hookTraceRecord

	err := json.Unmarshal(payload, &record)
	if err != nil {
		return Trace{}, fmt.Errorf("decode hook trace %q: %w", path, err)
	}

	record.TraceID = traceIDOrSourceFallback(record.TraceID, path)

	return Trace{
		ID:                record.TraceID,
		Kind:              "hook",
		RecordedAtUTC:     record.RecordedAtUTC,
		Cwd:               record.Cwd,
		Provider:          record.Provider,
		Event:             record.Event,
		Tool:              record.Tool,
		Status:            record.Status,
		SourcePath:        path,
		Raw:               payload,
		Findings:          record.Findings,
		AgentRemediation:  record.AgentRemediation,
		RemediationEvents: record.RemediationEvents,
		HookEvent:         hookEventAnalytics(record),
		HookDecisions:     hookDecisionAnalytics(record),
		HookTargets:       hookTargetAnalytics(record),
		DeleteIntents:     hookDeleteIntents(record),
	}, nil
}

type hookTraceRecord struct {
	Command           *hookTraceCommand           `json:"command,omitempty"`
	Tool              string                      `json:"tool,omitempty"`
	Matcher           string                      `json:"matcher,omitempty"`
	RecordedAtUTC     string                      `json:"recorded_at_utc"`
	Provider          string                      `json:"provider,omitempty"`
	Event             string                      `json:"event"`
	OperationKind     string                      `json:"operation_kind,omitempty"`
	SessionID         string                      `json:"session_id,omitempty"`
	TargetSetSHA256   string                      `json:"target_set_sha256,omitempty"`
	Source            string                      `json:"source,omitempty"`
	TranscriptPath    string                      `json:"transcript_path,omitempty"`
	Cwd               string                      `json:"cwd,omitempty"`
	TraceID           string                      `json:"trace_id"`
	TrackingID        string                      `json:"tracking_id,omitempty"`
	TargetKind        string                      `json:"target_kind,omitempty"`
	Status            string                      `json:"status"`
	RiskCategory      string                      `json:"risk_category,omitempty"`
	AgentRemediation  []agentmsg.Remediation      `json:"agent_remediation,omitempty"`
	Files             []string                    `json:"files,omitempty"`
	Decisions         []hookTraceDecision         `json:"decisions,omitempty"`
	Findings          []evidence.Finding          `json:"findings,omitempty"`
	RemediationEvents []evidence.RemediationEvent `json:"remediation_events,omitempty"`
	OutputShape       hookTraceOutputShape        `json:"output_shape"`
	RuntimeMS         int64                       `json:"runtime_ms,omitempty"`
}

type hookTraceCommand struct {
	SHA256      string `json:"sha256"`
	ShapeSHA256 string `json:"shape_sha256,omitempty"`
	Preview     string `json:"preview,omitempty"`
}

type hookTraceDecision struct {
	PolicyID        string   `json:"policy_id,omitempty"`
	Decision        string   `json:"decision,omitempty"`
	Severity        string   `json:"severity,omitempty"`
	SkillID         string   `json:"skill_id,omitempty"`
	Suggestion      string   `json:"suggestion,omitempty"`
	Implementation  string   `json:"implementation,omitempty"`
	Message         string   `json:"message,omitempty"`
	MessageHash     string   `json:"message_hash,omitempty"`
	SuggestionHash  string   `json:"suggestion_hash,omitempty"`
	PrincipleIDs    []string `json:"principle_ids,omitempty"`
	DiagnosticCount int      `json:"diagnostic_count,omitempty"`
}

type hookTraceOutputShape struct {
	HasUpdatedInput      bool `json:"has_updated_input"`
	HasAdditionalContext bool `json:"has_additional_context"`
	Blocked              bool `json:"blocked"`
}

func traceIDOrSourceFallback(traceID, path string) string {
	if strings.TrimSpace(traceID) != "" || strings.TrimSpace(path) == "" {
		return traceID
	}

	cleaned := filepath.Clean(path)
	parent := filepath.Base(filepath.Dir(cleaned))
	base := filepath.Base(cleaned)
	sum := sha256.Sum256([]byte(cleaned))

	return fmt.Sprintf("source-%s-%s-%x", parent, base, sum[:6])
}

func hookEventAnalytics(record hookTraceRecord) *HookEventAnalytics {
	event := &HookEventAnalytics{
		TraceID:           record.TraceID,
		TrackingID:        record.TrackingID,
		SessionID:         record.SessionID,
		Provider:          record.Provider,
		Event:             record.Event,
		Tool:              record.Tool,
		Status:            record.Status,
		OperationKind:     record.OperationKind,
		TargetKind:        record.TargetKind,
		RiskCategory:      record.RiskCategory,
		TargetSetSHA256:   record.TargetSetSHA256,
		Cwd:               record.Cwd,
		Source:            record.Source,
		Matcher:           record.Matcher,
		TranscriptPath:    record.TranscriptPath,
		RuntimeMS:         record.RuntimeMS,
		DecisionCount:     len(record.Decisions),
		Blocked:           record.OutputShape.Blocked || record.Status == "blocked",
		Rewritten:         record.OutputShape.HasUpdatedInput,
		AdditionalContext: record.OutputShape.HasAdditionalContext,
	}
	if record.Command != nil {
		event.CommandSHA256 = record.Command.SHA256
		event.CommandShapeSHA256 = record.Command.ShapeSHA256
	}

	return event
}

func hookDecisionAnalytics(record hookTraceRecord) []HookDecisionAnalytics {
	if len(record.Decisions) == 0 {
		return nil
	}

	decisions := make([]HookDecisionAnalytics, 0, len(record.Decisions))
	for index, decision := range record.Decisions {
		decisions = append(decisions, HookDecisionAnalytics{
			TraceID:         record.TraceID,
			TrackingID:      record.TrackingID,
			PolicyID:        decision.PolicyID,
			Decision:        decision.Decision,
			Severity:        decision.Severity,
			SkillID:         decision.SkillID,
			Implementation:  decision.Implementation,
			Message:         decision.Message,
			MessageHash:     decision.MessageHash,
			Suggestion:      decision.Suggestion,
			SuggestionHash:  decision.SuggestionHash,
			PrincipleIDs:    append([]string(nil), decision.PrincipleIDs...),
			DiagnosticCount: decision.DiagnosticCount,
			DecisionOrdinal: index,
		})
	}

	return decisions
}

func hookTargetAnalytics(record hookTraceRecord) []HookTargetAnalytics {
	if len(record.Files) == 0 {
		return nil
	}

	targets := make([]HookTargetAnalytics, 0, len(record.Files))
	for index, target := range record.Files {
		targets = append(targets, HookTargetAnalytics{
			TraceID:     record.TraceID,
			TargetPath:  target,
			TargetKind:  record.TargetKind,
			TargetIndex: index,
		})
	}

	return targets
}

func hookDeleteIntents(record hookTraceRecord) []CodeDeleteIntent {
	if record.Command == nil ||
		record.Command.Preview == "" ||
		record.Status == "blocked" ||
		record.OutputShape.Blocked {
		return nil
	}

	paths := deleteIntentPathsFromShell(record.Command.Preview)
	if len(paths) == 0 {
		return nil
	}

	intents := make([]CodeDeleteIntent, 0, len(paths))
	for _, path := range paths {
		intents = append(intents, CodeDeleteIntent{
			Path:           path,
			IntentKind:     "hook_command_delete",
			TraceID:        record.TraceID,
			RecordedAtUTC:  record.RecordedAtUTC,
			Provider:       record.Provider,
			Event:          record.Event,
			Tool:           record.Tool,
			Status:         record.Status,
			Cwd:            record.Cwd,
			CommandSHA256:  record.Command.SHA256,
			CommandPreview: record.Command.Preview,
		})
	}

	return intents
}

func deleteIntentPathsFromShell(command string) []string {
	commands, err := shellparse.Commands(command)
	if err != nil {
		return nil
	}

	paths := []string{}

	for _, parsed := range commands {
		if len(parsed.Argv) == 0 {
			continue
		}

		switch parsed.Argv[0] {
		case "rm":
			paths = append(paths, rmIntentPaths(parsed.Argv[1:])...)
		case "git":
			paths = append(paths, gitRMIntentPaths(parsed.Argv[1:])...)
		}
	}

	return cleanDeleteIntentPaths(paths)
}

func rmIntentPaths(args []string) []string {
	paths := []string{}
	skipNext := false

	for _, arg := range args {
		if skipNext {
			skipNext = false

			continue
		}

		if arg == "--" {
			continue
		}

		if strings.HasPrefix(arg, "--") {
			if strings.Contains(arg, "=") {
				continue
			}

			if arg == "--interactive" {
				skipNext = true
			}

			continue
		}

		if strings.HasPrefix(arg, "-") {
			continue
		}

		if path, ok := cleanRepoRelativeIntentPath(arg); ok {
			paths = append(paths, path)
		}
	}

	return paths
}

func gitRMIntentPaths(args []string) []string {
	if len(args) == 0 || args[0] != "rm" {
		return nil
	}

	return rmIntentPaths(args[1:])
}

func cleanRepoRelativeIntentPath(path string) (string, bool) {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" || strings.HasPrefix(path, "/") {
		return "", false
	}

	cleaned := filepath.ToSlash(filepath.Clean(path))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}

	return cleaned, true
}

func cleanDeleteIntentPaths(paths []string) []string {
	cleaned := []string{}
	seen := map[string]bool{}

	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" || seen[path] {
			continue
		}

		seen[path] = true
		cleaned = append(cleaned, path)
	}

	return cleaned
}
