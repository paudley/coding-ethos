// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintelcli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/contextadvisor"
	"blackcat.ca/coding-ethos/go/internal/feedback"
	"blackcat.ca/coding-ethos/go/internal/outputsurface"
)

func printContextAdvice(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("context-advice", flag.ExitOnError)
	storeFlags := addStoreFlags(flags, "Repository root containing .coding-ethos")
	provider := flags.String("provider", "", "Filter by agent provider")
	sessionID := flags.String("session-id", "", "Filter by agent session ID")
	format := flags.String("format", outputFormatJSON, "Output format: json or toon")
	includeTemp := flags.Bool("include-temp", false, "Include OS temp output surfaces")
	limit := addResultLimit(flags)

	err := parseCommandFlags(flags, args, "context-advice")
	if err != nil {
		return err
	}

	now := time.Now().UTC()

	snapshot, err := contextAdviceSnapshot(ctx, contextAdviceSnapshotInput{
		Root:      *storeFlags.root,
		DBPath:    *storeFlags.dbPath,
		Provider:  *provider,
		SessionID: *sessionID,
		Limit:     *limit,
		Now:       now,
	})
	if err != nil {
		return fmt.Errorf("query context advice session snapshot: %w", err)
	}

	surfaces, err := outputsurface.BuildReport(ctx, outputsurface.Options{
		Root:        *storeFlags.root,
		IncludeTemp: *includeTemp,
		Now:         now,
	})
	if err != nil {
		return fmt.Errorf("build context advice output surface report: %w", err)
	}

	thresholds, err := contextadvisor.LoadThresholds(*storeFlags.root)
	if err != nil {
		return fmt.Errorf("load context advisor thresholds: %w", err)
	}

	report := contextadvisor.Analyze(snapshot, surfaces, thresholds, now)

	switch strings.TrimSpace(*format) {
	case "", outputFormatJSON:
		return encodeJSON(os.Stdout, report)
	case outputFormatTOON:
		err = feedback.WriteRendered(
			os.Stdout,
			contextadvisor.FormatTOON(report),
			feedback.FormatTOON,
		)
		if err != nil {
			return fmt.Errorf("write context advice TOON: %w", err)
		}

		return nil
	default:
		return fmt.Errorf("%w: %q", errUnknownDownstreamAnalysisFormat, *format)
	}
}

type contextAdviceSnapshotInput struct {
	Now       time.Time
	Root      string
	DBPath    string
	Provider  string
	SessionID string
	Limit     int
}

func contextAdviceSnapshot(
	ctx context.Context,
	input contextAdviceSnapshotInput,
) (codeintel.SessionSnapshot, error) {
	store, err := openReadOnlyStore(ctx, input.Root, input.DBPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return missingContextAdviceSnapshot(input), nil
		}

		return codeintel.SessionSnapshot{}, fmt.Errorf(
			"open context advice code-intel store: %w",
			err,
		)
	}
	defer store.Close()

	snapshot, err := store.SessionSnapshot(ctx, codeintel.SessionSnapshotQuery{
		Root:      input.Root,
		Worktree:  input.Root,
		Provider:  input.Provider,
		SessionID: input.SessionID,
		Limit:     input.Limit,
		Now:       input.Now,
	})
	if err != nil {
		return codeintel.SessionSnapshot{}, fmt.Errorf(
			"query context advice code-intel snapshot: %w",
			err,
		)
	}

	return snapshot, nil
}

func missingContextAdviceSnapshot(
	input contextAdviceSnapshotInput,
) codeintel.SessionSnapshot {
	return codeintel.SessionSnapshot{
		Kind:           codeintel.SessionSnapshotKind,
		SchemaVersion:  "1",
		GeneratedAtUTC: input.Now.UTC().Format(time.RFC3339Nano),
		Session: codeintel.SessionIdentity{
			ID:       input.SessionID,
			Provider: input.Provider,
			Source:   "missing_index",
		},
		Repository: codeintel.SessionRepository{
			Root:     input.Root,
			Worktree: input.Root,
		},
		CodeIntel: codeintel.SessionCodeIntelSummary{
			Freshness:  "missing_index",
			StoreReady: false,
		},
		Provider: codeintel.SessionProviderSummary{Adapters: map[string]any{}},
	}
}
