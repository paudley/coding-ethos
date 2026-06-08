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

var errUnsupportedGraphReportFormat = apperror.StaticError(
	"unsupported graph-report format",
)

func printGraphReport(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("graph-report", flag.ExitOnError)
	storeFlags := addStoreFlags(flags, "Repository root containing .coding-ethos")
	path := flags.String("path", "", "Filter by source path or directory")
	format := flags.String(
		"format",
		feedback.FormatHuman,
		"Output format: human, json, or toon",
	)
	limit := addResultLimit(flags)
	symbolsPerFile := flags.Int(
		"symbols-per-file",
		defaultGraphReportSymbols,
		"Maximum symbols to show for each ranked file",
	)

	err := parseCommandFlags(flags, args, "graph-report")
	if err != nil {
		return err
	}

	store, err := openStore(ctx, *storeFlags.root, *storeFlags.dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	report, err := store.GraphReport(ctx, codeintel.GraphReportQuery{
		Root:           *storeFlags.root,
		Path:           *path,
		Limit:          *limit,
		SymbolsPerFile: *symbolsPerFile,
	})
	if err != nil {
		return fmt.Errorf("query graph report: %w", err)
	}

	message := graphReportFeedback(report)

	formatVal := strings.TrimSpace(*format)
	if formatVal == "" {
		formatVal = feedback.FormatHuman
	}

	switch formatVal {
	case feedback.FormatHuman, feedback.FormatTOON:
		err = feedback.Write(
			os.Stdout,
			message,
			formatVal,
		)
		if err != nil {
			return fmt.Errorf("write graph report %s output: %w", formatVal, err)
		}

		return nil
	case outputFormatJSON:
		err = encodeJSON(os.Stdout, report)
		if err != nil {
			return fmt.Errorf("write graph report JSON: %w", err)
		}

		return nil
	default:
		return fmt.Errorf("%w: %q", errUnsupportedGraphReportFormat, *format)
	}
}

func graphReportFeedback(report codeintel.GraphReport) feedback.Message {
	return feedback.Message{
		Scalars: []feedback.Scalar{
			feedback.S("kind", report.Kind),
			feedback.S("root", report.Root),
			feedback.S("path", report.Path),
			feedback.S("files", strconv.Itoa(report.Stats.Files)),
			feedback.S("code_chunks", strconv.Itoa(report.Stats.CodeChunks)),
			feedback.S("code_edges", strconv.Itoa(report.Stats.CodeEdges)),
			feedback.S("findings", strconv.Itoa(report.Stats.Findings)),
			feedback.S("remediations", strconv.Itoa(report.Stats.Remediations)),
			feedback.S(
				"code_health_targets",
				strconv.Itoa(report.Stats.CodeHealthTargets),
			),
		},
		Tables: graphReportTables(report),
	}
}

func graphReportTables(report codeintel.GraphReport) []feedback.Table {
	tables := []feedback.Table{}
	if len(report.CentralFiles) > 0 {
		tables = append(tables, graphReportCentralFilesTable(report.CentralFiles))
	}

	if len(report.HealthTargets) > 0 {
		tables = append(tables, graphReportHealthTargetsTable(report.HealthTargets))
	}

	if len(report.Warnings) > 0 {
		tables = append(tables, graphReportListTable("warnings", report.Warnings))
	}

	if len(report.SuggestedActions) > 0 {
		tables = append(
			tables,
			graphReportListTable("suggested_actions", report.SuggestedActions),
		)
	}

	return tables
}

func graphReportCentralFilesTable(
	files []codeintel.GraphReportFile,
) feedback.Table {
	rows := make([][]string, 0, len(files))
	for _, file := range files {
		rows = append(rows, []string{
			file.Path,
			file.Language,
			strconv.Itoa(file.LineCount),
			strconv.Itoa(file.Score),
			strconv.FormatFloat(file.HotspotScore, 'f', 1, 64),
			strconv.Itoa(file.HiddenCouplingCount),
			strconv.Itoa(file.SymbolCount),
			strconv.Itoa(file.ChunkCount),
			strings.Join(file.ProvenanceClasses, "|"),
			strings.Join(file.Reasons, "; "),
		})
	}

	return feedback.Table{
		Name: "central_files",
		Columns: []string{
			"path",
			"language",
			"lines",
			"score",
			"hotspot",
			"hidden_couplings",
			"symbols",
			"chunks",
			"provenance",
			"reasons",
		},
		Rows: rows,
	}
}

func graphReportHealthTargetsTable(
	targets []codeintel.CodeHealthTarget,
) feedback.Table {
	rows := make([][]string, 0, len(targets))
	for _, target := range targets {
		rows = append(rows, []string{
			strconv.Itoa(target.Rank),
			target.Path,
			strconv.FormatFloat(target.PriorityScore, 'f', 1, 64),
			strconv.FormatFloat(target.HealthScore, 'f', 1, 64),
			graphReportPrimaryEvidence(target),
		})
	}

	return feedback.Table{
		Name:    "health_targets",
		Columns: []string{"rank", "path", "priority", "health", "evidence"},
		Rows:    rows,
	}
}

func graphReportListTable(name string, values []string) feedback.Table {
	rows := make([][]string, 0, len(values))
	for _, value := range values {
		rows = append(rows, []string{value})
	}

	return feedback.Table{
		Name:    name,
		Columns: []string{"message"},
		Rows:    rows,
	}
}

func graphReportPrimaryEvidence(target codeintel.CodeHealthTarget) string {
	if len(target.Evidence) == 0 {
		return ""
	}

	return target.Evidence[0].Message
}
