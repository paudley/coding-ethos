// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestRunnerArgsInferGitHookFromExecutableName(t *testing.T) {
	t.Parallel()

	args := runnerArgs([]string{"/repo/.git/hooks/pre-commit"})

	want := []string{"git-hook", "pre-commit"}
	if !slices.Equal(args, want) {
		t.Fatalf("runnerArgs() = %#v, want %#v", args, want)
	}
}

func TestRunnerArgsInferLFSHookFromExecutableName(t *testing.T) {
	t.Parallel()

	args := runnerArgs([]string{"/repo/.git/hooks/post-merge"})

	want := []string{"lfs-hook", "post-merge"}
	if !slices.Equal(args, want) {
		t.Fatalf("runnerArgs() = %#v, want %#v", args, want)
	}
}

func TestRunnerArgsPreserveExplicitCommand(t *testing.T) {
	t.Parallel()

	args := runnerArgs([]string{"coding-ethos-run", "policy-lint", "--json"})

	want := []string{"policy-lint", "--json"}
	if !slices.Equal(args, want) {
		t.Fatalf("runnerArgs() = %#v, want %#v", args, want)
	}
}

func TestSplitCIFileListHandlesCommasAndNewlines(t *testing.T) {
	t.Parallel()

	files := splitCIFileList("a.py,b.py\n c.py \n")

	want := []string{"a.py", "b.py", "c.py"}
	if !slices.Equal(files, want) {
		t.Fatalf("splitCIFileList() = %#v, want %#v", files, want)
	}
}

func TestFilterExistingFilesKeepsOnlyRegularFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.py"), []byte("x\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "pkg"), 0o700); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}

	files := filterExistingFiles(root, []string{"a.py", "missing.py", "pkg"})

	want := []string{"a.py"}
	if !slices.Equal(files, want) {
		t.Fatalf("filterExistingFiles() = %#v, want %#v", files, want)
	}
}

func TestWriteCIFileListWritesNewlineSeparatedPaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path, err := writeCIFileList(root, []string{"a.py", "b.py"})
	if err != nil {
		t.Fatalf("writeCIFileList: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Remove(path)
	})
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file list: %v", err)
	}
	if strings.TrimSpace(string(payload)) != "a.py\nb.py" {
		t.Fatalf("file list payload = %q", payload)
	}
}

func TestPolicyToolLintArgsPropagateSandboxMode(t *testing.T) {
	t.Setenv("CODING_ETHOS_SANDBOX_MODE", "required")
	t.Setenv("CODING_ETHOS_POLICY_TOOL_SHIM", "1")

	args := policyToolLintArgs(runtimePaths{
		PolicyBundle:  "/repo/build/policy/policy-bundle.json",
		EthosRoot:     "/repo/coding-ethos",
		Root:          "/repo",
		InvocationCWD: "/repo/pkg",
	}, "ruff", []string{"check", "pkg"})

	for _, want := range []string{
		"--bundle",
		"/repo/build/policy/policy-bundle.json",
		"--managed-capture-tool",
		"ruff",
		"--ethos-root",
		"/repo/coding-ethos",
		"--consumer-root",
		"/repo",
		"--invocation-cwd",
		"/repo/pkg",
		"--sandbox-mode",
		"required",
		"--",
		"check",
		"pkg",
	} {
		if !slices.Contains(args, want) {
			t.Fatalf("policyToolLintArgs() missing %q: %#v", want, args)
		}
	}
	if slices.Index(args, "--sandbox-mode") > slices.Index(args, "--") {
		t.Fatalf("sandbox flag must precede tool args separator: %#v", args)
	}
}

func TestPolicyToolLintArgsOmitBlankSandboxMode(t *testing.T) {
	t.Setenv("CODING_ETHOS_SANDBOX_MODE", " ")
	t.Setenv("CODING_ETHOS_POLICY_TOOL_SHIM", "1")

	args := policyToolLintArgs(runtimePaths{}, "ruff", []string{"check"})

	if slices.Contains(args, "--sandbox-mode") {
		t.Fatalf("blank sandbox mode should not be forwarded: %#v", args)
	}
}

func TestPolicyToolLintArgsIgnoreAmbientSandboxModeOutsideShim(t *testing.T) {
	t.Setenv("CODING_ETHOS_SANDBOX_MODE", "required")

	args := policyToolLintArgs(runtimePaths{}, "ruff", []string{"check"})

	if slices.Contains(args, "--sandbox-mode") {
		t.Fatalf("ambient sandbox mode should not affect direct policy-tool calls: %#v", args)
	}
}

func TestCodeIntelArgsInsertRootAfterSubcommand(t *testing.T) {
	t.Parallel()

	args := codeIntelArgs("/repo", []string{"stats", "--db", "/tmp/code-intel.db"})

	want := []string{"stats", "--root", "/repo", "--db", "/tmp/code-intel.db"}
	if !slices.Equal(args, want) {
		t.Fatalf("codeIntelArgs() = %#v, want %#v", args, want)
	}
}

func TestCodeIntelArgsKeepExplicitRoot(t *testing.T) {
	t.Parallel()

	args := codeIntelArgs("/repo", []string{"stats", "--root", "/other"})

	want := []string{"stats", "--root", "/other"}
	if !slices.Equal(args, want) {
		t.Fatalf("codeIntelArgs() = %#v, want %#v", args, want)
	}
}
