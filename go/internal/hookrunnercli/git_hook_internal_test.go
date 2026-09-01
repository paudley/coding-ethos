// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//nolint:paralleltest,lll // Uses process-global fixtures.
package hookrunnercli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/realgit"
)

func TestHookFilesForPreCommitDiscoversStagedAndAllFiles(t *testing.T) {
	root := setupGitHookTestRepo(t)
	chdirForTest(t, root)
	mustWriteTestFile(t, "tracked.py", "print('tracked')\n")
	mustWriteTestFile(t, "staged.py", "print('staged')\n")
	runGitTestCommand(t, "add", "tracked.py")
	runGitTestCommand(t, "commit", "-m", "test: seed")
	runGitTestCommand(t, "add", "staged.py")

	staged, err := hookFilesForPreCommit(false)
	if err != nil {
		t.Fatalf("hookFilesForPreCommit(false) returned error: %v", err)
	}

	if !reflect.DeepEqual(staged, []string{"staged.py"}) {
		t.Fatalf("staged files = %#v, want staged.py", staged)
	}

	allFiles, err := hookFilesForPreCommit(true)
	if err != nil {
		t.Fatalf("hookFilesForPreCommit(true) returned error: %v", err)
	}

	if !reflect.DeepEqual(allFiles, []string{"staged.py", "tracked.py"}) {
		t.Fatalf("all files = %#v, want staged.py and tracked.py", allFiles)
	}
}

func TestHookGroupChildEnvironmentCarriesBundleRoot(t *testing.T) {
	t.Setenv(precommitRootEnv, "/repo/coding-ethos/pre-commit")

	env := hookGroupChildEnvironment("/tmp/result.json", "/repo")

	if !slices.Contains(env, precommitRootEnv+"=/repo/coding-ethos/pre-commit") {
		t.Fatalf("child env missing pre-commit root: %#v", env)
	}

	if !slices.Contains(env, consumerRootEnv+"=/repo") {
		t.Fatalf("child env missing consumer root: %#v", env)
	}
}

func TestUseLocalRootForPrePushTemporarilyOverridesConsumerRoot(t *testing.T) {
	t.Setenv(consumerRootEnv, "/repo")
	t.Setenv(localRootEnv, "/repo/coding-ethos")

	restore := useLocalRootForPrePush()

	if got := os.Getenv(consumerRootEnv); got != "/repo/coding-ethos" {
		t.Fatalf("pre-push consumer root = %q, want local root", got)
	}

	restore()

	if got := os.Getenv(consumerRootEnv); got != "/repo" {
		t.Fatalf("restored consumer root = %q, want parent root", got)
	}
}

func TestPushedFilesParsesPrePushRefs(t *testing.T) {
	root := setupGitHookTestRepo(t)
	chdirForTest(t, root)
	mustWriteTestFile(t, "base.py", "print('base')\n")
	runGitTestCommand(t, "add", "base.py")
	runGitTestCommand(t, "commit", "-m", "test: base")
	remoteSHA := gitTestOutput(t, "rev-parse", "HEAD")

	mustWriteTestFile(t, "feature.py", "print('feature')\n")
	runGitTestCommand(t, "add", "feature.py")
	runGitTestCommand(t, "commit", "-m", "test: feature")
	localSHA := gitTestOutput(t, "rev-parse", "HEAD")

	input := strings.NewReader(
		"refs/heads/main " + localSHA + " refs/heads/main " + remoteSHA + "\n",
	)

	files, err := pushedFiles(input, "")
	if err != nil {
		t.Fatalf("pushedFiles() returned error: %v", err)
	}

	if !reflect.DeepEqual(files, []string{"feature.py"}) {
		t.Fatalf("pushed files = %#v, want feature.py", files)
	}
}

func TestPushedCodeIntelFilesIncludesDeletionRenameSidesAndNewlines(t *testing.T) {
	root := setupGitHookTestRepo(t)
	chdirForTest(t, root)
	mustWriteTestFile(t, "old.py", "OLD = 1\n")
	mustWriteTestFile(t, "removed.py", "REMOVED = 1\n")
	mustWriteTestFile(t, "line\nbreak.py", "ODD = 1\n")
	runGitTestCommand(t, "add", "old.py", "removed.py", "line\nbreak.py")
	runGitTestCommand(t, "commit", "-m", "test: base")
	remoteSHA := gitTestOutput(t, "rev-parse", "HEAD")

	runGitTestCommand(t, "mv", "old.py", "new.py")
	runGitTestCommand(t, "rm", "removed.py")
	mustWriteTestFile(t, "line\nbreak.py", "ODD = 2\n")
	runGitTestCommand(t, "add", "line\nbreak.py")
	runGitTestCommand(t, "commit", "-m", "test: rename and delete")
	localSHA := gitTestOutput(t, "rev-parse", "HEAD")

	input := strings.NewReader(
		"refs/heads/main " + localSHA + " refs/heads/main " + remoteSHA + "\n",
	)
	files, err := pushedCodeIntelFiles(input, "")
	if err != nil {
		t.Fatalf("pushedCodeIntelFiles() returned error: %v", err)
	}

	for _, want := range []string{"line\nbreak.py", "new.py", "old.py", "removed.py"} {
		if !slices.Contains(files, want) {
			t.Fatalf("pushed code-intel files = %#v, missing %q", files, want)
		}
	}
}

func TestPushedFilesHandlesNewBranchAndDeleteRefs(t *testing.T) {
	root := setupGitHookTestRepo(t)
	chdirForTest(t, root)
	mustWriteTestFile(t, "first.py", "print('first')\n")
	runGitTestCommand(t, "add", "first.py")
	runGitTestCommand(t, "commit", "-m", "test: first")
	mustWriteTestFile(t, "second.py", "print('second')\n")
	runGitTestCommand(t, "add", "second.py")
	runGitTestCommand(t, "commit", "-m", "test: second")
	localSHA := gitTestOutput(t, "rev-parse", "HEAD")

	input := strings.NewReader(
		"refs/heads/new " + localSHA + " refs/heads/new " + allZeroSHA + "\n" +
			"refs/heads/gone " + allZeroSHA + " refs/heads/gone " + localSHA + "\n",
	)

	files, err := pushedFiles(input, "")
	if err != nil {
		t.Fatalf("pushedFiles() returned error: %v", err)
	}

	want := []string{"first.py", "second.py"}
	if !reflect.DeepEqual(files, want) {
		t.Fatalf("pushed files = %#v, want %#v", files, want)
	}
}

func TestPushedFilesScopesNewBranchToRemoteDefaultBranch(t *testing.T) {
	root := setupGitHookTestRepo(t)
	chdirForTest(t, root)
	mustWriteTestFile(t, "base.py", "print('base')\n")
	runGitTestCommand(t, "add", "base.py")
	runGitTestCommand(t, "commit", "-m", "test: base")
	baseSHA := gitTestOutput(t, "rev-parse", "HEAD")
	runGitTestCommand(t, "update-ref", "refs/remotes/origin/main", baseSHA)
	runGitTestCommand(
		t,
		"symbolic-ref",
		"refs/remotes/origin/HEAD",
		"refs/remotes/origin/main",
	)

	mustWriteTestFile(t, "feature.py", "print('feature')\n")
	runGitTestCommand(t, "add", "feature.py")
	runGitTestCommand(t, "commit", "-m", "test: feature")
	localSHA := gitTestOutput(t, "rev-parse", "HEAD")

	input := strings.NewReader(
		"refs/heads/feature " + localSHA + " refs/heads/feature " + allZeroSHA + "\n",
	)
	files, err := pushedFiles(input, "origin")
	if err != nil {
		t.Fatalf("pushedFiles() returned error: %v", err)
	}

	if !reflect.DeepEqual(files, []string{"feature.py"}) {
		t.Fatalf("pushed files = %#v, want feature.py", files)
	}
}

func TestPushedFilesDeduplicatesMultipleRefs(t *testing.T) {
	root := setupGitHookTestRepo(t)
	chdirForTest(t, root)
	mustWriteTestFile(t, "base.py", "print('base')\n")
	runGitTestCommand(t, "add", "base.py")
	runGitTestCommand(t, "commit", "-m", "test: base")
	remoteSHA := gitTestOutput(t, "rev-parse", "HEAD")
	mustWriteTestFile(t, "feature.py", "print('feature')\n")
	runGitTestCommand(t, "add", "feature.py")
	runGitTestCommand(t, "commit", "-m", "test: feature")
	localSHA := gitTestOutput(t, "rev-parse", "HEAD")

	line := "refs/heads/main " + localSHA + " refs/heads/main " + remoteSHA + "\n"

	files, err := pushedFiles(strings.NewReader(line+line), "")
	if err != nil {
		t.Fatalf("pushedFiles() returned error: %v", err)
	}

	if !reflect.DeepEqual(files, []string{"feature.py"}) {
		t.Fatalf("pushed files = %#v, want deduplicated feature.py", files)
	}
}

func TestDockerAndWorkflowFileSelection(t *testing.T) {
	files := []string{
		"Dockerfile",
		"docker/Dockerfile.worker",
		".github/workflows/ci.yml",
		".github/workflows/ci.yaml",
		".github/not-workflows/ci.yml",
		"notes.txt",
	}
	if got := dockerFiles(
		files,
	); !reflect.DeepEqual(
		got,
		[]string{"Dockerfile", "docker/Dockerfile.worker"},
	) {
		t.Fatalf("dockerFiles() = %#v", got)
	}

	if got := workflowFiles(
		files,
	); !reflect.DeepEqual(
		got,
		[]string{".github/workflows/ci.yml", ".github/workflows/ci.yaml"},
	) {
		t.Fatalf("workflowFiles() = %#v", got)
	}
}

func TestHookGroupResultFileRoundTrip(t *testing.T) {
	t.Parallel()

	resultPath := filepath.Join(t.TempDir(), "result.json")
	want := hookGroupResult{
		Name:       "syntax",
		Status:     statusFail,
		ExitCode:   1,
		DurationMS: 12,
		Commands: []hookCommandResult{
			{Name: "yamllint", Status: statusPass, ExitCode: 0, DurationMS: 4},
			{Name: "yamllint", Status: statusFail, ExitCode: 1, DurationMS: 8},
		},
	}

	writeHookGroupResultFile(resultPath, want)

	got, ok := readHookGroupResultFile(resultPath)
	if !ok {
		t.Fatal("readHookGroupResultFile() did not read result")
	}

	if got.Name != want.Name ||
		got.Status != want.Status ||
		got.ExitCode != want.ExitCode ||
		got.DurationMS != want.DurationMS ||
		!reflect.DeepEqual(got.Commands, want.Commands) {
		t.Fatalf("readHookGroupResultFile() = %#v, want %#v", got, want)
	}
}

func TestHookGroupResultFilePathAcceptsGoTempDir(t *testing.T) {
	goTempDir := filepath.Join(t.TempDir(), "go-temp")
	if err := os.MkdirAll(goTempDir, 0o700); err != nil {
		t.Fatalf("create Go temp dir: %v", err)
	}
	t.Setenv("GOTMPDIR", goTempDir)

	resultPath := filepath.Join(goTempDir, "result.json")
	cleanPath, ok := hookGroupResultFilePath(resultPath)
	if !ok || cleanPath != resultPath {
		t.Fatalf(
			"hookGroupResultFilePath() = %q, %t; want %q, true",
			cleanPath,
			ok,
			resultPath,
		)
	}
}

func TestReadHookGroupResultFileRejectsNonTempPath(t *testing.T) {
	t.Parallel()

	if result, ok := readHookGroupResultFile("go.mod"); ok {
		t.Fatalf("readHookGroupResultFile() = %#v, true for non-temp path", result)
	}
}

func TestHookGroupResultFilePathRejectsTempSymlinkEscape(t *testing.T) {
	t.Parallel()

	outsideDir, err := os.MkdirTemp(".", "hook-result-outside-")
	if err != nil {
		t.Fatalf("create outside temp dir: %v", err)
	}
	t.Cleanup(func() {
		if removeErr := os.RemoveAll(outsideDir); removeErr != nil {
			t.Fatalf("remove outside temp dir: %v", removeErr)
		}
	})

	outsidePath, err := filepath.Abs(outsideDir)
	if err != nil {
		t.Fatalf("resolve outside temp dir: %v", err)
	}

	linkPath := filepath.Join(t.TempDir(), "outside")
	if err := os.Symlink(outsidePath, linkPath); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	if cleanPath, ok := hookGroupResultFilePath(
		filepath.Join(linkPath, "result.json"),
	); ok {
		t.Fatalf("hookGroupResultFilePath() = %q, true for temp symlink escape", cleanPath)
	}
}

func TestRunGitHookCommandRejectsUnsupportedEntrypoints(t *testing.T) {
	stderr := captureStderr(t, func() {
		if got := runGitHookCommand(Config{}, nil); got != 1 {
			t.Fatalf("empty git-hook command = %d, want 1", got)
		}

		if got := runGitHookCommand(Config{}, []string{"commit-msg"}); got != 1 {
			t.Fatalf("commit-msg git-hook command = %d, want 1", got)
		}

		if got := runGitHookCommand(Config{}, []string{"unknown"}); got != 1 {
			t.Fatalf("unknown git-hook command = %d, want 1", got)
		}
	})
	for _, want := range []string{
		"Usage: coding-ethos-hook git-hook",
		"commit-msg is enforced by compiled policy preflight",
		`unknown git hook "unknown"`,
	} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr missing %q:\n%s", want, stderr)
		}
	}
}

func TestGitHooksRejectRemovedEnabledGroupsConfig(t *testing.T) {
	tempDir := setupGitHookTestRepo(t)
	t.Chdir(tempDir)
	t.Setenv("CODING_ETHOS_REAL_GIT", "")
	t.Setenv(consumerRootEnv, tempDir)
	bundleRoot := writeTestBundleRoot(t, tempDir)
	t.Setenv(precommitRootEnv, bundleRoot)

	overridePath := filepath.Join(tempDir, "repo_config.yaml")
	mustWriteTestFile(
		t,
		overridePath,
		strings.TrimSpace(`
hooks:
  enabled_groups:
    - security
  parallel_groups: false
`)+"\n",
	)
	t.Setenv(configEnv, overridePath)

	stderr := captureStderr(t, func() {
		if got := runPreCommitHook(Config{}, nil); got != 1 {
			t.Fatalf("runPreCommitHook() = %d, want 1", got)
		}
	})
	for _, want := range []string{
		"hooks.enabled_groups has been removed",
		"hook groups are policy-owned",
	} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr missing %q:\n%s", want, stderr)
		}
	}
}

func TestGitHooksRejectTimeoutsBeyondCriticalCeiling(t *testing.T) {
	tempDir := setupGitHookTestRepo(t)
	t.Chdir(tempDir)
	t.Setenv("CODING_ETHOS_REAL_GIT", "")
	t.Setenv(consumerRootEnv, tempDir)
	bundleRoot := writeTestBundleRoot(t, tempDir)
	t.Setenv(precommitRootEnv, bundleRoot)

	overridePath := filepath.Join(tempDir, "repo_config.yaml")
	mustWriteTestFile(
		t,
		overridePath,
		"hooks:\n  tool_timeout_seconds: 601\n",
	)
	t.Setenv(configEnv, overridePath)

	stderr := captureStderr(t, func() {
		if got := runPreCommitHook(Config{}, nil); got != 1 {
			t.Fatalf("runPreCommitHook() = %d, want 1", got)
		}
	})
	for _, want := range []string{"must not exceed 600", "critical failure"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr missing %q:\n%s", want, stderr)
		}
	}
}

func TestGitHooksRejectRemovedEnabledGroupsConfigSpellings(t *testing.T) {
	for _, key := range []string{"enabled-groups", "enabledGroups", "ENABLED_GROUPS"} {
		t.Run(key, func(t *testing.T) {
			tempDir := setupGitHookTestRepo(t)
			t.Chdir(tempDir)
			t.Setenv("CODING_ETHOS_REAL_GIT", "")
			t.Setenv(consumerRootEnv, tempDir)
			bundleRoot := writeTestBundleRoot(t, tempDir)
			t.Setenv(precommitRootEnv, bundleRoot)

			overridePath := filepath.Join(tempDir, "repo_config.yaml")
			mustWriteTestFile(
				t,
				overridePath,
				"hooks:\n  "+key+":\n    - security\n",
			)
			t.Setenv(configEnv, overridePath)

			stderr := captureStderr(t, func() {
				if got := runPreCommitHook(Config{}, nil); got != 1 {
					t.Fatalf("runPreCommitHook() = %d, want 1", got)
				}
			})
			if !strings.Contains(stderr, "hooks.enabled_groups has been removed") {
				t.Fatalf("stderr missing removal notice for %q:\n%s", key, stderr)
			}
		})
	}
}

func TestPushedFilesUsesTreeComparisonForNewBranch(t *testing.T) {
	tempDir := setupGitHookTestRepo(t)
	t.Chdir(tempDir)
	t.Setenv("CODING_ETHOS_REAL_GIT", "")

	fakeBin := filepath.Join(tempDir, "bin")
	mustWriteExecutable(
		t,
		filepath.Join(fakeBin, "git"),
		`#!/usr/bin/env sh
case "$*" in
  "diff --name-only 4b825dc642cb6eb9a060e54bf8d69288fbee4904 abc123")
    printf 'README.md\n'
    exit 0
    ;;
esac
printf 'unexpected git invocation: %s\n' "$*" >&2
exit 2
`,
	)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	files, err := pushedFiles(strings.NewReader(
		"refs/heads/feature abc123 refs/heads/feature "+allZeroSHA+"\n",
	), "")
	if err != nil {
		t.Fatalf("pushedFiles: %v", err)
	}

	if !slices.Equal(files, []string{"README.md"}) {
		t.Fatalf("pushed files = %#v", files)
	}
}

func TestPushedFilesRunsGitDiffInLocalRoot(t *testing.T) {
	tempDir := t.TempDir()
	parentRoot := filepath.Join(tempDir, "parent")
	localRoot := filepath.Join(parentRoot, "coding-ethos")

	err := os.MkdirAll(localRoot, 0o755)
	if err != nil {
		t.Fatalf("create test roots: %v", err)
	}

	t.Setenv(consumerRootEnv, parentRoot)
	t.Setenv(localRootEnv, localRoot)
	t.Setenv("CODING_ETHOS_REAL_GIT", "")

	fakeBin := filepath.Join(tempDir, "bin")
	mustWriteExecutable(
		t,
		filepath.Join(fakeBin, "git"),
		`#!/usr/bin/env sh
if [ "$PWD" != "$CODE_ETHOS_LOCAL_ROOT" ]; then
  printf 'git ran in %s, want %s\n' "$PWD" "$CODE_ETHOS_LOCAL_ROOT" >&2
  exit 2
fi
case "$*" in
  "diff --name-only remote123..local123")
    printf 'README.md\n'
    exit 0
    ;;
esac
printf 'unexpected git invocation: %s\n' "$*" >&2
exit 2
`,
	)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	files, err := pushedFiles(strings.NewReader(
		"refs/heads/feature local123 refs/heads/feature remote123\n",
	), "")
	if err != nil {
		t.Fatalf("pushedFiles: %v", err)
	}

	if !slices.Equal(files, []string{"README.md"}) {
		t.Fatalf("pushed files = %#v", files)
	}
}

func TestRunHookGroupCommandRejectsMissingAndUnknownGroups(t *testing.T) {
	stderr := captureStderr(t, func() {
		if got := runHookGroupCommand(Config{}, nil); got != 1 {
			t.Fatalf("missing group command = %d, want 1", got)
		}

		if got := runHookGroupCommand(Config{}, []string{"missing-group"}); got != 1 {
			t.Fatalf("unknown group command = %d, want 1", got)
		}
	})
	for _, want := range []string{
		"Usage: coding-ethos-hook run-group",
		`unknown hook group "missing-group"`,
	} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr missing %q:\n%s", want, stderr)
		}
	}
}

func TestRunHookPlanCommandPrintsPlan(t *testing.T) {
	output := captureStdout(t, func() {
		if got := runHookPlanCommand(Config{}, nil); got != 0 {
			t.Fatalf("runHookPlanCommand() = %d, want 0", got)
		}
	})
	if !strings.Contains(output, "HOOK PLAN") && !strings.Contains(output, "groups[") {
		t.Fatalf("plan output = %q", output)
	}
}

func TestFormatHookExecutionSummaryHumanIncludesFailedCommands(t *testing.T) {
	output := formatHookExecutionSummary(
		[]hookGroupResult{
			{
				Name:       "syntax",
				Status:     statusFail,
				ExitCode:   1,
				DurationMS: 12,
				Commands: []hookCommandResult{
					{Name: "yamllint", Status: statusFail, ExitCode: 1, DurationMS: 12},
				},
			},
		},
		"human",
	)
	for _, want := range []string{
		"HOOK EXECUTION SUMMARY",
		"fix_first: syntax",
		"yamllint: FAIL exit=1",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("human summary missing %q:\n%s", want, output)
		}
	}
}

func setupGitHookTestRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runGitTestCommandInDir(t, root, "init", "-q")
	runGitTestCommandInDir(t, root, "config", "user.email", "test@example.com")
	runGitTestCommandInDir(t, root, "config", "user.name", "Test User")

	return root
}

func chdirForTest(t *testing.T, root string) {
	t.Helper()

	t.Chdir(root)
}

func runGitTestCommand(t *testing.T, args ...string) {
	t.Helper()
	runGitTestCommandInDir(t, repoRoot(), args...)
}

func runGitTestCommandInDir(t *testing.T, dir string, args ...string) {
	t.Helper()

	gitPath, err := realgit.Resolve(context.Background(), "git")
	if err != nil {
		t.Fatalf("resolve git: %v", err)
	}

	cmd := exec.CommandContext(context.Background(), gitPath, args...)
	cmd.Dir = dir
	cmd.Env = cleanGitTestEnv()

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(output))
	}
}

func gitTestOutput(t *testing.T, args ...string) string {
	t.Helper()

	gitPath, err := realgit.Resolve(context.Background(), "git")
	if err != nil {
		t.Fatalf("resolve git: %v", err)
	}

	cmd := exec.CommandContext(context.Background(), gitPath, args...)
	cmd.Dir = repoRoot()
	cmd.Env = cleanGitTestEnv()

	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s failed: %v", strings.Join(args, " "), err)
	}

	return strings.TrimSpace(string(output))
}

func cleanGitTestEnv() []string {
	env := make([]string, 0, len(os.Environ()))
	for _, item := range os.Environ() {
		name, _, found := strings.Cut(item, "=")
		if found && gitLocalEnvName(name) {
			continue
		}

		env = append(env, item)
	}

	return append(
		env,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"XDG_CONFIG_HOME="+os.DevNull,
	)
}

func gitLocalEnvName(name string) bool {
	switch name {
	case "GIT_ALTERNATE_OBJECT_DIRECTORIES",
		"GIT_COMMON_DIR",
		"GIT_CONFIG_COUNT",
		"GIT_CONFIG_GLOBAL",
		"GIT_CONFIG_NOSYSTEM",
		"GIT_CONFIG_PARAMETERS",
		"GIT_CONFIG_SYSTEM",
		"GIT_DIR",
		"GIT_INDEX_FILE",
		"GIT_NAMESPACE",
		"GIT_OBJECT_DIRECTORY",
		"GIT_PREFIX",
		"GIT_QUARANTINE_PATH",
		"GIT_WORK_TREE",
		"XDG_CONFIG_HOME":
		return true
	default:
		return strings.HasPrefix(name, "GIT_CONFIG_KEY_") ||
			strings.HasPrefix(name, "GIT_CONFIG_VALUE_")
	}
}

func TestRepoPathResolvesRelativePath(t *testing.T) {
	root := setupGitHookTestRepo(t)
	chdirForTest(t, root)

	if got := repoPath("ruff.toml"); got != filepath.Join(root, "ruff.toml") {
		t.Fatalf("repoPath() = %q, want repo-root path", got)
	}
}
