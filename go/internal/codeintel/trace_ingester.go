// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/hooks"
	"blackcat.ca/coding-ethos/go/internal/lint"
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
	trace, err := DecodeHookTrace("", payload)
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

	if err == sql.ErrNoRows {
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
	var record hooks.HookTrace

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
	}, nil
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

func hookEventAnalytics(record hooks.HookTrace) *HookEventAnalytics {
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

func hookDecisionAnalytics(record hooks.HookTrace) []HookDecisionAnalytics {
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

func hookTargetAnalytics(record hooks.HookTrace) []HookTargetAnalytics {
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
