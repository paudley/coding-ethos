// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

const (
	defaultCentralNodeLimit   = 20
	analysisPathFilterArgSets = 2
	structuralDegreeWeight    = 4
	coChangeDegreeWeight      = 2
	hiddenCouplingWeight      = 8
	healthPriorityWeight      = 2
	repeatedOutcomeWeight     = 6
	findingSignalWeight       = 3
	crossDirectoryWeight      = 5
	crossLanguageWeight       = 6
	policyCodeWeight          = 8
	docCodeWeight             = 4
	testProductionWeight      = 7
)

// CentralNodeQuery controls central-node scope and result density.
type CentralNodeQuery struct {
	Path  string
	Root  string
	Limit int
}

// CentralNode ranks one indexed file by deterministic graph and quality signals.
type CentralNode struct {
	Path              string              `json:"path"`
	Language          string              `json:"language,omitempty"`
	ProvenanceClasses []string            `json:"provenance_classes,omitempty"`
	Signals           []CentralNodeSignal `json:"signals,omitempty"`
	Degree            int                 `json:"degree"`
	Score             int                 `json:"score"`
}

// CentralNodeSignal explains one contribution to a central-node score.
type CentralNodeSignal struct {
	Kind            string `json:"kind"`
	Message         string `json:"message"`
	EvidenceID      string `json:"evidence_id,omitempty"`
	ProvenanceClass string `json:"provenance_class,omitempty"`
	Score           int    `json:"score"`
}

// SurpriseEdgeQuery controls surprise-edge scope and result density.
type SurpriseEdgeQuery struct {
	Path  string
	Root  string
	Limit int
}

// SurpriseEdge highlights one deterministic cross-boundary relationship.
type SurpriseEdge struct {
	Kind            string               `json:"kind"`
	SourcePath      string               `json:"source_path"`
	TargetPath      string               `json:"target_path"`
	ProvenanceClass string               `json:"provenance_class,omitempty"`
	Reasons         []SurpriseEdgeReason `json:"reasons,omitempty"`
	CoChangeCount   int                  `json:"cochange_count,omitempty"`
	HiddenCoupling  bool                 `json:"hidden_coupling,omitempty"`
	Score           int                  `json:"score"`
}

// SurpriseEdgeReason explains why an edge is surprising.
type SurpriseEdgeReason struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Score   int    `json:"score"`
}

type centralNodeAccumulator struct {
	signals []CentralNodeSignal
	file    RepoMapFile
	degree  int
	score   int
}

type analysisFile struct {
	path     string
	language string
}

// CentralNodes ranks indexed files by explainable graph and quality signals.
func (store *Store) CentralNodes(
	ctx context.Context,
	query CentralNodeQuery,
) ([]CentralNode, error) {
	query.Root = strings.TrimSpace(query.Root)
	query.Path = strings.TrimSpace(query.Path)

	files, err := store.repoMapFiles(ctx, RepoMapQuery{
		Root:  query.Root,
		Path:  query.Path,
		Limit: centralNodeLimit(query),
	})
	if err != nil {
		return nil, fmt.Errorf("query central-node candidate files: %w", err)
	}

	if len(files) == 0 {
		return nil, nil
	}

	nodes := centralNodeAccumulators(files)

	err = store.addStructuralCentralitySignals(ctx, nodes)
	if err != nil {
		return nil, err
	}

	err = store.addCoChangeCentralitySignals(ctx, nodes)
	if err != nil {
		return nil, err
	}

	err = store.addHealthCentralitySignals(ctx, query, nodes)
	if err != nil {
		return nil, err
	}

	err = store.addFindingCentralitySignals(ctx, nodes)
	if err != nil {
		return nil, err
	}

	err = store.addRemediationCentralitySignals(ctx, nodes)
	if err != nil {
		return nil, err
	}

	results := make([]CentralNode, 0, len(nodes))
	for _, node := range nodes {
		results = append(results, node.toCentralNode())
	}

	sort.SliceStable(results, func(left, right int) bool {
		if results[left].Score != results[right].Score {
			return results[left].Score > results[right].Score
		}

		if results[left].Degree != results[right].Degree {
			return results[left].Degree > results[right].Degree
		}

		return results[left].Path < results[right].Path
	})

	return boundedCentralNodes(results, centralNodeLimit(query)), nil
}

// SurpriseEdges ranks deterministic cross-boundary relationships over indexed files.
func (store *Store) SurpriseEdges(
	ctx context.Context,
	query SurpriseEdgeQuery,
) ([]SurpriseEdge, error) {
	query.Root = strings.TrimSpace(query.Root)
	query.Path = strings.TrimSpace(query.Path)

	files, err := store.repoMapFiles(ctx, RepoMapQuery{
		Root:  query.Root,
		Path:  query.Path,
		Limit: surpriseEdgeLimit(query),
	})
	if err != nil {
		return nil, fmt.Errorf("query surprise-edge candidate files: %w", err)
	}

	if len(files) == 0 {
		return nil, nil
	}

	fileByPath := analysisFilesByPath(files)

	structural, err := store.structuralSurpriseEdges(ctx, fileByPath)
	if err != nil {
		return nil, err
	}

	cochanges, err := store.coChangeSurpriseEdges(ctx, fileByPath)
	if err != nil {
		return nil, err
	}

	results := mergeSurpriseEdges(structural, cochanges)
	sort.SliceStable(results, func(left, right int) bool {
		if results[left].Score != results[right].Score {
			return results[left].Score > results[right].Score
		}

		if results[left].SourcePath != results[right].SourcePath {
			return results[left].SourcePath < results[right].SourcePath
		}

		if results[left].TargetPath != results[right].TargetPath {
			return results[left].TargetPath < results[right].TargetPath
		}

		return results[left].Kind < results[right].Kind
	})

	return boundedSurpriseEdges(results, surpriseEdgeLimit(query)), nil
}

func centralNodeLimit(query CentralNodeQuery) int {
	if query.Limit > 0 {
		return query.Limit
	}

	return defaultCentralNodeLimit
}

func surpriseEdgeLimit(query SurpriseEdgeQuery) int {
	if query.Limit > 0 {
		return query.Limit
	}

	return defaultCentralNodeLimit
}

func centralNodeAccumulators(files []RepoMapFile) map[string]*centralNodeAccumulator {
	nodes := make(map[string]*centralNodeAccumulator, len(files))
	for _, file := range files {
		node := &centralNodeAccumulator{file: file}
		node.addSignal(CentralNodeSignal{
			Kind:            "repo_map",
			Message:         fmt.Sprintf("repo-map score %d", file.Score),
			Score:           file.Score,
			ProvenanceClass: ProvenanceExtracted,
		})
		nodes[file.Path] = node
	}

	return nodes
}

func (node *centralNodeAccumulator) addSignal(signal CentralNodeSignal) {
	if signal.Score <= 0 {
		return
	}

	node.score += signal.Score
	node.signals = append(node.signals, signal)
}

func (node *centralNodeAccumulator) toCentralNode() CentralNode {
	sort.SliceStable(node.signals, func(left, right int) bool {
		if node.signals[left].Score != node.signals[right].Score {
			return node.signals[left].Score > node.signals[right].Score
		}

		return node.signals[left].Kind < node.signals[right].Kind
	})

	return CentralNode{
		Path:              node.file.Path,
		Language:          node.file.Language,
		ProvenanceClasses: node.file.ProvenanceClasses,
		Signals:           slices.Clone(node.signals),
		Degree:            node.degree,
		Score:             node.score,
	}
}

func (store *Store) addStructuralCentralitySignals(
	ctx context.Context,
	nodes map[string]*centralNodeAccumulator,
) error {
	rows, err := queryAnalysisEdges(
		ctx,
		store.database,
		nodes,
		`SELECT edge_id, edge_kind, path, target_path, COALESCE(provenance_class, '')
			FROM code_edges
			WHERE path IN (%s) AND target_path IN (%s)`,
	)
	if err != nil {
		return fmt.Errorf("query structural central-node edges: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var edgeID, kind, sourcePath, targetPath, provenanceClass string

		err = rows.Scan(&edgeID, &kind, &sourcePath, &targetPath, &provenanceClass)
		if err != nil {
			return fmt.Errorf("scan structural central-node edge: %w", err)
		}

		if nodes[sourcePath] == nil || nodes[targetPath] == nil {
			continue
		}

		score := structuralDegreeWeight
		message := fmt.Sprintf("%s edge to %s", strings.TrimSpace(kind), targetPath)
		nodes[sourcePath].degree++
		nodes[sourcePath].addSignal(CentralNodeSignal{
			Kind:            "structural_degree",
			Message:         message,
			EvidenceID:      edgeID,
			Score:           score,
			ProvenanceClass: normalizeProvenanceClass(provenanceClass),
		})
		nodes[targetPath].degree++
		nodes[targetPath].addSignal(CentralNodeSignal{
			Kind:            "incoming_structural_degree",
			Message:         centralNodeIncomingEdgeMessage(kind, sourcePath),
			EvidenceID:      edgeID,
			Score:           score,
			ProvenanceClass: normalizeProvenanceClass(provenanceClass),
		})
	}

	err = rows.Err()
	if err != nil {
		return fmt.Errorf("iterate structural central-node edges: %w", err)
	}

	return nil
}

func (store *Store) addCoChangeCentralitySignals(
	ctx context.Context,
	nodes map[string]*centralNodeAccumulator,
) error {
	rows, err := queryAnalysisEdges(
		ctx,
		store.database,
		nodes,
		`SELECT path, related_path, cochange_count, hidden_coupling
			FROM git_cochanges
			WHERE path IN (%s) AND related_path IN (%s)`,
	)
	if err != nil {
		return fmt.Errorf("query co-change central-node edges: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			sourcePath, targetPath        string
			coChangeCount, hiddenCoupling int
		)

		err = rows.Scan(&sourcePath, &targetPath, &coChangeCount, &hiddenCoupling)
		if err != nil {
			return fmt.Errorf("scan co-change central-node edge: %w", err)
		}

		if nodes[sourcePath] == nil || nodes[targetPath] == nil {
			continue
		}

		score := coChangeCount * coChangeDegreeWeight
		if hiddenCoupling != 0 {
			score += hiddenCouplingWeight
		}

		message := fmt.Sprintf("co-changed with %s %d time(s)", targetPath, coChangeCount)
		nodes[sourcePath].degree++
		nodes[sourcePath].addSignal(CentralNodeSignal{
			Kind:            "git_cochange",
			Message:         message,
			Score:           score,
			ProvenanceClass: ProvenanceGitDerived,
		})
	}

	err = rows.Err()
	if err != nil {
		return fmt.Errorf("iterate co-change central-node edges: %w", err)
	}

	return nil
}

func (store *Store) addHealthCentralitySignals(
	ctx context.Context,
	query CentralNodeQuery,
	nodes map[string]*centralNodeAccumulator,
) error {
	health, found, err := store.StoredCodeHealth(ctx, CodeHealthQuery{
		Root:    query.Root,
		Path:    query.Path,
		Limit:   len(nodes),
		Refresh: false,
	})
	if err != nil {
		return fmt.Errorf("query central-node stored health: %w", err)
	}

	if !found {
		return nil
	}

	for _, target := range health.Targets {
		node := nodes[target.Path]
		if node == nil {
			continue
		}

		node.addSignal(CentralNodeSignal{
			Kind:            "health_priority",
			Message:         fmt.Sprintf("health priority %.1f", target.PriorityScore),
			Score:           int(target.PriorityScore * healthPriorityWeight),
			ProvenanceClass: ProvenancePolicyDerived,
		})
	}

	return nil
}

func (store *Store) addFindingCentralitySignals(
	ctx context.Context,
	nodes map[string]*centralNodeAccumulator,
) error {
	rows, err := querySinglePathFacts(
		ctx,
		store.database,
		nodes,
		`SELECT COALESCE(fo.path, f.path, ''), COUNT(*)
			FROM findings f
			LEFT JOIN finding_occurrences fo ON fo.finding_id = f.finding_id
			WHERE COALESCE(fo.path, f.path, '') IN (%s)
			GROUP BY COALESCE(fo.path, f.path, '')`,
	)
	if err != nil {
		return fmt.Errorf("query central-node finding signals: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			path  string
			count int
		)

		err = rows.Scan(&path, &count)
		if err != nil {
			return fmt.Errorf("scan central-node finding signal: %w", err)
		}

		if nodes[path] == nil {
			continue
		}

		nodes[path].addSignal(CentralNodeSignal{
			Kind:            "finding_count",
			Message:         fmt.Sprintf("%d finding occurrence(s)", count),
			Score:           count * findingSignalWeight,
			ProvenanceClass: ProvenanceTraceDerived,
		})
	}

	err = rows.Err()
	if err != nil {
		return fmt.Errorf("iterate central-node finding signals: %w", err)
	}

	return nil
}

func (store *Store) addRemediationCentralitySignals(
	ctx context.Context,
	nodes map[string]*centralNodeAccumulator,
) error {
	rows, err := querySinglePathFacts(
		ctx,
		store.database,
		nodes,
		`SELECT path, COUNT(*)
			FROM remediation_outcomes
			WHERE outcome = 'repeated' AND path IN (%s)
			GROUP BY path`,
	)
	if err != nil {
		return fmt.Errorf("query central-node remediation signals: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			path  string
			count int
		)

		err = rows.Scan(&path, &count)
		if err != nil {
			return fmt.Errorf("scan central-node remediation signal: %w", err)
		}

		if nodes[path] == nil {
			continue
		}

		nodes[path].addSignal(CentralNodeSignal{
			Kind:            "repeated_remediation",
			Message:         fmt.Sprintf("%d repeated remediation outcome(s)", count),
			Score:           count * repeatedOutcomeWeight,
			ProvenanceClass: ProvenanceTraceDerived,
		})
	}

	err = rows.Err()
	if err != nil {
		return fmt.Errorf("iterate central-node remediation signals: %w", err)
	}

	return nil
}

func (store *Store) structuralSurpriseEdges(
	ctx context.Context,
	files map[string]analysisFile,
) ([]SurpriseEdge, error) {
	rows, err := queryAnalysisFiles(
		ctx,
		store.database,
		files,
		`SELECT edge_kind, path, target_path, COALESCE(provenance_class, '')
			FROM code_edges
			WHERE path IN (%s) AND target_path IN (%s)
			ORDER BY path, target_path, edge_kind`,
	)
	if err != nil {
		return nil, fmt.Errorf("query structural surprise edges: %w", err)
	}
	defer rows.Close()

	edges := []SurpriseEdge{}

	for rows.Next() {
		var kind, sourcePath, targetPath, provenanceClass string

		err = rows.Scan(&kind, &sourcePath, &targetPath, &provenanceClass)
		if err != nil {
			return nil, fmt.Errorf("scan structural surprise edge: %w", err)
		}

		edge := surpriseEdgeFor(
			files[sourcePath],
			files[targetPath],
			kind,
			normalizeProvenanceClass(provenanceClass),
			0,
			false,
		)
		if edge.Score > 0 {
			edges = append(edges, edge)
		}
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate structural surprise edges: %w", err)
	}

	return edges, nil
}

func (store *Store) coChangeSurpriseEdges(
	ctx context.Context,
	files map[string]analysisFile,
) ([]SurpriseEdge, error) {
	rows, err := queryAnalysisFiles(
		ctx,
		store.database,
		files,
		`SELECT path, related_path, cochange_count, hidden_coupling
			FROM git_cochanges
			WHERE path IN (%s) AND related_path IN (%s)
			ORDER BY path, related_path`,
	)
	if err != nil {
		return nil, fmt.Errorf("query co-change surprise edges: %w", err)
	}
	defer rows.Close()

	edges := []SurpriseEdge{}

	for rows.Next() {
		var (
			sourcePath, targetPath        string
			coChangeCount, hiddenCoupling int
		)

		err = rows.Scan(&sourcePath, &targetPath, &coChangeCount, &hiddenCoupling)
		if err != nil {
			return nil, fmt.Errorf("scan co-change surprise edge: %w", err)
		}

		edge := surpriseEdgeFor(
			files[sourcePath],
			files[targetPath],
			"cochange",
			ProvenanceGitDerived,
			coChangeCount,
			hiddenCoupling != 0,
		)
		if edge.Score > 0 {
			edges = append(edges, edge)
		}
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate co-change surprise edges: %w", err)
	}

	return edges, nil
}

func surpriseEdgeFor(
	source analysisFile,
	target analysisFile,
	kind string,
	provenanceClass string,
	coChangeCount int,
	hiddenCoupling bool,
) SurpriseEdge {
	reasons := surpriseEdgeReasons(source, target, kind, hiddenCoupling)

	score := 0
	for _, reason := range reasons {
		score += reason.Score
	}

	if coChangeCount > 0 {
		score += coChangeCount
	}

	return SurpriseEdge{
		Kind:            strings.TrimSpace(kind),
		SourcePath:      source.path,
		TargetPath:      target.path,
		ProvenanceClass: provenanceClass,
		Reasons:         reasons,
		CoChangeCount:   coChangeCount,
		HiddenCoupling:  hiddenCoupling,
		Score:           score,
	}
}

func surpriseEdgeReasons(
	source analysisFile,
	target analysisFile,
	kind string,
	hiddenCoupling bool,
) []SurpriseEdgeReason {
	reasons := []SurpriseEdgeReason{}
	if topDirectory(source.path) != topDirectory(target.path) {
		reasons = append(reasons, SurpriseEdgeReason{
			Kind: "cross_directory",
			Message: fmt.Sprintf(
				"%s crosses into %s",
				topDirectory(source.path),
				topDirectory(target.path),
			),
			Score: crossDirectoryWeight,
		})
	}

	if source.language != "" && target.language != "" &&
		source.language != target.language {
		reasons = append(reasons, SurpriseEdgeReason{
			Kind:    "cross_language",
			Message: fmt.Sprintf("%s links to %s", source.language, target.language),
			Score:   crossLanguageWeight,
		})
	}

	if hiddenCoupling {
		reasons = append(reasons, SurpriseEdgeReason{
			Kind:    "hidden_cochange",
			Message: "git history marks this as hidden coupling",
			Score:   hiddenCouplingWeight,
		})
	}

	if isPolicyPath(source.path) != isPolicyPath(target.path) {
		reasons = append(reasons, SurpriseEdgeReason{
			Kind:    "policy_to_code",
			Message: "policy/config path links to implementation path",
			Score:   policyCodeWeight,
		})
	}

	if strings.TrimSpace(kind) == "documents" ||
		isDocPath(source.path) != isDocPath(target.path) {
		reasons = append(reasons, SurpriseEdgeReason{
			Kind:    "doc_to_code",
			Message: "documentation path links to code/config path",
			Score:   docCodeWeight,
		})
	}

	if isSurpriseTestPath(source.path) != isSurpriseTestPath(target.path) {
		reasons = append(reasons, SurpriseEdgeReason{
			Kind:    "test_to_production",
			Message: "test path links to production path",
			Score:   testProductionWeight,
		})
	}

	return reasons
}

func mergeSurpriseEdges(edges ...[]SurpriseEdge) []SurpriseEdge {
	merged := map[string]SurpriseEdge{}

	for _, group := range edges {
		for _, edge := range group {
			key := edge.SourcePath + "\x00" + edge.TargetPath + "\x00" + edge.Kind

			existing, ok := merged[key]
			if ok && existing.Score >= edge.Score {
				continue
			}

			merged[key] = edge
		}
	}

	results := make([]SurpriseEdge, 0, len(merged))
	for _, edge := range merged {
		results = append(results, edge)
	}

	return results
}

func analysisFilesByPath(files []RepoMapFile) map[string]analysisFile {
	result := make(map[string]analysisFile, len(files))
	for _, file := range files {
		result[file.Path] = analysisFile{path: file.Path, language: file.Language}
	}

	return result
}

func boundedCentralNodes(nodes []CentralNode, limit int) []CentralNode {
	if len(nodes) <= limit {
		return nodes
	}

	return slices.Clone(nodes[:limit])
}

func boundedSurpriseEdges(edges []SurpriseEdge, limit int) []SurpriseEdge {
	if len(edges) <= limit {
		return edges
	}

	return slices.Clone(edges[:limit])
}

func queryAnalysisEdges(
	ctx context.Context,
	database *sql.DB,
	nodes map[string]*centralNodeAccumulator,
	template string,
) (*sql.Rows, error) {
	paths := make([]string, 0, len(nodes))
	for path := range nodes {
		paths = append(paths, path)
	}

	return queryAnalysisPaths(ctx, database, paths, template)
}

func queryAnalysisFiles(
	ctx context.Context,
	database *sql.DB,
	files map[string]analysisFile,
	template string,
) (*sql.Rows, error) {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}

	return queryAnalysisPaths(ctx, database, paths, template)
}

func queryAnalysisPaths(
	ctx context.Context,
	database *sql.DB,
	paths []string,
	template string,
) (*sql.Rows, error) {
	inList, args := analysisPathFilter(paths)
	// #nosec G201 -- only generated placeholders are formatted; values stay bound.
	query := fmt.Sprintf(template, inList, inList)

	rows, err := database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query analysis paths: %w", err)
	}

	return rows, nil
}

func querySinglePathFacts(
	ctx context.Context,
	database *sql.DB,
	nodes map[string]*centralNodeAccumulator,
	template string,
) (*sql.Rows, error) {
	paths := make([]string, 0, len(nodes))
	for path := range nodes {
		paths = append(paths, path)
	}

	inList, args := singleAnalysisPathFilter(paths)
	// #nosec G201 -- only generated placeholders are formatted; values stay bound.
	query := fmt.Sprintf(template, inList)

	rows, err := database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query single-path facts: %w", err)
	}

	return rows, nil
}

func centralNodeIncomingEdgeMessage(kind, sourcePath string) string {
	return fmt.Sprintf("%s edge from %s", strings.TrimSpace(kind), sourcePath)
}

func analysisPathFilter(paths []string) (string, []any) {
	slices.Sort(paths)
	placeholders := make([]string, 0, len(paths))

	args := make([]any, 0, len(paths)*analysisPathFilterArgSets)
	for _, path := range paths {
		placeholders = append(placeholders, "?")
		args = append(args, path)
	}

	for _, path := range paths {
		args = append(args, path)
	}

	return strings.Join(placeholders, ","), args
}

func singleAnalysisPathFilter(paths []string) (string, []any) {
	slices.Sort(paths)
	placeholders := make([]string, 0, len(paths))

	args := make([]any, 0, len(paths))
	for _, path := range paths {
		placeholders = append(placeholders, "?")
		args = append(args, path)
	}

	return strings.Join(placeholders, ","), args
}

func topDirectory(path string) string {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" {
		return ""
	}

	before, _, ok := strings.Cut(path, "/")
	if ok {
		return before
	}

	return path
}

func isPolicyPath(path string) bool {
	path = strings.ToLower(filepath.ToSlash(path))

	return strings.Contains(path, "policy") ||
		strings.HasSuffix(path, ".yml") ||
		strings.HasSuffix(path, ".yaml") ||
		strings.HasSuffix(path, ".toml")
}

func isDocPath(path string) bool {
	path = strings.ToLower(filepath.ToSlash(path))

	return strings.HasPrefix(path, "docs/") ||
		strings.Contains(path, "/docs/") ||
		strings.HasSuffix(path, ".md")
}

func isSurpriseTestPath(path string) bool {
	path = strings.ToLower(filepath.ToSlash(path))
	base := filepath.Base(path)

	return strings.Contains(path, "_test.") ||
		strings.HasPrefix(base, "test_") ||
		strings.HasPrefix(path, "test/") ||
		strings.HasPrefix(path, "tests/") ||
		strings.Contains(path, "/test/") ||
		strings.Contains(path, "/tests/")
}
