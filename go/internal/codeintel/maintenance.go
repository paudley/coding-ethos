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

// IngestHookTraceFile records a single hook event trace that was just written
// by the hook runtime. The append-only event log is the durable telemetry path;
// the legacy SQLite store is still updated as a compatibility query index.
func IngestHookTraceFile(ctx context.Context, root, tracePath string) error {
	payload, err := os.ReadFile(filepath.Clean(tracePath))
	if err != nil {
		return fmt.Errorf("read hook trace for code-intel ingest: %w", err)
	}

	runID := strings.TrimSuffix(filepath.Base(tracePath), filepath.Ext(tracePath))

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

	return NewTraceIngester(store).IngestHookTraceSource(ctx, tracePath, payload)
}
