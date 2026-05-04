// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

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
	root := flags.String("root", ".", "Repository root containing .coding-ethos")
	dbPath := flags.String("db", "", "SQLite code intelligence database path")
	traceID := flags.String("trace-id", "", "Hook trace ID")
	trackingID := flags.String("tracking-id", "", "Hook tracking ID")
	disposition := flags.String("disposition", "", "Disposition: correct_block, false_positive, unclear_message, policy_too_broad, missing_allow")
	reviewer := flags.String("reviewer", "", "Reviewer identity")
	notes := flags.String("notes", "", "Review notes")
	recordedAt := flags.String("recorded-at", "", "Review timestamp in UTC RFC3339")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse record-hook-review flags: %w", err)
	}
	if *recordedAt == "" {
		*recordedAt = time.Now().UTC().Format(time.RFC3339)
	}

	store, err := openStore(ctx, *root, *dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	if err := store.RecordHookReview(ctx, codeintel.HookReview{
		TraceID:       *traceID,
		TrackingID:    *trackingID,
		Disposition:   *disposition,
		Reviewer:      *reviewer,
		Notes:         *notes,
		RecordedAtUTC: *recordedAt,
	}); err != nil {
		return err
	}

	stats, err := store.Stats(ctx)
	if err != nil {
		return err
	}

	return encodeJSON(os.Stdout, stats)
}

func printHookUsage(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("hook-usage", flag.ExitOnError)
	root := flags.String("root", ".", "Repository root containing .coding-ethos")
	dbPath := flags.String("db", "", "SQLite code intelligence database path")
	provider := flags.String("provider", "", "Filter by agent provider")
	status := flags.String("status", "", "Filter by hook status")
	policyID := flags.String("policy-id", "", "Filter by policy ID")
	skillID := flags.String("skill-id", "", "Filter by skill ID")
	operationKind := flags.String("operation-kind", "", "Filter by derived operation kind")
	targetKind := flags.String("target-kind", "", "Filter by derived target kind")
	riskCategory := flags.String("risk-category", "", "Filter by derived risk category")
	limit := flags.Int("limit", 20, "Maximum result count")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse hook-usage flags: %w", err)
	}

	store, err := openStore(ctx, *root, *dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	results, err := store.HookUsage(ctx, codeintel.HookUsageQuery{
		Provider:      *provider,
		Status:        *status,
		PolicyID:      *policyID,
		SkillID:       *skillID,
		OperationKind: *operationKind,
		TargetKind:    *targetKind,
		RiskCategory:  *riskCategory,
		Limit:         *limit,
	})
	if err != nil {
		return err
	}

	return encodeJSON(os.Stdout, results)
}

func printHookReviews(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("hook-reviews", flag.ExitOnError)
	root := flags.String("root", ".", "Repository root containing .coding-ethos")
	dbPath := flags.String("db", "", "SQLite code intelligence database path")
	traceID := flags.String("trace-id", "", "Filter by hook trace ID")
	trackingID := flags.String("tracking-id", "", "Filter by hook tracking ID")
	disposition := flags.String("disposition", "", "Filter by disposition")
	limit := flags.Int("limit", 20, "Maximum result count")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse hook-reviews flags: %w", err)
	}

	store, err := openStore(ctx, *root, *dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	results, err := store.HookReviews(ctx, codeintel.HookReviewQuery{
		TraceID:     *traceID,
		TrackingID:  *trackingID,
		Disposition: *disposition,
		Limit:       *limit,
	})
	if err != nil {
		return err
	}

	return encodeJSON(os.Stdout, results)
}
