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

func TestPruneFormatsIncludeCandidatesAndDBMaintenance(t *testing.T) {
	t.Parallel()

	report := PruneReport{
		GeneratedAtUTC: "2026-05-22T12:00:00Z",
		Root:           "/repo",
		TracePath:      "/repo/.coding-ethos/prune-runs/run.json",
		Apply:          true,
		DeletedFiles:   1,
		DeletedBytes:   42,
		Skipped:        1,
		Candidates: []PruneCandidate{
			{
				SurfaceID: "lint_traces",
				Path:      "/repo/.coding-ethos/lint-runs/old.json",
				Kind:      "file",
				Reason:    "max_age=24h",
				Bytes:     42,
				Deleted:   true,
			},
			{
				SurfaceID: "hook_runs",
				Path:      "/repo/.coding-ethos/hook-runs/old",
				Kind:      "directory",
				Reason:    "waiting for code-intel ingest",
				Skipped:   true,
			},
		},
		DBMaintenance: []DBMaintenance{
			{
				SurfaceID:          "code_intel_db",
				CutoffUTC:          "2026-05-21T12:00:00Z",
				DeletedTraces:      2,
				DeletedProxyEvents: 3,
				Vacuumed:           true,
			},
		},
		Errors: []string{"sample error"},
	}

	toon := FormatPruneTOON(report)
	if !strings.Contains(toon, "candidates[2]") ||
		!strings.Contains(toon, "trace_path: /repo/.coding-ethos/prune-runs/run.json") ||
		!strings.Contains(toon, "db_maintenance[1]") {
		t.Fatalf("TOON prune report missing details:\n%s", toon)
	}

	human := FormatPruneHuman(report)
	if !strings.Contains(human, "mode: apply") ||
		!strings.Contains(human, "- lint_traces deleted") ||
		!strings.Contains(
			human,
			"- code_intel_db rows: deleted_traces=2 deleted_proxy_events=3 vacuumed=true",
		) ||
		!strings.Contains(human, "trace_path: /repo/.coding-ethos/prune-runs/run.json") ||
		!strings.Contains(human, "sample error") {
		t.Fatalf("human prune report missing details:\n%s", human)
	}
}

func TestSortInventoriesOrdersBySurfaceID(t *testing.T) {
	t.Parallel()

	items := []Inventory{
		{Definition: Definition{ID: "lint_traces"}},
		{Definition: Definition{ID: "hook_runs"}},
		{Definition: Definition{ID: "code_intel_db"}},
	}
	SortInventories(items)

	got := []string{items[0].ID, items[1].ID, items[2].ID}
	want := []string{"code_intel_db", "hook_runs", "lint_traces"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("sorted inventories = %#v, want %#v", got, want)
	}
}

func TestInspectGlobRecordsErrorsDirectoriesAndStaleFiles(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	root := t.TempDir()
	dirMatch := filepath.Join(root, "match-dir")
	if err := os.MkdirAll(dirMatch, 0o755); err != nil {
		t.Fatalf("create glob dir: %v", err)
	}
	fileMatch := filepath.Join(root, "match-file.log")
	writeReportFixture(t, root, "match-file.log", "old\n")
	oldTime := now.Add(-48 * time.Hour)
	if err := os.Chtimes(fileMatch, oldTime, oldTime); err != nil {
		t.Fatalf("set file mtime: %v", err)
	}

	inventory := Inventory{}
	inspectGlob(
		filepath.Join(root, "match-*"),
		SurfaceRetentionPolicy{MaxAge: 24 * time.Hour},
		now,
		&inventory,
	)
	if !inventory.Exists ||
		inventory.DirCount != 1 ||
		inventory.FileCount != 1 ||
		inventory.StaleCount != 1 {
		t.Fatalf("glob inventory = %#v", inventory)
	}

	inspectGlob("[", SurfaceRetentionPolicy{}, now, &inventory)
	if len(inventory.Errors) == 0 {
		t.Fatalf("invalid glob did not record an error: %#v", inventory)
	}
}

func TestRetentionPolicyFallsBackToDefinitionAndGlobalDisables(t *testing.T) {
	t.Parallel()

	settings := DefaultSettings()
	delete(settings.Prune.Surfaces, "lint_traces")
	settings.Prune.Enabled = false
	settings.Prune.AutoEnabled = false
	policy := retentionPolicy(settings, Definition{
		ID:             "lint_traces",
		CommandPrune:   true,
		AutomaticPrune: true,
		RequiresIngest: true,
		maxAge:         2 * time.Hour,
	})

	if policy.Enabled || policy.Auto || !policy.RequireCodeIntelIngest ||
		policy.MaxAge != 2*time.Hour {
		t.Fatalf("fallback retention policy = %#v", policy)
	}
}

func TestAddCodeIntelStatsRecordsOpenError(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "code-intel.db")
	if err := os.WriteFile(path, []byte("not sqlite"), 0o600); err != nil {
		t.Fatalf("write invalid db: %v", err)
	}

	inventory := Inventory{}
	addCodeIntelStats(context.Background(), path, &inventory)
	if len(inventory.Errors) == 0 {
		t.Fatalf("invalid code-intel db did not record an error")
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
