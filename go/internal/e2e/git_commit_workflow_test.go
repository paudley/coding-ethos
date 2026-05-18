// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/e2e"
)

func TestManagedGitCommitWorkflowPreservesUserFacingFailures(t *testing.T) {
	repo := preparedManagedGitCommitRepo(t)

	beforeSuccess := repoHead(t, repo)
	repo.Touch(t, "pkg/commit_success.py", cleanCommitPython())
	repo.Git(t, "add", "pkg/commit_success.py")
	success := managedGit(t, repo, "commit", "-m", "test(repo): add clean commit fixture")
	success.RequireExit(t, 0)
	afterSuccess := repoHead(t, repo)
	if beforeSuccess == afterSuccess {
		t.Fatalf("successful managed git commit did not advance HEAD: %s", beforeSuccess)
	}
	assertNoCommitHeadPolicyLeak(t, success)

	beforeFailure := repoHead(t, repo)
	repo.ResetTraces(t)
	repo.Touch(t, "pkg/module_doc_failure.py", moduleDocFailurePython())
	repo.Git(t, "add", "pkg/module_doc_failure.py")
	failure := managedGit(
		t,
		repo,
		"commit",
		"-m",
		"test(repo): add policy failure fixture",
	)
	if failure.Code == 0 {
		t.Fatalf("managed git commit unexpectedly succeeded:\n%s", failure.Combined)
	}
	afterFailure := repoHead(t, repo)
	if beforeFailure != afterFailure {
		t.Fatalf(
			"failed managed git commit advanced HEAD: before=%s after=%s\n%s",
			beforeFailure,
			afterFailure,
			failure.Combined,
		)
	}
	for _, want := range []string{
		"tool: module_docstrings",
		"pkg/module_doc_failure.py",
		"python.module_docs",
		"module docstring has",
	} {
		failure.RequireContains(t, want)
	}
	assertNoCommitHeadPolicyLeak(t, failure)

	trace := retainedTraceContent(t, repo)
	for _, want := range []string{
		`"scope": "tool:module_docstrings"`,
		`"file": "pkg/module_doc_failure.py"`,
		`"policy_id": "python.module_docs"`,
		`"message": "module docstring has`,
	} {
		if !strings.Contains(trace, want) {
			t.Fatalf("failed commit trace missing %q:\n%s", want, trace)
		}
	}
}

func preparedManagedGitCommitRepo(t *testing.T) e2e.Repo {
	t.Helper()

	repo := preparedManagedLintRepo(t)
	install := repo.CodingEthosRun(t, "parent-install", "--repo", repo.Root)
	install.RequireExit(t, 0)

	status := repo.Git(t, "status", "--short")
	status.RequireExit(t, 0)
	if strings.TrimSpace(status.Stdout) != "" {
		repo.Git(t, "add", ".")
		baseline := repo.Git(
			t,
			"commit",
			"-m",
			"test(repo): install coding ethos runtime",
		)
		baseline.RequireExit(t, 0)
	}
	installManagedGitEntrypoints(t, repo)

	repo.ResetTraces(t)

	return repo
}

func cleanCommitPython() string {
	return strings.Join([]string{
		"# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>",
		"# SPDX-License-Identifier: AGPL-3.0-only",
		"",
		`"""Clean fixture used by the managed git commit e2e workflow.`,
		"",
		"The module gives the successful commit path a real Python file to stage.",
		"It stays small so hook failures point at runtime issues.",
		`"""`,
		"",
		"",
		"def committed_answer() -> int:",
		`    """Return the value committed by the successful workflow."""`,
		"    return 7",
		"",
	}, "\n")
}

func moduleDocFailurePython() string {
	return strings.Join([]string{
		"# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>",
		"# SPDX-License-Identifier: AGPL-3.0-only",
		"",
		`"""Too short."""`,
		"",
		"",
		"def failing_answer() -> int:",
		`    """Return a stable value."""`,
		"    return 11",
		"",
	}, "\n")
}

func installManagedGitEntrypoints(t *testing.T, repo e2e.Repo) {
	t.Helper()

	hooksDir := repo.Git(
		t,
		"rev-parse",
		"--path-format=absolute",
		"--git-path",
		"hooks",
	)
	hooksDir.RequireExit(t, 0)

	runner := filepath.Join(repo.EthosRoot, "bin", "coding-ethos-run")
	toolchain := filepath.Join(repo.EthosRoot, "bin", "coding-ethos-toolchain")
	binDir := filepath.Join(repo.EthosRoot, "bin")

	hooks := e2e.Run(
		t,
		repo.Root,
		toolchain,
		"install-git-hooks",
		"--hooks-dir",
		strings.TrimSpace(hooksDir.Stdout),
		"--runner",
		runner,
	)
	hooks.RequireExit(t, 0)

	shim := e2e.Run(
		t,
		repo.Root,
		toolchain,
		"install-git-shim",
		"--dest-dir",
		binDir,
		"--real-git",
		"/usr/bin/git",
		"--runner",
		runner,
	)
	shim.RequireExit(t, 0)
}

func retainedTraceContent(t *testing.T, repo e2e.Repo) string {
	t.Helper()

	var content strings.Builder
	for _, trace := range repo.TraceFiles(t) {
		payload, err := os.ReadFile(trace)
		if err != nil {
			t.Fatalf("read trace %s: %v", trace, err)
		}
		content.Write(payload)
		content.WriteByte('\n')
	}

	return content.String()
}

func assertNoCommitHeadPolicyLeak(t *testing.T, result e2e.CommandResult) {
	t.Helper()

	for _, unwanted := range []string{
		"git.commit_head_advanced",
		"commit head did not advance",
	} {
		if strings.Contains(result.Combined, unwanted) {
			t.Fatalf("managed git output leaked %q:\n%s", unwanted, result.Combined)
		}
	}
}

func managedGit(t *testing.T, repo e2e.Repo, args ...string) e2e.CommandResult {
	t.Helper()

	managedPath := filepath.Join(repo.EthosRoot, "bin") +
		string(os.PathListSeparator) +
		os.Getenv("PATH")

	return e2e.RunWithEnv(
		t,
		repo.Root,
		map[string]string{"PATH": managedPath},
		append([]string{
			"git",
		}, args...)...)
}

func repoHead(t *testing.T, repo e2e.Repo) string {
	t.Helper()

	result := repo.Git(t, "rev-parse", "HEAD")
	result.RequireExit(t, 0)

	return strings.TrimSpace(result.Stdout)
}
