// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintelcli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunRejectsUnknownCommand(t *testing.T) {
	t.Parallel()

	err := run(context.Background(), []string{"unknown"})
	if err == nil {
		t.Fatalf("expected unknown command error")
	}
}

func TestStatsCreatesStore(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dbPath := filepath.Join(root, ".coding-ethos", "code-intel.db")

	err := run(context.Background(), []string{"stats", "--root", root, "--db", dbPath})
	if err != nil {
		t.Fatalf("stats command returned error: %v", err)
	}
}

func TestVectorStatsCreatesSQLiteVectorStore(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	err := run(context.Background(), []string{"vector-stats", "--root", root})
	if err != nil {
		t.Fatalf("vector-stats command returned error: %v", err)
	}
}

func TestIndexCodeAndCodeChunksCommands(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dbPath := filepath.Join(root, ".coding-ethos", "code-intel.db")

	sourcePath := filepath.Join(root, "cmd", "app.go")

	err := os.MkdirAll(filepath.Dir(sourcePath), 0o700)
	if err != nil {
		t.Fatalf("create source dir: %v", err)
	}

	err = os.WriteFile(sourcePath, []byte(`package main

func runApp() {
	println("ok")
}
`), 0o600)
	if err != nil {
		t.Fatalf("write source: %v", err)
	}

	ctx := context.Background()

	err = run(ctx, []string{"index-code", "--root", root, "--db", dbPath, "cmd"})
	if err != nil {
		t.Fatalf("index-code command returned error: %v", err)
	}

	err = run(ctx, []string{
		"code-chunks", "--root", root, "--db", dbPath,
		"--path", "cmd/app.go", "--symbol-name", "runApp",
	})
	if err != nil {
		t.Fatalf("code-chunks command returned error: %v", err)
	}

	err = run(ctx, []string{
		"code-context", "--root", root, "--db", dbPath,
		"--path", "cmd/app.go", "--symbol-path", "runApp",
	})
	if err != nil {
		t.Fatalf("code-context by symbol returned error: %v", err)
	}

	err = run(ctx, []string{
		"code-context", "--root", root, "--db", dbPath,
		"--path", "cmd/app.go", "--line", "3",
	})
	if err != nil {
		t.Fatalf("code-context by line returned error: %v", err)
	}

	err = run(ctx, []string{"code-context", "--root", root, "--db", dbPath})
	if err == nil ||
		!strings.Contains(err.Error(), "--chunk-id") {
		t.Fatalf("code-context without identifier error = %v", err)
	}
}

func TestRecordAndQueryCommands(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dbPath := filepath.Join(root, ".coding-ethos", "code-intel.db")
	ctx := context.Background()
	baseArgs := []string{"--root", root, "--db", dbPath}

	runCodeIntelCommands(t, ctx, recordCommandArgs(root, baseArgs))
	runCodeIntelCommands(t, ctx, queryCommandArgs(root, baseArgs))
}

func runCodeIntelCommands(
	t *testing.T,
	ctx context.Context,
	commands [][]string,
) {
	t.Helper()

	for _, args := range commands {
		err := run(ctx, args)
		if err != nil {
			t.Fatalf("run(%s) returned error: %v", args[0], err)
		}
	}
}

func recordCommandArgs(root string, baseArgs []string) [][]string {
	return [][]string{
		append([]string{
			"record-outcome",
			"--remediation-id", "rem-1",
			"--finding-id", "finding-1",
			"--policy-id", "policy.one",
			"--skill-id", "skill-one",
			"--path", "pkg/app.py",
			"--provider", "codex",
			"--tool", "Edit",
			"--outcome", "fixed",
			"--attempt", "2",
		}, baseArgs...),
		append([]string{
			"record-hook-review",
			"--trace-id", "trace-1",
			"--tracking-id", "track-1",
			"--disposition", "correct_block",
			"--reviewer", "qa",
			"--notes", "expected",
			"--recorded-at", "2026-01-02T03:04:05Z",
		}, baseArgs...),
		append([]string{
			"record-proxy-event",
			"--event-id", "proxy-event-1",
			"--session-id", "proxy-session-1",
			"--kind", "file_read",
			"--provider", "codex",
			"--model", "test-model",
			"--target-path", "pkg/app.py",
			"--input-hash", "input",
			"--output-hash", "output",
			"--policy-id", "proxy.read",
			"--decision", "allow",
			"--input-tokens", "7",
			"--output-tokens", "11",
		}, baseArgs...),
		append([]string{
			"record-embedding",
			"--backend", "sqlite-vec",
			"--collection", "remediations",
			"--model-id", "code-model",
			"--record-kind", "remediation",
			"--record-id", "rem-1",
			"--dimension", "2",
			"--path", "pkg/app.py",
			"--policy-id", "policy.one",
			"--skill-id", "skill-one",
			"--backend-row-id", "vec-1",
		}, baseArgs...),
		append([]string{
			"upsert-vector",
			"--uri", filepath.Join(root, ".coding-ethos", "vectors.db"),
			"--collection", "remediations",
			"--model-id", "code-model",
			"--id", "vec-1",
			"--vector", "0.1,0.2",
			"--record-kind", "remediation",
			"--record-id", "rem-1",
			"--policy-id", "policy.one",
			"--skill-id", "skill-one",
			"--path", "pkg/app.py",
			"--outcome", "fixed",
			"--message", "split large file",
		}, baseArgs...),
	}
}

func queryCommandArgs(root string, baseArgs []string) [][]string {
	return [][]string{
		append(
			[]string{"remediation-outcomes", "--policy-id", "policy.one"},
			baseArgs...),
		append(
			[]string{"remediation-effectiveness", "--policy-id", "policy.one"},
			baseArgs...),
		append(
			[]string{"embedding-records", "--record-kind", "remediation"},
			baseArgs...),
		append(
			[]string{"embedding-candidates", "--record-kind", "remediation"},
			baseArgs...),
		append([]string{"hook-reviews", "--trace-id", "trace-1"}, baseArgs...),
		append([]string{"proxy-sessions", "--provider", "codex"}, baseArgs...),
		append([]string{"proxy-events", "--session-id", "proxy-session-1"}, baseArgs...),
		append(
			[]string{
				"index-status",
				"--uri",
				filepath.Join(root, ".coding-ethos", "vectors.db"),
				"--collection",
				"remediations",
				"--model-id",
				"code-model",
			},
			baseArgs...),
		append(
			[]string{
				"hybrid-search",
				"--uri",
				filepath.Join(root, ".coding-ethos", "vectors.db"),
				"--text",
				"split",
				"--vector",
				"0.1,0.2",
				"--model-id",
				"code-model",
			},
			baseArgs...),
		append([]string{"search", "--text", "split"}, baseArgs...),
	}
}

func TestSARIFCommands(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dbPath := filepath.Join(root, ".coding-ethos", "code-intel.db")
	sarifPath := filepath.Join(root, "policy.sarif")

	payload := `{
  "version": "2.1.0",
  "runs": [{
    "tool": {"driver": {"name": "coding-ethos", "rules": [{"id": "policy.one"}]}},
    "automationDetails": {"id": "policy"},
    "results": [{
      "ruleId": "policy.one",
      "level": "error",
      "message": {"text": "split large file"},
      "locations": [{
        "physicalLocation": {
          "artifactLocation": {"uri": "pkg/app.py"},
          "region": {"startLine": 4}
        }
      }],
      "partialFingerprints": {
        "findingId": "finding-1",
        "remediationId": "rem-1",
        "policyId": "policy.one",
        "skillId": "skill-one"
      }
    }]
  }]
}`

	err := os.WriteFile(sarifPath, []byte(payload), 0o600)
	if err != nil {
		t.Fatalf("write sarif: %v", err)
	}

	ctx := context.Background()

	baseArgs := []string{"--root", root, "--db", dbPath}

	err = run(ctx, append([]string{"ingest-sarif", "--file", sarifPath}, baseArgs...))
	if err != nil {
		t.Fatalf("ingest-sarif returned error: %v", err)
	}

	err = run(
		ctx,
		append([]string{"sarif-results", "--policy-id", "policy.one"}, baseArgs...),
	)
	if err != nil {
		t.Fatalf("sarif-results returned error: %v", err)
	}

	err = run(
		ctx,
		append([]string{"repeated-failures", "--policy-id", "policy.one"}, baseArgs...),
	)
	if err != nil {
		t.Fatalf("repeated-failures returned error: %v", err)
	}
}

func TestIngestTracesAndHookUsageCommands(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dbPath := filepath.Join(root, ".coding-ethos", "code-intel.db")

	traceDir := filepath.Join(root, ".coding-ethos", "hook-runs", "run-a")

	err := os.MkdirAll(traceDir, 0o700)
	if err != nil {
		t.Fatalf("create trace dir: %v", err)
	}

	payload := `{
  "trace_id": "hook-trace-a",
  "tracking_id": "deny-a",
  "provider": "codex",
  "event": "PreToolUse",
  "tool": "Bash",
  "cwd": "/repo",
  "command": {
    "sha256": "` + strings.Repeat("a", 64) + `",
    "shape_sha256": "` + strings.Repeat("b", 64) + `",
    "preview": "git reset --hard"
  },
  "status": "denied",
  "decision": "block",
  "recorded_at_utc": "2026-01-01T00:00:00Z",
  "diagnostics": [{
    "policy_id": "git.destructive_command",
    "skill_id": "safe-git-workflow",
    "message": "destructive git command",
    "severity": "block"
  }]
}`

	err = os.WriteFile(filepath.Join(traceDir, "event.json"), []byte(payload), 0o600)
	if err != nil {
		t.Fatalf("write hook trace: %v", err)
	}

	ctx := context.Background()

	baseArgs := []string{"--root", root, "--db", dbPath}

	err = run(ctx, append([]string{"ingest-traces"}, baseArgs...))
	if err != nil {
		t.Fatalf("ingest-traces returned error: %v", err)
	}

	err = run(
		ctx,
		append(
			[]string{"hook-usage", "--provider", "codex", "--status", "denied"},
			baseArgs...),
	)
	if err != nil {
		t.Fatalf("hook-usage returned error: %v", err)
	}
}

func TestParseVectorHelpers(t *testing.T) {
	t.Parallel()

	vector, err := parseOptionalVector("1.5, 2, , 3")
	if err != nil {
		t.Fatalf("parseOptionalVector returned error: %v", err)
	}

	if len(vector) != 3 || vector[0] != 1.5 || vector[2] != 3 {
		t.Fatalf("vector = %#v", vector)
	}

	empty, err := parseOptionalVector(" ")
	if err != nil || empty != nil {
		t.Fatalf("empty vector = %#v, %v", empty, err)
	}

	for _, value := range []string{"", " , ", "1, nope"} {
		_, err := parseVector(value)
		if err == nil || !strings.Contains(err.Error(), "vector") {
			t.Fatalf("parseVector(%q) error = %v", value, err)
		}
	}
}
