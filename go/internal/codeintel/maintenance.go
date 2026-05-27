// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MaintenanceSummary reports repo-local code-intel refresh work.
type MaintenanceSummary struct {
	CodeIndex    CodeIndexSummary       `json:"code_index"`
	TraceIngest  IngestSummary          `json:"trace_ingest"`
	DiffPatterns DiffEditPatternSummary `json:"diff_patterns"`
}

// RefreshRepository opens the repo-local code-intel store, ingests retained
// traces, and refreshes AST facts for the requested paths.
func RefreshRepository(
	ctx context.Context,
	root string,
	paths []string,
) (MaintenanceSummary, error) {
	store, err := Open(ctx, DefaultDBPath(root))
	if err != nil {
		return MaintenanceSummary{}, err
	}
	defer store.Close()

	traceSummary, err := NewTraceIngester(store).IngestTraceDirs(ctx, root)
	if err != nil {
		return MaintenanceSummary{}, fmt.Errorf("ingest code-intel traces: %w", err)
	}

	indexSummary, err := NewASTIndexer(store).IndexPaths(ctx, root, paths)
	if err != nil {
		return MaintenanceSummary{}, fmt.Errorf("refresh code-intel AST index: %w", err)
	}

	patternCount, err := store.RefreshDiffEditPatterns(ctx, root)
	if err != nil {
		return MaintenanceSummary{}, fmt.Errorf(
			"refresh code-intel diff edit patterns: %w",
			err,
		)
	}

	return MaintenanceSummary{
		TraceIngest:  traceSummary,
		CodeIndex:    indexSummary,
		DiffPatterns: DiffEditPatternSummary{PatternsRecorded: patternCount},
	}, nil
}

// RefreshLintFiles refreshes code-intel AST facts for lint-scoped paths.
func RefreshLintFiles(
	ctx context.Context,
	root string,
	paths []string,
) (CodeIndexSummary, error) {
	paths = existingLintIndexPaths(root, paths)
	if len(paths) == 0 {
		return CodeIndexSummary{}, nil
	}

	store, err := Open(ctx, DefaultDBPath(root))
	if err != nil {
		return CodeIndexSummary{}, err
	}
	defer store.Close()

	summary, err := NewASTIndexer(store).IndexPaths(ctx, root, paths)
	if err != nil {
		return CodeIndexSummary{}, fmt.Errorf("refresh code-intel lint files: %w", err)
	}

	_, err = store.RefreshDiffEditPatterns(ctx, root)
	if err != nil {
		return CodeIndexSummary{}, fmt.Errorf(
			"refresh code-intel diff edit patterns: %w",
			err,
		)
	}

	return summary, nil
}

func existingLintIndexPaths(root string, paths []string) []string {
	selected := []string{}
	seen := map[string]struct{}{}

	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return selected
	}

	rootAbs = filepath.Clean(rootAbs)

	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}

		statPath := path
		if !filepath.IsAbs(statPath) {
			statPath = filepath.Join(rootAbs, statPath)
		}

		statPath = filepath.Clean(statPath)

		relative, inside := repoRelativePath(rootAbs, statPath)
		if !inside {
			continue
		}

		info, err := os.Stat(statPath)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}

		if _, ok := seen[statPath]; ok {
			continue
		}

		seen[statPath] = struct{}{}

		selected = append(selected, relative)
	}

	return selected
}

func repoRelativePath(rootAbs, pathAbs string) (string, bool) {
	relative, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil ||
		relative == "." ||
		relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) ||
		filepath.IsAbs(relative) {
		return "", false
	}

	return filepath.ToSlash(relative), true
}

func rootedTracePath(root, tracePath string) (string, error) {
	if filepath.IsAbs(tracePath) {
		return filepath.Clean(tracePath), nil
	}

	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve trace root: %w", err)
	}

	return filepath.Clean(filepath.Join(rootAbs, tracePath)), nil
}

// IngestHookTraceFile records a single hook event trace that was just written
// by the hook runtime. The append-only event log is the durable telemetry path;
// the DuckDB store is updated as the query index.
func IngestHookTraceFile(ctx context.Context, root, tracePath string) error {
	resolvedTracePath, err := rootedTracePath(root, tracePath)
	if err != nil {
		return err
	}

	payload, err := os.ReadFile(resolvedTracePath)
	if err != nil {
		return fmt.Errorf("read hook trace for code-intel ingest: %w", err)
	}

	runID := hookTraceEventRunID(resolvedTracePath)

	err = NewEventLog(DefaultEventLogDir(root)).Append(runID, []EventRecord{
		{
			Kind:        "hook_trace",
			SourceRunID: runID,
			Payload:     payload,
		},
	})
	if err != nil {
		return fmt.Errorf("append hook trace event: %w", err)
	}

	store, err := Open(ctx, DefaultDBPath(root))
	if err != nil {
		return err
	}
	defer store.Close()

	return NewTraceIngester(store).IngestHookTraceSource(ctx, resolvedTracePath, payload)
}

func hookTraceEventRunID(resolvedTracePath string) string {
	fileName := filepath.Base(resolvedTracePath)
	runID := strings.TrimSuffix(fileName, filepath.Ext(fileName))

	if fileName != "event.json" {
		return runID
	}

	parentRunID := filepath.Base(filepath.Dir(resolvedTracePath))
	if strings.TrimSpace(parentRunID) == "" ||
		parentRunID == "." ||
		parentRunID == string(filepath.Separator) {
		return runID
	}

	return parentRunID
}

// IngestLintTraceFile records a single lint trace that was just written.
func IngestLintTraceFile(ctx context.Context, root, tracePath string) error {
	resolvedTracePath, err := rootedTracePath(root, tracePath)
	if err != nil {
		return err
	}

	payload, err := os.ReadFile(resolvedTracePath)
	if err != nil {
		return fmt.Errorf("read lint trace for code-intel ingest: %w", err)
	}

	runID := strings.TrimSuffix(
		filepath.Base(resolvedTracePath),
		filepath.Ext(resolvedTracePath),
	)

	err = NewEventLog(DefaultEventLogDir(root)).Append(runID, []EventRecord{
		{
			Kind:        "lint_trace",
			SourceRunID: runID,
			Payload:     payload,
		},
	})
	if err != nil {
		return fmt.Errorf("append lint trace event: %w", err)
	}

	store, err := Open(ctx, DefaultDBPath(root))
	if err != nil {
		return err
	}
	defer store.Close()

	return NewTraceIngester(store).IngestLintTraceSource(ctx, resolvedTracePath, payload)
}
