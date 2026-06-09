// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintelcli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/feedback"
)

var errDecisionCommandRequired = apperror.StaticError(
	"decision command is required",
)

var errUnsupportedDecisionFormat = apperror.StaticError(
	"unsupported decisions format",
)

func decisions(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errDecisionCommandRequired
	}

	switch args[0] {
	case "list":
		return listDecisions(ctx, args[1:])
	case "add":
		return addDecision(ctx, args[1:])
	case "link":
		return linkDecision(ctx, args[1:])
	case "health":
		return decisionHealth(ctx, args[1:])
	default:
		return fmt.Errorf("%w: %q", errUnknownCodeIntelCommand, "decisions "+args[0])
	}
}

func listDecisions(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("decisions list", flag.ExitOnError)
	storeFlags := addStoreFlags(flags, "Repository root containing .coding-ethos")
	query := flags.String("query", "", "Decision search text")
	path := flags.String("path", "", "Filter by affected path")
	symbolPath := flags.String("symbol-path", "", "Filter by affected symbol path")
	status := flags.String("status", "", "Filter by decision status")
	format := flags.String(
		"format",
		feedback.FormatHuman,
		"Output format: human, json, or toon",
	)
	limit := addResultLimit(flags)

	err := parseCommandFlags(flags, args, "decisions list")
	if err != nil {
		return err
	}

	store, err := openStore(ctx, *storeFlags.root, *storeFlags.dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	records, err := store.Decisions(ctx, codeintel.DecisionQuery{
		Text:       *query,
		Path:       *path,
		SymbolPath: *symbolPath,
		Status:     *status,
		Limit:      *limit,
	})
	if err != nil {
		return fmt.Errorf("query decisions: %w", err)
	}

	return writeDecisionRecords(*format, records)
}

func addDecision(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("decisions add", flag.ExitOnError)
	storeFlags := addStoreFlags(flags, "Repository root containing .coding-ethos")
	title := flags.String("title", "", "Decision title")
	rationale := flags.String("rationale", "", "Decision rationale")
	alternatives := flags.String("alternatives", "", "Decision alternatives")
	status := flags.String("status", codeintel.DecisionStatusAccepted, "Decision status")
	path := flags.String("path", "", "Affected path")
	symbolPath := flags.String("symbol-path", "", "Affected symbol path")
	author := flags.String("author", "", "Decision author")
	format := flags.String(
		"format",
		outputFormatJSON,
		"Output format: json, human, or toon",
	)

	err := parseCommandFlags(flags, args, "decisions add")
	if err != nil {
		return err
	}

	store, err := openStore(ctx, *storeFlags.root, *storeFlags.dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	links := []codeintel.DecisionLink{}
	if strings.TrimSpace(*path) != "" {
		links = append(links, codeintel.DecisionLink{
			Path:       *path,
			SymbolPath: *symbolPath,
			Kind:       codeintel.DecisionLinkAffects,
		})
	}

	record, err := store.RecordDecision(ctx, codeintel.DecisionRecord{
		Title:        *title,
		Status:       *status,
		Rationale:    *rationale,
		Alternatives: *alternatives,
		SourceKind:   codeintel.DecisionSourceManual,
		Author:       *author,
		Links:        links,
	})
	if err != nil {
		return fmt.Errorf("record decision: %w", err)
	}

	return writeDecisionRecords(*format, []codeintel.DecisionRecord{record})
}

func linkDecision(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("decisions link", flag.ExitOnError)
	storeFlags := addStoreFlags(flags, "Repository root containing .coding-ethos")
	decisionID := flags.String("id", "", "Decision ID")
	path := flags.String("path", "", "Affected path")
	symbolPath := flags.String("symbol-path", "", "Affected symbol path")

	err := parseCommandFlags(flags, args, "decisions link")
	if err != nil {
		return err
	}

	store, err := openStore(ctx, *storeFlags.root, *storeFlags.dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	err = store.LinkDecision(ctx, *decisionID, []codeintel.DecisionLink{{
		Path:       *path,
		SymbolPath: *symbolPath,
		Kind:       codeintel.DecisionLinkAffects,
	}})
	if err != nil {
		return fmt.Errorf("link decision: %w", err)
	}

	return encodeJSON(os.Stdout, map[string]string{
		"kind":        "code_intel.decisions.link.v1",
		"decision_id": *decisionID,
		"path":        *path,
	})
}

func decisionHealth(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("decisions health", flag.ExitOnError)
	storeFlags := addStoreFlags(flags, "Repository root containing .coding-ethos")
	path := flags.String("path", "", "Filter by affected path")
	format := flags.String(
		"format",
		outputFormatJSON,
		"Output format: json, human, or toon",
	)
	limit := addResultLimit(flags)

	err := parseCommandFlags(flags, args, "decisions health")
	if err != nil {
		return err
	}

	store, err := openStore(ctx, *storeFlags.root, *storeFlags.dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	health, err := store.DecisionHealth(ctx, codeintel.DecisionQuery{
		Path:  *path,
		Limit: *limit,
	})
	if err != nil {
		return fmt.Errorf("query decision health: %w", err)
	}

	return writeDecisionHealth(*format, health)
}

func writeDecisionRecords(format string, records []codeintel.DecisionRecord) error {
	switch strings.TrimSpace(format) {
	case "", outputFormatJSON:
		return encodeJSON(os.Stdout, map[string]any{
			"kind":      "code_intel.decisions.v1",
			"decisions": records,
		})
	case feedback.FormatHuman, feedback.FormatTOON:
		err := feedback.Write(os.Stdout, feedback.Message{
			Scalars: []feedback.Scalar{
				feedback.S("kind", "code_intel.decisions.v1"),
				feedback.S("decisions", strconv.Itoa(len(records))),
			},
			Tables: []feedback.Table{decisionRecordsTable(records)},
		}, format)
		if err != nil {
			return fmt.Errorf("write decisions feedback: %w", err)
		}

		return nil
	default:
		return fmt.Errorf("%w: %q", errUnsupportedDecisionFormat, format)
	}
}

func writeDecisionHealth(format string, health codeintel.DecisionHealth) error {
	switch strings.TrimSpace(format) {
	case "", outputFormatJSON:
		return encodeJSON(os.Stdout, map[string]any{
			"kind":   "code_intel.decision_health.v1",
			"health": health,
		})
	case feedback.FormatHuman, feedback.FormatTOON:
		err := feedback.Write(os.Stdout, feedback.Message{
			Scalars: []feedback.Scalar{
				feedback.S("kind", "code_intel.decision_health.v1"),
				feedback.S("decisions", strconv.Itoa(health.Summary.DecisionCount)),
				feedback.S("stale", strconv.Itoa(health.Summary.StaleCount)),
				feedback.S("conflicts", strconv.Itoa(health.Summary.ConflictCount)),
				feedback.S("ungoverned", strconv.Itoa(health.Summary.UngovernedCount)),
			},
			Tables: []feedback.Table{decisionRecordsTable(health.Stale)},
		}, format)
		if err != nil {
			return fmt.Errorf("write decision health feedback: %w", err)
		}

		return nil
	default:
		return fmt.Errorf("%w: %q", errUnsupportedDecisionFormat, format)
	}
}

func decisionRecordsTable(records []codeintel.DecisionRecord) feedback.Table {
	rows := make([][]string, 0, len(records))
	for _, record := range records {
		rows = append(rows, []string{
			record.ID,
			record.Status,
			record.Title,
			record.SourceKind,
			record.SourcePath,
			decisionLinksCell(record.Links),
		})
	}

	return feedback.Table{
		Name:    "decisions",
		Columns: []string{"id", "status", "title", "source_kind", "source_path", "links"},
		Rows:    rows,
	}
}

func decisionLinksCell(links []codeintel.DecisionLink) string {
	parts := make([]string, 0, len(links))
	for _, link := range links {
		part := link.Path
		if link.SymbolPath != "" {
			part += "#" + link.SymbolPath
		}

		parts = append(parts, part)
	}

	return strings.Join(parts, ",")
}
