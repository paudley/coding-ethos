// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/codeintel"
)

func TestRunStatusReportsHealthyAndWritesHandoff(t *testing.T) {
	paths := operatorStatusTestPaths(t, true)
	writeOperatorStatusHookRun(t, paths.Root, "run-pass", 0)
	createOperatorStatusCodeIntelDB(t, paths.Root, false)

	writePath := filepath.Join(paths.Root, "reports", "status.md")
	output := captureRuntimeStdout(t, func() {
		err := runStatusHandler(paths, []string{
			"--format",
			"toon",
			"--write",
			writePath,
		})
		if err != nil {
			t.Fatalf("run status: %v", err)
		}
	})

	for _, want := range []string{
		"kind: operator_status",
		"status: PASS",
		"policy_bundle,PASS",
		"code_intel_db,PASS",
		"recent_hook_failures: 0",
		"hook_reviews: 0",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("status output missing %q:\n%s", want, output)
		}
	}

	content, err := os.ReadFile(writePath)
	if err != nil {
		t.Fatalf("read handoff report: %v", err)
	}
	if !strings.Contains(string(content), "coding-ethos operator status") ||
		!strings.Contains(string(content), "status: PASS") {
		t.Fatalf("handoff report missing status:\n%s", content)
	}
}

func TestRunStatusReportsBlockersAsJSON(t *testing.T) {
	paths := operatorStatusTestPaths(t, false)
	writeOperatorStatusHookRun(t, paths.Root, "run-fail", 1)
	createOperatorStatusCodeIntelDB(t, paths.Root, true)

	output := captureRuntimeStdout(t, func() {
		err := runStatusHandler(paths, []string{"--format", "json"})
		if err != nil {
			t.Fatalf("run status: %v", err)
		}
	})

	for _, want := range []string{
		`"kind": "operator_status"`,
		`"status": "BLOCKED"`,
		`"recent_hook_failures": 1`,
		`"hook_reviews": 1`,
		`"false_positives": 1`,
		`"name": "policy_bundle"`,
		`Run make build to regenerate runtime artifacts.`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("status JSON missing %q:\n%s", want, output)
		}
	}
}

func TestRunStatusReturnsFlagParseErrors(t *testing.T) {
	paths := operatorStatusTestPaths(t, true)

	err := runStatusHandler(paths, []string{"--not-a-real-status-flag"})
	if err == nil ||
		!strings.Contains(err.Error(), "parse status flags") {
		t.Fatalf("expected returned flag parse error, got %v", err)
	}
}

func TestOperatorStatusTOONEscapesCommaCells(t *testing.T) {
	report := operatorStatusReport{
		Kind:           operatorStatusKind,
		Status:         operatorStatusWarn,
		GeneratedAtUTC: "2026-05-28T00:00:00Z",
		Root:           "/repo/with,comma",
		Summary:        "WARN: needs, operator review",
		Checks: []operatorStatusCheck{{
			Name:   "code_intel_db",
			Status: operatorStatusWarn,
			Detail: "contains,comma",
		}},
	}

	output := formatOperatorStatusTOON(report)
	for _, want := range []string{
		`root: /repo/with\,comma`,
		`summary: WARN: needs\, operator review`,
		`code_intel_db,WARN,contains\,comma`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("TOON output missing escaped cell %q:\n%s", want, output)
		}
	}
}

func operatorStatusTestPaths(t *testing.T, createArtifacts bool) runtimePaths {
	t.Helper()

	root := t.TempDir()
	paths := runtimePaths{
		Root:            root,
		HooksDir:        filepath.Join(root, ".git", "hooks"),
		BinDir:          filepath.Join(root, "bin"),
		GitHookRunner:   filepath.Join(root, "bin", "coding-ethos-hook-runner"),
		PolicyBundle:    filepath.Join(root, "build", "policy", "policy-bundle.json"),
		PolicyMetadata:  filepath.Join(root, "build", "policy", "policy-metadata.json"),
		ManagedManifest: filepath.Join(root, "build", "toolchain", "manifest.tsv"),
	}

	if !createArtifacts {
		return paths
	}

	for _, dir := range []string{
		paths.HooksDir,
		filepath.Dir(paths.PolicyBundle),
		filepath.Dir(paths.ManagedManifest),
		paths.BinDir,
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}

	for _, file := range []string{
		paths.GitHookRunner,
		paths.PolicyBundle,
		paths.PolicyMetadata,
		paths.ManagedManifest,
	} {
		if err := os.WriteFile(file, []byte("{}\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", file, err)
		}
	}

	return paths
}

func writeOperatorStatusHookRun(t *testing.T, root, name string, exitCode int) {
	t.Helper()

	runDir := filepath.Join(root, ".coding-ethos", "hook-runs", name)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("create hook run dir: %v", err)
	}

	content := "run_id='" + name + "'\nexit_code='" + strconv.Itoa(exitCode) + "'\n"
	if err := os.WriteFile(
		filepath.Join(runDir, "metadata.env"),
		[]byte(content),
		0o600,
	); err != nil {
		t.Fatalf("write hook metadata: %v", err)
	}
}

func createOperatorStatusCodeIntelDB(t *testing.T, root string, review bool) {
	t.Helper()

	store, err := codeintel.Open(
		context.Background(),
		filepath.Join(root, ".coding-ethos", "code-intel.duckdb"),
	)
	if err != nil {
		t.Fatalf("open code-intel db: %v", err)
	}

	if review {
		err = store.RecordHookReview(context.Background(), codeintel.HookReview{
			TraceID:       "trace-1",
			Disposition:   "false_positive",
			Reviewer:      "operator",
			RecordedAtUTC: "2026-05-27T00:00:00Z",
			Notes:         "test review",
		})
		if err != nil {
			t.Fatalf("record hook review: %v", err)
		}
	}

	if err := store.Close(); err != nil {
		t.Fatalf("close code-intel db: %v", err)
	}
}
