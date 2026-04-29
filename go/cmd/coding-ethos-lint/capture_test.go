// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestRunCapturedToolLogsRuffTrace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture uses POSIX sh")
	}

	repo := t.TempDir()
	tool := filepath.Join(repo, "ruff-fixture")
	if err := os.WriteFile(
		tool,
		[]byte(`#!/usr/bin/env sh
case " $* " in
  *" --output-format=json "*) ;;
  *) echo "missing json output flag" >&2; exit 2 ;;
esac
printf '%s\n' '[{"filename":"pkg/app.py","code":"F401","message":"unused import","location":{"row":4,"column":8}}]'
exit 1
`),
		0o700,
	); err != nil {
		t.Fatalf("write fixture tool: %v", err)
	}

	t.Setenv("CODE_ETHOS_HOOK_OUTPUT_FORMAT", "toon")
	output := captureStdout(t, func() {
		exitCode := runCapturedTool("ruff", tool, repo, []string{"check", "pkg/app.py"})
		if exitCode != 1 {
			t.Fatalf("exit code = %d, want 1", exitCode)
		}
	})
	for _, want := range []string{
		"format: toon",
		"tool: ruff",
		"status: FAIL",
		"ruff,pkg/app.py,4,8,error,F401,,unused import",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("normalized output missing %q:\n%s", want, output)
		}
	}

	matches, err := filepath.Glob(filepath.Join(repo, ".coding-ethos", "lint-runs", "*.json"))
	if err != nil {
		t.Fatalf("glob traces: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("trace files = %#v", matches)
	}

	content, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	for _, want := range []string{
		`"scope": "tool:ruff"`,
		`"source_tool": "ruff"`,
		`"code": "F401"`,
		`"message": "unused import"`,
	} {
		if !strings.Contains(string(content), want) {
			t.Fatalf("trace missing %q:\n%s", want, content)
		}
	}
}

func TestRunCapturedToolLogsShellcheckTrace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture uses POSIX sh")
	}

	repo := t.TempDir()
	tool := filepath.Join(repo, "shellcheck-fixture")
	if err := os.WriteFile(
		tool,
		[]byte(`#!/usr/bin/env sh
case " $* " in
  *" --format=json "*) ;;
  *) echo "missing shellcheck json flag" >&2; exit 2 ;;
esac
printf '%s\n' '{"comments":[{"file":"script.sh","line":3,"column":7,"level":"warning","code":2086,"message":"Double quote"}]}'
exit 1
`),
		0o700,
	); err != nil {
		t.Fatalf("write fixture tool: %v", err)
	}

	exitCode := runCapturedTool("shellcheck", tool, repo, []string{"script.sh"})
	if exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}

	content := singleTraceContent(t, repo)
	for _, want := range []string{
		`"scope": "tool:shellcheck"`,
		`"source_tool": "shellcheck"`,
		`"code": "SC2086"`,
		`"message": "Double quote"`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("trace missing %q:\n%s", want, content)
		}
	}
}

func TestCapturedToolArgsForceMachineReadableOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tool string
		args []string
		want []string
	}{
		{
			name: "ruff",
			tool: "ruff",
			args: []string{"check", "pkg"},
			want: []string{"check", "--output-format=json", "pkg"},
		},
		{
			name: "pyright",
			tool: "pyright",
			args: []string{"pkg"},
			want: []string{"--outputjson", "pkg"},
		},
		{
			name: "mypy",
			tool: "mypy",
			args: []string{"pkg"},
			want: []string{"--output=json", "pkg"},
		},
		{
			name: "pylint",
			tool: "pylint",
			args: []string{"pkg"},
			want: []string{"--output-format=json", "pkg"},
		},
		{
			name: "shellcheck",
			tool: "shellcheck",
			args: []string{"script.sh"},
			want: []string{"--format=json", "script.sh"},
		},
		{
			name: "yamllint",
			tool: "yamllint",
			args: []string{"config.yaml"},
			want: []string{"-f", "parsable", "config.yaml"},
		},
		{
			name: "hadolint",
			tool: "hadolint",
			args: []string{"Dockerfile"},
			want: []string{"--format", "json", "Dockerfile"},
		},
		{
			name: "actionlint",
			tool: "actionlint",
			args: []string{".github/workflows/ci.yml"},
			want: []string{"-format", "{{json .}}", ".github/workflows/ci.yml"},
		},
		{
			name: "golangci",
			tool: "golangci-lint",
			args: []string{"run", "./..."},
			want: []string{
				"run",
				"--output.json.path=stdout",
				"--output.text.path=stderr",
				"./...",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := capturedToolArgs(test.tool, test.args)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("capturedToolArgs() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestCapturedToolArgsOverrideExplicitOutputFormat(t *testing.T) {
	t.Parallel()

	args := []string{"check", "--output-format=github", "pkg"}
	got := capturedToolArgs("ruff", args)
	want := []string{"check", "--output-format=json", "pkg"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("capturedToolArgs() = %#v, want %#v", got, want)
	}
}

func singleTraceContent(t *testing.T, repo string) string {
	t.Helper()

	matches, err := filepath.Glob(filepath.Join(repo, ".coding-ethos", "lint-runs", "*.json"))
	if err != nil {
		t.Fatalf("glob traces: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("trace files = %#v", matches)
	}

	content, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}

	return string(content)
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	os.Stdout = writer

	output := make(chan string, 1)
	go func() {
		var buffer bytes.Buffer
		_, _ = io.Copy(&buffer, reader)
		output <- buffer.String()
	}()

	fn()

	if err := writer.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	os.Stdout = oldStdout

	return <-output
}
