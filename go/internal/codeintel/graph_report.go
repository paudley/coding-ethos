// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"database/sql"
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
	CentralNodes     []CentralNode      `json:"central_nodes,omitempty"`
	Communities      []CodeCommunity    `json:"communities,omitempty"`
	DocumentLinks    []DocumentLink     `json:"document_links,omitempty"`
	SurpriseEdges    []SurpriseEdge     `json:"surprise_edges,omitempty"`
	HealthTargets    []CodeHealthTarget `json:"health_targets,omitempty"`
	SuggestedActions []string           `json:"suggested_actions,omitempty"`
	Warnings         []string           `json:"warnings,omitempty"`
	Stats            Stats              `json:"stats"`
}

// DocumentLink describes documentation-derived code graph context.
type DocumentLink struct {
	SourcePath       string `json:"source_path"`
	SourceHeading    string `json:"source_heading,omitempty"`
	TargetPath       string `json:"target_path,omitempty"`
	TargetSymbolPath string `json:"target_symbol_path,omitempty"`
	TargetName       string `json:"target_name,omitempty"`
	Kind             string `json:"kind"`
	ProvenanceClass  string `json:"provenance_class"`
	Evidence         string `json:"evidence,omitempty"`
	StartLine        int    `json:"start_line,omitempty"`
}

// GraphReportFile describes one ranked file in the graph orientation report.
type GraphReportFile struct {
	Path                string          `json:"path"`
	Language            string          `json:"language,omitempty"`
	PrimaryAuthorEmail  string          `json:"primary_author_email,omitempty"`
	ProvenanceClasses   []string        `json:"provenance_classes,omitempty"`
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

	topology, err := store.graphReportTopology(ctx, query)
	if err != nil {
		return GraphReport{}, err
	}

	report := GraphReport{
		Kind:             graphReportKind,
		Root:             query.Root,
		Path:             query.Path,
		Stats:            stats,
		RepoMap:          repoMap,
		CentralFiles:     graphReportFiles(repoMap.Files),
		CentralNodes:     topology.centralNodes,
		Communities:      topology.communities,
		DocumentLinks:    topology.documentLinks,
		SurpriseEdges:    topology.surpriseEdges,
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

type graphReportTopology struct {
	centralNodes  []CentralNode
	communities   []CodeCommunity
	documentLinks []DocumentLink
	surpriseEdges []SurpriseEdge
}

func (store *Store) graphReportTopology(
	ctx context.Context,
	query GraphReportQuery,
) (graphReportTopology, error) {
	communities, err := store.CodeCommunities(ctx, CodeCommunityQuery{
		Root:  query.Root,
		Path:  query.Path,
		Limit: graphReportLimit(query),
	})
	if err != nil {
		return graphReportTopology{}, fmt.Errorf("query graph report communities: %w", err)
	}

	centralNodes, err := store.CentralNodes(ctx, CentralNodeQuery{
		Root:  query.Root,
		Path:  query.Path,
		Limit: graphReportLimit(query),
	})
	if err != nil {
		return graphReportTopology{}, fmt.Errorf("query graph report central nodes: %w", err)
	}

	documentLinks, err := store.DocumentLinks(ctx, query)
	if err != nil {
		return graphReportTopology{}, err
	}

	surpriseEdges, err := store.SurpriseEdges(ctx, SurpriseEdgeQuery{
		Root:  query.Root,
		Path:  query.Path,
		Limit: graphReportLimit(query),
	})
	if err != nil {
		return graphReportTopology{}, fmt.Errorf("query graph report surprise edges: %w", err)
	}

	return graphReportTopology{
		centralNodes:  centralNodes,
		communities:   communities,
		documentLinks: documentLinks,
		surpriseEdges: surpriseEdges,
	}, nil
}

// DocumentLinks returns documentation-derived graph links for graph reports.
func (store *Store) DocumentLinks(
	ctx context.Context,
	query GraphReportQuery,
) ([]DocumentLink, error) {
	return queryDocumentLinks(ctx, store.database, query)
}

func queryDocumentLinks(
	ctx context.Context,
	database *sql.DB,
	query GraphReportQuery,
) ([]DocumentLink, error) {
	pathFilter := repoMapPathFilter(query.Path)

	rows, err := database.QueryContext(
		ctx,
		`SELECT edge.path,
			COALESCE(source.symbol_name, '') AS source_heading,
			COALESCE(edge.target_path, '') AS target_path,
			COALESCE(edge.target_symbol_path, '') AS target_symbol_path,
			COALESCE(edge.target_name, '') AS target_name,
			edge.edge_kind,
			COALESCE(edge.provenance_class, 'EXTRACTED') AS provenance_class,
			COALESCE(edge.raw_text, '') AS evidence,
			COALESCE(source.start_line, 0) AS start_line
		FROM code_edges edge
		LEFT JOIN code_chunks source ON source.chunk_id = edge.source_chunk_id
		WHERE edge.edge_kind IN ('documents', 'mentions', 'rationale_for')
			AND (
				? = '' OR edge.path = ? OR edge.path LIKE ? ESCAPE '\'
				OR edge.target_path = ? OR edge.target_path LIKE ? ESCAPE '\'
			)
		ORDER BY CASE edge.edge_kind
				WHEN 'rationale_for' THEN 0
				WHEN 'documents' THEN 1
				ELSE 2
			END,
			edge.path,
			start_line,
			edge.target_path,
			edge.target_name
		LIMIT ?`,
		pathFilter.Exact,
		pathFilter.Exact,
		pathFilter.PrefixLike,
		pathFilter.Exact,
		pathFilter.PrefixLike,
		graphReportLimit(query),
	)
	if err != nil {
		return nil, fmt.Errorf("query graph report document links: %w", err)
	}
	defer rows.Close()

	links := []DocumentLink{}

	for rows.Next() {
		var link DocumentLink

		err = rows.Scan(
			&link.SourcePath,
			&link.SourceHeading,
			&link.TargetPath,
			&link.TargetSymbolPath,
			&link.TargetName,
			&link.Kind,
			&link.ProvenanceClass,
			&link.Evidence,
			&link.StartLine,
		)
		if err != nil {
			return nil, fmt.Errorf("scan graph report document link: %w", err)
		}

		link.ProvenanceClass = normalizeProvenanceClass(link.ProvenanceClass)
		links = append(links, link)
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate graph report document links: %w", err)
	}

	return links, nil
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
			ProvenanceClasses:   file.ProvenanceClasses,
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
