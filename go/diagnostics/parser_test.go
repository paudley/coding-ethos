// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package diagnostics_test

import (
	"reflect"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/diagnostics"
)

func TestParseRuffDiagnostics(t *testing.T) {
	t.Parallel()

	parsed := diagnostics.Parse(
		"ruff",
		ruffFixture,
		"",
	)

	assertDiagnostic(t, parsed, diagnostics.Diagnostic{
		Tool:     "ruff",
		File:     "pkg/app.py",
		Line:     4,
		Column:   8,
		Severity: "error",
		Code:     "F401",
		Message:  "unused import",
	})
}

func TestParseRuffTextDiagnostics(t *testing.T) {
	t.Parallel()

	parsed := diagnostics.Parse(
		"ruff",
		"pkg/app.py:4:8: F401 unused import\n",
		"",
	)

	assertDiagnostic(t, parsed, diagnostics.Diagnostic{
		Tool:     "ruff",
		File:     "pkg/app.py",
		Line:     4,
		Column:   8,
		Severity: "error",
		Code:     "F401",
		Message:  "unused import",
	})
}

func TestParsePreservesStdoutAndStderrDiagnostics(t *testing.T) {
	t.Parallel()

	parsed := diagnostics.Parse(
		"ruff",
		"pkg/stdout.py:4:8: F401 unused import\n",
		"pkg/stderr.py:9:2: F821 undefined name\n",
	)

	if len(parsed) != 2 {
		t.Fatalf("diagnostics.Parse() = %#v, want two diagnostics", parsed)
	}

	assertDiagnosticAt(t, parsed, 0, diagnostics.Diagnostic{
		Tool:     "ruff",
		File:     "pkg/stdout.py",
		Line:     4,
		Column:   8,
		Severity: "error",
		Code:     "F401",
		Message:  "unused import",
	})
	assertDiagnosticAt(t, parsed, 1, diagnostics.Diagnostic{
		Tool:     "ruff",
		File:     "pkg/stderr.py",
		Line:     9,
		Column:   2,
		Severity: "error",
		Code:     "F821",
		Message:  "undefined name",
	})
}

func TestParseRuffFormatDiagnostics(t *testing.T) {
	t.Parallel()

	parsed := diagnostics.Parse(
		"ruff",
		"Would reformat: lib/python/pkg.py\n1 file would be reformatted\n",
		"",
	)

	assertDiagnostic(t, parsed, diagnostics.Diagnostic{
		Tool:     "ruff",
		File:     "lib/python/pkg.py",
		Line:     1,
		Column:   1,
		Severity: "error",
		Code:     "format",
		Message:  "File would be reformatted by ruff format.",
	})
}

func TestParsePyrightDiagnostics(t *testing.T) {
	t.Parallel()

	parsed := diagnostics.Parse(
		"pyright",
		pyrightFixture,
		"",
	)

	assertDiagnostic(t, parsed, diagnostics.Diagnostic{
		Tool:     "pyright",
		File:     "pkg/app.py",
		Line:     5,
		Column:   3,
		Severity: "error",
		Code:     "reportAssignmentType",
		Message:  "bad type",
	})
}

func TestParseMypyJSONLinesDiagnostics(t *testing.T) {
	t.Parallel()

	parsed := diagnostics.Parse(
		"mypy",
		mypyFixture,
		"",
	)

	assertDiagnostic(t, parsed, diagnostics.Diagnostic{
		Tool:     "mypy",
		File:     "pkg/app.py",
		Line:     88,
		Column:   12,
		Severity: "error",
		Code:     "no-any-return",
		Message:  "Returning Any",
	})
}

func TestParseMypyTextDiagnostics(t *testing.T) {
	t.Parallel()

	parsed := diagnostics.Parse(
		"mypy",
		"pkg/app.py:88:12: error: Returning Any [no-any-return]\n",
		"",
	)

	assertDiagnostic(t, parsed, diagnostics.Diagnostic{
		Tool:     "mypy",
		File:     "pkg/app.py",
		Line:     88,
		Column:   12,
		Severity: "error",
		Code:     "no-any-return",
		Message:  "Returning Any",
	})
}

func TestParsePylintJSON2Diagnostics(t *testing.T) {
	t.Parallel()

	parsed := diagnostics.Parse(
		"pylint",
		`{"messages":[{"path":"pkg/app.py","line":7,"column":4,`+
			`"type":"warning","symbol":"unused-import",`+
			`"messageId":"W0611","message":"Unused import os"}]}`,
		"",
	)

	assertDiagnostic(t, parsed, diagnostics.Diagnostic{
		Tool:     "pylint",
		File:     "pkg/app.py",
		Line:     7,
		Column:   5,
		Severity: "warning",
		Code:     "unused-import",
		Message:  "Unused import os",
	})
}

func TestParseESLintJSONDiagnostics(t *testing.T) {
	t.Parallel()

	parsed := diagnostics.Parse(
		"eslint",
		`[{"filePath":"web/app.js","messages":[`+
			`{"ruleId":"no-undef","severity":2,"message":"`+
			`'tool' is not defined.","line":3,"column":10,`+
			`"endLine":3,"endColumn":14},`+
			`{"ruleId":"no-console","severity":1,"message":"`+
			`Unexpected console statement.","line":4,"column":3}`+
			`]}]`,
		"",
	)

	if len(parsed) != 2 {
		t.Fatalf("diagnostics.Parse() = %#v, want two diagnostics", parsed)
	}

	endMetadata := parsed[0].Metadata
	parsed[0].Metadata = nil
	assertDiagnosticAt(t, parsed, 0, diagnostics.Diagnostic{
		Tool:     "eslint",
		File:     "web/app.js",
		Line:     3,
		Column:   10,
		Severity: "error",
		Code:     "no-undef",
		Message:  "'tool' is not defined.",
	})
	assertDiagnosticAt(t, parsed, 1, diagnostics.Diagnostic{
		Tool:     "eslint",
		File:     "web/app.js",
		Line:     4,
		Column:   3,
		Severity: "warning",
		Code:     "no-console",
		Message:  "Unexpected console statement.",
	})

	if endMetadata["end_line"] != 3 || endMetadata["end_column"] != 14 {
		t.Fatalf("eslint end location metadata = %#v", endMetadata)
	}
}

func TestParseESLintFatalDiagnostic(t *testing.T) {
	t.Parallel()

	parsed := diagnostics.Parse(
		"eslint",
		`[{"filePath":"web/app.js","messages":[`+
			`{"ruleId":null,"fatal":true,"severity":2,`+
			`"message":"Parsing error: Unexpected token","line":1,"column":7}`+
			`]}]`,
		"",
	)

	assertDiagnostic(t, parsed, diagnostics.Diagnostic{
		Tool:     "eslint",
		File:     "web/app.js",
		Line:     1,
		Column:   7,
		Severity: "error",
		Code:     "fatal",
		Message:  "Parsing error: Unexpected token",
	})
}

func TestParseTSCDiagnostics(t *testing.T) {
	t.Parallel()

	parsed := diagnostics.Parse(
		"tsc",
		"src/index.ts(3,7): error TS2322: "+
			"Type 'string' is not assignable to type 'number'.\n",
		"",
	)

	assertDiagnostic(t, parsed, diagnostics.Diagnostic{
		Tool:     "tsc",
		File:     "src/index.ts",
		Line:     3,
		Column:   7,
		Severity: "error",
		Code:     "TS2322",
		Message:  "Type 'string' is not assignable to type 'number'.",
	})
}

func TestParseTSCPathlessDiagnostic(t *testing.T) {
	t.Parallel()

	parsed := diagnostics.Parse(
		"tsc",
		"error TS5058: The specified path does not exist: 'tsconfig.json'.\n",
		"",
	)

	assertDiagnostic(t, parsed, diagnostics.Diagnostic{
		Tool:     "tsc",
		Severity: "error",
		Code:     "TS5058",
		Message:  "The specified path does not exist: 'tsconfig.json'.",
	})
}

func TestParseKubeLinterDiagnostic(t *testing.T) {
	t.Parallel()

	parsed := diagnostics.Parse("kube-linter", kubeLinterPayload(), "")

	assertDiagnostic(t, parsed, diagnostics.Diagnostic{
		Tool:     "kube-linter",
		File:     "deploy/pod.yaml",
		Severity: "error",
		Code:     "privileged-container",
		Message:  `container "app" is privileged`,
		Advice:   "Do not run your container as privileged unless it is required.",
		Metadata: map[string]any{
			"kind":      "Pod",
			"name":      "unsafe-pod",
			"namespace": "default",
			"version":   "v1",
		},
	})
}

func TestParseKubeLinterWarningDiagnostic(t *testing.T) {
	t.Parallel()

	parsed := diagnostics.Parse(
		"kube-linter",
		kubeLinterPayloadWithSeverity("warning"),
		"",
	)

	assertDiagnostic(t, parsed, diagnostics.Diagnostic{
		Tool:     "kube-linter",
		File:     "deploy/pod.yaml",
		Severity: "warning",
		Code:     "privileged-container",
		Message:  `container "app" is privileged`,
		Advice:   "Do not run your container as privileged unless it is required.",
		Metadata: map[string]any{
			"kind":      "Pod",
			"name":      "unsafe-pod",
			"namespace": "default",
			"version":   "v1",
		},
	})
}

func TestParseKubeLinterEmptyAndMalformedOutput(t *testing.T) {
	t.Parallel()

	for _, output := range []string{
		`{"Reports":[]}`,
		`not json`,
	} {
		if parsed := diagnostics.Parse("kube-linter", output, ""); len(parsed) != 0 {
			t.Fatalf("Parse(kube-linter, %q) = %#v, want no diagnostics", output, parsed)
		}
	}
}

func TestParseKubeLinterMultipleReports(t *testing.T) {
	t.Parallel()

	parsed := diagnostics.Parse(
		"kube-linter",
		`{"Reports":[`+
			kubeLinterReportPayload("latest-tag", "error")+
			`,`+
			kubeLinterReportPayload("run-as-non-root", "warning")+
			`]}`,
		"",
	)

	assertDiagnosticAt(t, parsed, 0, diagnostics.Diagnostic{
		Tool:     "kube-linter",
		File:     "deploy/pod.yaml",
		Severity: "error",
		Code:     "latest-tag",
		Message:  `container "app" is privileged`,
		Advice:   "Do not run your container as privileged unless it is required.",
		Metadata: map[string]any{
			"kind":      "Pod",
			"name":      "unsafe-pod",
			"namespace": "default",
			"version":   "v1",
		},
	})
	assertDiagnosticAt(t, parsed, 1, diagnostics.Diagnostic{
		Tool:     "kube-linter",
		File:     "deploy/pod.yaml",
		Severity: "warning",
		Code:     "run-as-non-root",
		Message:  `container "app" is privileged`,
		Advice:   "Do not run your container as privileged unless it is required.",
		Metadata: map[string]any{
			"kind":      "Pod",
			"name":      "unsafe-pod",
			"namespace": "default",
			"version":   "v1",
		},
	})
}

func TestParseTypeCheckerPolicyCodes(t *testing.T) {
	t.Parallel()

	tests := []toolchainDiagnosticCase{
		{
			name: "pyright optional member",
			tool: "pyright",
			output: `{"generalDiagnostics":[{"file":"pkg/app.py","severity":"error",` +
				`"message":"\"run\" is not a known attribute of \"None\"",` +
				`"rule":"reportOptionalMemberAccess",` +
				`"range":{"start":{"line":10,"character":6}}}]}`,
			want: diagnostics.Diagnostic{
				Tool:     "pyright",
				File:     "pkg/app.py",
				Line:     11,
				Column:   7,
				Severity: "error",
				Code:     "reportOptionalMemberAccess",
				Message:  `"run" is not a known attribute of "None"`,
			},
		},
		{
			name: "mypy no untyped def",
			tool: "mypy",
			output: "pkg/app.py:12:1: error: " +
				"Function is missing a type annotation [no-untyped-def]",
			want: diagnostics.Diagnostic{
				Tool:     "mypy",
				File:     "pkg/app.py",
				Line:     12,
				Column:   1,
				Severity: "error",
				Code:     "no-untyped-def",
				Message:  "Function is missing a type annotation",
			},
		},
		{
			name: "pylint no member",
			tool: "pylint",
			output: `[{"path":"pkg/app.py","line":20,"column":10,` +
				`"type":"error","symbol":"no-member","messageId":"E1101",` +
				`"message":"Instance has no member \"run\""}]`,
			want: diagnostics.Diagnostic{
				Tool:     "pylint",
				File:     "pkg/app.py",
				Line:     20,
				Column:   11,
				Severity: "error",
				Code:     "no-member",
				Message:  `Instance has no member "run"`,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assertDiagnostic(
				t,
				diagnostics.Parse(test.tool, test.output, ""),
				test.want,
			)
		})
	}
}

func TestParseGolangciLintDiagnostics(t *testing.T) {
	t.Parallel()

	parsed := diagnostics.Parse(
		"golangci-lint",
		golangciFixture,
		"",
	)

	assertDiagnostic(t, parsed, diagnostics.Diagnostic{
		Tool:     "golangci-lint",
		File:     "pkg/app.go",
		Line:     8,
		Column:   2,
		Severity: "error",
		Code:     "ineffassign",
		Message:  "ineffectual assignment to err",
	})
}

func TestParseGoTestDiagnosticsSuppressesPassingPackages(t *testing.T) {
	t.Parallel()

	parsed := diagnostics.Parse(
		"go-test",
		goTestFixture,
		"",
	)

	if len(parsed) != 2 {
		t.Fatalf("diagnostic count = %d, want 2: %#v", len(parsed), parsed)
	}

	if got := parsed[0]; got.Tool != "go-test" ||
		got.File != "go/cmd/coding-ethos-policy/main_test.go" ||
		got.Line != 42 ||
		got.Code != "TestPolicyUsage" ||
		got.Message != "expected exit 0, got 1" {
		t.Fatalf("first diagnostic = %#v", got)
	}

	if got := parsed[1]; got.Tool != "go-test" ||
		got.Code != "TestPolicyUsage" ||
		got.File != "go/cmd/coding-ethos-policy/other_test.go" ||
		got.Line != 17 ||
		got.Message != "second failure" {
		t.Fatalf("second diagnostic = %#v", got)
	}
}

func TestParseGoTestPackageFailureWithoutTestOutput(t *testing.T) {
	t.Parallel()

	parsed := diagnostics.Parse(
		"go-test",
		`{"Action":"fail","Package":"blackcat.ca/coding-ethos/go/cmd/tool","Elapsed":0.013}`,
		"",
	)

	assertDiagnostic(t, parsed, diagnostics.Diagnostic{
		Tool:     "go-test",
		Severity: "error",
		Code:     "package_failed",
		Message:  "Go test package failed: blackcat.ca/coding-ethos/go/cmd/tool",
	})
}

func TestParseGoTestCoverageDiagnostics(t *testing.T) {
	t.Parallel()

	parsed := diagnostics.Parse(
		"go-test",
		`{"Action":"output","Package":"blackcat.ca/coding-ethos/go/pkg",`+
			`"Output":"coverage: 82.4% of statements\n"}`+"\n"+
			`{"Action":"output","Package":"blackcat.ca/coding-ethos/go/pkg",`+
			`"Output":"pkg/app.go:12: App 74.2%\n"}`+"\n"+
			`{"Action":"output","Output":"total: (statements) 80.1%\n"}`,
		"",
	)

	if len(parsed) != 3 {
		t.Fatalf("go test coverage diagnostics = %#v, want 3", parsed)
	}

	assertDiagnosticAt(t, parsed, 0, diagnostics.Diagnostic{
		Metadata: map[string]any{
			"coverage_percent": 82.4,
			"package":          "blackcat.ca/coding-ethos/go/pkg",
		},
		Tool:     "go-test",
		Severity: "record",
		Code:     "coverage-package",
		Message:  "Go test coverage for blackcat.ca/coding-ethos/go/pkg is 82.40%.",
	})
	assertDiagnosticAt(t, parsed, 1, diagnostics.Diagnostic{
		Metadata: map[string]any{
			"coverage_percent": 74.2,
			"package":          "blackcat.ca/coding-ethos/go/pkg",
		},
		Tool:     "go-test",
		Severity: "record",
		Code:     "coverage-file",
		File:     "pkg/app.go",
		Line:     12,
		Message:  "Go test coverage for pkg/app.go is 74.20%.",
	})
	assertDiagnosticAt(t, parsed, 2, diagnostics.Diagnostic{
		Metadata: map[string]any{
			"coverage_percent": 80.1,
		},
		Tool:     "go-test",
		Severity: "record",
		Code:     "coverage-total",
		Message:  "Go test coverage is 80.10%.",
	})
}

func TestParseToolchainDiagnostics(t *testing.T) {
	t.Parallel()

	for _, test := range toolchainDiagnosticCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assertDiagnostic(
				t,
				diagnostics.Parse(test.tool, test.output, ""),
				test.want,
			)
		})
	}
}

func TestRegisteredParsersHaveFixtures(t *testing.T) {
	t.Parallel()

	fixtures := parserFixtureTools()
	for _, parser := range diagnostics.RegisteredParsers() {
		if !fixtures[parser] {
			t.Fatalf("registered parser %q has no parser fixture", parser)
		}
	}
}

func TestManagedAliasParsersUseBaseToolParsers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		tool   string
		output string
		want   string
	}{
		{
			tool:   "ruff-format",
			output: "Would reformat: pkg/example.py",
			want:   "ruff:pkg/example.py:format",
		},
		{
			tool: "golangci-lint-autofix",
			output: "go/internal/example.go:12:3: " +
				"line is 121 characters (lll)",
			want: "golangci-lint:go/internal/example.go:lll",
		},
	}

	for _, test := range tests {
		t.Run(test.tool, func(t *testing.T) {
			t.Parallel()

			parsed := diagnostics.Parse(test.tool, test.output, "")
			if len(parsed) == 0 {
				t.Fatalf("Parse(%q) returned no diagnostics", test.tool)
			}

			got := parsed[0].Tool + ":" + parsed[0].File + ":" + parsed[0].Code
			if got != test.want {
				t.Fatalf("Parse(%q) = %q, want %q", test.tool, got, test.want)
			}
		})
	}
}

func TestParserRegistryHelpers(t *testing.T) {
	t.Parallel()

	if !diagnostics.HasParser("ruff-format") {
		t.Fatal("expected ruff-format parser alias")
	}

	if diagnostics.HasParser("missing-tool") {
		t.Fatal("unexpected parser for missing tool")
	}

	if got := diagnostics.InferTool(nil); got != "" {
		t.Fatalf("InferTool(nil) = %q, want empty", got)
	}

	if got := diagnostics.Parse("ruff", "", ""); got != nil {
		t.Fatalf("Parse empty streams = %#v, want nil", got)
	}

	parsed := diagnostics.Parse("missing-tool", "pkg/app.py:7: warning: note", "")
	assertDiagnostic(t, parsed, diagnostics.Diagnostic{
		Tool:     "missing-tool",
		File:     "pkg/app.py",
		Line:     7,
		Severity: "warning",
		Message:  "note",
	})

	gemini := diagnostics.Parse(
		"gemini",
		`[{"severity":"info","file":"pkg/app.py","line":2,"message":"consider refactor"}]`,
		"",
	)
	assertDiagnostic(t, gemini, diagnostics.Diagnostic{
		Tool:     "gemini",
		File:     "pkg/app.py",
		Line:     2,
		Severity: "notice",
		Message:  "consider refactor",
	})
}

func parserFixtureTools() map[string]bool {
	fixtures := map[string]bool{
		"mypy":                  true,
		"pylint":                true,
		"pyright":               true,
		"ruff":                  true,
		"eslint":                true,
		"tsc":                   true,
		"kube-linter":           true,
		"ruff-autofix":          true,
		"ruff-format":           true,
		"pytest":                true,
		"gemini-check":          true,
		"gofmt-check":           true,
		"go-test":               true,
		"golangci-lint":         true,
		"golangci-lint-autofix": true,
		"golangci-lint-format":  true,
	}

	for _, test := range toolchainDiagnosticCases() {
		fixtures[test.tool] = true
	}

	return fixtures
}

type toolchainDiagnosticCase struct {
	name   string
	tool   string
	output string
	want   diagnostics.Diagnostic
}

func toolchainDiagnosticCases() []toolchainDiagnosticCase {
	tests := dockerAndShellToolchainDiagnosticCases()
	tests = append(tests, goToolchainDiagnosticCases()...)
	tests = append(tests, pythonToolchainDiagnosticCases()...)
	tests = append(tests, agentToolchainDiagnosticCases()...)
	tests = append(tests, yamlToolchainDiagnosticCases()...)
	tests = append(tests, configToolchainDiagnosticCases()...)

	return tests
}

func dockerAndShellToolchainDiagnosticCases() []toolchainDiagnosticCase {
	return []toolchainDiagnosticCase{
		{
			name: "hadolint-json",
			tool: "hadolint",
			output: `[{"file":"Dockerfile","line":3,"column":1,` +
				`"level":"warning","code":"DL3008",` +
				`"message":"Pin versions in apt get install."}]`,
			want: diagnostics.Diagnostic{
				Tool:     "hadolint",
				File:     "Dockerfile",
				Line:     3,
				Column:   1,
				Severity: "warning",
				Code:     "DL3008",
				Message:  "Pin versions in apt get install.",
			},
		},
		{
			name: "actionlint-jsonl",
			tool: "actionlint",
			output: `{"filepath":".github/workflows/ci.yml",` +
				`"line":12,"column":5,"kind":"syntax-check",` +
				`"message":"property \"run\" is not defined"}`,
			want: diagnostics.Diagnostic{
				Tool:     "actionlint",
				File:     ".github/workflows/ci.yml",
				Line:     12,
				Column:   5,
				Severity: "error",
				Code:     "syntax-check",
				Message:  `property "run" is not defined`,
			},
		},
		{
			name: "shellcheck-json",
			tool: "shellcheck",
			output: `{"comments":[{"file":"script.sh","line":3,` +
				`"column":7,"level":"warning","code":2086,` +
				`"message":"Double quote to prevent globbing and word splitting."}]}`,
			want: diagnostics.Diagnostic{
				Tool:     "shellcheck",
				File:     "script.sh",
				Line:     3,
				Column:   7,
				Severity: "warning",
				Code:     "SC2086",
				Message:  "Double quote to prevent globbing and word splitting.",
			},
		},
		{
			name: "shfmt-diff",
			tool: "shfmt",
			output: "--- scripts/run.sh.orig\n" +
				"+++ scripts/run.sh\n" +
				"@@ -8,3 +8,3 @@\n",
			want: diagnostics.Diagnostic{
				Tool:     "shfmt",
				File:     "scripts/run.sh",
				Line:     8,
				Column:   1,
				Severity: "error",
				Code:     "format",
				Message:  "Shell file is not shfmt-formatted.",
			},
		},
	}
}

func goToolchainDiagnosticCases() []toolchainDiagnosticCase {
	return []toolchainDiagnosticCase{
		{
			name:   "gofmt-filename-list",
			tool:   "gofmt",
			output: "go/pkg/app.go\n",
			want: diagnostics.Diagnostic{
				Tool:     "gofmt",
				File:     "go/pkg/app.go",
				Line:     1,
				Severity: "error",
				Code:     "format",
				Message:  "Go file is not gofmt-formatted.",
			},
		},
		{
			name:   "gofmt-check-filename-list",
			tool:   "gofmt-check",
			output: "go/pkg/check.go\n",
			want: diagnostics.Diagnostic{
				Tool:     "gofmt",
				File:     "go/pkg/check.go",
				Line:     1,
				Severity: "error",
				Code:     "format",
				Message:  "Go file is not gofmt-formatted.",
			},
		},
		{
			name: "go-vet-text",
			tool: "go-vet",
			output: "# blackcat.ca/coding-ethos/go/pkg\n" +
				"pkg/app.go:12:4: fmt.Println call has possible Printf formatting directive %s",
			want: diagnostics.Diagnostic{
				Metadata: map[string]any{
					"package": "blackcat.ca/coding-ethos/go/pkg",
				},
				Tool:     "go-vet",
				File:     "pkg/app.go",
				Line:     12,
				Column:   4,
				Severity: "error",
				Code:     "vet",
				Message:  "fmt.Println call has possible Printf formatting directive %s",
			},
		},
	}
}

func pythonToolchainDiagnosticCases() []toolchainDiagnosticCase {
	return []toolchainDiagnosticCase{
		{
			name:   "vulture-text",
			tool:   "vulture",
			output: "pkg/app.py:17: unused function 'helper' (60% confidence)",
			want: diagnostics.Diagnostic{
				Tool:     "vulture",
				File:     "pkg/app.py",
				Line:     17,
				Severity: "warning",
				Code:     "unused-code",
				Message:  "unused function 'helper' (60% confidence)",
			},
		},
		{
			name: "radon-complexity-json",
			tool: "radon-complexity",
			output: `{"pkg/app.py":[{"type":"function","rank":"C",` +
				`"name":"build_payload","lineno":8,"complexity":19}]}`,
			want: diagnostics.Diagnostic{
				Metadata: map[string]any{
					"rank":       "C",
					"type":       "function",
					"complexity": 19,
					"threshold":  15,
				},
				Tool:     "radon-complexity",
				File:     "pkg/app.py",
				Line:     8,
				Severity: "error",
				Code:     "cyclomatic-complexity",
				Message:  "build_payload",
				Detail:   "complexity: 19",
			},
		},
		{
			name:   "radon-maintainability-json",
			tool:   "radon-maintainability",
			output: `{"pkg/app.py":{"mi":42.5,"rank":"C"}}`,
			want: diagnostics.Diagnostic{
				Metadata: map[string]any{
					"rank":      "C",
					"mi":        42.5,
					"threshold": 50,
				},
				Tool:     "radon-maintainability",
				File:     "pkg/app.py",
				Severity: "warning",
				Code:     "maintainability-index",
				Message:  "Maintainability index below configured threshold.",
				Detail:   "MI: 42.50",
			},
		},
		{
			name: "bandit-json",
			tool: "bandit",
			output: `{"results":[{"filename":"pkg/app.py","line_number":10,` +
				`"issue_severity":"HIGH","issue_confidence":"HIGH",` +
				`"test_id":"B602","issue_text":"subprocess call with shell=True"}]}`,
			want: diagnostics.Diagnostic{
				Tool:     "bandit",
				File:     "pkg/app.py",
				Line:     10,
				Severity: "error",
				Code:     "B602",
				Message:  "subprocess call with shell=True",
			},
		},
	}
}

func agentToolchainDiagnosticCases() []toolchainDiagnosticCase {
	return []toolchainDiagnosticCase{
		{
			name:   "pytest-text",
			tool:   "pytest-gate",
			output: "tests/test_app.py:42: AssertionError: expected true",
			want: diagnostics.Diagnostic{
				Tool:     "pytest",
				File:     "tests/test_app.py",
				Line:     42,
				Severity: "error",
				Code:     "test-failed",
				Message:  "AssertionError: expected true",
			},
		},
		{
			name:   "pytest-alias-text",
			tool:   "pytest",
			output: "tests/test_unit.py:11: ValueError: bad fixture",
			want: diagnostics.Diagnostic{
				Tool:     "pytest",
				File:     "tests/test_unit.py",
				Line:     11,
				Severity: "error",
				Code:     "test-failed",
				Message:  "ValueError: bad fixture",
			},
		},
		{
			name: "gemini-result-json",
			tool: "gemini",
			output: `{"verdict":"fail","violations":[{"severity":"CRITICAL",` +
				`"file":"pkg/app.py","line":7,"ethos_section":"security",` +
				`"message":"unsafe action"}]}`,
			want: diagnostics.Diagnostic{
				Tool:     "gemini",
				File:     "pkg/app.py",
				Line:     7,
				Severity: "error",
				Code:     "security",
				Message:  "unsafe action",
			},
		},
		{
			name: "gemini-check-array-json",
			tool: "gemini-check",
			output: `[{"severity":"MEDIUM","file":"pkg/other.py",` +
				`"line":3,"ethos_section":"testing","message":"missing test"}]`,
			want: diagnostics.Diagnostic{
				Tool:     "gemini",
				File:     "pkg/other.py",
				Line:     3,
				Severity: "warning",
				Code:     "testing",
				Message:  "missing test",
			},
		},
	}
}

func yamlToolchainDiagnosticCases() []toolchainDiagnosticCase {
	return []toolchainDiagnosticCase{
		{
			name:   "yamllint-parsable",
			tool:   "yamllint",
			output: `config.yaml:2:5: [error] wrong indentation (indentation)`,
			want: diagnostics.Diagnostic{
				Tool:     "yamllint",
				File:     "config.yaml",
				Line:     2,
				Column:   5,
				Severity: "error",
				Code:     "indentation",
				Message:  "wrong indentation",
			},
		},
		{
			name: "yamllint-path-with-colon",
			tool: "yamllint",
			output: strings.Join([]string{
				"configs/app:dev.yaml:9:3:",
				"[warning] missing document start (document-start)",
			}, " "),
			want: diagnostics.Diagnostic{
				Tool:     "yamllint",
				File:     "configs/app:dev.yaml",
				Line:     9,
				Column:   3,
				Severity: "warning",
				Code:     "document-start",
				Message:  "missing document start",
			},
		},
	}
}

func configToolchainDiagnosticCases() []toolchainDiagnosticCase {
	return []toolchainDiagnosticCase{
		{
			name: "sqlfluff-json",
			tool: "sqlfluff",
			output: `[{"filepath":"queries/app.sql","violations":[{` +
				`"line_no":2,"line_pos":7,"code":"LT01",` +
				`"description":"Expected single whitespace."}]}]`,
			want: diagnostics.Diagnostic{
				Tool:     "sqlfluff",
				File:     "queries/app.sql",
				Line:     2,
				Column:   7,
				Severity: "error",
				Code:     "LT01",
				Message:  "Expected single whitespace.",
			},
		},
		{
			name: "tombi-text",
			tool: "tombi",
			output: "\x1b[1;31m  Error\x1b[0m: invalid key\n" +
				"    at config.toml:2:4\n",
			want: diagnostics.Diagnostic{
				Tool:     "tombi",
				File:     "config.toml",
				Line:     2,
				Column:   4,
				Severity: "error",
				Message:  "invalid key",
			},
		},
		{
			name: "tombi-schema-text-with-help",
			tool: "tombi",
			output: "warning: unknown field `jobs`\n" +
				"  help: expected key from schema\n" +
				"    at pyproject.toml:12:1\n",
			want: diagnostics.Diagnostic{
				Tool:     "tombi",
				File:     "pyproject.toml",
				Line:     12,
				Column:   1,
				Severity: "warning",
				Message:  "unknown field `jobs`",
			},
		},
		{
			name:   "dotenv-linter-text",
			tool:   "dotenv-linter",
			output: `.env.example:3 LowercaseKey: The key should be uppercase`,
			want: diagnostics.Diagnostic{
				Tool:     "dotenv-linter",
				File:     ".env.example",
				Line:     3,
				Severity: "warning",
				Code:     "LowercaseKey",
				Message:  "The key should be uppercase",
			},
		},
	}
}

func TestParseTextToolDiagnostics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		tool   string
		output string
		want   diagnostics.Diagnostic
	}{
		{
			name:   "golangci-text",
			tool:   "golangci-lint",
			output: "pkg/app.go:8:2: unchecked error (errcheck)\n",
			want: diagnostics.Diagnostic{
				Tool:     "golangci-lint",
				File:     "pkg/app.go",
				Line:     8,
				Column:   2,
				Severity: "error",
				Code:     "errcheck",
				Message:  "unchecked error",
			},
		},
		{
			name:   "hadolint-text",
			tool:   "hadolint",
			output: "Dockerfile:3 DL3008 warning: Pin versions in apt get install.\n",
			want: diagnostics.Diagnostic{
				Tool:     "hadolint",
				File:     "Dockerfile",
				Line:     3,
				Severity: "warning",
				Code:     "DL3008",
				Message:  "Pin versions in apt get install.",
			},
		},
		{
			name: "actionlint-text",
			tool: "actionlint",
			output: ".github/workflows/ci.yml:12:5: " +
				"property \"run\" is not defined [syntax-check]\n",
			want: diagnostics.Diagnostic{
				Tool:     "actionlint",
				File:     ".github/workflows/ci.yml",
				Line:     12,
				Column:   5,
				Severity: "error",
				Code:     "syntax-check",
				Message:  `property "run" is not defined`,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assertDiagnostic(
				t,
				diagnostics.Parse(test.tool, test.output, ""),
				test.want,
			)
		})
	}
}

func TestInferToolRecognizesCommonDirectTools(t *testing.T) {
	t.Parallel()

	tests := map[string][]string{
		"actionlint":    {"actionlint", "-format", "{{json .}}"},
		"bandit":        {"python", "-m", "bandit", "-r", "pkg"},
		"golangci-lint": {"golangci-lint", "run"},
		"hadolint":      {"/usr/local/bin/hadolint", "Dockerfile"},
		"mypy":          {"uv", "run", "mypy", "pkg"},
		"custom-tool":   {"custom-tool", "arg"},
		"yamllint":      {"yamllint", "."},
	}
	for want, argv := range tests {
		if got := diagnostics.InferTool(argv); got != want {
			t.Fatalf("InferTool(%#v) = %q, want %q", argv, got, want)
		}
	}
}

func TestFallbackParserUsesStderrWhenStdoutEmpty(t *testing.T) {
	t.Parallel()

	parsed := diagnostics.Parse(
		"unknown-tool",
		"",
		"pkg/app.py:10:2: warning: fix this",
	)

	assertDiagnostic(t, parsed, diagnostics.Diagnostic{
		Tool:     "unknown-tool",
		File:     "pkg/app.py",
		Line:     10,
		Column:   2,
		Severity: "warning",
		Message:  "fix this",
	})
}

func TestDotenvLinterParserHandlesRoutineAndMultiFileOutput(t *testing.T) {
	t.Parallel()

	parsed := diagnostics.Parse(
		"dotenv-linter",
		"Checking .env\n"+
			"No problems found\n"+
			".env:3 LowercaseKey: The key should be uppercase\n"+
			".env.example: DuplicateName: The name is duplicated\n"+
			".env.missing: FileNotFound: The file does not exist\n",
		"",
	)

	if len(parsed) != 3 {
		t.Fatalf("dotenv diagnostics = %#v, want 3", parsed)
	}

	want := []diagnostics.Diagnostic{
		{
			Tool:     "dotenv-linter",
			File:     ".env",
			Line:     3,
			Severity: "warning",
			Code:     "LowercaseKey",
			Message:  "The key should be uppercase",
		},
		{
			Tool:     "dotenv-linter",
			File:     ".env.example",
			Severity: "warning",
			Code:     "DuplicateName",
			Message:  "The name is duplicated",
		},
		{
			Tool:     "dotenv-linter",
			File:     ".env.missing",
			Severity: "warning",
			Code:     "FileNotFound",
			Message:  "The file does not exist",
		},
	}

	for index := range want {
		assertDiagnostic(t, []diagnostics.Diagnostic{parsed[index]}, want[index])
	}
}

func TestYamllintParserHandlesMultipleDiagnostics(t *testing.T) {
	t.Parallel()

	parsed := diagnostics.Parse(
		"yamllint",
		"config.yaml:2:5: [error] wrong indentation (indentation)\n"+
			"configs/app:dev.yaml:9:3: [warning] missing document start (document-start)\n",
		"",
	)

	if len(parsed) != 2 {
		t.Fatalf("yamllint diagnostics = %#v, want 2", parsed)
	}

	assertDiagnosticAt(t, parsed, 0, diagnostics.Diagnostic{
		Tool:     "yamllint",
		File:     "config.yaml",
		Line:     2,
		Column:   5,
		Severity: "error",
		Code:     "indentation",
		Message:  "wrong indentation",
	})
	assertDiagnosticAt(t, parsed, 1, diagnostics.Diagnostic{
		Tool:     "yamllint",
		File:     "configs/app:dev.yaml",
		Line:     9,
		Column:   3,
		Severity: "warning",
		Code:     "document-start",
		Message:  "missing document start",
	})
}

func TestPytestParserHandlesSummaryFailures(t *testing.T) {
	t.Parallel()

	parsed := diagnostics.Parse(
		"pytest-gate",
		"FAILED tests/test_app.py::test_saves_record - AssertionError: expected save\n"+
			"ERROR tests/test_other.py - RuntimeError: fixture setup failed\n",
		"",
	)

	if len(parsed) != 2 {
		t.Fatalf("pytest diagnostics = %#v, want 2", parsed)
	}

	assertDiagnosticAt(t, parsed, 0, diagnostics.Diagnostic{
		Metadata: map[string]any{
			"test": "test_saves_record",
		},
		Tool:     "pytest",
		File:     "tests/test_app.py",
		Severity: "error",
		Code:     "pytest-failed",
		Message:  "AssertionError: expected save",
	})
	assertDiagnosticAt(t, parsed, 1, diagnostics.Diagnostic{
		Tool:     "pytest",
		File:     "tests/test_other.py",
		Severity: "error",
		Code:     "pytest-error",
		Message:  "RuntimeError: fixture setup failed",
	})
}

func TestPytestParserCapturesCoverageTotal(t *testing.T) {
	t.Parallel()

	parsed := diagnostics.Parse(
		"pytest-gate",
		"Name                Stmts   Miss  Cover\n"+
			"pkg/app.py             10      3    70%\n"+
			"TOTAL                 100     21    79.5%\n",
		"",
	)

	if len(parsed) != 2 {
		t.Fatalf("pytest coverage diagnostics = %#v, want 2", parsed)
	}

	assertDiagnosticAt(t, parsed, 0, diagnostics.Diagnostic{
		Metadata: map[string]any{
			"coverage_percent": 70.0,
		},
		Tool:     "pytest",
		File:     "pkg/app.py",
		Severity: "record",
		Code:     "coverage-file",
		Message:  "Pytest coverage for pkg/app.py is 70.00%.",
	})
	assertDiagnosticAt(t, parsed, 1, diagnostics.Diagnostic{
		Metadata: map[string]any{
			"coverage_percent": 79.5,
		},
		Tool:     "pytest",
		Severity: "record",
		Code:     "coverage-total",
		Message:  "Pytest coverage total is 79.50%.",
	})
}

func TestInferToolScansWrappedCommands(t *testing.T) {
	t.Parallel()

	tool := diagnostics.InferTool(
		[]string{"uv", "run", "--project", "/repo", "ruff", "check"},
	)
	if tool != "ruff" {
		t.Fatalf("InferTool() = %q, want ruff", tool)
	}
}

func TestEnrichMapsKnownDiagnosticEvidence(t *testing.T) {
	t.Parallel()

	enriched := diagnostics.Enrich(
		[]diagnostics.Diagnostic{
			{Tool: "ruff", Code: "PLC" + "0415", Message: "import outside top-level"},
			{Tool: "ruff", Code: "F401", Message: "unused import"},
		},
		[]diagnostics.EvidenceMap{
			{
				Source:       "ruff",
				Codes:        []string{"PLC" + "0415"},
				PolicyID:     "python.conditional_imports",
				SkillID:      "conditional-imports",
				PrincipleIDs: []string{"no-conditional-imports"},
				Confidence:   "high",
				Meaning:      "import away from module scope",
				Advice: diagnostics.EvidenceAdvice{
					Summary: "Move required imports to module scope.",
					Steps:   []string{"Import at module scope."},
					Rerun:   []string{"make pre-commit"},
				},
			},
		},
	)

	if enriched[0].PolicyID != "python.conditional_imports" {
		t.Fatalf("mapped policy = %q", enriched[0].PolicyID)
	}

	if enriched[0].Advice != "Move required imports to module scope." {
		t.Fatalf("mapped advice = %q", enriched[0].Advice)
	}

	if enriched[0].SkillID != "conditional-imports" {
		t.Fatalf("mapped skill = %q", enriched[0].SkillID)
	}

	if len(enriched[0].PrincipleIDs) != 1 ||
		enriched[0].PrincipleIDs[0] != "no-conditional-imports" {
		t.Fatalf("mapped principles = %#v", enriched[0].PrincipleIDs)
	}

	if enriched[1].PolicyID != "" {
		t.Fatalf("unmapped policy = %q, want empty", enriched[1].PolicyID)
	}
}

func TestEnrichMapsWildcardDiagnosticEvidence(t *testing.T) {
	t.Parallel()

	enriched := diagnostics.Enrich(
		[]diagnostics.Diagnostic{
			{Tool: "shellcheck", Code: "SC2086", Message: "quote expansion"},
		},
		[]diagnostics.EvidenceMap{
			{
				Source:   "shellcheck",
				Codes:    []string{"SC*"},
				PolicyID: "shell.static_analysis",
				Advice: diagnostics.EvidenceAdvice{
					Summary: "Fix shell diagnostics structurally.",
				},
			},
		},
	)

	if enriched[0].PolicyID != "shell.static_analysis" {
		t.Fatalf("wildcard policy = %q", enriched[0].PolicyID)
	}
}

func TestEnrichMapsMessageDiagnosticEvidence(t *testing.T) {
	t.Parallel()

	enriched := diagnostics.Enrich(
		[]diagnostics.Diagnostic{
			{
				Tool:    "pyright",
				Message: "Import cycle detected between pkg.a and pkg.b",
			},
		},
		[]diagnostics.EvidenceMap{
			{
				Source: "pyright",
				MessageSubstrings: []string{
					"import cycle detected",
				},
				PolicyID: "python.import_cycles",
				Advice: diagnostics.EvidenceAdvice{
					Summary: "Break the concrete dependency cycle.",
				},
			},
		},
	)

	if enriched[0].PolicyID != "python.import_cycles" {
		t.Fatalf("message-mapped policy = %q", enriched[0].PolicyID)
	}

	if enriched[0].Advice != "Break the concrete dependency cycle." {
		t.Fatalf("message-mapped advice = %q", enriched[0].Advice)
	}
}

func TestDedupeMergesSamePolicyLocation(t *testing.T) {
	t.Parallel()

	deduped := diagnostics.Dedupe([]diagnostics.Diagnostic{
		{
			Tool:         "mypy",
			File:         "pkg/app.py",
			Line:         12,
			Column:       4,
			Code:         "union-attr",
			PolicyID:     "python.optional_required_types",
			Message:      "Item None has no attribute run",
			Advice:       "Make the required contract explicit.",
			PrincipleIDs: []string{"no-optional-types-for-required-dependencies"},
		},
		{
			Tool:         "pyright",
			File:         "pkg/app.py",
			Line:         12,
			Column:       4,
			Code:         "reportOptionalMemberAccess",
			PolicyID:     "python.optional_required_types",
			Message:      `"run" is not a known attribute of None`,
			PrincipleIDs: []string{"static-analysis-is-the-first-line-of-defense"},
		},
	})

	if len(deduped) != 1 {
		t.Fatalf("deduped = %#v", deduped)
	}

	if deduped[0].Tool != "mypy" ||
		!strings.Contains(deduped[0].Detail, "pyright:reportOptionalMemberAccess") {
		t.Fatalf("merged diagnostic = %#v", deduped[0])
	}

	if len(deduped[0].PrincipleIDs) != 2 {
		t.Fatalf("principles = %#v", deduped[0].PrincipleIDs)
	}
}

func TestDedupeKeepsUnkeyedDiagnosticsAndFillsScalars(t *testing.T) {
	t.Parallel()

	deduped := diagnostics.Dedupe([]diagnostics.Diagnostic{
		{Message: "global warning"},
		{
			Tool:    "ruff",
			File:    "pkg/app.py",
			Line:    4,
			Code:    "F401",
			Message: "unused import",
		},
		{
			Tool:        "ruff",
			File:        "pkg/app.py",
			Line:        4,
			Code:        "F401",
			Severity:    "error",
			SkillID:     "lint-remediation",
			Advice:      "remove the unused import",
			AdviceSteps: []string{"edit file"},
			Rerun:       []string{"ruff check"},
			Tags:        []string{"python"},
		},
	})

	if len(deduped) != 2 {
		t.Fatalf("deduped diagnostics = %#v", deduped)
	}

	merged := deduped[1]
	if merged.Code != "F401" ||
		merged.Severity != "error" ||
		merged.SkillID != "lint-remediation" ||
		len(merged.AdviceSteps) != 1 ||
		len(merged.Rerun) != 1 ||
		len(merged.Tags) != 1 {
		t.Fatalf("merged diagnostic = %#v", merged)
	}
}

func assertDiagnostic(
	t *testing.T,
	parsed []diagnostics.Diagnostic,
	want diagnostics.Diagnostic,
) {
	t.Helper()

	if len(parsed) != 1 {
		t.Fatalf("diagnostic count = %d, want 1: %#v", len(parsed), parsed)
	}

	got := parsed[0]
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("diagnostic = %#v, want %#v", got, want)
	}
}

func assertDiagnosticAt(
	t *testing.T,
	parsed []diagnostics.Diagnostic,
	index int,
	want diagnostics.Diagnostic,
) {
	t.Helper()

	if index >= len(parsed) {
		t.Fatalf("diagnostic index %d missing from %#v", index, parsed)
	}

	if got := parsed[index]; !reflect.DeepEqual(got, want) {
		t.Fatalf("diagnostic[%d] = %#v, want %#v", index, got, want)
	}
}

const ruffFixture = `[
  {
    "filename": "pkg/app.py",
    "code": "F401",
    "message": "unused import",
    "location": {"row": 4, "column": 8}
  }
]`

const pyrightFixture = `{
  "generalDiagnostics": [
    {
      "file": "pkg/app.py",
      "severity": "error",
      "message": "bad type",
      "rule": "reportAssignmentType",
      "range": {"start": {"line": 4, "character": 2}}
    }
  ]
}`

const mypyFixture = `{"file":"pkg/app.py","line":88,"column":12,` +
	`"severity":"error","code":"no-any-return","message":"Returning Any"}`

const golangciFixture = `level=warning msg="runner warning"
{
  "Issues": [
    {
      "FromLinter": "ineffassign",
      "Text": "ineffectual assignment to err",
      "Severity": "error",
      "Pos": {"Filename": "pkg/app.go", "Line": 8, "Column": 2}
    }
  ]
}`

const goTestFixture = `{"Action":"run","Package":"blackcat.ca/coding-ethos/` +
	`go/cmd/coding-ethos-policy","Test":"TestPolicyUsage"}` + "\n" +
	`{"Action":"output","Package":"blackcat.ca/coding-ethos/` +
	`go/cmd/coding-ethos-policy","Test":"TestPolicyUsage",` +
	`"Output":"    go/cmd/coding-ethos-policy/main_test.go:42: ` +
	`expected exit 0, got 1\n"}` + "\n" +
	`{"Action":"output","Package":"blackcat.ca/coding-ethos/` +
	`go/cmd/coding-ethos-policy","Test":"TestPolicyUsage",` +
	`"Output":"    go/cmd/coding-ethos-policy/other_test.go:17: ` +
	`second failure\n"}` + "\n" +
	`{"Action":"fail","Package":"blackcat.ca/coding-ethos/` +
	`go/cmd/coding-ethos-policy","Test":"TestPolicyUsage","Elapsed":0}` + "\n" +
	`{"Action":"pass","Package":"blackcat.ca/coding-ethos/go/internal/hooks",` +
	`"Elapsed":0.187}`

func kubeLinterPayload() string {
	return `{"Reports":[` +
		kubeLinterReportPayload("privileged-container", "") +
		`]}`
}

func kubeLinterPayloadWithSeverity(severity string) string {
	return `{"Reports":[` +
		kubeLinterReportPayload("privileged-container", severity) +
		`]}`
}

func kubeLinterReportPayload(check, severity string) string {
	severityField := ""
	if severity != "" {
		severityField = `"Severity":"` + severity + `",`
	}

	return `{"Diagnostic":{` + severityField +
		`"Message":"container \"app\" is privileged"},` +
		strings.ReplaceAll(`"Check":"CHECK",`, "CHECK", check) +
		`"Remediation":"Do not run your container as privileged unless it is required.",` +
		`"Object":{"Metadata":{"FilePath":"deploy/pod.yaml"},` +
		`"K8sObject":{"Namespace":"default","Name":"unsafe-pod",` +
		`"GroupVersionKind":{"Group":"","Version":"v1","Kind":"Pod"}}}}`
}
