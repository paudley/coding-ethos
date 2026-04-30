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

	"blackcat.ca/coding-ethos/go/diagnostics"
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
		exitCode := runCapturedTool(
			"ruff",
			tool,
			repo,
			[]string{"check", "pkg/app.py"},
			[]diagnostics.EvidenceMap{{
				Source:       "ruff",
				Codes:        []string{"F401"},
				PolicyID:     "python.unused_imports",
				PrincipleIDs: []string{"static-analysis-is-the-first-line-of-defense"},
				Advice: diagnostics.EvidenceAdvice{
					Summary: "Remove unused imports instead of suppressing Ruff.",
				},
			}},
		)
		if exitCode != 1 {
			t.Fatalf("exit code = %d, want 1", exitCode)
		}
	})
	for _, want := range []string{
		"format: toon",
		"tool: ruff",
		"status: FAIL",
		"ruff,pkg/app.py,4,8,error,F401,python.unused_imports,unused import,Remove unused imports instead of suppressing Ruff.",
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
		`"policy_id": "python.unused_imports"`,
		`"message": "unused import"`,
		`"advice": "Remove unused imports instead of suppressing Ruff."`,
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

	exitCode := runCapturedTool("shellcheck", tool, repo, []string{"script.sh"}, nil)
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

func TestCapturedFindingsClassifyUnparseableToolFailures(t *testing.T) {
	t.Parallel()

	findings := capturedFindings(
		"pyright",
		[]string{"pkg"},
		[]string{"--outputjson", "pkg"},
		2,
		"/work/repo",
		"",
		"pyright: config file not found in /work/repo/pyrightconfig.json",
		nil,
	)
	if len(findings) != 1 {
		t.Fatalf("findings = %#v", findings)
	}
	if findings[0].RawOutcome["category"] != "configuration_error" {
		t.Fatalf("raw outcome = %#v", findings[0].RawOutcome)
	}
	if !strings.Contains(findings[0].Message, "configuration or usage failed") {
		t.Fatalf("message = %q", findings[0].Message)
	}
	if findings[0].RawOutcome["output"] != "pyright: config file not found in <repo>/pyrightconfig.json" {
		t.Fatalf("raw outcome output = %#v", findings[0].RawOutcome)
	}
}

func TestRunCapturedToolLogsForcedStructuredFormats(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture uses POSIX sh")
	}

	tests := []struct {
		name      string
		tool      string
		args      []string
		required  string
		output    string
		wantCode  string
		wantTool  string
		wantFile  string
		wantError string
	}{
		{
			name:     "mypy",
			tool:     "mypy",
			args:     []string{"pkg"},
			required: "--output=json",
			output:   `{"file":"pkg/app.py","line":3,"column":4,"severity":"error","code":"no-any-return","message":"Returning Any"}`,
			wantCode: "no-any-return",
			wantTool: "mypy",
			wantFile: "pkg/app.py",
		},
		{
			name:     "pyright",
			tool:     "pyright",
			args:     []string{"pkg"},
			required: "--outputjson",
			output:   `{"generalDiagnostics":[{"file":"pkg/app.py","severity":"error","message":"bad type","rule":"reportAssignmentType","range":{"start":{"line":1,"character":2}}}]}`,
			wantCode: "reportAssignmentType",
			wantTool: "pyright",
			wantFile: "pkg/app.py",
		},
		{
			name:     "pylint",
			tool:     "pylint",
			args:     []string{"pkg"},
			required: "--output-format=json",
			output:   `[{"path":"pkg/app.py","type":"warning","symbol":"cyclic-import","message":"Cyclic import","line":9,"column":0}]`,
			wantCode: "cyclic-import",
			wantTool: "pylint",
			wantFile: "pkg/app.py",
		},
		{
			name:     "golangci-lint",
			tool:     "golangci-lint",
			args:     []string{"run", "./..."},
			required: "--output.json.path=stdout",
			output:   `{"Issues":[{"FromLinter":"errcheck","Text":"unchecked error","Severity":"error","Pos":{"Filename":"pkg/app.go","Line":8,"Column":2}}]}`,
			wantCode: "errcheck",
			wantTool: "golangci-lint",
			wantFile: "pkg/app.go",
		},
		{
			name:     "actionlint",
			tool:     "actionlint",
			args:     []string{".github/workflows/ci.yml"},
			required: "{{json .}}",
			output:   `{"filepath":".github/workflows/ci.yml","line":12,"column":5,"kind":"syntax-check","message":"property run is not defined"}`,
			wantCode: "syntax-check",
			wantTool: "actionlint",
			wantFile: ".github/workflows/ci.yml",
		},
		{
			name:     "hadolint",
			tool:     "hadolint",
			args:     []string{"Dockerfile"},
			required: "--format json",
			output:   `[{"file":"Dockerfile","line":3,"column":1,"level":"warning","code":"DL3008","message":"Pin versions in apt get install."}]`,
			wantCode: "DL3008",
			wantTool: "hadolint",
			wantFile: "Dockerfile",
		},
		{
			name:     "yamllint",
			tool:     "yamllint",
			args:     []string{"config.yaml"},
			required: "-f parsable",
			output:   `config.yaml:2:5: [error] wrong indentation (indentation)`,
			wantCode: "indentation",
			wantTool: "yamllint",
			wantFile: "config.yaml",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := t.TempDir()
			tool := writeCaptureFixtureTool(t, repo, test.required, test.output)

			exitCode := runCapturedTool(test.tool, tool, repo, test.args, nil)
			if exitCode != 1 {
				t.Fatalf("exit code = %d, want 1", exitCode)
			}

			content := singleTraceContent(t, repo)
			for _, want := range []string{
				`"source_tool": "` + test.wantTool + `"`,
				`"file": "` + test.wantFile + `"`,
				`"code": "` + test.wantCode + `"`,
			} {
				if !strings.Contains(content, want) {
					t.Fatalf("trace missing %q:\n%s", want, content)
				}
			}
		})
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

func writeCaptureFixtureTool(
	t *testing.T,
	repo string,
	required string,
	output string,
) string {
	t.Helper()

	tool := filepath.Join(repo, "tool-fixture")
	script := `#!/usr/bin/env sh
case " $* " in
  *"` + required + `"*) ;;
  *) echo "missing required output flags: ` + required + `" >&2; exit 2 ;;
esac
cat <<'EOF'
` + output + `
EOF
exit 1
`
	if err := os.WriteFile(tool, []byte(script), 0o700); err != nil {
		t.Fatalf("write fixture tool: %v", err)
	}

	return tool
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
