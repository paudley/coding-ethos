// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//go:build linux

package sandboxexec

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParseOptionsNormalizesPathsAndPreservesCommand(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	parsed, err := parseOptions([]string{
		"--cwd", root,
		"--repo-root", root,
		"--write-path", ".coding-ethos/cache",
		"--",
		"/bin/true",
		"--flag",
	})
	if err != nil {
		t.Fatalf("parseOptions() error = %v", err)
	}

	if parsed.paths.cwd != root || parsed.paths.repoRoot != root {
		t.Fatalf("paths = %#v", parsed.paths)
	}
	if !slices.Equal(parsed.writePaths, []string{".coding-ethos/cache"}) {
		t.Fatalf("write paths = %#v", parsed.writePaths)
	}
	if !slices.Equal(parsed.commandArgv, []string{"/bin/true", "--flag"}) {
		t.Fatalf("command argv = %#v", parsed.commandArgv)
	}
}

func TestParseOptionsRequiresCommand(t *testing.T) {
	t.Parallel()

	_, err := parseOptions([]string{"--cwd", t.TempDir()})
	if !errors.Is(err, errSandboxExecCommand) {
		t.Fatalf("parseOptions() error = %v, want command error", err)
	}
}

func TestRunReturnsFailureForInvalidInvocation(t *testing.T) {
	t.Parallel()

	if code := Run([]string{"--cwd", t.TempDir()}); code != sandboxExecFailureExitCode {
		t.Fatalf("Run() exit = %d, want %d", code, sandboxExecFailureExitCode)
	}
}

func TestExecSandboxedCommandReportsStartFailure(t *testing.T) {
	t.Parallel()

	err := execSandboxedCommand(options{
		paths:       &sandboxPaths{cwd: t.TempDir()},
		commandArgv: []string{filepath.Join(t.TempDir(), "missing-tool")},
	})
	if err == nil {
		t.Fatal("execSandboxedCommand() error = nil")
	}
}

func TestSandboxExecEnvRemovesGitOverrides(t *testing.T) {
	t.Parallel()

	got := sandboxExecEnv([]string{
		"PATH=/bin",
		"GIT_DIR=/tmp/git",
		"GIT_CONFIG_KEY_0=core.sshCommand",
		"GIT_CONFIG_VALUE_0=ssh -i key",
		"KEEP=value",
	})
	joined := strings.Join(got, "\n")

	for _, blocked := range []string{"GIT_DIR=", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0"} {
		if strings.Contains(joined, blocked) {
			t.Fatalf("environment retained blocked %q: %#v", blocked, got)
		}
	}
	for _, kept := range []string{"PATH=/bin", "KEEP=value"} {
		if !slices.Contains(got, kept) {
			t.Fatalf("environment missing %q: %#v", kept, got)
		}
	}
}

func TestCleanPolicyPathStaysInsideRepo(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	path, ok := cleanPolicyPath(root, "pkg/../.coding-ethos/cache")
	if !ok || path != filepath.Join(root, ".coding-ethos", "cache") {
		t.Fatalf("cleanPolicyPath relative = %q %t", path, ok)
	}

	outside, ok := cleanPolicyPath(root, filepath.Dir(root))
	if ok || outside == "" {
		t.Fatalf("cleanPolicyPath outside = %q %t", outside, ok)
	}
}

func TestPrepareWritablePathsFiltersGitAndCreatesRelativePaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	config := options{
		paths: &sandboxPaths{repoRoot: root},
		writePaths: []string{
			".coding-ethos/cache",
			".git/config",
			filepath.Join(root, "external"),
		},
	}

	paths, err := prepareWritablePaths(config)
	if err != nil {
		t.Fatalf("prepareWritablePaths() error = %v", err)
	}

	want := filepath.Join(root, ".coding-ethos", "cache")
	if !slices.Contains(paths, want) {
		t.Fatalf("write paths missing %s: %#v", want, paths)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("declared relative write path was not created: %v", err)
	}
	if slices.Contains(paths, filepath.Join(root, ".git", "config")) {
		t.Fatalf(".git write path leaked through: %#v", paths)
	}
}

func TestJoinPolicyErrors(t *testing.T) {
	t.Parallel()

	err := joinPolicyErrors(errors.New("first"), errors.New("second"))
	if err == nil || !strings.Contains(err.Error(), "first") ||
		!strings.Contains(err.Error(), "second") {
		t.Fatalf("joinPolicyErrors() = %v", err)
	}
}
