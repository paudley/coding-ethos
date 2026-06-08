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
