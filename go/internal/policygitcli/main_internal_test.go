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
	"blackcat.ca/coding-ethos/go/internal/realgit"
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

func TestParsePolicyGitArgsPreservesGitGlobalOptions(t *testing.T) {
	t.Parallel()

	parsed, err := parsePolicyGitArgs([]string{
		"--bundle", "/policy/bundle.json",
		"--real-git=/usr/bin/git",
		"--check-only",
		"-c", "core.useBuiltinFSMonitor=false",
		"init", "--template=/tmp/hooks",
	})
	if err != nil {
		t.Fatalf("parsePolicyGitArgs: %v", err)
	}

	if parsed.bundlePath != "/policy/bundle.json" || parsed.realGit != "/usr/bin/git" ||
		!parsed.checkOnly {
		t.Fatalf("wrapper options = %#v", parsed)
	}
	want := "-c core.useBuiltinFSMonitor=false init --template=/tmp/hooks"
	if got := strings.Join(parsed.gitArgv, " "); got != want {
		t.Fatalf("git argv = %q, want %q", got, want)
	}
}

func TestParsePolicyGitArgsHonorsExplicitBoundary(t *testing.T) {
	t.Parallel()

	parsed, err := parsePolicyGitArgs([]string{
		"--bundle=/policy/bundle.json",
		"--json=false",
		"--",
		"--future-git-global", "value", "status",
	})
	if err != nil {
		t.Fatalf("parsePolicyGitArgs: %v", err)
	}
	if parsed.jsonOutput {
		t.Fatal("--json=false was not retained")
	}
	if got := strings.Join(
		parsed.gitArgv,
		" ",
	); got != "--future-git-global value status" {
		t.Fatalf("git argv = %q", got)
	}
}

func TestParsePolicyGitArgsRejectsUnknownWrapperOption(t *testing.T) {
	t.Parallel()

	_, err := parsePolicyGitArgs([]string{
		"--bundle=/policy/bundle.json",
		"--typo-wrapper-option",
		"status",
	})
	if err == nil || !strings.Contains(err.Error(), "unknown policy-git option") {
		t.Fatalf("error = %v, want unknown wrapper option", err)
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

func TestGitOptionsDropsOnlyInitialRedundantChangeDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		argv []string
		want string
	}{
		{
			name: "redundant current directory",
			argv: []string{"-C", ".", "diff", "--staged"},
			want: "diff --staged",
		},
		{
			name: "real directory change remains policy visible",
			argv: []string{"-C", "../other", "diff", "--staged"},
			want: "-C ../other diff --staged",
		},
		{
			name: "operation change detection flag is untouched",
			argv: []string{"diff", "-C", "."},
			want: "diff -C .",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			options, err := gitOptions(test.argv, t.TempDir(), false)
			if err != nil {
				t.Fatalf("gitOptions: %v", err)
			}

			if got := strings.Join(options.Argv, " "); got != test.want {
				t.Fatalf("argv = %q, want %q", got, test.want)
			}
		})
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
	fixture := t.TempDir()
	marker := filepath.Join(fixture, "git-ran")

	realGit := filepath.Join(fixture, "git")

	inlineErr0 := os.WriteFile(
		realGit,
		[]byte(
			"#!/bin/sh\n"+
				"if [ \"$1\" = \"--version\" ]; then printf 'git version fake\\n'; exit 0; fi\n"+
				"printf '%s\\n' \"$@\" > "+marker+"\n",
		),
		0o600,
	)
	if inlineErr0 != nil {
		t.Fatalf("write fake git: %v", inlineErr0)
	}

	inlineErr1 := os.Chmod(realGit, 0o700)
	if inlineErr1 != nil {
		t.Fatalf("chmod fake git: %v", inlineErr1)
	}

	runOutsideGitWorkTree(t, func() {
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatalf("get outside-worktree cwd: %v", err)
		}

		err = executeGitWithPostChecks(
			policy.ExampleBundle(),
			realGit,
			gitwrap.Options{
				Cwd:  cwd,
				Argv: []string{"status", "--short"},
			},
			false,
		)
		if err != nil {
			t.Fatalf("executeGitWithPostChecks() error = %v", err)
		}
	})

	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read fake git marker: %v", err)
	}

	if strings.TrimSpace(string(data)) != "status\n--short" {
		t.Fatalf("fake git argv = %q", string(data))
	}
}

func TestExposeRealGitForPolicyEvaluationRestoresEnvironment(t *testing.T) {
	testlock.ProcessState(t, "coding-ethos-git-env")

	t.Setenv(realgit.Env, "/previous/git")

	restore := exposeRealGitForPolicyEvaluation("/real/git")
	if got := os.Getenv(realgit.Env); got != "/real/git" {
		t.Fatalf("%s = %q, want real git", realgit.Env, got)
	}

	restore()
	if got := os.Getenv(realgit.Env); got != "/previous/git" {
		t.Fatalf("%s = %q, want restored", realgit.Env, got)
	}
}

func TestRunUsesProcessArgs(t *testing.T) {
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
			Evidence: map[string]any{
				"files": []string{"bin/coding-ethos-run"},
			},
		}},
	}

	output := captureGitStderr(t, func() {
		printBlocked(result)
	})
	for _, want := range []string{
		"status: blocked",
		"policy_id: git.hook_bypass",
		"message: blocked",
		"suggestion: fix it",
		"files[1]{path}:",
		"bin/coding-ethos-run",
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

	root := policyGitCheckoutRoot(t)
	previousCeiling, ceilingExisted := os.LookupEnv("GIT_CEILING_DIRECTORIES")
	if err := os.Setenv("GIT_CEILING_DIRECTORIES", root); err != nil {
		t.Fatalf("set GIT_CEILING_DIRECTORIES: %v", err)
	}
	defer func() {
		if ceilingExisted {
			if err := os.Setenv("GIT_CEILING_DIRECTORIES", previousCeiling); err != nil {
				t.Fatalf("restore GIT_CEILING_DIRECTORIES: %v", err)
			}

			return
		}

		if err := os.Unsetenv("GIT_CEILING_DIRECTORIES"); err != nil {
			t.Fatalf("unset GIT_CEILING_DIRECTORIES: %v", err)
		}
	}()

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

func policyGitCheckoutRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}

	for {
		if policyGitFileExists(filepath.Join(dir, "coding_ethos.yml")) &&
			policyGitFileExists(filepath.Join(dir, "config.yaml")) {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("coding-ethos checkout root not found from %s", dir)
		}

		dir = parent
	}
}

func policyGitFileExists(path string) bool {
	info, err := os.Stat(path)

	return err == nil && !info.IsDir()
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
