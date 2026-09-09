// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// MaintenanceSummary reports repo-local code-intel refresh work.
type MaintenanceSummary struct {
	CodeIndex    CodeIndexSummary       `json:"code_index"`
	TraceIngest  IngestSummary          `json:"trace_ingest"`
	DiffPatterns DiffEditPatternSummary `json:"diff_patterns"`
}

// RefreshRepository explicitly opens the repo-local code-intel store, ingests
// retained traces, and refreshes AST facts for the requested paths. Normal
// hook execution must use the event and changed-file ingestion paths instead;
// a whole-repository refresh is an operator-requested maintenance action.
func RefreshRepository(
	ctx context.Context,
	root string,
	paths []string,
) (MaintenanceSummary, error) {
	stateRoot := ResolveStateRoot(root)

	store, err := Open(ctx, DefaultDBPath(stateRoot))
	if err != nil {
		return MaintenanceSummary{}, err
	}
	defer store.Close()

	traceSummary, err := NewTraceIngester(store).IngestTraceDirs(ctx, stateRoot)
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

// RefreshLintFiles incrementally refreshes code-intel AST facts for explicit
// lint-scoped files and tombstones explicit files that have been deleted. It
// rejects directories and out-of-repository paths so ordinary hook execution
// cannot turn this API into a whole-repository walk.
func RefreshLintFiles(
	ctx context.Context,
	root string,
	paths []string,
) (CodeIndexSummary, error) {
	selected := selectLintIndexPaths(root, paths)
	if len(selected.requested) == 0 {
		return CodeIndexSummary{}, nil
	}

	root, err := filepath.Abs(root)
	if err != nil {
		return CodeIndexSummary{}, fmt.Errorf("resolve lint index root: %w", err)
	}

	store, err := Open(ctx, DefaultDBPath(ResolveStateRoot(root)))
	if err != nil {
		return CodeIndexSummary{}, err
	}
	defer store.Close()

	existingFiles, err := explicitCodeFilesByPath(ctx, store, selected.requested)
	if err != nil {
		return CodeIndexSummary{}, err
	}

	summary, ignored, err := indexExplicitLintFiles(
		ctx,
		store,
		root,
		selected.existing,
		existingFiles,
	)
	if err != nil {
		return CodeIndexSummary{}, fmt.Errorf(
			"refresh code-intel lint files: %w",
			err,
		)
	}

	deleted := activeExplicitCodeFiles(existingFiles, selected.deleted)
	if len(deleted) > 0 {
		err = store.markDeletedCodeFiles(
			ctx,
			root,
			deleted,
			gitStagedDeletedPathsFor(ctx, root, deleted),
		)
		if err != nil {
			return CodeIndexSummary{}, fmt.Errorf(
				"tombstone deleted code-intel lint files: %w",
				err,
			)
		}
	}

	err = markExplicitCodeFilesInactive(ctx, store, ignored, "ignored")
	if err != nil {
		return CodeIndexSummary{}, fmt.Errorf(
			"tombstone ignored code-intel lint files: %w",
			err,
		)
	}

	summary.Deleted = append(summary.Deleted, deleted...)
	summary.Deleted = append(summary.Deleted, ignored...)
	slices.Sort(summary.Deleted)
	summary.Deleted = slices.Compact(summary.Deleted)

	return summary, nil
}

func explicitCodeFilesByPath(
	ctx context.Context,
	store *Store,
	paths []string,
) (map[string]CodeFile, error) {
	files := make(map[string]CodeFile, len(paths))

	for _, path := range paths {
		file, found, err := store.GetCodeFile(ctx, path)
		if err != nil {
			return nil, err
		}

		if found {
			files[path] = file
		}
	}

	return files, nil
}

func indexExplicitLintFiles(
	ctx context.Context,
	store *Store,
	root string,
	paths []string,
	existingFiles map[string]CodeFile,
) (CodeIndexSummary, []string, error) {
	options, err := LoadIndexOptions(root)
	if err != nil {
		return CodeIndexSummary{}, nil, err
	}

	ignoreMatcher := gitIgnoreMatcher{
		root:   root,
		active: gitWorkTreeAvailable(ctx, root),
	}
	indexer := NewASTIndexer(store)
	summary := CodeIndexSummary{}
	ignored := []string{}

	for _, path := range paths {
		absolutePath := filepath.Join(root, filepath.FromSlash(path))

		info, statErr := os.Lstat(absolutePath)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				continue
			}

			return CodeIndexSummary{}, nil, fmt.Errorf(
				"stat lint index file %q: %w",
				path,
				statErr,
			)
		}

		if !info.Mode().IsRegular() {
			continue
		}

		if ignoreMatcher.ignoredFile(ctx, absolutePath) ||
			pathHasSkippedDir(path) ||
			excludedByConfig(path, options.ExcludePatterns) {
			if existing, found := existingFiles[path]; found &&
				existing.DeletedAtUTC == "" {
				ignored = append(ignored, path)
			}

			continue
		}

		err = indexer.indexFile(
			ctx,
			root,
			absolutePath,
			info,
			gitIgnoreMatcher{},
			existingFiles,
			options,
			&summary,
		)
		if err != nil {
			return CodeIndexSummary{}, nil, err
		}
	}

	return summary, ignored, nil
}

func activeExplicitCodeFiles(
	existingFiles map[string]CodeFile,
	paths []string,
) []string {
	active := make([]string, 0, len(paths))

	for _, path := range paths {
		if existing, found := existingFiles[path]; found && existing.DeletedAtUTC == "" {
			active = append(active, path)
		}
	}

	return active
}

func markExplicitCodeFilesInactive(
	ctx context.Context,
	store *Store,
	paths []string,
	reason string,
) error {
	if len(paths) == 0 {
		return nil
	}

	inactiveAt := time.Now().UTC().Format(time.RFC3339)

	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin explicit code file refresh: %w", err)
	}
	defer rollbackUnlessCommitted(transaction)

	for _, path := range paths {
		err = markCodeFileInactive(ctx, transaction, path, inactiveAt, reason)
		if err != nil {
			return err
		}
	}

	err = transaction.Commit()
	if err != nil {
		return fmt.Errorf("commit explicit code file refresh: %w", err)
	}

	return nil
}

type lintIndexPathSelection struct {
	requested []string
	existing  []string
	deleted   []string
}

func selectLintIndexPaths(root string, paths []string) lintIndexPathSelection {
	selected := lintIndexPathSelection{}
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

		if _, ok := seen[relative]; ok {
			continue
		}

		info, statErr := os.Lstat(statPath)
		if statErr != nil && !os.IsNotExist(statErr) {
			continue
		}

		if statErr == nil && !info.Mode().IsRegular() {
			continue
		}

		seen[relative] = struct{}{}
		selected.requested = append(selected.requested, relative)

		if statErr == nil {
			selected.existing = append(selected.existing, relative)
		} else {
			selected.deleted = append(selected.deleted, relative)
		}
	}

	slices.Sort(selected.requested)
	slices.Sort(selected.existing)
	slices.Sort(selected.deleted)

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
	eventLog := NewEventLog(DefaultEventLogDir(ResolveStateRoot(root)))

	err = eventLog.Append(runID, []EventRecord{
		{
			Kind:        "hook_trace",
			SourceRunID: runID,
			Payload:     payload,
		},
	})
	if err != nil {
		return fmt.Errorf("append hook trace event: %w", err)
	}

	store, err := Open(ctx, DefaultDBPath(ResolveStateRoot(root)))
	if err != nil {
		if IsStoreLockError(err) {
			return nil
		}

		return err
	}
	defer store.Close()

	return NewTraceIngester(store).IngestHookTraceSource(ctx, resolvedTracePath, payload)
}

func hookTraceEventRunID(resolvedTracePath string) string {
	fileName := filepath.Base(resolvedTracePath)
	runID := strings.TrimSuffix(fileName, filepath.Ext(fileName))

	if fileName != downstreamEventJSONFile {
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

	err = NewEventLog(
		DefaultEventLogDir(ResolveStateRoot(root)),
	).Append(runID, []EventRecord{
		{
			Kind:        "lint_trace",
			SourceRunID: runID,
			Payload:     payload,
		},
	})
	if err != nil {
		return fmt.Errorf("append lint trace event: %w", err)
	}

	store, err := Open(ctx, DefaultDBPath(ResolveStateRoot(root)))
	if err != nil {
		if IsStoreLockError(err) {
			return nil
		}

		return err
	}
	defer store.Close()

	return NewTraceIngester(store).IngestLintTraceSource(ctx, resolvedTracePath, payload)
}
