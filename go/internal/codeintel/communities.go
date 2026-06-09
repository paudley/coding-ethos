// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"sort"
	"strings"
)

const (
	defaultCodeCommunityLimit        = 20
	codeCommunityRepresentativeLimit = 3
	codeCommunityCentralMemberLimit  = 5
	codeCommunityEvidenceLimit       = 5
	codeCommunityStructuralWeight    = 3
	codeCommunityCoChangeMaxWeight   = 10
	codeCommunityHiddenWeight        = 2
	codeCommunityPathFilterArgSets   = 2
)

// CodeCommunityQuery controls topology community scope and result density.
type CodeCommunityQuery struct {
	Path  string
	Root  string
	Limit int
}

// CodeCommunity summarizes one deterministic topology neighborhood.
type CodeCommunity struct {
	ID                  string                  `json:"id"`
	MemberPaths         []string                `json:"member_paths,omitempty"`
	RepresentativePaths []string                `json:"representative_paths,omitempty"`
	CentralMembers      []CodeCommunityMember   `json:"central_members,omitempty"`
	Evidence            []CodeCommunityEvidence `json:"evidence,omitempty"`
	ProvenanceClasses   []string                `json:"provenance_classes,omitempty"`
	MemberCount         int                     `json:"member_count"`
	Score               int                     `json:"score"`
}

// CodeCommunityMember describes one file member in a topology community.
type CodeCommunityMember struct {
	Path           string `json:"path"`
	Language       string `json:"language,omitempty"`
	WeightedDegree int    `json:"weighted_degree"`
	Score          int    `json:"score"`
}

// CodeCommunityEvidence records a graph edge that supports a community.
type CodeCommunityEvidence struct {
	Kind            string `json:"kind"`
	SourcePath      string `json:"source_path"`
	TargetPath      string `json:"target_path"`
	ProvenanceClass string `json:"provenance_class,omitempty"`
	Weight          int    `json:"weight"`
	HiddenCoupling  bool   `json:"hidden_coupling,omitempty"`
	CoChangeCount   int    `json:"cochange_count,omitempty"`
}

// CodeCommunities derives deterministic file-level graph communities from
// indexed structural edges and git co-change evidence.
func (store *Store) CodeCommunities(
	ctx context.Context,
	query CodeCommunityQuery,
) ([]CodeCommunity, error) {
	query.Root = strings.TrimSpace(query.Root)
	query.Path = strings.TrimSpace(query.Path)

	files, err := store.repoMapFiles(ctx, RepoMapQuery{
		Root:  query.Root,
		Path:  query.Path,
		Limit: codeCommunityLimit(query),
	})
	if err != nil {
		return nil, fmt.Errorf("query community candidate files: %w", err)
	}

	if len(files) == 0 {
		return nil, nil
	}

	return codeCommunitiesFromFiles(ctx, store.database, query, files)
}

func (store *Store) codeCommunityIDsByPath(
	ctx context.Context,
	query CodeCommunityQuery,
	files []RepoMapFile,
) (map[string]string, error) {
	communities, err := codeCommunitiesFromFiles(ctx, store.database, query, files)
	if err != nil {
		return nil, err
	}

	return codeCommunityIDsByPath(communities), nil
}

func (store *DuckDBStore) codeCommunityIDsByPath(
	ctx context.Context,
	query CodeCommunityQuery,
	files []RepoMapFile,
) (map[string]string, error) {
	communities, err := codeCommunitiesFromFiles(ctx, store.database, query, files)
	if err != nil {
		return nil, err
	}

	return codeCommunityIDsByPath(communities), nil
}

type codeCommunityNode struct {
	file           RepoMapFile
	weightedDegree int
}

type codeCommunityEdge struct {
	left     string
	right    string
	evidence CodeCommunityEvidence
}

func newCodeCommunityNodes(files []RepoMapFile) map[string]*codeCommunityNode {
	nodes := make(map[string]*codeCommunityNode, len(files))
	for _, file := range files {
		nodes[file.Path] = &codeCommunityNode{file: file}
	}

	return nodes
}

func codeCommunityIDsByPath(communities []CodeCommunity) map[string]string {
	ids := map[string]string{}

	for _, community := range communities {
		for _, path := range community.MemberPaths {
			ids[path] = community.ID
		}
	}

	return ids
}

func codeCommunitiesFromFiles(
	ctx context.Context,
	database *sql.DB,
	query CodeCommunityQuery,
	files []RepoMapFile,
) ([]CodeCommunity, error) {
	if len(files) == 0 {
		return nil, nil
	}

	nodes := newCodeCommunityNodes(files)

	edges, err := codeCommunityEdges(ctx, database, nodes)
	if err != nil {
		return nil, err
	}

	return buildCodeCommunities(nodes, edges, codeCommunityLimit(query)), nil
}

func codeCommunityEdges(
	ctx context.Context,
	database *sql.DB,
	nodes map[string]*codeCommunityNode,
) ([]codeCommunityEdge, error) {
	structural, err := structuralCommunityEdges(ctx, database, nodes)
	if err != nil {
		return nil, err
	}

	cochanges, err := coChangeCommunityEdges(ctx, database, nodes)
	if err != nil {
		return nil, err
	}

	edges := make([]codeCommunityEdge, 0, len(structural)+len(cochanges))
	edges = append(edges, structural...)
	edges = append(edges, cochanges...)

	for _, edge := range edges {
		nodes[edge.left].weightedDegree += edge.evidence.Weight
		nodes[edge.right].weightedDegree += edge.evidence.Weight
	}

	return edges, nil
}

func structuralCommunityEdges(
	ctx context.Context,
	database *sql.DB,
	nodes map[string]*codeCommunityNode,
) ([]codeCommunityEdge, error) {
	if len(nodes) == 0 {
		return nil, nil
	}

	inList, args := codeCommunityPathFilter(nodes)
	// #nosec G201 -- only placeholder counts are formatted; paths remain bound parameters.
	query := fmt.Sprintf(
		`SELECT edge_kind, path, target_path, COALESCE(provenance_class, '')
			FROM code_edges
			WHERE path IN (%s) AND target_path IN (%s)
			ORDER BY path, target_path, edge_kind`,
		inList,
		inList,
	)

	rows, err := database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query structural community edges: %w", err)
	}
	defer rows.Close()

	seen := map[string]bool{}
	edges := []codeCommunityEdge{}

	for rows.Next() {
		var kind, sourcePath, targetPath, provenanceClass string

		err = rows.Scan(&kind, &sourcePath, &targetPath, &provenanceClass)
		if err != nil {
			return nil, fmt.Errorf("scan structural community edge: %w", err)
		}

		edge, valid := newCodeCommunityEdge(
			nodes,
			kind,
			sourcePath,
			targetPath,
			normalizeProvenanceClass(provenanceClass),
			codeCommunityStructuralWeight,
		)
		if !valid {
			continue
		}

		key := edge.left + "\x00" + edge.right + "\x00" + edge.evidence.Kind
		if seen[key] {
			continue
		}

		seen[key] = true

		edges = append(edges, edge)
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate structural community edges: %w", err)
	}

	return edges, nil
}

func coChangeCommunityEdges(
	ctx context.Context,
	database *sql.DB,
	nodes map[string]*codeCommunityNode,
) ([]codeCommunityEdge, error) {
	if len(nodes) == 0 {
		return nil, nil
	}

	inList, args := codeCommunityPathFilter(nodes)
	// #nosec G201 -- only placeholder counts are formatted; paths remain bound parameters.
	query := fmt.Sprintf(
		`SELECT path, related_path, cochange_count, hidden_coupling
			FROM git_cochanges
			WHERE path IN (%s) AND related_path IN (%s)
			ORDER BY path, related_path`,
		inList,
		inList,
	)

	rows, err := database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query co-change community edges: %w", err)
	}
	defer rows.Close()

	seen := map[string]bool{}
	edges := []codeCommunityEdge{}

	for rows.Next() {
		var (
			sourcePath, targetPath string
			coChangeCount          int
			hiddenCoupling         int
		)

		err = rows.Scan(&sourcePath, &targetPath, &coChangeCount, &hiddenCoupling)
		if err != nil {
			return nil, fmt.Errorf("scan co-change community edge: %w", err)
		}

		weight := min(max(coChangeCount, 1), codeCommunityCoChangeMaxWeight)
		hidden := hiddenCoupling != 0

		if hidden {
			weight += codeCommunityHiddenWeight
		}

		edge, valid := newCodeCommunityEdge(
			nodes,
			"cochange",
			sourcePath,
			targetPath,
			ProvenanceGitDerived,
			weight,
		)
		if !valid {
			continue
		}

		key := edge.left + "\x00" + edge.right + "\x00" + edge.evidence.Kind
		if seen[key] {
			continue
		}

		seen[key] = true

		edge.evidence.HiddenCoupling = hidden
		edge.evidence.CoChangeCount = coChangeCount
		edges = append(edges, edge)
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate co-change community edges: %w", err)
	}

	return edges, nil
}

func codeCommunityPathFilter(nodes map[string]*codeCommunityNode) (string, []any) {
	paths := make([]string, 0, len(nodes))
	for path := range nodes {
		paths = append(paths, path)
	}

	slices.Sort(paths)

	placeholders := make([]string, 0, len(paths))
	args := make([]any, 0, len(paths)*codeCommunityPathFilterArgSets)

	for _, path := range paths {
		placeholders = append(placeholders, "?")
		args = append(args, path)
	}

	for _, path := range paths {
		args = append(args, path)
	}

	return strings.Join(placeholders, ","), args
}

func newCodeCommunityEdge(
	nodes map[string]*codeCommunityNode,
	kind string,
	sourcePath string,
	targetPath string,
	provenanceClass string,
	weight int,
) (codeCommunityEdge, bool) {
	sourcePath = strings.TrimSpace(sourcePath)
	targetPath = strings.TrimSpace(targetPath)

	if sourcePath == "" || targetPath == "" || sourcePath == targetPath {
		return codeCommunityEdge{}, false
	}

	if nodes[sourcePath] == nil || nodes[targetPath] == nil {
		return codeCommunityEdge{}, false
	}

	left, right := sourcePath, targetPath
	if right < left {
		left, right = right, left
	}

	return codeCommunityEdge{
		left:  left,
		right: right,
		evidence: CodeCommunityEvidence{
			Kind:            strings.TrimSpace(kind),
			SourcePath:      sourcePath,
			TargetPath:      targetPath,
			ProvenanceClass: normalizeProvenanceClass(provenanceClass),
			Weight:          weight,
		},
	}, true
}

func buildCodeCommunities(
	nodes map[string]*codeCommunityNode,
	edges []codeCommunityEdge,
	limit int,
) []CodeCommunity {
	parent := newCodeCommunityParents(nodes)

	for _, edge := range edges {
		unionCodeCommunity(parent, edge.left, edge.right)
	}

	components := map[string][]string{}

	for path := range nodes {
		root := findCodeCommunityParent(parent, path)
		components[root] = append(components[root], path)
	}

	evidenceByRoot := map[string][]CodeCommunityEvidence{}

	for _, edge := range edges {
		root := findCodeCommunityParent(parent, edge.left)
		evidenceByRoot[root] = append(evidenceByRoot[root], edge.evidence)
	}

	communities := make([]CodeCommunity, 0, len(components))

	for _, paths := range components {
		slices.Sort(paths)

		root := findCodeCommunityParent(parent, paths[0])

		communities = append(
			communities,
			codeCommunityForComponent(nodes, paths, evidenceByRoot[root]),
		)
	}

	sort.SliceStable(communities, func(left, right int) bool {
		if communities[left].Score != communities[right].Score {
			return communities[left].Score > communities[right].Score
		}

		if communities[left].MemberCount != communities[right].MemberCount {
			return communities[left].MemberCount > communities[right].MemberCount
		}

		return communities[left].ID < communities[right].ID
	})

	if len(communities) > limit {
		return communities[:limit]
	}

	return communities
}

func codeCommunityForComponent(
	nodes map[string]*codeCommunityNode,
	paths []string,
	evidence []CodeCommunityEvidence,
) CodeCommunity {
	members := make([]CodeCommunityMember, 0, len(paths))
	score := 0
	provenance := map[string]bool{}

	for _, path := range paths {
		node := nodes[path]
		score += node.file.Score + node.weightedDegree

		for _, class := range node.file.ProvenanceClasses {
			provenance[class] = true
		}

		members = append(members, CodeCommunityMember{
			Path:           path,
			Language:       node.file.Language,
			WeightedDegree: node.weightedDegree,
			Score:          node.file.Score,
		})
	}

	for _, item := range evidence {
		provenance[item.ProvenanceClass] = true
	}

	sort.SliceStable(members, func(left, right int) bool {
		leftScore := members[left].Score + members[left].WeightedDegree
		rightScore := members[right].Score + members[right].WeightedDegree

		if leftScore != rightScore {
			return leftScore > rightScore
		}

		return members[left].Path < members[right].Path
	})

	evidence = rankedCodeCommunityEvidence(evidence)

	return CodeCommunity{
		ID:                  codeCommunityID(paths[0]),
		MemberPaths:         slices.Clone(paths),
		RepresentativePaths: codeCommunityRepresentativePaths(members),
		CentralMembers:      boundedCodeCommunityMembers(members),
		Evidence:            evidence,
		ProvenanceClasses:   sortedCommunityProvenance(provenance),
		MemberCount:         len(paths),
		Score:               score,
	}
}

func codeCommunityID(firstPath string) string {
	replacer := strings.NewReplacer("/", "-", "\\", "-", " ", "-")

	return "community:" + replacer.Replace(strings.TrimSpace(firstPath))
}

func codeCommunityRepresentativePaths(members []CodeCommunityMember) []string {
	limit := min(len(members), codeCommunityRepresentativeLimit)
	paths := make([]string, 0, limit)

	for _, member := range members[:limit] {
		paths = append(paths, member.Path)
	}

	return paths
}

func boundedCodeCommunityMembers(members []CodeCommunityMember) []CodeCommunityMember {
	limit := min(len(members), codeCommunityCentralMemberLimit)

	return slices.Clone(members[:limit])
}

func rankedCodeCommunityEvidence(
	evidence []CodeCommunityEvidence,
) []CodeCommunityEvidence {
	sort.SliceStable(evidence, func(left, right int) bool {
		if evidence[left].Weight != evidence[right].Weight {
			return evidence[left].Weight > evidence[right].Weight
		}

		if evidence[left].SourcePath != evidence[right].SourcePath {
			return evidence[left].SourcePath < evidence[right].SourcePath
		}

		if evidence[left].TargetPath != evidence[right].TargetPath {
			return evidence[left].TargetPath < evidence[right].TargetPath
		}

		return evidence[left].Kind < evidence[right].Kind
	})

	limit := min(len(evidence), codeCommunityEvidenceLimit)

	return slices.Clone(evidence[:limit])
}

func sortedCommunityProvenance(provenance map[string]bool) []string {
	classes := make([]string, 0, len(provenance))

	for class := range provenance {
		classes = append(classes, class)
	}

	slices.Sort(classes)

	return classes
}

func codeCommunityLimit(query CodeCommunityQuery) int {
	if query.Limit > 0 {
		return query.Limit
	}

	return defaultCodeCommunityLimit
}

func newCodeCommunityParents(
	nodes map[string]*codeCommunityNode,
) map[string]string {
	parent := make(map[string]string, len(nodes))

	for path := range nodes {
		parent[path] = path
	}

	return parent
}

func findCodeCommunityParent(parent map[string]string, path string) string {
	root := path

	for parent[root] != root {
		root = parent[root]
	}

	for parent[path] != path {
		next := parent[path]
		parent[path] = root
		path = next
	}

	return root
}

func unionCodeCommunity(parent map[string]string, left, right string) {
	leftRoot := findCodeCommunityParent(parent, left)
	rightRoot := findCodeCommunityParent(parent, right)

	if leftRoot == rightRoot {
		return
	}

	if rightRoot < leftRoot {
		leftRoot, rightRoot = rightRoot, leftRoot
	}

	parent[rightRoot] = leftRoot
}
