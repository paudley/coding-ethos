// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package policygitcli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/gitwrap"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/testlock"
)

func TestGitCommitReadsMessageFromStdin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		argv []string
		want bool
	}{
		{
			name: "separate file flag stdin",
			argv: []string{"commit", "-F", "-"},
			want: true,
		},
		{
			name: "compact short file flag stdin",
			argv: []string{"commit", "-F-"},
			want: true,
		},
		{
			name: "long file flag stdin",
			argv: []string{"commit", "--file=-"},
			want: true,
		},
		{
			name: "message flag does not read stdin",
			argv: []string{"commit", "-m", "fix(wrapper): avoid stdin"},
		},
		{
			name: "non commit does not read stdin",
			argv: []string{"status", "--short"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := gitCommitReadsMessageFromStdin(test.argv); got != test.want {
				t.Fatalf(
					"gitCommitReadsMessageFromStdin(%#v) = %v, want %v",
					test.argv,
					got,
					test.want,
				)
			}
		})
	}
}

func TestGitOptionsForNonStdinCommand(t *testing.T) {
	t.Parallel()

	options, err := gitOptions([]string{"status", "--short"}, t.TempDir(), false)
	if err != nil {
		t.Fatalf("gitOptions: %v", err)
	}

	if strings.Join(options.Argv, " ") != "status --short" {
		t.Fatalf("argv = %#v", options.Argv)
	}

	if len(options.Stdin) != 0 {
		t.Fatalf("stdin should be empty: %q", options.Stdin)
	}
}

func TestReadBundleAndMaybePrintJSON(t *testing.T) {
	t.Parallel()

	bundlePath := writeGitTestBundle(t)

	bundle, err := readBundle(bundlePath)
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}

	if bundle.BundleID != policy.ExampleBundle().BundleID {
		t.Fatalf("bundle id = %q", bundle.BundleID)
	}

	result := gitwrap.Result{Status: "allowed", Argv: []string{"status"}}

	output := captureGitStdout(t, func() {
		err := maybePrintJSON(true, result)
		if err != nil {
			t.Fatalf("maybePrintJSON: %v", err)
		}
	})
	if !strings.Contains(output, `"status": "allowed"`) {
		t.Fatalf("json output = %q", output)
	}
}

func TestRunCheckOnlyAllowedCommand(t *testing.T) {
	t.Parallel()

	bundlePath := writeGitTestBundle(t)

	output := captureGitStdout(t, func() {
		runOutsideGitWorkTree(t, func() {
			err := runWithArgs([]string{
				"--bundle", bundlePath,
				"--check-only",
				"status",
				"--short",
			})
			if err != nil {
				t.Fatalf("runWithArgs() returned error: %v", err)
			}
		})
	})
	if !strings.Contains(output, "git policy check allowed") {
		t.Fatalf("check-only output = %q", output)
	}
}

func TestRunWithArgsCheckOnlyJSONSuppressesHumanOutput(t *testing.T) {
	t.Parallel()

	bundlePath := writeGitTestBundle(t)

	output := captureGitStdout(t, func() {
		runOutsideGitWorkTree(t, func() {
			err := runWithArgs([]string{
				"--bundle", bundlePath,
				"--check-only",
				"--json",
				"status",
				"--short",
			})
			if err != nil {
				t.Fatalf("runWithArgs() returned error: %v", err)
			}
		})
	})
	if !strings.Contains(output, `"status": "allowed"`) ||
		strings.Contains(output, "git policy check allowed") {
		t.Fatalf("json check-only output = %q", output)
	}
}

func TestExecuteGitWithPostChecksRunsResolvedGitAndPostPolicy(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	marker := filepath.Join(root, "git-ran")

	realGit := filepath.Join(root, "git")

	inlineErr0 := os.WriteFile(
		realGit,
		[]byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > "+marker+"\n"),
		0o600,
	)
	if inlineErr0 != nil {
		t.Fatalf("write fake git: %v", inlineErr0)
	}

	inlineErr1 := os.Chmod(realGit, 0o700)
	if inlineErr1 != nil {
		t.Fatalf("chmod fake git: %v", inlineErr1)
	}

	err := executeGitWithPostChecks(
		policy.ExampleBundle(),
		realGit,
		gitwrap.Options{
			Cwd:  root,
			Argv: []string{"status", "--short"},
		},
		false,
	)
	if err != nil {
		t.Fatalf("executeGitWithPostChecks() error = %v", err)
	}

	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read fake git marker: %v", err)
	}

	if strings.TrimSpace(string(data)) != "status\n--short" {
		t.Fatalf("fake git argv = %q", string(data))
	}
}

func TestRunUsesProcessArgs(t *testing.T) {
	t.Parallel()

	bundlePath := writeGitTestBundle(t)
	testlock.ProcessState(t, "coding-ethos-git")

	originalArgs := os.Args

	t.Cleanup(func() {
		os.Args = originalArgs
	})

	os.Args = []string{
		"coding-ethos-git",
		"--bundle", bundlePath,
		"--check-only",
		"status",
		"--short",
	}

	output := captureGitStdoutUnlocked(t, func() {
		runOutsideGitWorkTree(t, func() {
			err := run()
			if err != nil {
				t.Fatalf("run() returned error: %v", err)
			}
		})
	})
	if !strings.Contains(output, "git policy check allowed") {
		t.Fatalf("check-only output = %q", output)
	}
}

func TestRunRequiresBundleFlag(t *testing.T) {
	t.Parallel()

	err := runWithArgs([]string{"status"})
	if !errors.Is(err, errBundleRequired) {
		t.Fatalf("runWithArgs() error = %v, want %v", err, errBundleRequired)
	}
}

func TestPrintBlockedReportsBlockingDecision(t *testing.T) {
	t.Parallel()

	result := gitwrap.Result{
		Decisions: []policy.Decision{{
			PolicyID:   "git.hook_bypass",
			Decision:   "block",
			Message:    "blocked",
			Suggestion: "fix it",
		}},
	}

	output := captureGitStderr(t, func() {
		printBlocked(result)
	})
	for _, want := range []string{
		"[coding-ethos:git.hook_bypass] blocked",
		"Suggestion: fix it",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stderr missing %q:\n%s", want, output)
		}
	}
}

func writeGitTestBundle(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "policy-bundle.json")

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create bundle: %v", err)
	}

	inlineErr1 := policy.EncodeBundle(file, policy.ExampleBundle())
	if inlineErr1 != nil {
		t.Fatalf("encode bundle: %v", inlineErr1)
	}

	inlineErr2 := file.Close()
	if inlineErr2 != nil {
		t.Fatalf("close bundle: %v", inlineErr2)
	}

	return path
}

func runOutsideGitWorkTree(t *testing.T, run func()) {
	t.Helper()

	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}

	err = os.Chdir(t.TempDir())
	if err != nil {
		t.Fatalf("chdir temp: %v", err)
	}

	defer func() {
		err = os.Chdir(original)
		if err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()

	run()
}

func captureGitStdout(t *testing.T, run func()) string {
	t.Helper()
	testlock.ProcessState(t, "coding-ethos-git")

	return captureGitStdoutUnlocked(t, run)
}

func captureGitStdoutUnlocked(t *testing.T, run func()) string {
	t.Helper()

	original := os.Stdout

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}

	os.Stdout = writer

	defer func() {
		os.Stdout = original
	}()

	run()

	return readGitPipe(t, reader, writer)
}

func captureGitStderr(t *testing.T, run func()) string {
	t.Helper()
	testlock.ProcessState(t, "coding-ethos-git")

	original := os.Stderr

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stderr: %v", err)
	}

	os.Stderr = writer

	defer func() {
		os.Stderr = original
	}()

	run()

	return readGitPipe(t, reader, writer)
}

func readGitPipe(t *testing.T, reader, writer *os.File) string {
	t.Helper()

	err := writer.Close()
	if err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}

	var buffer bytes.Buffer

	_, inlineErrA := io.Copy(&buffer, reader)
	if inlineErrA != nil {
		t.Fatalf("read pipe: %v", inlineErrA)
	}

	err = reader.Close()
	if err != nil {
		t.Fatalf("close pipe reader: %v", err)
	}

	return buffer.String()
}
