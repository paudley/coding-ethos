// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package outputsurface

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"blackcat.ca/coding-ethos/go/internal/codeintel"
)

func TestBuildReportInventoriesRepoSurfaces(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeReportFixture(
		t,
		root,
		".coding-ethos/hook-runs/run-1/stdout.log",
		"hook output\n",
	)
	writeReportFixture(t, root, ".coding-ethos/lint-runs/ruff.json", "{}\n")
	writeReportFixture(t, root, ".coding-ethos/state/commit-head.json", `{"head":"abc"}`)

	dbPath := filepath.Join(root, ".coding-ethos", "code-intel.db")
	store, err := codeintel.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("create code-intel store: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close code-intel store: %v", err)
	}

	report, err := BuildReport(context.Background(), Options{
		Root: root,
		Now:  time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("BuildReport returned error: %v", err)
	}

	if report.Root != filepath.Clean(root) {
		t.Fatalf("report root = %q, want %q", report.Root, filepath.Clean(root))
	}
	if report.IncludeTemp {
		t.Fatal("temp surfaces should be excluded by default")
	}

	hookRuns := inventoryByID(t, report, "hook_runs")
	if !hookRuns.Exists || hookRuns.FileCount != 1 || hookRuns.TotalBytes == 0 {
		t.Fatalf("hook_runs inventory = %#v", hookRuns)
	}

	hookStdout := inventoryByID(t, report, "hook_stdout_logs")
	if !hookStdout.Exists || hookStdout.FileCount != 1 {
		t.Fatalf("hook_stdout_logs inventory = %#v", hookStdout)
	}

	lintTraces := inventoryByID(t, report, "lint_traces")
	if !lintTraces.Exists || lintTraces.FileCount != 1 {
		t.Fatalf("lint_traces inventory = %#v", lintTraces)
	}

	codeIntelDB := inventoryByID(t, report, "code_intel_db")
	if !codeIntelDB.Exists || codeIntelDB.DBStats == nil {
		t.Fatalf("code_intel_db inventory = %#v", codeIntelDB)
	}

	if _, found := findInventory(report, "proxy_temp_evidence"); found {
		t.Fatal("proxy temp evidence should require IncludeTemp")
	}
}

func TestBuildReportIncludesTempSurfacesOnRequest(t *testing.T) {
	t.Parallel()

	report, err := BuildReport(context.Background(), Options{
		Root:        t.TempDir(),
		IncludeTemp: true,
		Now:         time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("BuildReport returned error: %v", err)
	}

	if _, found := findInventory(report, "proxy_temp_evidence"); !found {
		t.Fatal("proxy temp evidence surface missing")
	}
	if _, found := findInventory(report, "process_state_locks"); !found {
		t.Fatal("process state lock surface missing")
	}
}

func TestReportFormatsIncludeInventoryRows(t *testing.T) {
	t.Parallel()

	report := Report{
		GeneratedAtUTC: "2026-05-22T12:00:00Z",
		Root:           "/repo",
		Surfaces: []Inventory{
			{
				Definition: Definition{
					ID:         "hook_runs",
					Root:       rootRepo,
					RecordKind: "directory",
				},
				Exists:     true,
				Path:       "/repo/.coding-ethos/hook-runs",
				FileCount:  2,
				DirCount:   1,
				TotalBytes: 10,
			},
		},
	}

	toon := FormatTOON(report)
	if !strings.Contains(toon, "surfaces[1]") || !strings.Contains(toon, "hook_runs") {
		t.Fatalf("TOON report missing inventory row:\n%s", toon)
	}

	human := FormatHuman(report)
	if !strings.Contains(human, "coding-ethos output surface report") ||
		!strings.Contains(human, "- hook_runs: present") {
		t.Fatalf("human report missing inventory row:\n%s", human)
	}
}

func inventoryByID(t *testing.T, report Report, id string) Inventory {
	t.Helper()

	inventory, found := findInventory(report, id)
	if !found {
		t.Fatalf("surface %q missing from report", id)
	}

	return inventory
}

func findInventory(report Report, id string) (Inventory, bool) {
	for _, inventory := range report.Surfaces {
		if inventory.ID == id {
			return inventory, true
		}
	}

	return Inventory{}, false
}

func writeReportFixture(t *testing.T, root, path, payload string) {
	t.Helper()

	absolutePath := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolutePath), 0o755); err != nil {
		t.Fatalf("create fixture directory: %v", err)
	}
	if err := os.WriteFile(absolutePath, []byte(payload), 0o600); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
}
