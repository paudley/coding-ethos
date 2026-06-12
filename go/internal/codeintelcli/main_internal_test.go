// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintelcli

import (
	"context"
	"encoding/json"
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

func TestWorkspaceCommandsScanAndStatus(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	repo := filepath.Join(root, "api")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	runCodeIntelCLIGit(t, ctx, repo, "init")
	runCodeIntelCLIGit(t, ctx, repo, "config", "user.name", "Test User")
	runCodeIntelCLIGit(t, ctx, repo, "config", "user.email", "test@example.invalid")
	if err := os.WriteFile(
		filepath.Join(repo, "README.md"),
		[]byte("# api\n"),
		0o600,
	); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runCodeIntelCLIGit(t, ctx, repo, "add", ".")
	runCodeIntelCLIGit(t, ctx, repo, "commit", "-m", "initial api")

	var scanErr error
	scanOutput := captureStdout(t, func() {
		scanErr = run(ctx, []string{"workspace", "scan", "--root", root})
	})
	if scanErr != nil {
		t.Fatalf("workspace scan returned error: %v", scanErr)
	}
	if !strings.Contains(scanOutput, `"alias": "api"`) ||
		!strings.Contains(scanOutput, `.coding-ethos/code-intel.duckdb`) {
		t.Fatalf("workspace scan output missing repo fields:\n%s", scanOutput)
	}

	var statusErr error
	statusOutput := captureStdout(t, func() {
		statusErr = run(ctx, []string{"workspace", "status", "--root", root})
	})
	if statusErr != nil {
		t.Fatalf("workspace status returned error: %v", statusErr)
	}
	if !strings.Contains(statusOutput, `"repos": 1`) ||
		!strings.Contains(statusOutput, `"store_available": false`) {
		t.Fatalf("workspace status output missing expected status:\n%s", statusOutput)
	}
}

func TestWorkspaceListRejectsUnsupportedFormatWithWorkspaceError(t *testing.T) {
	t.Parallel()

	err := writeWorkspaceOutput(nil, "xml")
	if err == nil {
		t.Fatalf("expected unsupported workspace format error")
	}
	if !strings.Contains(err.Error(), "unknown workspace output format") {
		t.Fatalf("error = %q, want workspace format message", err)
	}
}

func TestDecisionsCommandsRecordListAndReportHealth(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, ".coding-ethos", "code-intel.duckdb")

	var addErr error
	addOutput := captureStdout(t, func() {
		addErr = run(context.Background(), []string{
			"decisions",
			"add",
			"--root", root,
			"--db", dbPath,
			"--title", "Use managed caches",
			"--rationale", "Managed caches keep tool startup deterministic.",
			"--path", "pkg/cache.go",
		})
	})
	if addErr != nil {
		t.Fatalf("decisions add returned error: %v", addErr)
	}
	if !strings.Contains(addOutput, `"kind": "code_intel.decisions.v1"`) ||
		!strings.Contains(addOutput, `"pkg/cache.go"`) {
		t.Fatalf("decisions add output missing stable fields:\n%s", addOutput)
	}

	var addPayload struct {
		Decisions []struct {
			ID string `json:"id"`
		} `json:"decisions"`
	}
	if err := json.Unmarshal([]byte(addOutput), &addPayload); err != nil {
		t.Fatalf("decode decisions add output: %v", err)
	}
	if len(addPayload.Decisions) != 1 || addPayload.Decisions[0].ID == "" {
		t.Fatalf("decisions add payload missing ID: %#v", addPayload)
	}

	var linkErr error
	linkOutput := captureStdout(t, func() {
		linkErr = run(context.Background(), []string{
			"decisions",
			"link",
			"--root", root,
			"--db", dbPath,
			"--id", addPayload.Decisions[0].ID,
			"--path", "pkg/cache.go",
			"--symbol-path", "Cache.Start",
		})
	})
	if linkErr != nil {
		t.Fatalf("decisions link returned error: %v", linkErr)
	}
	if !strings.Contains(linkOutput, `"kind": "code_intel.decisions.link.v1"`) ||
		!strings.Contains(linkOutput, `"pkg/cache.go"`) {
		t.Fatalf("decisions link output missing stable fields:\n%s", linkOutput)
	}

	var listErr error
	listOutput := captureStdout(t, func() {
		listErr = run(context.Background(), []string{
			"decisions",
			"list",
			"--root", root,
			"--db", dbPath,
			"--query", "deterministic",
			"--path", "pkg/cache.go",
			"--format", "json",
		})
	})
	if listErr != nil {
		t.Fatalf("decisions list returned error: %v", listErr)
	}
	if !strings.Contains(listOutput, `"decisions": [`) ||
		!strings.Contains(listOutput, `"Use managed caches"`) {
		t.Fatalf("decisions list output missing record:\n%s", listOutput)
	}

	var humanListErr error
	humanListOutput := captureStdout(t, func() {
		humanListErr = run(context.Background(), []string{
			"decisions",
			"list",
			"--root", root,
			"--db", dbPath,
			"--path", "pkg/cache.go",
			"--format", "human",
		})
	})
	if humanListErr != nil {
		t.Fatalf("decisions human list returned error: %v", humanListErr)
	}
	if !strings.Contains(humanListOutput, "pkg/cache.go#Cache.Start") {
		t.Fatalf("decisions human list missing linked symbol:\n%s", humanListOutput)
	}

	var healthErr error
	healthOutput := captureStdout(t, func() {
		healthErr = run(context.Background(), []string{
			"decisions",
			"health",
			"--root", root,
			"--db", dbPath,
			"--path", "pkg/cache.go",
		})
	})
	if healthErr != nil {
		t.Fatalf("decisions health returned error: %v", healthErr)
	}
	if !strings.Contains(healthOutput, `"kind": "code_intel.decision_health.v1"`) ||
		!strings.Contains(healthOutput, `"decision_count": 1`) {
		t.Fatalf("decisions health output missing summary:\n%s", healthOutput)
	}
}

func TestDecisionsImportCommandRecordsDecisionDocument(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, ".coding-ethos", "code-intel.duckdb")
	decisionPath := filepath.Join(root, "docs", "decisions", "startup.md")
	writeCodeIntelCLIFile(t, decisionPath, []byte(`---
coding_ethos_decision: true
title: Use explicit startup
rationale: Startup should fail before serving requests.
affected_paths:
  - pkg/app.go
---
`))

	var importErr error
	importOutput := captureStdout(t, func() {
		importErr = run(context.Background(), []string{
			"decisions",
			"import",
			"--root", root,
			"--db", dbPath,
			"docs/decisions",
		})
	})
	if importErr != nil {
		t.Fatalf("decisions import returned error: %v", importErr)
	}
	if !strings.Contains(importOutput, `"kind": "code_intel.decision_import.v1"`) ||
		!strings.Contains(importOutput, `"decisions_imported": 1`) {
		t.Fatalf("decisions import output missing summary:\n%s", importOutput)
	}

	var listErr error
	listOutput := captureStdout(t, func() {
		listErr = run(context.Background(), []string{
			"decisions",
			"list",
			"--root", root,
			"--db", dbPath,
			"--path", "pkg/app.go",
			"--format", "json",
		})
	})
	if listErr != nil {
		t.Fatalf("decisions list returned error: %v", listErr)
	}
	if !strings.Contains(listOutput, `"Use explicit startup"`) {
		t.Fatalf("decisions list missing imported decision:\n%s", listOutput)
	}
}

func TestDecisionsListRejectsUnsupportedFormatWithDecisionError(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dbPath := filepath.Join(root, ".coding-ethos", "code-intel.duckdb")

	err := run(context.Background(), []string{
		"decisions",
		"list",
		"--root", root,
		"--db", dbPath,
		"--format", "xml",
	})
	if err == nil {
		t.Fatalf("expected unsupported decisions format error")
	}
	if !strings.Contains(err.Error(), "unsupported decisions format") {
		t.Fatalf("error = %q, want decisions format message", err)
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

func TestSessionSnapshotCommandEmitsJSONAndTOON(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dbPath := filepath.Join(root, ".coding-ethos", "code-intel.duckdb")

	var jsonErr error
	jsonOutput := captureStdout(t, func() {
		jsonErr = run(context.Background(), []string{
			"session-snapshot",
			"--root", root,
			"--db", dbPath,
		})
	})
	if jsonErr != nil {
		t.Fatalf("session-snapshot JSON returned error: %v", jsonErr)
	}
	if !strings.Contains(jsonOutput, `"kind": "coding_ethos.session.v1"`) ||
		!strings.Contains(jsonOutput, `"session": {`) {
		t.Fatalf("session-snapshot JSON missing stable fields:\n%s", jsonOutput)
	}

	var toonErr error
	toonOutput := captureStdout(t, func() {
		toonErr = run(context.Background(), []string{
			"session-snapshot",
			"--root", root,
			"--db", dbPath,
			"--format", "toon",
		})
	})
	if toonErr != nil {
		t.Fatalf("session-snapshot TOON returned error: %v", toonErr)
	}
	if !strings.Contains(toonOutput, "kind: coding_ethos.session.v1") ||
		!strings.Contains(toonOutput, "session_source: fallback") {
		t.Fatalf("session-snapshot TOON missing stable fields:\n%s", toonOutput)
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

	docPath := filepath.Join(root, "docs", "architecture.md")
	err = os.MkdirAll(filepath.Dir(docPath), 0o700)
	if err != nil {
		t.Fatalf("create docs dir: %v", err)
	}

	err = os.WriteFile(docPath, []byte(`# Architecture

The command implementation is cmd/app.go.

## Decision

Use cmd/app.go#runApp as the documented entry point.
`), 0o600)
	if err != nil {
		t.Fatalf("write docs: %v", err)
	}

	ctx := context.Background()

	err = run(ctx, []string{"index-code", "--root", root, "--db", dbPath, "cmd", "docs"})
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

	var graphReportMarkdownErr error
	graphReportMarkdown := captureStdout(t, func() {
		graphReportMarkdownErr = run(ctx, []string{
			"graph-report", "--root", root, "--db", dbPath,
			"--path", "cmd", "--format", "human",
		})
	})
	if graphReportMarkdownErr != nil {
		t.Fatalf("graph-report human returned error: %v", graphReportMarkdownErr)
	}
	if !strings.Contains(
		graphReportMarkdown,
		"kind: code_intel.graph_report.v1",
	) ||
		!strings.Contains(graphReportMarkdown, "cmd/app.go") ||
		!strings.Contains(graphReportMarkdown, "EXTRACTED") {
		t.Fatalf("graph-report human missing expected content:\n%s", graphReportMarkdown)
	}

	var graphReportTOONErr error
	graphReportTOON := captureStdout(t, func() {
		graphReportTOONErr = run(ctx, []string{
			"graph-report", "--root", root, "--db", dbPath,
			"--path", "cmd", "--format", "toon",
		})
	})
	if graphReportTOONErr != nil {
		t.Fatalf("graph-report TOON returned error: %v", graphReportTOONErr)
	}
	if !strings.Contains(graphReportTOON, "kind: code_intel.graph_report.v1") ||
		!strings.Contains(graphReportTOON, "central_files[") ||
		!strings.Contains(graphReportTOON, "central_nodes[") ||
		!strings.Contains(graphReportTOON, "communities[") ||
		!strings.Contains(graphReportTOON, "document_links[") ||
		!strings.Contains(graphReportTOON, "cmd/app.go") ||
		!strings.Contains(graphReportTOON, "provenance") ||
		!strings.Contains(graphReportTOON, "EXTRACTED") {
		t.Fatalf("graph-report TOON missing expected content:\n%s", graphReportTOON)
	}

	err = run(ctx, []string{
		"centrality", "--root", root, "--db", dbPath,
		"--path", "cmd", "--format", "json",
	})
	if err != nil {
		t.Fatalf("centrality returned error: %v", err)
	}

	err = run(ctx, []string{
		"surprises", "--root", root, "--db", dbPath,
		"--path", "cmd", "--format", "json",
	})
	if err != nil {
		t.Fatalf("surprises returned error: %v", err)
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

func writeCodeIntelCLIFile(t *testing.T, path string, payload []byte) {
	t.Helper()

	err := os.MkdirAll(filepath.Dir(path), 0o700)
	if err != nil {
		t.Fatalf("create fixture dir: %v", err)
	}

	err = os.WriteFile(path, payload, 0o600)
	if err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}
