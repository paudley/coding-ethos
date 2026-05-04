// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"blackcat.ca/coding-ethos/go/internal/hooks"
	"blackcat.ca/coding-ethos/go/internal/lint"
)

type TraceIngester struct {
	store *Store
}

func NewTraceIngester(store *Store) TraceIngester {
	return TraceIngester{store: store}
}

func (ingester TraceIngester) IngestLintTrace(ctx context.Context, payload []byte) error {
	trace, err := DecodeLintTrace("", payload)
	if err != nil {
		return err
	}

	return ingester.store.IngestTrace(ctx, trace)
}

func (ingester TraceIngester) IngestHookTrace(ctx context.Context, payload []byte) error {
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
	if err := ingester.ingestTraceDir(
		ctx,
		filepath.Join(root, ".coding-ethos", "lint-runs"),
		"lint",
		&summary,
	); err != nil {
		return summary, err
	}
	if err := ingester.ingestTraceDir(
		ctx,
		filepath.Join(root, ".coding-ethos", "hook-runs"),
		"hook",
		&summary,
	); err != nil {
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
	return filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return filepath.SkipDir
			}
			return fmt.Errorf("walk trace dir %q: %w", dir, err)
		}
		if entry.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}
		if kind == "hook" && filepath.Base(path) != "event.json" {
			return nil
		}

		summary.FilesScanned++
		payload, readErr := os.ReadFile(filepath.Clean(path))
		if readErr != nil {
			return fmt.Errorf("read trace %q: %w", path, readErr)
		}
		if ingestErr := ingester.ingestTracePayload(ctx, kind, path, payload); ingestErr != nil {
			return ingestErr
		}
		summary.FilesIngested++

		return nil
	})
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
		return fmt.Errorf("unsupported trace kind %q", kind)
	}
	if err != nil {
		return err
	}

	return ingester.store.IngestTrace(ctx, trace)
}

func DecodeLintTrace(path string, payload []byte) (Trace, error) {
	var record lint.TraceRecord
	if err := json.Unmarshal(payload, &record); err != nil {
		return Trace{}, fmt.Errorf("decode lint trace %q: %w", path, err)
	}

	return Trace{
		ID:                record.TraceID,
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
	if err := json.Unmarshal(payload, &record); err != nil {
		return Trace{}, fmt.Errorf("decode hook trace %q: %w", path, err)
	}

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
