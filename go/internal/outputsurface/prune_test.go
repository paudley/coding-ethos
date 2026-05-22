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

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/codeintel"
)

func TestPruneDryRunReportsCandidatesWithoutDeleting(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	tracePath := filepath.Join(root, ".coding-ethos", "lint-runs", "old.json")
	writePruneFixture(t, tracePath, "{}\n")
	oldTime := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(tracePath, oldTime, oldTime); err != nil {
		t.Fatalf("set fixture mtime: %v", err)
	}

	report, err := Prune(context.Background(), PruneOptions{
		Root:      root,
		Settings:  settingsWithoutIngest("lint_traces"),
		Scopes:    []string{"lint_traces"},
		OlderThan: 24 * time.Hour,
		Now:       time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Prune returned error: %v", err)
	}
	if len(report.Candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1: %#v", len(report.Candidates), report)
	}
	if _, err := os.Stat(tracePath); err != nil {
		t.Fatalf("dry-run deleted fixture: %v", err)
	}
}

func TestPruneApplyDeletesCandidates(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	tracePath := filepath.Join(root, ".coding-ethos", "lint-runs", "old.json")
	writePruneFixture(t, tracePath, "{}\n")
	oldTime := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(tracePath, oldTime, oldTime); err != nil {
		t.Fatalf("set fixture mtime: %v", err)
	}

	report, err := Prune(context.Background(), PruneOptions{
		Root:      root,
		Settings:  settingsWithoutIngest("lint_traces"),
		Scopes:    []string{"lint_traces"},
		OlderThan: 24 * time.Hour,
		Now:       time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC),
		Apply:     true,
	})
	if err != nil {
		t.Fatalf("Prune returned error: %v", err)
	}
	if report.DeletedFiles != 1 {
		t.Fatalf("deleted files = %d, want 1: %#v", report.DeletedFiles, report)
	}
	if _, err := os.Stat(tracePath); !os.IsNotExist(err) {
		t.Fatalf("fixture still exists after apply: %v", err)
	}
}

func TestPruneApplyDeletesDirectoryCandidates(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	runDir := filepath.Join(root, ".coding-ethos", "hook-runs", "run-old")
	writePruneFixture(t, filepath.Join(runDir, "stdout.log"), "hook output\n")
	oldTime := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(runDir, oldTime, oldTime); err != nil {
		t.Fatalf("set run dir mtime: %v", err)
	}

	report, err := Prune(context.Background(), PruneOptions{
		Root:      root,
		Settings:  settingsWithoutIngest("hook_runs"),
		Scopes:    []string{"hook_runs"},
		OlderThan: 24 * time.Hour,
		Now:       time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC),
		Apply:     true,
	})
	if err != nil {
		t.Fatalf("Prune returned error: %v", err)
	}
	if report.DeletedDirs != 1 || report.DeletedBytes != int64(len("hook output\n")) {
		t.Fatalf("directory delete report = %#v", report)
	}
	if _, err := os.Stat(runDir); !os.IsNotExist(err) {
		t.Fatalf("hook run dir still exists after apply: %v", err)
	}
}

func TestAutoPruneSurfaceSkipsWhenPolicyIsNotAuto(t *testing.T) {
	root := t.TempDir()
	tracePath := filepath.Join(root, ".coding-ethos", "lint-runs", "old.json")
	writePruneFixture(t, tracePath, "{}\n")
	oldTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(tracePath, oldTime, oldTime); err != nil {
		t.Fatalf("set fixture mtime: %v", err)
	}
	writeConfigFixture(t, root, "repo_config.toml", `
[outputs.prune]
auto_enabled = true

[outputs.prune.surfaces.lint_traces]
max_age = "1h"
require_code_intel_ingest = false
`)

	if err := AutoPruneSurface(
		context.Background(),
		root,
		"lint_traces",
		false,
	); err != nil {
		t.Fatalf("AutoPruneSurface returned error: %v", err)
	}
	if _, err := os.Stat(tracePath); err != nil {
		t.Fatalf("non-auto policy deleted trace: %v", err)
	}
}

func TestAutoPruneSurfaceAppliesEnabledPolicy(t *testing.T) {
	root := t.TempDir()
	tracePath := filepath.Join(root, ".coding-ethos", "lint-runs", "old.json")
	writePruneFixture(t, tracePath, "{}\n")
	oldTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(tracePath, oldTime, oldTime); err != nil {
		t.Fatalf("set fixture mtime: %v", err)
	}
	writeConfigFixture(t, root, "repo_config.toml", `
[outputs.prune]
auto_enabled = true

[outputs.prune.surfaces.lint_traces]
auto = true
max_age = "1h"
require_code_intel_ingest = false
`)

	if err := AutoPruneSurface(
		context.Background(),
		root,
		"lint_traces",
		false,
	); err != nil {
		t.Fatalf("AutoPruneSurface returned error: %v", err)
	}
	if _, err := os.Stat(tracePath); !os.IsNotExist(err) {
		t.Fatalf("auto-prune did not delete trace: %v", err)
	}
}

func TestPruneIncludesTempSurfacesWhenRequested(t *testing.T) {
	root := t.TempDir()
	tempRoot := t.TempDir()
	t.Setenv("TMPDIR", tempRoot)

	evidencePath := filepath.Join(tempRoot, "coding-ethos-tool-output-test-prune.log")
	writePruneFixture(t, evidencePath, "tool output\n")
	oldTime := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(evidencePath, oldTime, oldTime); err != nil {
		t.Fatalf("set temp fixture mtime: %v", err)
	}

	report, err := Prune(context.Background(), PruneOptions{
		Root:        root,
		Scopes:      []string{"proxy_temp_evidence"},
		IncludeTemp: true,
		OlderThan:   24 * time.Hour,
		Now:         time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Prune returned error: %v", err)
	}
	if len(report.Candidates) != 1 ||
		report.Candidates[0].SurfaceID != "proxy_temp_evidence" {
		t.Fatalf("temp candidates = %#v", report.Candidates)
	}
	if _, err := os.Stat(evidencePath); err != nil {
		t.Fatalf("dry-run deleted temp evidence: %v", err)
	}
}

func TestPruneAppliesFileSurfaceCandidate(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sarifPath := filepath.Join(root, "coding-ethos.sarif")
	writePruneFixture(t, sarifPath, "{}\n")
	oldTime := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(sarifPath, oldTime, oldTime); err != nil {
		t.Fatalf("set SARIF mtime: %v", err)
	}

	report, err := Prune(context.Background(), PruneOptions{
		Root:      root,
		Scopes:    []string{"ci_sarif_artifact"},
		OlderThan: 24 * time.Hour,
		Now:       time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC),
		Apply:     true,
	})
	if err != nil {
		t.Fatalf("Prune returned error: %v", err)
	}
	if report.DeletedFiles != 1 || len(report.Candidates) != 1 ||
		!report.Candidates[0].Deleted {
		t.Fatalf("file prune report = %#v", report)
	}
	if _, err := os.Stat(sarifPath); !os.IsNotExist(err) {
		t.Fatalf("SARIF artifact still exists after apply: %v", err)
	}
}

func TestPruneDisabledPolicySkipsUnlessAllOrOverrideIsUsed(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	tracePath := filepath.Join(root, ".coding-ethos", "lint-runs", "old.json")
	writePruneFixture(t, tracePath, "{}\n")
	oldTime := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(tracePath, oldTime, oldTime); err != nil {
		t.Fatalf("set trace mtime: %v", err)
	}

	settings := settingsWithoutIngest("lint_traces")
	policy := settings.Prune.Surfaces["lint_traces"]
	policy.Enabled = false
	policy.MaxAge = 24 * time.Hour
	settings.Prune.Surfaces["lint_traces"] = policy

	report, err := Prune(context.Background(), PruneOptions{
		Root:     root,
		Settings: settings,
		Scopes:   []string{"lint_traces"},
		Now:      time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC),
		Apply:    true,
	})
	if err != nil {
		t.Fatalf("Prune returned error: %v", err)
	}
	if len(report.Candidates) != 0 {
		t.Fatalf("disabled policy should not produce candidates: %#v", report.Candidates)
	}
	if _, err := os.Stat(tracePath); err != nil {
		t.Fatalf("disabled policy deleted trace: %v", err)
	}

	report, err = Prune(context.Background(), PruneOptions{
		Root:     root,
		Settings: settings,
		Scopes:   []string{"lint_traces"},
		Now:      time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC),
		Apply:    true,
		All:      true,
	})
	if err != nil {
		t.Fatalf("Prune with --all returned error: %v", err)
	}
	if report.DeletedFiles != 1 {
		t.Fatalf("--all did not delete disabled-policy candidate: %#v", report)
	}
}

func TestPruneHelperFunctionsCoverPathAndSizeBranches(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "candidate-dir")
	writePruneFixture(t, filepath.Join(dir, "nested", "one.txt"), "one")
	writePruneFixture(t, filepath.Join(dir, "two.txt"), "two")

	info, err := os.Lstat(dir)
	if err != nil {
		t.Fatalf("stat candidate dir: %v", err)
	}
	if candidateKind(info) != recordKindDirectory {
		t.Fatalf("candidateKind(dir) = %q", candidateKind(info))
	}
	size, err := candidateSize(dir, info)
	if err != nil {
		t.Fatalf("candidateSize returned error: %v", err)
	}
	if size != 6 {
		t.Fatalf("candidate dir size = %d, want 6", size)
	}
	if !pathWithinRoot(root, dir) {
		t.Fatalf("pathWithinRoot rejected child path")
	}
	if pathWithinRoot(root, filepath.Dir(root)) {
		t.Fatalf("pathWithinRoot accepted parent path")
	}

	candidate := skippedCandidate(
		"lint_traces",
		filepath.Join(root, "trace.json"),
		"reason",
	)
	if !candidate.Skipped || candidate.SurfaceID != "lint_traces" ||
		!strings.Contains(candidate.Path, "trace.json") {
		t.Fatalf("skippedCandidate = %#v", candidate)
	}
}

func TestPruneHonorsKeepLastAndMaxBytes(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, ".coding-ethos", "lint-runs")
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	for index := range 3 {
		path := filepath.Join(dir, "trace-"+string(rune('a'+index))+".json")
		writePruneFixture(t, path, strings.Repeat("x", 10))
		mtime := now.Add(-time.Duration(index) * time.Minute)
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatalf("set fixture mtime: %v", err)
		}
	}

	settings := DefaultSettings()
	policy := settings.Prune.Surfaces["lint_traces"]
	policy.KeepLast = 1
	policy.MaxBytes = 15
	policy.RequireCodeIntelIngest = false
	settings.Prune.Surfaces["lint_traces"] = policy

	report, err := Prune(context.Background(), PruneOptions{
		Root:     root,
		Settings: settings,
		Scopes:   []string{"lint_traces"},
		Now:      now,
	})
	if err != nil {
		t.Fatalf("Prune returned error: %v", err)
	}
	if len(report.Candidates) != 2 {
		t.Fatalf(
			"candidate count = %d, want 2: %#v",
			len(report.Candidates),
			report.Candidates,
		)
	}
	for _, candidate := range report.Candidates {
		if !strings.Contains(candidate.Reason, "keep_last") &&
			!strings.Contains(candidate.Reason, "max_bytes") {
			t.Fatalf(
				"candidate reason %q does not mention keep_last or max_bytes",
				candidate.Reason,
			)
		}
	}
}

func TestPruneApplyWritesTrace(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	tracePath := filepath.Join(root, ".coding-ethos", "lint-runs", "old.json")
	writePruneFixture(t, tracePath, "{}\n")
	oldTime := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(tracePath, oldTime, oldTime); err != nil {
		t.Fatalf("set fixture mtime: %v", err)
	}

	report, err := Prune(context.Background(), PruneOptions{
		Root:      root,
		Settings:  settingsWithoutIngest("lint_traces"),
		Scopes:    []string{"lint_traces"},
		OlderThan: 24 * time.Hour,
		Now:       time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC),
		Apply:     true,
	})
	if err != nil {
		t.Fatalf("Prune returned error: %v", err)
	}
	if report.TracePath == "" {
		t.Fatalf("trace path missing: %#v", report)
	}
	if _, err := os.Stat(filepath.FromSlash(report.TracePath)); err != nil {
		t.Fatalf("stat prune trace: %v", err)
	}
}

func TestPruneSkipsUningestedRequiredTrace(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	tracePath := filepath.Join(root, ".coding-ethos", "lint-runs", "old.json")
	writePruneFixture(t, tracePath, "{}\n")
	oldTime := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(tracePath, oldTime, oldTime); err != nil {
		t.Fatalf("set fixture mtime: %v", err)
	}

	report, err := Prune(context.Background(), PruneOptions{
		Root:      root,
		Scopes:    []string{"lint_traces"},
		OlderThan: 24 * time.Hour,
		Now:       time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC),
		Apply:     true,
	})
	if err != nil {
		t.Fatalf("Prune returned error: %v", err)
	}
	if len(report.Candidates) != 1 || !report.Candidates[0].Skipped {
		t.Fatalf("candidate was not skipped for missing ingest: %#v", report.Candidates)
	}
	if report.DeletedFiles != 0 {
		t.Fatalf("deleted files = %d, want 0: %#v", report.DeletedFiles, report)
	}
	if _, err := os.Stat(tracePath); err != nil {
		t.Fatalf("uningested fixture was deleted: %v", err)
	}
}

func TestPruneDeletesIngestedRequiredTrace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	tracePath := filepath.Join(root, ".coding-ethos", "lint-runs", "old.json")
	writePruneFixture(t, tracePath, "{}\n")
	oldTime := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(tracePath, oldTime, oldTime); err != nil {
		t.Fatalf("set fixture mtime: %v", err)
	}
	store, err := codeintel.Open(ctx, codeintel.DefaultDBPath(root))
	if err != nil {
		t.Fatalf("open code-intel store: %v", err)
	}
	if err := store.IngestTrace(ctx, codeintel.Trace{
		ID:            "trace-1",
		Kind:          "lint",
		RecordedAtUTC: "2026-05-20T12:00:00Z",
		RepoRoot:      root,
		SourcePath:    tracePath,
		Raw:           []byte("{}"),
	}); err != nil {
		t.Fatalf("ingest trace: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close code-intel store: %v", err)
	}

	report, err := Prune(ctx, PruneOptions{
		Root:      root,
		Scopes:    []string{"lint_traces"},
		OlderThan: 24 * time.Hour,
		Now:       time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC),
		Apply:     true,
	})
	if err != nil {
		t.Fatalf("Prune returned error: %v", err)
	}
	if report.DeletedFiles != 1 {
		t.Fatalf("deleted files = %d, want 1: %#v", report.DeletedFiles, report)
	}
	if _, err := os.Stat(tracePath); !os.IsNotExist(err) {
		t.Fatalf("fixture still exists after apply: %v", err)
	}
}

func TestPruneCodeIntelRows(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := codeintel.Open(ctx, codeintel.DefaultDBPath(root))
	if err != nil {
		t.Fatalf("open code-intel store: %v", err)
	}
	oldAt := "2026-05-20T12:00:00Z"
	newAt := "2026-05-22T11:00:00Z"
	for _, trace := range []codeintel.Trace{
		{ID: "old-trace", Kind: "lint", RecordedAtUTC: oldAt, RepoRoot: root, Raw: []byte("{}")},
		{ID: "new-trace", Kind: "lint", RecordedAtUTC: newAt, RepoRoot: root, Raw: []byte("{}")},
	} {
		if err := store.IngestTrace(ctx, trace); err != nil {
			t.Fatalf("ingest trace: %v", err)
		}
	}
	if err := store.RecordProxyEvent(ctx, agentproxy.ProviderEvent{
		ID:            "old-event",
		SessionID:     "session-1",
		Kind:          agentproxy.EventFileRead,
		RecordedAtUTC: time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("record old proxy event: %v", err)
	}
	if err := store.RecordProxyEvent(ctx, agentproxy.ProviderEvent{
		ID:            "new-event",
		SessionID:     "session-1",
		Kind:          agentproxy.EventFileRead,
		RecordedAtUTC: time.Date(2026, 5, 22, 11, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("record new proxy event: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close code-intel store: %v", err)
	}

	report, err := Prune(ctx, PruneOptions{
		Root:      root,
		Scopes:    []string{"code_intel_db"},
		OlderThan: 24 * time.Hour,
		Now:       time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC),
		Apply:     true,
		Vacuum:    true,
	})
	if err != nil {
		t.Fatalf("Prune returned error: %v", err)
	}
	if len(report.DBMaintenance) != 1 ||
		report.DBMaintenance[0].DeletedTraces != 1 ||
		report.DBMaintenance[0].DeletedProxyEvents != 1 ||
		!report.DBMaintenance[0].Vacuumed {
		t.Fatalf("db maintenance = %#v", report.DBMaintenance)
	}

	store, err = codeintel.Open(ctx, codeintel.DefaultDBPath(root))
	if err != nil {
		t.Fatalf("reopen code-intel store: %v", err)
	}
	defer store.Close()
	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("read code-intel stats: %v", err)
	}
	if stats.Traces != 1 || stats.ProxyEvents != 1 {
		t.Fatalf("stats after prune = %#v", stats)
	}
}

func TestPruneCodeIntelRowsDryRunReportsWithoutDeleting(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := codeintel.Open(ctx, codeintel.DefaultDBPath(root))
	if err != nil {
		t.Fatalf("open code-intel store: %v", err)
	}
	if err := store.IngestTrace(ctx, codeintel.Trace{
		ID:            "old-trace",
		Kind:          "lint",
		RecordedAtUTC: "2026-05-20T12:00:00Z",
		RepoRoot:      root,
		Raw:           []byte("{}"),
	}); err != nil {
		t.Fatalf("ingest trace: %v", err)
	}
	if err := store.RecordProxyEvent(ctx, agentproxy.ProviderEvent{
		ID:            "old-event",
		SessionID:     "session-1",
		Kind:          agentproxy.EventFileRead,
		RecordedAtUTC: time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("record old proxy event: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close code-intel store: %v", err)
	}

	report, err := Prune(ctx, PruneOptions{
		Root:      root,
		Scopes:    []string{"code_intel_db"},
		OlderThan: 24 * time.Hour,
		Now:       time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Prune returned error: %v", err)
	}
	if len(report.DBMaintenance) != 1 ||
		report.DBMaintenance[0].DeletedTraces != 1 ||
		report.DBMaintenance[0].DeletedProxyEvents != 1 {
		t.Fatalf("dry-run db maintenance = %#v", report.DBMaintenance)
	}

	store, err = codeintel.Open(ctx, codeintel.DefaultDBPath(root))
	if err != nil {
		t.Fatalf("reopen code-intel store: %v", err)
	}
	defer store.Close()
	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("read code-intel stats: %v", err)
	}
	if stats.Traces != 1 || stats.ProxyEvents != 1 {
		t.Fatalf("dry-run deleted rows: %#v", stats)
	}
}

func TestPruneRejectsUnknownScope(t *testing.T) {
	t.Parallel()

	_, err := Prune(context.Background(), PruneOptions{
		Root:   t.TempDir(),
		Scopes: []string{"unknown"},
	})
	if err == nil {
		t.Fatal("Prune accepted unknown scope")
	}
}

func settingsWithoutIngest(surfaceID string) Settings {
	settings := DefaultSettings()
	policy := settings.Prune.Surfaces[surfaceID]
	policy.RequireCodeIntelIngest = false
	settings.Prune.Surfaces[surfaceID] = policy

	return settings
}

func writePruneFixture(t *testing.T, path, payload string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create fixture dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}
