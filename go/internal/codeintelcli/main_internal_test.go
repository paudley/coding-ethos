// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintelcli

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/realgit"
)

var stdoutCaptureMu sync.Mutex

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
	dbPath := filepath.Join(root, ".coding-ethos", "code-intel.duckdb")

	err := run(context.Background(), []string{"stats", "--root", root, "--db", dbPath})
	if err != nil {
		t.Fatalf("stats command returned error: %v", err)
	}
}

func TestGitSignalsCommandRefreshesRealRepository(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	runCodeIntelCLIGit(t, ctx, root, "init", "--initial-branch", "main")
	runCodeIntelCLIGit(t, ctx, root, "config", "user.name", "Test User")
	runCodeIntelCLIGit(t, ctx, root, "config", "user.email", "test@example.invalid")
	runCodeIntelCLIGit(t, ctx, root, "config", "commit.gpgsign", "false")

	sourcePath := filepath.Join(root, "pkg", "app.go")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o700); err != nil {
		t.Fatalf("create source dir: %v", err)
	}
	if err := os.WriteFile(sourcePath, []byte("package pkg\n"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	runCodeIntelCLIGit(t, ctx, root, "add", "pkg/app.go")
	runCodeIntelCLIGit(t, ctx, root, "commit", "-m", "test(repo): add app")

	dbPath := filepath.Join(root, ".coding-ethos", "code-intel.duckdb")
	err := run(ctx, []string{
		"git-signals",
		"--root", root,
		"--db", dbPath,
		"--path", "pkg/app.go",
	})
	if err != nil {
		t.Fatalf("git-signals command returned error: %v", err)
	}
}

func TestDownstreamAnalysisDoesNotRequireExistingStore(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	err := run(context.Background(), []string{"downstream-analysis", "--root", root})
	if err != nil {
		t.Fatalf("downstream-analysis command returned error: %v", err)
	}

	stateDir := filepath.Join(root, ".coding-ethos")
	if _, statErr := os.Stat(stateDir); !os.IsNotExist(statErr) {
		t.Fatalf("downstream-analysis created state dir %q: %v", stateDir, statErr)
	}

	dbPath := filepath.Join(stateDir, "code-intel.duckdb")
	if _, statErr := os.Stat(dbPath); !os.IsNotExist(statErr) {
		t.Fatalf("downstream-analysis created store %q: %v", dbPath, statErr)
	}
}

func runCodeIntelCLIGit(
	t *testing.T,
	ctx context.Context,
	root string,
	args ...string,
) {
	t.Helper()

	gitPath, err := realgit.Resolve(ctx, "git")
	if err != nil {
		t.Fatalf("resolve git: %v", err)
	}

	command := exec.CommandContext(ctx, gitPath, args...)
	command.Dir = root
	command.Env = append(
		os.Environ(),
		"GIT_AUTHOR_NAME=Test User",
		"GIT_AUTHOR_EMAIL=test@example.invalid",
		"GIT_COMMITTER_NAME=Test User",
		"GIT_COMMITTER_EMAIL=test@example.invalid",
	)

	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func TestVectorStatsCreatesDuckDBVectorStore(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	err := run(context.Background(), []string{"vector-stats", "--root", root})
	if err != nil {
		t.Fatalf("vector-stats command returned error: %v", err)
	}
}

func TestHealthCommandReturnsSnapshot(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, ".coding-ethos", "code-intel.duckdb")

	var runErr error
	output := captureStdout(t, func() {
		runErr = run(context.Background(), []string{
			"health",
			"--root", root,
			"--db", dbPath,
			"--refresh",
		})
	})
	if runErr != nil {
		t.Fatalf("health command returned error: %v", runErr)
	}

	for _, want := range []string{
		`"kind": "code_intel_health"`,
		`"health": {`,
		`"total_health_score": 100`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("health command output missing %q:\n%s", want, output)
		}
	}
}

func captureStdout(t *testing.T, runCommand func()) string {
	t.Helper()

	stdoutCaptureMu.Lock()
	defer stdoutCaptureMu.Unlock()

	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	os.Stdout = writer
	defer func() {
		os.Stdout = original
	}()

	runCommand()

	if err := writer.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}

	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read stdout pipe: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close stdout reader: %v", err)
	}

	return string(output)
}

func TestHealthCommandAcceptsDirectoryPathFilter(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dbPath := filepath.Join(root, ".coding-ethos", "code-intel.duckdb")

	err := run(context.Background(), []string{
		"health",
		"--root", root,
		"--db", dbPath,
		"--refresh",
		"--path", "pkg",
	})
	if err != nil {
		t.Fatalf("health command returned error: %v", err)
	}
}

func TestIndexCodeAndCodeChunksCommands(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dbPath := filepath.Join(root, ".coding-ethos", "code-intel.duckdb")

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

	nestedPath := filepath.Join(root, "cmd", "nested", "deep.go")
	err = os.MkdirAll(filepath.Dir(nestedPath), 0o700)
	if err != nil {
		t.Fatalf("create nested source dir: %v", err)
	}

	err = os.WriteFile(nestedPath, []byte(`package nested

func deep() {}
`), 0o600)
	if err != nil {
		t.Fatalf("write nested source: %v", err)
	}

	ctx := context.Background()

	err = run(ctx, []string{"index-code", "--root", root, "--db", dbPath, "cmd"})
	if err != nil {
		t.Fatalf("index-code command returned error: %v", err)
	}

	err = run(ctx, []string{
		"anatomy-map", "--root", root, "--db", dbPath,
		"--path", "cmd", "--symbols-per-file", "3", "--format", "toon",
	})
	if err != nil {
		t.Fatalf("anatomy-map command returned error: %v", err)
	}

	listingPath := filepath.Join(root, "listing.txt")
	err = os.WriteFile(listingPath, []byte("app.go\n"), 0o600)
	if err != nil {
		t.Fatalf("write listing: %v", err)
	}

	err = run(ctx, []string{
		"enrich-listing", "--root", root, "--db", dbPath,
		"--command", "ls -la cmd", "--listing-file", listingPath,
		"--symbols-per-file", "3",
	})
	if err != nil {
		t.Fatalf("enrich-listing command returned error: %v", err)
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

	err = run(ctx, []string{
		"repo-map", "--root", root, "--db", dbPath,
		"--path", "cmd/app.go",
	})
	if err != nil {
		t.Fatalf("repo-map command returned error: %v", err)
	}

	err = run(ctx, []string{
		"compact-context", "--root", root, "--db", dbPath,
		"--path", "cmd/app.go",
	})
	if err != nil {
		t.Fatalf("compact-context command returned error: %v", err)
	}

	err = run(ctx, []string{"code-context", "--root", root, "--db", dbPath})
	if err == nil ||
		!strings.Contains(err.Error(), "--chunk-id") {
		t.Fatalf("code-context without identifier error = %v", err)
	}
}

func TestListingTargetPathParsesSupportedCommands(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		path    string
		command string
		want    string
	}{
		{
			name:    "explicit path wins",
			path:    "pkg",
			command: "ls docs",
			want:    "pkg",
		},
		{
			name:    "ls command path",
			command: "ls -la cmd",
			want:    "cmd",
		},
		{
			name:    "tree command path",
			command: "tree -L 2 go/internal",
			want:    "go/internal",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := listingTargetPath(test.path, test.command)
			if err != nil {
				t.Fatalf("listingTargetPath returned error: %v", err)
			}

			if got != test.want {
				t.Fatalf("path = %q, want %q", got, test.want)
			}
		})
	}
}

func TestListingInvocationPreservesCommandShapeWithExplicitPath(t *testing.T) {
	t.Parallel()

	invocation, err := listingInvocation("pkg", "tree -L 2 docs")
	if err != nil {
		t.Fatalf("listingInvocation returned error: %v", err)
	}

	if invocation.Path != "pkg" {
		t.Fatalf("path = %q, want pkg", invocation.Path)
	}

	if !invocation.Recursive {
		t.Fatalf("expected recursive invocation: %#v", invocation)
	}

	if invocation.MaxDepth != 2 {
		t.Fatalf("max depth = %d, want 2", invocation.MaxDepth)
	}
}

func TestRepoRelativePathNormalizesInsideRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(root, "pkg")

	got, err := repoRelativePath(root, target)
	if err != nil {
		t.Fatalf("repoRelativePath returned error: %v", err)
	}

	if got != "pkg" {
		t.Fatalf("path = %q, want pkg", got)
	}

	got, err = repoRelativePath(root, ".")
	if err != nil {
		t.Fatalf("repoRelativePath root returned error: %v", err)
	}

	if got != "." {
		t.Fatalf("root path = %q, want .", got)
	}
}

func TestRepoRelativePathRejectsOutsideRoot(t *testing.T) {
	t.Parallel()

	_, err := repoRelativePath(t.TempDir(), filepath.Join(t.TempDir(), "other"))
	if err == nil {
		t.Fatal("expected outside-root path error")
	}
}

func TestListingTargetPathRejectsUnsupportedCommands(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		command string
	}{
		{name: "missing target", command: ""},
		{name: "unsupported tool", command: "find . -maxdepth 1"},
		{name: "multiple shell commands", command: "ls cmd; ls docs"},
		{name: "multiple listing targets", command: "ls cmd docs"},
		{name: "dynamic shell target", command: "ls $DIR"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := listingTargetPath("", test.command)
			if err == nil {
				t.Fatal("expected listingTargetPath error")
			}
		})
	}
}

func TestReadListingInputReadsFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "listing.txt")
	err := os.WriteFile(path, []byte("app.go\n"), 0o600)
	if err != nil {
		t.Fatalf("write listing: %v", err)
	}

	got, err := readListingInput(path)
	if err != nil {
		t.Fatalf("readListingInput returned error: %v", err)
	}

	if got != "app.go\n" {
		t.Fatalf("listing = %q", got)
	}

	_, err = readListingInput(filepath.Join(t.TempDir(), "missing.txt"))
	if err == nil {
		t.Fatal("expected missing listing file error")
	}
}

func TestRecordAndQueryCommands(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755)
	if err != nil {
		t.Fatalf("mkdir pkg: %v", err)
	}

	err = os.WriteFile(
		filepath.Join(root, "pkg", "app.py"),
		[]byte("print('hello')\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write app.py: %v", err)
	}

	dbPath := filepath.Join(root, ".coding-ethos", "code-intel.duckdb")
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
			"proxy-file-read",
			"--event-id", "proxy-file-read-1",
			"--session-id", "proxy-session-1",
			"--provider", "codex",
			"--tool", "Read",
			"--path", "pkg/app.py",
		}, baseArgs...),
		append([]string{
			"proxy-file-read",
			"--event-id", "proxy-file-read-2",
			"--session-id", "proxy-session-1",
			"--provider", "codex",
			"--tool", "Read",
			"--path", "pkg/app.py",
		}, baseArgs...),
		append([]string{
			"record-embedding",
			"--backend", "duckdb-vss",
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
	dbPath := filepath.Join(root, ".coding-ethos", "code-intel.duckdb")
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
	dbPath := filepath.Join(root, ".coding-ethos", "code-intel.duckdb")

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
