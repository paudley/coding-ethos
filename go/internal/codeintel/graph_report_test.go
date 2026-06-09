// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/codeintel"
)

func TestGraphReportRanksIndexedFiles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	runCodeIntelGit(t, root, "init")
	writeFile(t, filepath.Join(root, "cmd", "app.go"), []byte(`package main

func main() {
	runApp()
}

func runApp() {}
`))
	writeFile(t, filepath.Join(root, "pkg", "worker.py"), []byte(
		"def helper():\n"+
			"    return \"ok\"\n",
	))
	store := openTestStoreAt(
		t,
		ctx,
		filepath.Join(root, ".coding-ethos", "code-intel.duckdb"),
	)

	_, err := NewASTIndexer(store).IndexPaths(ctx, root, []string{"."})
	if err != nil {
		t.Fatalf("index code: %v", err)
	}

	report, err := store.GraphReport(ctx, GraphReportQuery{
		Root:           root,
		Limit:          5,
		SymbolsPerFile: 2,
	})
	if err != nil {
		t.Fatalf("graph report: %v", err)
	}

	if report.Kind != "code_intel.graph_report.v1" ||
		report.Stats.Files != 2 ||
		len(report.CentralFiles) != 2 ||
		report.CentralFiles[0].Path == "" ||
		!hasProvenanceClass(report.CentralFiles[0].ProvenanceClasses, ProvenanceExtracted) ||
		!strings.Contains(strings.Join(report.CentralFiles[0].Reasons, "; "), "score") {
		t.Fatalf("unexpected graph report:\n%#v", report)
	}
}

func TestGraphReportIncludesMarkdownDocumentLinks(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	runCodeIntelGit(t, root, "init")
	writeFile(t, filepath.Join(root, "cmd", "app.go"), []byte(`package main

func runApp() {}
`))
	writeFile(t, filepath.Join(root, "docs", "architecture.md"), []byte(`# Architecture

The runtime is implemented in cmd/app.go.
The CLI entrypoint is cmd/app.go#runApp.

## Rationale

The entry point is cmd/app.go#runApp because the CLI owns startup wiring.

## Examples

Example snippets may mention cmd/app.go but are still advisory documentation.

## Reason for Startup Boundary

cmd/app.go owns startup initialization.
`))
	store := openTestStoreAt(
		t,
		ctx,
		filepath.Join(root, ".coding-ethos", "code-intel.duckdb"),
	)

	_, err := NewASTIndexer(store).IndexPaths(ctx, root, []string{"."})
	if err != nil {
		t.Fatalf("index code: %v", err)
	}

	report, err := store.GraphReport(ctx, GraphReportQuery{
		Root:  root,
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("graph report: %v", err)
	}

	if !documentLinksContain(
		report.DocumentLinks,
		"documents",
		"docs/architecture.md",
		"",
		"cmd/app.go",
		"",
		ProvenanceDocDerived,
	) {
		t.Fatalf("missing Markdown documents link:\n%#v", report.DocumentLinks)
	}

	if !documentLinksContain(
		report.DocumentLinks,
		"rationale_for",
		"docs/architecture.md",
		"",
		"cmd/app.go",
		"runApp",
		ProvenanceInferred,
	) {
		t.Fatalf("missing Markdown rationale link:\n%#v", report.DocumentLinks)
	}

	if !documentLinksContain(
		report.DocumentLinks,
		"rationale_for",
		"docs/architecture.md",
		"",
		"cmd/app.go",
		"",
		ProvenanceInferred,
	) {
		t.Fatalf("missing Markdown reason-prefix rationale link:\n%#v", report.DocumentLinks)
	}

	if !documentLinksContain(
		report.DocumentLinks,
		"mentions",
		"docs/architecture.md",
		"",
		"cmd/app.go",
		"runApp",
		ProvenanceDocDerived,
	) {
		t.Fatalf("missing Markdown mentions link:\n%#v", report.DocumentLinks)
	}

	if documentLinksContain(
		report.DocumentLinks,
		"rationale_for",
		"docs/architecture.md",
		"Examples",
		"cmd/app.go",
		"",
		ProvenanceInferred,
	) {
		t.Fatalf(
			"example path reference was classified as rationale:\n%#v",
			report.DocumentLinks,
		)
	}
}

func TestCodeCommunitiesDeriveTopologyComponents(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store := openTestStoreAt(
		t,
		ctx,
		filepath.Join(root, ".coding-ethos", "code-intel.duckdb"),
	)

	writeFile(t, filepath.Join(root, "cmd", "app.go"), []byte("package main\n"))
	writeFile(t, filepath.Join(root, "pkg", "worker.go"), []byte("package pkg\n"))
	seedCommunityFile(t, ctx, store, "cmd/app.go", "go", []CodeEdge{{
		ID:              "edge-app-worker",
		Kind:            "calls",
		Path:            "cmd/app.go",
		TargetPath:      "pkg/worker.go",
		ProvenanceClass: ProvenanceExtracted,
	}})
	seedCommunityFile(t, ctx, store, "pkg/worker.go", "go", nil)
	seedCommunityFile(t, ctx, store, "docs/readme.md", "markdown", []CodeEdge{{
		ID:              "edge-docs-policy",
		Kind:            "documents",
		Path:            "docs/readme.md",
		TargetPath:      "policy/rules.md",
		ProvenanceClass: ProvenanceExtracted,
	}})
	seedCommunityFile(t, ctx, store, "policy/rules.md", "markdown", nil)
	seedCommunityCoChange(t, ctx, store, "cmd/app.go", "pkg/worker.go", 4, true)
	seedCommunityCoChange(t, ctx, store, "docs/readme.md", "policy/rules.md", 2, false)

	communities, err := store.CodeCommunities(ctx, CodeCommunityQuery{
		Root:  root,
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("code communities: %v", err)
	}

	if len(communities) != 2 {
		t.Fatalf("communities = %#v, want 2 components", communities)
	}

	appCommunity := codeCommunityContaining(communities, "cmd/app.go")
	if appCommunity == nil ||
		appCommunity.MemberCount != 2 ||
		!hasProvenanceClass(appCommunity.ProvenanceClasses, ProvenanceExtracted) ||
		!hasProvenanceClass(appCommunity.ProvenanceClasses, ProvenanceGitDerived) ||
		!codeCommunityHasEvidence(*appCommunity, "cochange", "cmd/app.go", "pkg/worker.go") {
		t.Fatalf("unexpected app community:\n%#v", appCommunity)
	}

	docsCommunity := codeCommunityContaining(communities, "docs/readme.md")
	if docsCommunity == nil ||
		docsCommunity.MemberCount != 2 ||
		!hasProvenanceClass(docsCommunity.ProvenanceClasses, ProvenanceExtracted) ||
		!hasProvenanceClass(docsCommunity.ProvenanceClasses, ProvenanceGitDerived) ||
		!codeCommunityHasEvidence(
			*docsCommunity,
			"cochange",
			"docs/readme.md",
			"policy/rules.md",
		) {
		t.Fatalf("unexpected docs community:\n%#v", docsCommunity)
	}
}

func TestCodeCommunitiesExcludeRepoMapProtectedPaths(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store := openTestStoreAt(
		t,
		ctx,
		filepath.Join(root, ".coding-ethos", "code-intel.duckdb"),
	)

	writeFile(t, filepath.Join(root, "pkg", "app.go"), []byte("package pkg\n"))
	writeFile(
		t,
		filepath.Join(root, ".agents", "generated.go"),
		[]byte("package agents\n"),
	)

	seedCommunityFile(t, ctx, store, "pkg/app.go", "go", []CodeEdge{{
		ID:              "edge-app-agent",
		Kind:            "calls",
		Path:            "pkg/app.go",
		TargetPath:      ".agents/generated.go",
		ProvenanceClass: ProvenanceExtracted,
	}})
	seedCommunityFile(t, ctx, store, ".agents/generated.go", "go", nil)

	communities, err := store.CodeCommunities(ctx, CodeCommunityQuery{
		Root:  root,
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("code communities: %v", err)
	}

	if len(communities) != 1 ||
		communities[0].MemberCount != 1 ||
		communities[0].CentralMembers[0].Path != "pkg/app.go" {
		t.Fatalf("protected path leaked into communities:\n%#v", communities)
	}
}

func TestCentralNodesRankStructuralAndGitSignals(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store := openTestStoreAt(
		t,
		ctx,
		filepath.Join(root, ".coding-ethos", "code-intel.duckdb"),
	)

	writeFile(t, filepath.Join(root, "cmd", "app.go"), []byte("package main\n"))
	writeFile(t, filepath.Join(root, "pkg", "worker.go"), []byte("package pkg\n"))
	seedCommunityFile(t, ctx, store, "cmd/app.go", "go", []CodeEdge{{
		ID:              "edge-app-worker",
		Kind:            "calls",
		Path:            "cmd/app.go",
		TargetPath:      "pkg/worker.go",
		ProvenanceClass: ProvenanceExtracted,
	}})
	seedCommunityFile(t, ctx, store, "pkg/worker.go", "go", nil)
	seedCommunityCoChange(t, ctx, store, "cmd/app.go", "pkg/worker.go", 4, true)

	nodes, err := store.CentralNodes(ctx, CentralNodeQuery{
		Root:  root,
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("central nodes: %v", err)
	}

	appNode := centralNodeForPath(nodes, "cmd/app.go")
	if appNode == nil ||
		appNode.Score == 0 ||
		appNode.Degree == 0 ||
		!centralNodeHasSignal(*appNode, "git_cochange") ||
		!centralNodeHasSignal(*appNode, "structural_degree") {
		t.Fatalf("unexpected central nodes:\n%#v", nodes)
	}
}

func TestSurpriseEdgesExplainCrossBoundarySignals(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store := openTestStoreAt(
		t,
		ctx,
		filepath.Join(root, ".coding-ethos", "code-intel.duckdb"),
	)

	writeFile(t, filepath.Join(root, "docs", "policy.md"), []byte("# Policy\n"))
	writeFile(t, filepath.Join(root, "pkg", "worker.go"), []byte("package pkg\n"))
	seedCommunityFile(t, ctx, store, "docs/policy.md", "markdown", []CodeEdge{{
		ID:              "edge-doc-worker",
		Kind:            "documents",
		Path:            "docs/policy.md",
		TargetPath:      "pkg/worker.go",
		ProvenanceClass: ProvenanceDocDerived,
	}})
	seedCommunityFile(t, ctx, store, "pkg/worker.go", "go", nil)

	edges, err := store.SurpriseEdges(ctx, SurpriseEdgeQuery{
		Root:  root,
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("surprise edges: %v", err)
	}

	edge := surpriseEdgeForPaths(edges, "docs/policy.md", "pkg/worker.go")
	if edge == nil ||
		edge.Score == 0 ||
		!surpriseEdgeHasReason(*edge, "cross_directory") ||
		!surpriseEdgeHasReason(*edge, "cross_language") ||
		!surpriseEdgeHasReason(*edge, "doc_to_code") {
		t.Fatalf("unexpected surprise edges:\n%#v", edges)
	}
}

func TestGraphReportIncludesCommunities(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store := openTestStoreAt(
		t,
		ctx,
		filepath.Join(root, ".coding-ethos", "code-intel.duckdb"),
	)

	writeFile(t, filepath.Join(root, "cmd", "app.go"), []byte("package main\n"))
	writeFile(t, filepath.Join(root, "pkg", "worker.go"), []byte("package pkg\n"))
	_, err := NewASTIndexer(store).IndexPaths(ctx, root, []string{"."})
	if err != nil {
		t.Fatalf("index graph report community files: %v", err)
	}
	_, err = store.Database().ExecContext(
		ctx,
		`INSERT INTO code_edges(
			edge_id, edge_kind, path, target_path, provenance_class
		) VALUES ('edge-app-worker', 'calls', 'cmd/app.go', 'pkg/worker.go', ?)`,
		ProvenanceExtracted,
	)
	if err != nil {
		t.Fatalf("insert graph report community edge: %v", err)
	}

	report, err := store.GraphReport(ctx, GraphReportQuery{
		Root:  root,
		Limit: 5,
	})
	if err != nil {
		t.Fatalf("graph report: %v", err)
	}

	if len(report.Communities) != 1 ||
		report.Communities[0].MemberCount != 2 ||
		!strings.Contains(report.Communities[0].ID, "cmd-app.go") {
		t.Fatalf("unexpected graph report communities:\n%#v", report.Communities)
	}

	if len(report.CentralNodes) == 0 ||
		len(report.SurpriseEdges) == 0 {
		t.Fatalf("graph report missing centrality/surprise sections:\n%#v", report)
	}
}

func TestGraphReportReportsEmptyIndexWarnings(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store := openTestStoreAt(
		t,
		ctx,
		filepath.Join(root, ".coding-ethos", "code-intel.duckdb"),
	)

	report, err := store.GraphReport(ctx, GraphReportQuery{Root: root})
	if err != nil {
		t.Fatalf("graph report: %v", err)
	}

	if report.Stats.Files != 0 ||
		report.Stats.CodeChunks != 0 ||
		!graphReportHasWarning(report, "code index is empty") ||
		!graphReportHasWarning(report, "repo map has no ranked files") {
		t.Fatalf("unexpected empty-index report:\n%#v", report)
	}
}

func TestGraphReportReportsMissingHealthSnapshot(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	runCodeIntelGit(t, root, "init")
	writeFile(t, filepath.Join(root, "pkg", "worker.py"), []byte(
		"def helper():\n"+
			"    return \"ok\"\n",
	))
	store := openTestStoreAt(
		t,
		ctx,
		filepath.Join(root, ".coding-ethos", "code-intel.duckdb"),
	)

	_, err := NewASTIndexer(store).IndexPaths(ctx, root, []string{"."})
	if err != nil {
		t.Fatalf("index code: %v", err)
	}

	report, err := store.GraphReport(ctx, GraphReportQuery{Root: root})
	if err != nil {
		t.Fatalf("graph report: %v", err)
	}

	if !graphReportHasWarning(report, "code health snapshot unavailable") {
		t.Fatalf("missing health warning not reported:\n%#v", report)
	}
}

func TestGraphReportReportsEmptyRepoMap(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	runCodeIntelGit(t, root, "init")
	writeFile(t, filepath.Join(root, "pkg", "worker.py"), []byte(
		"def helper():\n"+
			"    return \"ok\"\n",
	))
	store := openTestStoreAt(
		t,
		ctx,
		filepath.Join(root, ".coding-ethos", "code-intel.duckdb"),
	)

	_, err := NewASTIndexer(store).IndexPaths(ctx, root, []string{"."})
	if err != nil {
		t.Fatalf("index code: %v", err)
	}

	report, err := store.GraphReport(ctx, GraphReportQuery{
		Root: root,
		Path: "missing",
	})
	if err != nil {
		t.Fatalf("graph report: %v", err)
	}

	if report.Stats.Files == 0 ||
		len(report.RepoMap.Files) != 0 ||
		!graphReportHasWarning(report, "repo map has no ranked files") {
		t.Fatalf("unexpected empty repo-map report:\n%#v", report)
	}
}

func graphReportHasWarning(report GraphReport, want string) bool {
	for _, warning := range report.Warnings {
		if strings.Contains(warning, want) {
			return true
		}
	}

	return false
}

func documentLinksContain(
	links []DocumentLink,
	kind string,
	sourcePath string,
	sourceHeading string,
	targetPath string,
	targetSymbol string,
	provenance string,
) bool {
	for _, link := range links {
		if link.Kind == kind &&
			link.SourcePath == sourcePath &&
			(sourceHeading == "" || link.SourceHeading == sourceHeading) &&
			link.TargetPath == targetPath &&
			link.TargetSymbolPath == targetSymbol &&
			link.ProvenanceClass == provenance {
			return true
		}
	}

	return false
}

func seedCommunityFile(
	t *testing.T,
	ctx context.Context,
	store *Store,
	path string,
	language string,
	edges []CodeEdge,
) {
	t.Helper()

	err := store.ReplaceCodeFileIndex(
		ctx,
		CodeFile{
			Path:         path,
			Language:     language,
			ContentHash:  "hash-" + strings.ReplaceAll(path, "/", "-"),
			IndexedAtUTC: "2026-06-08T00:00:00Z",
			SizeBytes:    64,
			LineCount:    4,
		},
		[]CodeChunk{{
			ID:          "chunk-" + strings.ReplaceAll(path, "/", "-"),
			Path:        path,
			Language:    language,
			NodeKind:    "function",
			SymbolKind:  "function",
			SymbolName:  "Run",
			SymbolPath:  "Run",
			ContentHash: "chunk-hash-" + strings.ReplaceAll(path, "/", "-"),
			SearchText:  "Run",
			RawText:     "func Run() {}",
			StartByte:   0,
			EndByte:     12,
			StartLine:   1,
			EndLine:     1,
		}},
		edges,
	)
	if err != nil {
		t.Fatalf("seed community file %s: %v", path, err)
	}
}

func seedCommunityCoChange(
	t *testing.T,
	ctx context.Context,
	store *Store,
	path string,
	relatedPath string,
	count int,
	hidden bool,
) {
	t.Helper()

	hiddenValue := 0
	if hidden {
		hiddenValue = 1
	}

	_, err := store.Database().ExecContext(
		ctx,
		`INSERT INTO git_cochanges(
			path, related_path, cochange_count, last_seen_utc, hidden_coupling
		) VALUES (?, ?, ?, '2026-06-08T00:00:00Z', ?)`,
		path,
		relatedPath,
		count,
		hiddenValue,
	)
	if err != nil {
		t.Fatalf("seed community cochange: %v", err)
	}
}

func codeCommunityContaining(
	communities []CodeCommunity,
	path string,
) *CodeCommunity {
	for index := range communities {
		for _, member := range communities[index].CentralMembers {
			if member.Path == path {
				return &communities[index]
			}
		}
	}

	return nil
}

func codeCommunityHasEvidence(
	community CodeCommunity,
	kind string,
	sourcePath string,
	targetPath string,
) bool {
	for _, evidence := range community.Evidence {
		if evidence.Kind == kind &&
			evidence.SourcePath == sourcePath &&
			evidence.TargetPath == targetPath {
			return true
		}
	}

	return false
}

func centralNodeForPath(nodes []CentralNode, path string) *CentralNode {
	for index := range nodes {
		if nodes[index].Path == path {
			return &nodes[index]
		}
	}

	return nil
}

func centralNodeHasSignal(node CentralNode, kind string) bool {
	for _, signal := range node.Signals {
		if signal.Kind == kind {
			return true
		}
	}

	return false
}

func surpriseEdgeForPaths(
	edges []SurpriseEdge,
	sourcePath string,
	targetPath string,
) *SurpriseEdge {
	for index := range edges {
		if edges[index].SourcePath == sourcePath && edges[index].TargetPath == targetPath {
			return &edges[index]
		}
	}

	return nil
}

func surpriseEdgeHasReason(edge SurpriseEdge, kind string) bool {
	for _, reason := range edge.Reasons {
		if reason.Kind == kind {
			return true
		}
	}

	return false
}
