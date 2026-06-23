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

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/outputsurface"
)

func TestRunStatusReportsHealthyAndWritesHandoff(t *testing.T) {
	t.Setenv("CODE_ETHOS_AGENT_API_PROXY", "")
	t.Setenv("CODE_ETHOS_AGENT_API_PROXY_URL", "")

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
		"agent_api_proxy,PASS,routing disabled",
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
	t.Setenv("CODE_ETHOS_AGENT_API_PROXY", "1")
	t.Setenv("CODE_ETHOS_AGENT_API_PROXY_URL", "")

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
		`routing enabled without CODE_ETHOS_AGENT_API_PROXY_URL`,
		`Run make build to regenerate runtime artifacts.`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("status JSON missing %q:\n%s", want, output)
		}
	}
}

func TestRunStatusIncludesContextAdviceWhenThresholdCrossed(t *testing.T) {
	t.Setenv("CODE_ETHOS_AGENT_API_PROXY", "")
	t.Setenv("CODE_ETHOS_AGENT_API_PROXY_URL", "")

	paths := operatorStatusTestPaths(t, true)
	writeOperatorStatusHookRun(t, paths.Root, "run-pass", 0)
	createOperatorStatusCodeIntelDB(t, paths.Root, false)
	recordOperatorStatusProxyEvent(t, paths.Root, "context-advice-read-1")

	err := os.WriteFile(
		filepath.Join(paths.Root, "repo_config.yaml"),
		[]byte("proxy:\n  context_advisor:\n    warning_file_reads: 1\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write repo config: %v", err)
	}

	output := captureRuntimeStdout(t, func() {
		err := runStatusHandler(paths, []string{"--format", "toon"})
		if err != nil {
			t.Fatalf("run status: %v", err)
		}
	})

	for _, want := range []string{
		"context_token_economy,WARN",
		"context_advice[",
		"repeated_file_reads",
		"proxy_file_reads=1",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("status output missing %q:\n%s", want, output)
		}
	}
}

func TestAgentAPIProxyRoutingCheckWarnsOnInvalidURL(t *testing.T) {
	t.Setenv("CODE_ETHOS_AGENT_API_PROXY", "1")
	t.Setenv("CODE_ETHOS_AGENT_API_PROXY_URL", "127.0.0.1:8080")

	check := agentAPIProxyRoutingCheck()
	if check.Status != operatorStatusWarn ||
		!strings.Contains(check.Detail, "invalid CODE_ETHOS_AGENT_API_PROXY_URL") {
		t.Fatalf("check = %#v", check)
	}
}

func TestAgentAPIProxyRoutingCheckPassesValidURL(t *testing.T) {
	t.Setenv("CODE_ETHOS_AGENT_API_PROXY", "1")
	t.Setenv("CODE_ETHOS_AGENT_API_PROXY_URL", "http://127.0.0.1:8080")

	check := agentAPIProxyRoutingCheck()
	if check.Status != operatorStatusPass ||
		!strings.Contains(check.Detail, "explicit proxy URL") {
		t.Fatalf("check = %#v", check)
	}
}

func interceptionTestPaths(t *testing.T, mode string) runtimePaths {
	t.Helper()

	ethosRoot := t.TempDir()
	config := "proxy:\n  interception:\n    mode: \"" + mode + "\"\n"
	if err := os.WriteFile(
		filepath.Join(ethosRoot, "config.yaml"),
		[]byte(config),
		0o600,
	); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return runtimePaths{Root: t.TempDir(), EthosRoot: ethosRoot}
}

func TestAgentAPIProxyInterceptionCheckDisabledByDefault(t *testing.T) {
	t.Setenv("CODE_ETHOS_AGENT_PROXY_INTERCEPT", "")

	paths := interceptionTestPaths(t, "off")

	check := agentAPIProxyInterceptionCheck(paths)
	if check.Name != "agent_api_proxy_interception" ||
		check.Status != operatorStatusPass ||
		check.Detail != "disabled" {
		t.Fatalf("check = %#v", check)
	}
}

func TestAgentAPIProxyInterceptionCheckDeniesStaleConfig(t *testing.T) {
	t.Setenv("CODE_ETHOS_AGENT_PROXY_INTERCEPT", "")

	paths := interceptionTestPaths(t, "required")

	check := agentAPIProxyInterceptionCheck(paths)
	if check.Status != operatorStatusWarn ||
		!strings.Contains(check.Detail, "stale-config guard") {
		t.Fatalf("check = %#v", check)
	}
}

func TestAgentAPIProxyInterceptionCheckEnabled(t *testing.T) {
	t.Setenv("CODE_ETHOS_AGENT_PROXY_INTERCEPT", "1")

	paths := interceptionTestPaths(t, "required")

	check := agentAPIProxyInterceptionCheck(paths)
	if check.Status != operatorStatusPass ||
		!strings.Contains(check.Detail, "enabled (ca ") {
		t.Fatalf("check = %#v", check)
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

func TestOutputSurfaceChecksReportMissingAndUnavailableCodeIntelStats(t *testing.T) {
	t.Parallel()

	checks := outputSurfaceChecks(outputsurface.Report{})
	if len(checks) != 2 ||
		checks[0].Name != "output_surfaces" ||
		checks[0].Status != operatorStatusPass ||
		checks[1].Name != "code_intel_db" ||
		checks[1].Status != operatorStatusWarn ||
		!strings.Contains(checks[1].Detail, "DuckDB store is missing") {
		t.Fatalf("missing code-intel checks = %#v", checks)
	}

	checks = outputSurfaceChecks(outputsurface.Report{
		Surfaces: []outputsurface.Inventory{{
			Definition: outputsurface.Definition{ID: "code_intel_db"},
			Exists:     true,
		}},
	})
	if len(checks) != 2 ||
		checks[1].Name != "code_intel_db" ||
		checks[1].Status != operatorStatusWarn ||
		!strings.Contains(checks[1].Detail, "stats were unavailable") {
		t.Fatalf("unavailable stats checks = %#v", checks)
	}
}

func TestOutputSurfaceChecksReportStaleAndErrorCounts(t *testing.T) {
	t.Parallel()

	checks := outputSurfaceChecks(outputsurface.Report{
		Surfaces: []outputsurface.Inventory{
			{
				Definition: outputsurface.Definition{ID: "hook_runs"},
				Exists:     true,
				StaleCount: 2,
			},
			{
				Definition: outputsurface.Definition{ID: "code_intel_db"},
				Exists:     true,
				DBStats:    &codeintel.Stats{Files: 3, CodeChunks: 4},
				Errors:     []string{"permission denied"},
			},
		},
	})
	if len(checks) != 2 ||
		checks[0].Name != "output_surfaces" ||
		checks[0].Status != operatorStatusWarn ||
		!strings.Contains(checks[0].Detail, "surfaces=2 stale=2 errors=1") ||
		checks[1].Status != operatorStatusPass ||
		!strings.Contains(checks[1].Detail, "files=3 chunks=4") {
		t.Fatalf("surface checks = %#v", checks)
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

func recordOperatorStatusProxyEvent(t *testing.T, root, eventID string) {
	t.Helper()

	store, err := codeintel.Open(
		context.Background(),
		filepath.Join(root, ".coding-ethos", "code-intel.duckdb"),
	)
	if err != nil {
		t.Fatalf("open code-intel db: %v", err)
	}
	defer func() {
		closeErr := store.Close()
		if closeErr != nil {
			t.Fatalf("close code-intel db: %v", closeErr)
		}
	}()

	err = store.RecordProxyEvent(context.Background(), agentproxy.ProviderEvent{
		ID:        eventID,
		SessionID: "context-advice-session",
		Kind:      agentproxy.EventFileRead,
		Provider:  "codex",
		RepoRoot:  root,
		Decision:  "allow",
	})
	if err != nil {
		t.Fatalf("record proxy event: %v", err)
	}
}
