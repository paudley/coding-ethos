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
		!strings.Contains(strings.Join(report.CentralFiles[0].Reasons, "; "), "score") {
		t.Fatalf("unexpected graph report:\n%#v", report)
	}
}
