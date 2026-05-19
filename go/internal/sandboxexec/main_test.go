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
	if !slices.Contains(got, "CODING_ETHOS_SANDBOX_ACTIVE=1") {
		t.Fatalf("environment missing sandbox marker: %#v", got)
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

	nullDevice, ok := cleanPolicyPath(root, os.DevNull)
	if !ok || nullDevice != os.DevNull {
		t.Fatalf("cleanPolicyPath dev null = %q %t", nullDevice, ok)
	}
}

func TestPrepareWritablePathsFiltersGitAndCreatesRelativePaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	external := filepath.Join(root, "external")
	if err := os.Mkdir(external, 0o700); err != nil {
		t.Fatalf("create absolute write path fixture: %v", err)
	}

	config := options{
		paths: &sandboxPaths{repoRoot: root},
		writePaths: []string{
			".coding-ethos/cache",
			".git/config",
			external,
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

func TestPrepareWritablePathsRejectsSymlinkEscape(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(root, ".coding-ethos")); err != nil {
		t.Fatalf("create symlink fixture: %v", err)
	}

	_, err := prepareWritablePaths(options{
		paths:      &sandboxPaths{repoRoot: root},
		writePaths: []string{".coding-ethos/cache"},
	})
	if !errors.Is(err, errSymlinkWritePath) {
		t.Fatalf("prepareWritablePaths() error = %v, want symlink rejection", err)
	}
}

func TestPrepareWritablePathsAllowsFilePath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile := filepath.Join(root, ".coding-ethos")
	if err := os.WriteFile(writeFile, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("create file fixture: %v", err)
	}

	_, err := prepareWritablePaths(options{
		paths:      &sandboxPaths{repoRoot: root},
		writePaths: []string{".coding-ethos"},
	})
	if err != nil {
		t.Fatalf("prepareWritablePaths() error = %v", err)
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
