// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintelcli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"blackcat.ca/coding-ethos/go/internal/codeintel"
)

func recordHookReview(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("record-hook-review", flag.ExitOnError)
	storeFlags := addStoreFlags(flags, "Repository root containing .coding-ethos")
	traceID := flags.String("trace-id", "", "Hook trace ID")
	trackingID := flags.String("tracking-id", "", "Hook tracking ID")
	disposition := flags.String(
		"disposition",
		"",
		"Disposition: correct_block, false_positive, unclear_message, "+
			"policy_too_broad, missing_allow",
	)
	reviewer := flags.String("reviewer", "", "Reviewer identity")
	notes := flags.String("notes", "", "Review notes")

	recordedAt := flags.String("recorded-at", "", "Review timestamp in UTC RFC3339")

	err := parseCommandFlags(flags, args, "record-hook-review")
	if err != nil {
		return err
	}

	if *recordedAt == "" {
		*recordedAt = time.Now().UTC().Format(time.RFC3339)
	}

	store, err := openStore(ctx, *storeFlags.root, *storeFlags.dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	err = store.RecordHookReview(ctx, codeintel.HookReview{
		TraceID:       *traceID,
		TrackingID:    *trackingID,
		Disposition:   *disposition,
		Reviewer:      *reviewer,
		Notes:         *notes,
		RecordedAtUTC: *recordedAt,
	})
	if err != nil {
		return fmt.Errorf("record hook review: %w", err)
	}

	stats, err := store.Stats(ctx)
	if err != nil {
		return fmt.Errorf("read code intelligence stats: %w", err)
	}

	return encodeJSON(os.Stdout, stats)
}

func printHookUsage(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("hook-usage", flag.ExitOnError)
	storeFlags := addStoreFlags(flags, "Repository root containing .coding-ethos")
	provider := flags.String("provider", "", "Filter by agent provider")
	status := flags.String("status", "", "Filter by hook status")
	filters := addPolicySkillFlags(flags)
	operationKind := flags.String(
		"operation-kind",
		"",
		"Filter by derived operation kind",
	)
	targetKind := flags.String("target-kind", "", "Filter by derived target kind")
	riskCategory := flags.String("risk-category", "", "Filter by derived risk category")
	limit := addResultLimit(flags)

	return parseAndPrintStoreJSON(
		ctx,
		args,
		"hook-usage",
		flags,
		storeFlags,
		func(store *codeintel.Store) (any, error) {
			return store.HookUsage(ctx, codeintel.HookUsageQuery{
				Provider:      *provider,
				Status:        *status,
				PolicyID:      *filters.policyID,
				SkillID:       *filters.skillID,
				OperationKind: *operationKind,
				TargetKind:    *targetKind,
				RiskCategory:  *riskCategory,
				Limit:         *limit,
			})
		},
	)
}

func printHookReviews(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("hook-reviews", flag.ExitOnError)
	storeFlags := addStoreFlags(flags, "Repository root containing .coding-ethos")
	traceID := flags.String("trace-id", "", "Filter by hook trace ID")
	trackingID := flags.String("tracking-id", "", "Filter by hook tracking ID")
	disposition := flags.String("disposition", "", "Filter by disposition")
	limit := addResultLimit(flags)

	return parseAndPrintStoreJSON(
		ctx,
		args,
		"hook-reviews",
		flags,
		storeFlags,
		func(store *codeintel.Store) (any, error) {
			return store.HookReviews(ctx, codeintel.HookReviewQuery{
				TraceID:     *traceID,
				TrackingID:  *trackingID,
				Disposition: *disposition,
				Limit:       *limit,
			})
		},
	)
}

func printRepeatedEdits(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("repeated-edits", flag.ExitOnError)
	storeFlags := addStoreFlags(flags, "Repository root containing .coding-ethos")
	diffSource := flags.String(
		"diff-source",
		"",
		"Filter by diff source: worktree or staged",
	)
	path := flags.String("path", "", "Filter by edited path")
	limit := addResultLimit(flags)

	return parseAndPrintStoreJSON(
		ctx,
		args,
		"repeated-edits",
		flags,
		storeFlags,
		func(store *codeintel.Store) (any, error) {
			return store.RepeatedDiffEditPatterns(ctx, codeintel.DiffEditPatternQuery{
				DiffSource: *diffSource,
				Path:       *path,
				Limit:      *limit,
			})
		},
	)
}
