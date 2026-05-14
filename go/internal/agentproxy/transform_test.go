// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package agentproxy_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/apperror"
)

type trimTransform struct{}

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
		output.Metadata["coding_ethos.compressed"] != "true" ||
		output.Metadata["coding_ethos.compressed_lines_omitted"] != "5" ||
		output.Record.Name != "tool-output-compression" ||
		output.Record.BytesRemoved <= 0 {
		t.Fatalf("compression record = %#v metadata = %#v output = %q",
			output.Record,
			output.Metadata,
			output.Text,
		)
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
}
