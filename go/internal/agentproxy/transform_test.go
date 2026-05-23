// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agentproxy_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/apperror"
)

type trimTransform struct{}

type runeTokenizer struct{}

func (runeTokenizer) Count(text string) int {
	return len([]rune(text))
}

func (trimTransform) Name() string {
	return "trim"
}

func (trimTransform) Apply(
	_ context.Context,
	input agentproxy.TransformInput,
) (agentproxy.TransformOutput, error) {
	return agentproxy.TransformOutput{
		Text:     strings.TrimSpace(input.Text),
		Metadata: input.Metadata,
		Record: agentproxy.TransformRecord{
			Reason: "normalize whitespace",
		},
	}, nil
}

func TestPipelineRecordsOrderedTokenAndHashEvidence(t *testing.T) {
	t.Parallel()

	output, err := agentproxy.NewPipeline(
		agentproxy.WhitespaceTokenizer{},
		trimTransform{},
	).Apply(
		context.Background(),
		agentproxy.TransformInput{Text: "  alpha beta  "},
	)
	if err != nil {
		t.Fatalf("apply pipeline: %v", err)
	}

	if output.Text != "alpha beta" {
		t.Fatalf("text = %q", output.Text)
	}

	if output.Record.Name != "trim" ||
		output.Record.InputHash == "" ||
		output.Record.OutputHash == "" ||
		output.Record.InputHash == output.Record.OutputHash ||
		output.Record.InputTokens != 2 ||
		output.Record.OutputTokens != 2 ||
		output.Record.BytesRemoved != 4 {
		t.Fatalf("record = %#v", output.Record)
	}

	if len(output.Records) != 1 || output.Records[0].Name != "trim" {
		t.Fatalf("records = %#v", output.Records)
	}
}

func TestPipelineClonesMetadataAndRejectsNilTransform(t *testing.T) {
	t.Parallel()

	metadata := map[string]string{"provider": "codex"}

	output, err := agentproxy.NewPipeline(nil).Apply(
		context.Background(),
		agentproxy.TransformInput{
			Metadata: metadata,
			Text:     "alpha beta",
		},
	)
	if err != nil {
		t.Fatalf("apply empty pipeline: %v", err)
	}

	output.Metadata["provider"] = "mutated"

	if metadata["provider"] != "codex" {
		t.Fatalf("pipeline leaked metadata mutation: %#v", metadata)
	}

	_, err = agentproxy.NewPipeline(nil, nil).Apply(
		context.Background(),
		agentproxy.TransformInput{Text: "alpha"},
	)

	if !errors.Is(err, apperror.StaticError("nil content transform")) {
		t.Fatalf("nil transform error = %v", err)
	}
}

func TestToolOutputCompressionPreservesHeadTailAndRecordsSavings(t *testing.T) {
	t.Parallel()

	input := strings.Join([]string{
		"$ go test ./...",
		"package alpha",
		"line 03: verbose compiler progress chunk with repeated metadata",
		"line 04: verbose compiler progress chunk with repeated metadata",
		"line 05: verbose compiler progress chunk with repeated metadata",
		"line 06: verbose compiler progress chunk with repeated metadata",
		"line 07: verbose compiler progress chunk with repeated metadata",
		"line 08",
		"FAIL",
	}, "\n") + "\n"

	output, err := agentproxy.NewPipeline(
		agentproxy.WhitespaceTokenizer{},
		agentproxy.ToolOutputCompressionTransform{
			MaxLines: 5,
			Head:     2,
			Tail:     2,
		},
	).Apply(
		context.Background(),
		agentproxy.TransformInput{
			Metadata: map[string]string{"provider": "codex"},
			Text:     input,
		},
	)
	if err != nil {
		t.Fatalf("apply compression: %v", err)
	}

	if !strings.Contains(output.Text, "$ go test ./...") ||
		!strings.Contains(output.Text, "package alpha") ||
		!strings.Contains(output.Text, "line 08") ||
		!strings.Contains(output.Text, "FAIL") {
		t.Fatalf("compressed output lost required context:\n%s", output.Text)
	}

	if strings.Contains(output.Text, "line 04:") {
		t.Fatalf("compressed output kept omitted body line:\n%s", output.Text)
	}

	if !strings.Contains(output.Text, "5 of 9 lines omitted") ||
		!strings.Contains(output.Text, "full output: "+output.Record.EvidencePath) ||
		output.Metadata["coding_ethos.compressed"] != "true" ||
		output.Metadata["coding_ethos.compressed_lines_omitted"] != "5" ||
		output.Metadata["coding_ethos.full_output_path"] == "" ||
		output.Record.Name != "tool-output-compression" ||
		output.Record.EvidencePath == "" ||
		output.Record.BytesRemoved <= 0 {
		t.Fatalf("compression record = %#v metadata = %#v output = %q",
			output.Record,
			output.Metadata,
			output.Text,
		)
	}

	assertEvidenceFileContains(t, output.Record.EvidencePath, input)
}

func TestToolOutputDiagnosticSummaryCondensesParsedDiagnostics(t *testing.T) {
	t.Parallel()

	input := strings.Join([]string{
		"checking package metadata",
		"src/app.ts(1,1): error TS2304: Cannot find name 'missing'.",
		"Files:              318",
		"Found 1 error in src/app.ts:1",
	}, "\n")

	output, err := agentproxy.NewPipeline(
		nil,
		agentproxy.ToolOutputDiagnosticSummaryTransform{Tool: "tsc"},
	).Apply(
		context.Background(),
		agentproxy.TransformInput{Text: input},
	)
	if err != nil {
		t.Fatalf("apply diagnostic summary: %v", err)
	}

	for _, expected := range []string{
		"diagnostic summary",
		"findings[1]{tool,file,line,column,severity,code,message}",
		"tsc,src/app.ts,1,1,error,TS2304,Cannot find name 'missing'.",
		"full output: " + output.Record.EvidencePath,
	} {
		if !strings.Contains(output.Text, expected) {
			t.Fatalf("diagnostic summary missing %q:\n%s", expected, output.Text)
		}
	}

	if strings.Contains(output.Text, "Files:              318") ||
		output.Record.Decision != "summarize" ||
		output.Record.FindingsCount != 1 ||
		output.Metadata["coding_ethos.diagnostic_summary"] != "true" ||
		output.Metadata["coding_ethos.full_output_path"] == "" {
		t.Fatalf("diagnostic summary output = %#v", output)
	}
}

func TestToolOutputCompressionPreservesPythonTracebackException(t *testing.T) {
	t.Parallel()

	lines := make([]string, 0, 46)
	lines = append(lines,
		"Traceback (most recent call last):",
		`  File "tests/test_cli.py", line 1, in test_cli`,
		"    run_cli()",
	)

	for index := range 40 {
		lines = append(lines, "  noisy dependency frame "+string(rune('a'+index%26)))
	}

	lines = append(lines,
		`  File "coding_ethos/cli.py", line 42, in run_cli`,
		"    raise ConfigError('missing repo root')",
		"coding_ethos.errors.ConfigError: missing repo root",
	)

	output, err := agentproxy.NewPipeline(
		nil,
		agentproxy.ToolOutputCompressionTransform{
			MaxLines: 10,
			Head:     3,
			Tail:     3,
		},
	).Apply(
		context.Background(),
		agentproxy.TransformInput{Text: strings.Join(lines, "\n")},
	)
	if err != nil {
		t.Fatalf("apply compression: %v", err)
	}

	for _, expected := range []string{
		"Traceback (most recent call last):",
		`File "tests/test_cli.py"`,
		`File "coding_ethos/cli.py"`,
		"coding_ethos.errors.ConfigError: missing repo root",
		"40 of 46 lines omitted",
	} {
		if !strings.Contains(output.Text, expected) {
			t.Fatalf("compressed traceback missing %q:\n%s", expected, output.Text)
		}
	}
}

func TestToolOutputCompressionNormalizesCRLFLines(t *testing.T) {
	t.Parallel()

	output, err := agentproxy.NewPipeline(
		nil,
		agentproxy.ToolOutputCompressionTransform{
			MaxLines: 5,
			Head:     2,
			Tail:     2,
		},
	).Apply(
		context.Background(),
		agentproxy.TransformInput{
			Text: strings.Join([]string{
				"line 01",
				"line 02",
				"line 03",
				"line 04",
				"line 05",
				"line 06",
			}, "\r\n") + "\r\n",
		},
	)
	if err != nil {
		t.Fatalf("apply compression: %v", err)
	}

	if strings.Contains(output.Text, "\r") {
		t.Fatalf("compressed output kept CRLF carriage returns:\n%q", output.Text)
	}

	if !strings.HasSuffix(output.Text, "\n") ||
		!strings.Contains(output.Text, "2 of 6 lines omitted") {
		t.Fatalf("compressed CRLF output = %q", output.Text)
	}

	if output.Record.PolicyID != "proxy.token_budget" ||
		output.Record.Decision != "truncate" {
		t.Fatalf("compressed CRLF record = %#v", output.Record)
	}
}

func TestToolOutputCompressionGoldenToolOutputs(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		lines    []string
		expected []string
	}{
		{
			name: "go test",
			lines: append(
				append(
					[]string{"ok  blackcat.ca/coding-ethos/go/internal/a  0.001s"},
					repeatedLines("ok  blackcat.ca/coding-ethos/go/internal/noise  0.001s", 32)...,
				),
				"--- FAIL: TestPolicy (0.00s)",
				"    policy_test.go:42: expected block",
				"FAIL",
			),
			expected: []string{"ok  blackcat", "TestPolicy", "policy_test.go:42", "FAIL"},
		},
		{
			name: "pytest",
			lines: append(
				append(
					[]string{
						"============================= test session starts =============================",
					},
					repeatedLines("tests/test_cli.py::test_generated_contract PASSED", 32)...,
				),
				"FAILED tests/test_cli.py::test_policy - AssertionError: missing finding",
			),
			expected: []string{
				"test session starts",
				"FAILED tests/test_cli.py",
				"AssertionError",
			},
		},
		{
			name: "tsc",
			lines: append(
				append(
					[]string{"src/app.ts(1,1): error TS2304: Cannot find name 'missing'."},
					repeatedLines("Files:              318", 32)...,
				),
				"Found 1 error in src/app.ts:1",
			),
			expected: []string{"TS2304", "Cannot find name", "Found 1 error"},
		},
		{
			name: "noisy linter",
			lines: append(
				append(
					[]string{"ruff...Failed"},
					repeatedLines("checking package metadata and cache state", 32)...,
				),
				"src/app.py:10:1: F401 unused import os",
			),
			expected: []string{"ruff...Failed", "F401", "unused import os"},
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			output, err := agentproxy.NewPipeline(
				nil,
				agentproxy.ToolOutputCompressionTransform{
					MaxLines: 8,
					Head:     2,
					Tail:     3,
				},
			).Apply(
				context.Background(),
				agentproxy.TransformInput{Text: strings.Join(testCase.lines, "\n")},
			)
			if err != nil {
				t.Fatalf("apply compression: %v", err)
			}

			for _, expected := range testCase.expected {
				if !strings.Contains(output.Text, expected) {
					t.Fatalf("%s output missing %q:\n%s",
						testCase.name,
						expected,
						output.Text,
					)
				}
			}
			if !strings.Contains(output.Text, "compressed tool output") {
				t.Fatalf("%s output was not compressed:\n%s", testCase.name, output.Text)
			}
			assertEvidenceFileContains(
				t,
				output.Record.EvidencePath,
				strings.Join(testCase.lines, "\n"),
			)
		})
	}
}

func TestToolOutputTokenBudgetPreservesTailFailure(t *testing.T) {
	t.Parallel()

	lines := []string{"$ pytest tests"}
	for range 40 {
		lines = append(lines, "collected test progress with repeated fixture output")
	}
	lines = append(lines, "E   AssertionError: expected failure remains visible")

	output, err := agentproxy.NewPipeline(
		nil,
		agentproxy.ToolOutputTokenBudgetTransform{
			MaxTokens:  128,
			HeadTokens: 32,
			TailTokens: 32,
		},
	).Apply(
		context.Background(),
		agentproxy.TransformInput{Text: strings.Join(lines, "\n")},
	)
	if err != nil {
		t.Fatalf("apply token budget: %v", err)
	}

	for _, expected := range []string{
		"token_budget: status=truncated max_tokens=128",
		"$ pytest tests",
		"full_output=" + output.Record.EvidencePath,
		"E   AssertionError: expected failure remains visible",
	} {
		if !strings.Contains(output.Text, expected) {
			t.Fatalf("token-budget output missing %q:\n%s", expected, output.Text)
		}
	}

	if output.Record.PolicyID != "proxy.token_budget" ||
		output.Record.Decision != "truncate" ||
		output.Record.EvidencePath == "" ||
		output.Record.OutputTokens > 128 ||
		output.Metadata["coding_ethos.token_budget_exceeded"] != "true" {
		t.Fatalf("token-budget record = %#v metadata = %#v", output.Record, output.Metadata)
	}

	assertEvidenceFileContains(
		t,
		output.Record.EvidencePath,
		strings.Join(lines, "\n"),
	)
}

func TestToolOutputTokenBudgetCountsDenseOneLinePayload(t *testing.T) {
	t.Parallel()

	payload := `{"events":[` + strings.Repeat(
		`{"id":"abcdef0123456789","value":"payload"},`,
		80,
	) + `]}`

	output, err := agentproxy.NewPipeline(
		nil,
		agentproxy.ToolOutputTokenBudgetTransform{
			MaxTokens:  128,
			HeadTokens: 24,
			TailTokens: 24,
		},
	).Apply(
		context.Background(),
		agentproxy.TransformInput{Text: payload},
	)
	if err != nil {
		t.Fatalf("apply dense token budget: %v", err)
	}

	if output.Record.Decision != "truncate" ||
		output.Record.InputTokens <= 128 ||
		output.Record.OutputTokens > 128 ||
		output.Record.OutputTokens >= output.Record.InputTokens ||
		output.Record.EvidencePath == "" ||
		!strings.Contains(output.Text, "token_budget: status=truncated") {
		t.Fatalf("dense token-budget output = %#v text=%q", output.Record, output.Text)
	}

	if strings.Contains(output.Text, payload) || len(output.Text) >= len(payload) {
		t.Fatalf("dense payload was not bounded:\n%s", output.Text)
	}

	assertEvidenceFileContains(t, output.Record.EvidencePath, payload)
}

func TestToolOutputTokenBudgetDefaultsToApproximateTokenizer(t *testing.T) {
	t.Parallel()

	payload := strings.Repeat("x", 260)
	output, err := agentproxy.ToolOutputTokenBudgetTransform{
		MaxTokens:  32,
		HeadTokens: 8,
		TailTokens: 8,
	}.Apply(context.Background(), agentproxy.TransformInput{Text: payload})
	if err != nil {
		t.Fatalf("apply token budget: %v", err)
	}

	if output.Record.Decision != "truncate" ||
		output.Record.InputTokens != (agentproxy.ApproximateTokenizer{}).Count(payload) ||
		output.Record.OutputTokens > 32 ||
		strings.Contains(output.Text, payload) {
		t.Fatalf("token-budget default tokenizer output = %#v text=%q",
			output.Record,
			output.Text,
		)
	}
}

func TestToolOutputTokenBudgetReusesLineCompressionEvidencePath(t *testing.T) {
	t.Parallel()

	lines := []string{"$ noisy command with important invocation"}
	for range 24 {
		lines = append(
			lines,
			"verbose progress output with enough repeated words to exceed budgets",
		)
	}
	lines = append(lines, "fatal: final diagnostic remains visible")
	input := strings.Join(lines, "\n")

	output, err := agentproxy.NewPipeline(
		nil,
		agentproxy.ToolOutputCompressionTransform{
			MaxLines: 6,
			Head:     2,
			Tail:     2,
		},
		agentproxy.ToolOutputTokenBudgetTransform{
			MaxTokens:  80,
			HeadTokens: 16,
			TailTokens: 16,
		},
	).Apply(
		context.Background(),
		agentproxy.TransformInput{Text: input},
	)
	if err != nil {
		t.Fatalf("apply compression and token budget: %v", err)
	}

	if len(output.Records) != 2 {
		t.Fatalf("records = %#v", output.Records)
	}

	compressionPath := output.Records[0].EvidencePath
	if output.Record.Name != "tool-output-token-budget" ||
		output.Record.EvidencePath != compressionPath ||
		output.Metadata["coding_ethos.full_output_path"] != compressionPath {
		t.Fatalf("record = %#v metadata = %#v", output.Record, output.Metadata)
	}

	if !strings.Contains(output.Text, "token_budget: status=truncated") ||
		!strings.Contains(output.Text, "full_output="+compressionPath) {
		t.Fatalf(
			"token-budget output did not expose original evidence path:\n%s",
			output.Text,
		)
	}

	if output.Record.OutputTokens > 80 {
		t.Fatalf("token-budget output exceeded max tokens: %#v", output.Record)
	}

	assertEvidenceFileContains(t, compressionPath, input)
}

func TestToolOutputTokenBudgetFitsControlTextWithinSmallBudget(t *testing.T) {
	t.Parallel()

	output, err := agentproxy.ToolOutputTokenBudgetTransform{
		MaxTokens:  32,
		HeadTokens: 8,
		TailTokens: 8,
	}.Apply(
		context.Background(),
		agentproxy.TransformInput{Text: strings.Repeat("important output ", 100)},
	)
	if err != nil {
		t.Fatalf("apply token budget: %v", err)
	}

	if output.Record.Decision != "truncate" ||
		output.Record.OutputTokens > 32 ||
		!strings.Contains(output.Text, "token_budget: status=truncated") {
		t.Fatalf("small-budget output = %#v text=%q", output.Record, output.Text)
	}
}

func TestToolOutputTokenBudgetBoundsLongEvidencePath(t *testing.T) {
	t.Parallel()

	tokenizer := runeTokenizer{}
	output, err := agentproxy.ToolOutputTokenBudgetTransform{
		MaxTokens:  32,
		HeadTokens: 8,
		TailTokens: 8,
	}.Apply(
		context.Background(),
		agentproxy.TransformInput{
			Text: strings.Repeat("important output ", 100),
			Metadata: map[string]string{
				"coding_ethos.full_output_path": strings.Repeat("long-path/", 20),
			},
			Tokenizer: tokenizer,
		},
	)
	if err != nil {
		t.Fatalf("apply token budget: %v", err)
	}

	if output.Record.Decision != "truncate" ||
		output.Record.OutputTokens > 32 ||
		tokenizer.Count(output.Text) > 32 {
		t.Fatalf("long-evidence token-budget output = %#v text=%q",
			output.Record,
			output.Text,
		)
	}
}

func repeatedLines(line string, count int) []string {
	lines := make([]string, count)
	for index := range lines {
		lines[index] = line
	}

	return lines
}

func assertEvidenceFileContains(t *testing.T, path, expected string) {
	t.Helper()

	if path == "" {
		t.Fatal("missing evidence path")
	}

	if filepath.Dir(path) != os.TempDir() ||
		!strings.HasPrefix(filepath.Base(path), "coding-ethos-tool-output-") {
		t.Fatalf("evidence path = %q", path)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read evidence path %s: %v", path, err)
	}
	t.Cleanup(func() {
		_ = os.Remove(path)
	})

	if string(content) != expected {
		t.Fatalf("evidence content mismatch\nwant:\n%s\ngot:\n%s", expected, content)
	}
}
