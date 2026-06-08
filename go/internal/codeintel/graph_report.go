// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"fmt"
	"strings"
)

const (
	graphReportKind           = "code_intel.graph_report.v1"
	defaultGraphReportLimit   = 20
	graphReportSymbolsPerFile = 4
	graphReportHealthTrend    = 0
	graphReportHotspotFormat  = "%.1f"
	graphReportReasonCapacity = 5
	graphReportWarnCapacity   = 2
)

// GraphReportQuery controls graph report scope and ranked-symbol density.
type GraphReportQuery struct {
	Path           string
	Root           string
	Limit          int
	SymbolsPerFile int
}

// GraphReport summarizes the local code-intel graph for agent orientation.
type GraphReport struct {
	Kind             string             `json:"kind"`
	Root             string             `json:"root,omitempty"`
	Path             string             `json:"path,omitempty"`
	RepoMap          RepoMap            `json:"repo_map"`
	CentralFiles     []GraphReportFile  `json:"central_files,omitempty"`
	HealthTargets    []CodeHealthTarget `json:"health_targets,omitempty"`
	SuggestedActions []string           `json:"suggested_actions,omitempty"`
	Warnings         []string           `json:"warnings,omitempty"`
	Stats            Stats              `json:"stats"`
}

// GraphReportFile describes one ranked file in the graph orientation report.
type GraphReportFile struct {
	Path                string          `json:"path"`
	Language            string          `json:"language,omitempty"`
	PrimaryAuthorEmail  string          `json:"primary_author_email,omitempty"`
	Symbols             []RepoMapSymbol `json:"symbols,omitempty"`
	Reasons             []string        `json:"reasons,omitempty"`
	SymbolCount         int             `json:"symbol_count"`
	ChunkCount          int             `json:"chunk_count"`
	LineCount           int             `json:"line_count"`
	HiddenCouplingCount int             `json:"hidden_coupling_count,omitempty"`
	Score               int             `json:"score"`
	HotspotScore        float64         `json:"hotspot_score,omitempty"`
}

// GraphReport composes indexed code facts, repo-map signals, and stored health.
func (store *Store) GraphReport(
	ctx context.Context,
	query GraphReportQuery,
) (GraphReport, error) {
	query.Root = strings.TrimSpace(query.Root)
	query.Path = strings.TrimSpace(query.Path)

	stats, err := store.Stats(ctx)
	if err != nil {
		return GraphReport{}, fmt.Errorf("query graph report stats: %w", err)
	}

	repoMap, err := store.GlobalRepoMap(ctx, RepoMapQuery{
		Root:           query.Root,
		Path:           query.Path,
		Limit:          graphReportLimit(query),
		SymbolsPerFile: graphReportSymbolsPerFileFor(query),
	})
	if err != nil {
		return GraphReport{}, fmt.Errorf("query graph report repo map: %w", err)
	}

	report := GraphReport{
		Kind:             graphReportKind,
		Root:             query.Root,
		Path:             query.Path,
		Stats:            stats,
		RepoMap:          repoMap,
		CentralFiles:     graphReportFiles(repoMap.Files),
		SuggestedActions: graphReportSuggestedActions(),
		Warnings:         graphReportWarnings(stats, repoMap),
	}

	health, found, err := store.StoredCodeHealth(ctx, CodeHealthQuery{
		Root:    query.Root,
		Path:    query.Path,
		Limit:   graphReportLimit(query),
		Trend:   graphReportHealthTrend,
		Refresh: false,
	})
	if err != nil {
		return GraphReport{}, fmt.Errorf("query stored graph report health: %w", err)
	}

	if found {
		report.HealthTargets = health.Targets
	} else {
		report.Warnings = append(
			report.Warnings,
			"code health snapshot unavailable; run code-intel health --refresh",
		)
	}

	return report, nil
}

func graphReportLimit(query GraphReportQuery) int {
	if query.Limit > 0 {
		return query.Limit
	}

	return defaultGraphReportLimit
}

func graphReportSymbolsPerFileFor(query GraphReportQuery) int {
	if query.SymbolsPerFile > 0 {
		return query.SymbolsPerFile
	}

	return graphReportSymbolsPerFile
}

func graphReportFiles(files []RepoMapFile) []GraphReportFile {
	reportFiles := make([]GraphReportFile, 0, len(files))

	for _, file := range files {
		reportFiles = append(reportFiles, GraphReportFile{
			Path:                file.Path,
			Language:            file.Language,
			PrimaryAuthorEmail:  file.PrimaryAuthorEmail,
			Symbols:             file.Symbols,
			Reasons:             graphReportFileReasons(file),
			SymbolCount:         file.SymbolCount,
			ChunkCount:          file.ChunkCount,
			LineCount:           file.LineCount,
			HiddenCouplingCount: file.HiddenCouplingCount,
			Score:               file.Score,
			HotspotScore:        file.HotspotScore,
		})
	}

	return reportFiles
}

func graphReportFileReasons(file RepoMapFile) []string {
	reasons := make([]string, 0, graphReportReasonCapacity)

	if file.Score > 0 {
		reasons = append(reasons, fmt.Sprintf("repo-map score %d", file.Score))
	}

	if file.SymbolCount > 0 {
		reasons = append(
			reasons,
			fmt.Sprintf("%d indexed symbol(s)", file.SymbolCount),
		)
	}

	if file.ChunkCount > 0 {
		reasons = append(
			reasons,
			fmt.Sprintf("%d indexed chunk(s)", file.ChunkCount),
		)
	}

	if file.HotspotScore > 0 {
		reasons = append(
			reasons,
			fmt.Sprintf("git hotspot "+graphReportHotspotFormat, file.HotspotScore),
		)
	}

	if file.HiddenCouplingCount > 0 {
		reasons = append(
			reasons,
			fmt.Sprintf("%d hidden coupling(s)", file.HiddenCouplingCount),
		)
	}

	return reasons
}

func graphReportWarnings(stats Stats, repoMap RepoMap) []string {
	warnings := make([]string, 0, graphReportWarnCapacity)

	if stats.Files == 0 || stats.CodeChunks == 0 {
		warnings = append(
			warnings,
			"code index is empty; run code-intel index-code or rebuild-index",
		)
	}

	if len(repoMap.Files) == 0 {
		warnings = append(warnings, "repo map has no ranked files")
	}

	return warnings
}

func graphReportSuggestedActions() []string {
	return []string{
		"Open a context card for the highest-ranked path before editing.",
		"Run change-risk analysis for the target paths before broad changes.",
		"Refresh code health when health targets are missing or stale.",
	}
}
