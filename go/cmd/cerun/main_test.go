// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestCerunRuntimeArgs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "default agent shell",
			args: []string{"--", "make", "test"},
			want: []string{"agent-shell", "--", "make", "test"},
		},
		{
			name: "git shortcut",
			args: []string{"git", "status"},
			want: []string{"agent-shell", "--rewrite", "--", "git", "status"},
		},
		{
			name: "absolute git shortcut",
			args: []string{"/usr/bin/git", "status"},
			want: []string{"agent-shell", "--rewrite", "--", "git", "status"},
		},
		{
			name: "python shortcut",
			args: []string{"python", "-m", "pytest"},
			want: []string{"agent-shell", "--rewrite", "--", "python", "-m", "pytest"},
		},
		{
			name: "absolute python shortcut",
			args: []string{"/usr/bin/python", "-m", "pytest"},
			want: []string{"agent-shell", "--rewrite", "--", "python", "-m", "pytest"},
		},
		{
			name: "lint shortcut",
			args: []string{"lint", "--scope", "staged"},
			want: []string{"policy-lint", "--scope", "staged"},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := cerunRuntimeArgs(test.args)
			if !slices.Equal(got, test.want) {
				t.Fatalf("cerunRuntimeArgs() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestCerunExitCode(t *testing.T) {
	t.Parallel()

	if got := cerunExitCode(nil); got != 0 {
		t.Fatalf("cerunExitCode(nil) = %d, want 0", got)
	}

	missing := &exec.Error{Name: "missing", Err: exec.ErrNotFound}
	if got := cerunExitCode(missing); got != cerunMissingRuntimeExitCode {
		t.Fatalf("cerunExitCode(exec.Error) = %d, want %d", got, cerunMissingRuntimeExitCode)
	}

	if got := cerunExitCode(errors.New("plain")); got != 1 {
		t.Fatalf("cerunExitCode(plain) = %d, want 1", got)
	}
}

func TestSiblingRunnerResolvesNextToExecutable(t *testing.T) {
	t.Parallel()

	got, err := siblingRunner()
	if err != nil {
		t.Fatalf("siblingRunner() error = %v", err)
	}

	if filepath.Base(got) != "coding-ethos-run" {
		t.Fatalf("siblingRunner() = %q, want coding-ethos-run basename", got)
	}

	if !filepath.IsAbs(got) {
		t.Fatalf("siblingRunner() = %q, want absolute path", got)
	}
}

func TestRunCerunMissingSiblingRunnerReturnsMissingRuntime(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}

	runner := filepath.Join(filepath.Dir(executable), "coding-ethos-run")
	_ = os.Remove(runner)

	if got := runCerun([]string{"git", "status"}); got != cerunMissingRuntimeExitCode {
		t.Fatalf("runCerun() = %d, want %d", got, cerunMissingRuntimeExitCode)
	}
}

func TestRunCerunDispatchesToSiblingRunner(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}

	runner := filepath.Join(filepath.Dir(executable), "coding-ethos-run")
	outputPath := filepath.Join(t.TempDir(), "runner-argv")
	script := "#!/usr/bin/env sh\nprintf '%s\\n' \"$@\" > " +
		shellQuoteForTest(outputPath) + "\n"

	err = os.WriteFile(runner, []byte(script), 0o700)
	if err != nil {
		t.Skipf("test executable directory is not writable: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(runner) })

	if got := runCerun([]string{"git", "status"}); got != 0 {
		t.Fatalf("runCerun() = %d, want 0", got)
	}

	payload, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read runner argv: %v", err)
	}

	got := strings.Fields(string(payload))
	want := []string{"agent-shell", "--rewrite", "--", "git", "status"}
	if !slices.Equal(got, want) {
		t.Fatalf("runner argv = %#v, want %#v", got, want)
	}
}

func shellQuoteForTest(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
