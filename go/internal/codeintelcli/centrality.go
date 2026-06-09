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

	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/feedback"
)

func printCentrality(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("centrality", flag.ExitOnError)
	storeFlags := addStoreFlags(flags, "Repository root containing .coding-ethos")
	path := flags.String("path", "", "Filter by source path or directory")
	format := flags.String(
		"format",
		feedback.FormatHuman,
		"Output format: human, json, or toon",
	)
	limit := addResultLimit(flags)

	err := parseCommandFlags(flags, args, "centrality")
	if err != nil {
		return err
	}

	store, err := openStore(ctx, *storeFlags.root, *storeFlags.dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	nodes, err := store.CentralNodes(ctx, codeintel.CentralNodeQuery{
		Root:  *storeFlags.root,
		Path:  *path,
		Limit: *limit,
	})
	if err != nil {
		return fmt.Errorf("query central nodes: %w", err)
	}

	return writeCentralityOutput(*format, nodes)
}

func printSurprises(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("surprises", flag.ExitOnError)
	storeFlags := addStoreFlags(flags, "Repository root containing .coding-ethos")
	path := flags.String("path", "", "Filter by source path or directory")
	format := flags.String(
		"format",
		feedback.FormatHuman,
		"Output format: human, json, or toon",
	)
	limit := addResultLimit(flags)

	err := parseCommandFlags(flags, args, "surprises")
	if err != nil {
		return err
	}

	store, err := openStore(ctx, *storeFlags.root, *storeFlags.dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	edges, err := store.SurpriseEdges(ctx, codeintel.SurpriseEdgeQuery{
		Root:  *storeFlags.root,
		Path:  *path,
		Limit: *limit,
	})
	if err != nil {
		return fmt.Errorf("query surprise edges: %w", err)
	}

	return writeSurprisesOutput(*format, edges)
}

func writeCentralityOutput(format string, nodes []codeintel.CentralNode) error {
	format = strings.TrimSpace(format)
	if format == "" {
		format = feedback.FormatHuman
	}

	switch format {
	case "", feedback.FormatHuman, feedback.FormatTOON:
		err := feedback.Write(
			os.Stdout,
			feedback.Message{
				Scalars: []feedback.Scalar{
					feedback.S("kind", "code_intel.centrality.v1"),
					feedback.S("central_nodes", strconv.Itoa(len(nodes))),
				},
				Tables: []feedback.Table{centralNodesTable(nodes)},
			},
			format,
		)
		if err != nil {
			return fmt.Errorf("write centrality output: %w", err)
		}

		return nil
	case outputFormatJSON:
		return encodeJSON(os.Stdout, map[string]any{
			"kind":          "code_intel.centrality.v1",
			"central_nodes": nodes,
		})
	default:
		return fmt.Errorf("%w: %q", errUnsupportedGraphReportFormat, format)
	}
}

func writeSurprisesOutput(format string, edges []codeintel.SurpriseEdge) error {
	format = strings.TrimSpace(format)
	if format == "" {
		format = feedback.FormatHuman
	}

	switch format {
	case "", feedback.FormatHuman, feedback.FormatTOON:
		err := feedback.Write(
			os.Stdout,
			feedback.Message{
				Scalars: []feedback.Scalar{
					feedback.S("kind", "code_intel.surprises.v1"),
					feedback.S("surprise_edges", strconv.Itoa(len(edges))),
				},
				Tables: []feedback.Table{surpriseEdgesTable(edges)},
			},
			format,
		)
		if err != nil {
			return fmt.Errorf("write surprises output: %w", err)
		}

		return nil
	case outputFormatJSON:
		return encodeJSON(os.Stdout, map[string]any{
			"kind":           "code_intel.surprises.v1",
			"surprise_edges": edges,
		})
	default:
		return fmt.Errorf("%w: %q", errUnsupportedGraphReportFormat, format)
	}
}

func centralNodesTable(nodes []codeintel.CentralNode) feedback.Table {
	rows := make([][]string, 0, len(nodes))
	for _, node := range nodes {
		rows = append(rows, []string{
			node.Path,
			node.Language,
			strconv.Itoa(node.Score),
			strconv.Itoa(node.Degree),
			strings.Join(node.ProvenanceClasses, "|"),
			centralNodeSignals(node.Signals),
		})
	}

	return feedback.Table{
		Name:    "central_nodes",
		Columns: []string{"path", "language", "score", "degree", "provenance", "signals"},
		Rows:    rows,
	}
}

func surpriseEdgesTable(edges []codeintel.SurpriseEdge) feedback.Table {
	rows := make([][]string, 0, len(edges))
	for _, edge := range edges {
		rows = append(rows, []string{
			edge.Kind,
			edge.SourcePath,
			edge.TargetPath,
			strconv.Itoa(edge.Score),
			edge.ProvenanceClass,
			surpriseEdgeReasons(edge.Reasons),
		})
	}

	return feedback.Table{
		Name: "surprise_edges",
		Columns: []string{
			"kind",
			"source",
			"target",
			"score",
			"provenance",
			"reasons",
		},
		Rows: rows,
	}
}

func centralNodeSignals(signals []codeintel.CentralNodeSignal) string {
	parts := make([]string, 0, len(signals))
	for _, signal := range signals {
		parts = append(parts, fmt.Sprintf(
			"%s(score=%d,provenance=%s): %s",
			signal.Kind,
			signal.Score,
			signal.ProvenanceClass,
			signal.Message,
		))
	}

	return strings.Join(parts, "; ")
}

func surpriseEdgeReasons(reasons []codeintel.SurpriseEdgeReason) string {
	parts := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		parts = append(parts, fmt.Sprintf(
			"%s(score=%d): %s",
			reason.Kind,
			reason.Score,
			reason.Message,
		))
	}

	return strings.Join(parts, "; ")
}
