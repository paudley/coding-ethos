// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package e2e_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/e2e"
)

func TestManagedGitCommitWorkflowPreservesUserFacingFailures(t *testing.T) {
	t.Parallel()

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
		"MODULE DOCSTRING CHECK FAILED",
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

	if testing.Short() {
		t.Skip("real managed git e2e is skipped in short mode")
	}

	if runtime.GOOS == windowsGOOS {
		t.Skip("real managed git e2e uses POSIX paths")
	}

	sourceRoot := repoRootFromWorkingDirectory(t)
	e2e.RequireRuntime(t, sourceRoot)
	repo := e2e.FromReference(t, sourceRoot, "policy-lint-basic")
	repo.EthosRoot = repoLocalCoverageRuntime(t, repo)
	syncManagedGitGeneratedFixtures(t, repo)
	repo.Touch(t, "repo_config.yaml", managedGitCommitRepoConfig())
	installManagedGitPolicyArtifacts(t, repo)
	installManagedGitEntrypoints(t, repo)

	repo.ResetTraces(t)

	return repo
}

func syncManagedGitGeneratedFixtures(t *testing.T, repo e2e.Repo) {
	t.Helper()

	sync := repo.CodingEthosRun(
		t,
		"policy",
		"sync-tool-configs",
		"--ethos-root",
		repo.EthosRoot,
		"--repo",
		repo.Root,
	)
	sync.RequireExit(t, 0)

	syncGemini := repo.CodingEthosRun(
		t,
		"policy",
		"sync-gemini-prompts",
		"--ethos-root",
		repo.EthosRoot,
		"--repo",
		repo.Root,
		"--primary",
		filepath.Join(repo.EthosRoot, "coding_ethos.yml"),
		"--repo-ethos",
		filepath.Join(repo.EthosRoot, "repo_ethos.yml"),
	)
	syncGemini.RequireExit(t, 0)

	status := repo.Git(t, "status", "--porcelain")
	status.RequireExit(t, 0)
	if strings.TrimSpace(status.Stdout) == "" {
		return
	}

	repo.Git(t, "add", ".").RequireExit(t, 0)
	repo.Git(t, "commit", "-m", "test(repo): sync generated fixtures").RequireExit(t, 0)
}

func installManagedGitPolicyArtifacts(t *testing.T, repo e2e.Repo) {
	t.Helper()

	gitCommonDir := repo.Git(
		t,
		"rev-parse",
		"--path-format=absolute",
		"--git-common-dir",
	)
	gitCommonDir.RequireExit(t, 0)

	policyDir := filepath.Join(
		strings.TrimSpace(gitCommonDir.Stdout),
		"coding-ethos-hooks",
		"policy",
	)
	if err := os.MkdirAll(policyDir, 0o700); err != nil {
		t.Fatalf("create managed git policy artifact dir: %v", err)
	}

	for _, name := range []string{"policy-bundle.json", "policy-metadata.json"} {
		copyRuntimeFile(
			t,
			filepath.Join(repo.EthosRoot, "build", "policy", name),
			filepath.Join(policyDir, name),
		)
	}
}

func managedGitCommitRepoConfig() string {
	return strings.Join([]string{
		"hooks:",
		"  enabled_groups:",
		"    - python-policy",
		"git:",
		"  signed_operations:",
		"    enabled: false",
		"python:",
		"  optional_returns:",
		"    enabled: false",
		"  comment_suppressions:",
		"    enabled: false",
		"  direct_imports:",
		"    enabled: false",
		"  util_centralization:",
		"    enabled: false",
		"  security_patterns:",
		"    enabled: false",
		"  catch_and_silence:",
		"    enabled: false",
		"  conditional_imports:",
		"    enabled: false",
		"  type_checking_imports:",
		"    enabled: false",
		"  structured_logging:",
		"    enabled: false",
		"  sql_centralization:",
		"    enabled: false",
		"  version_consistency:",
		"    enabled: false",
		"  pytest_gate:",
		"    enabled: false",
		"  file_docstrings:",
		"    enabled: true",
		"lint:",
		"  source_roots:",
		"    - pkg",
		"",
	}, "\n")
}

func cleanCommitPython() string {
	return strings.Join([]string{
		"# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>",
		"# SPDX-License-Identifier: AGPL-3.0-only",
		"",
		`"""Clean fixture used by the managed git commit e2e workflow.`,
		"",
		"The module gives the successful commit path a staged Python file.",
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
	hooksRoot := strings.TrimSpace(hooksDir.Stdout)

	runner := filepath.Join(repo.EthosRoot, "bin", "coding-ethos-run")
	toolchain := filepath.Join(repo.EthosRoot, "bin", "coding-ethos-toolchain")
	binDir := managedGitBinDir(t, repo)

	hooks := e2e.Run(
		t,
		repo.Root,
		toolchain,
		"install-git-hooks",
		"--hooks-dir",
		hooksRoot,
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
		realGitPath(t),
		"--runner",
		runner,
	)
	shim.RequireExit(t, 0)
}

func managedGitBinDir(t *testing.T, repo e2e.Repo) string {
	t.Helper()

	path := filepath.Join(repo.Root, ".git", "coding-ethos-e2e-bin")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("create managed git shim bin dir: %v", err)
	}

	return path
}

func repoLocalCoverageRuntime(t *testing.T, repo e2e.Repo) string {
	t.Helper()

	if strings.TrimSpace(os.Getenv("GOCOVERDIR")) == "" {
		return repo.EthosRoot
	}

	runtimeRoot := filepath.Join(repo.Root, ".git", "coding-ethos-e2e-runtime")
	binRoot := filepath.Join(runtimeRoot, "bin")
	err := os.MkdirAll(binRoot, 0o700)
	if err != nil {
		t.Fatalf("create repo-local coverage runtime: %v", err)
	}

	binaries, err := filepath.Glob(filepath.Join(repo.EthosRoot, "bin", "*"))
	if err != nil {
		t.Fatalf("list instrumented runtime binaries: %v", err)
	}
	for _, source := range binaries {
		info, statErr := os.Stat(source)
		if statErr != nil {
			t.Fatalf("stat repo-local runtime binary %s: %v", source, statErr)
		}
		if !info.Mode().IsRegular() {
			continue
		}

		copyRuntimeFile(t, source, filepath.Join(binRoot, filepath.Base(source)))
	}

	for _, entry := range []string{
		"build",
		"config.yaml",
		"coding_ethos.yml",
		".venv",
		"go",
		"pre-commit",
		"repo_ethos.yml",
	} {
		source := filepath.Join(repo.EthosRoot, entry)
		info, err := os.Stat(source)
		if err != nil {
			if entry == ".venv" && os.IsNotExist(err) {
				continue
			}
			t.Fatalf("stat runtime entry %s: %v", source, err)
		}

		target := filepath.Join(runtimeRoot, entry)
		if info.Mode().IsRegular() {
			copyRuntimeFile(t, source, target)
			continue
		}

		if err := os.Symlink(source, target); err != nil {
			t.Fatalf("symlink runtime entry %s: %v", entry, err)
		}
	}

	return runtimeRoot
}

func copyRuntimeFile(t *testing.T, source, target string) {
	t.Helper()

	info, err := os.Stat(source)
	if err != nil {
		t.Fatalf("stat runtime binary %s: %v", source, err)
	}
	payload, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read runtime binary %s: %v", source, err)
	}
	if err := os.WriteFile(target, payload, info.Mode().Perm()); err != nil {
		t.Fatalf("write runtime binary %s: %v", target, err)
	}
}

func realGitPath(t *testing.T) string {
	t.Helper()

	path, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("resolve real git path: %v", err)
	}

	return path
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

	shim := filepath.Join(managedGitBinDir(t, repo), "git")

	return e2e.RunWithEnv(
		t,
		repo.Root,
		map[string]string{
			"CODE_ETHOS_PRECOMMIT_CONFIG": filepath.Join(repo.Root, "repo_config.yaml"),
			"CODE_ETHOS_CONSUMER_ROOT":    repo.Root,
			"CODE_ETHOS_PRECOMMIT_ROOT":   filepath.Join(repo.EthosRoot, "pre-commit"),
			"PATH": filepath.Join(repo.EthosRoot, "bin") +
				string(os.PathListSeparator) +
				os.Getenv("PATH"),
		},
		append([]string{
			shim,
		}, args...)...)
}

func repoHead(t *testing.T, repo e2e.Repo) string {
	t.Helper()

	result := repo.Git(t, "rev-parse", "HEAD")
	result.RequireExit(t, 0)

	return strings.TrimSpace(result.Stdout)
}
