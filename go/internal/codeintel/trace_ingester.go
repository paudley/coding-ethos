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
	}, nil
}
