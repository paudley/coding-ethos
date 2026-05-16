// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package agentproxy_test

import (
	"context"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
)

func TestFileReadPaginationTransformPaginatesWithEvidence(t *testing.T) {
	t.Parallel()

	output, err := agentproxy.NewPipeline(
		nil,
		agentproxy.FileReadPaginationTransform{
			Path:    "pkg/app.py",
			PageEnd: 3,
		},
	).Apply(
		context.Background(),
		agentproxy.TransformInput{Text: "one\ntwo\nthree\nfour\nfive\n"},
	)
	if err != nil {
		t.Fatalf("apply transform: %v", err)
	}

	for _, expected := range []string{
		"paginated file read",
		"showing lines 1-3 of 5",
		"1 | one",
		"3 | three",
		"next page: sed -n '4,5p' pkg/app.py",
	} {
		if !strings.Contains(output.Text, expected) {
			t.Fatalf("paginated output missing %q: %s", expected, output.Text)
		}
	}

	if len(output.Records) != 1 {
		t.Fatalf("records = %#v", output.Records)
	}

	record := output.Records[0]
	if record.Name != agentproxy.FileReadPaginationTransformName ||
		record.PolicyID != agentproxy.FileReadPaginationPolicyID ||
		record.Decision != "truncate" ||
		record.EvidencePath == "" {
		t.Fatalf("record = %#v", record)
	}
}

func TestFileReadPaginationTransformQuotesContinuationPath(t *testing.T) {
	t.Parallel()

	output, err := agentproxy.NewPipeline(
		nil,
		agentproxy.FileReadPaginationTransform{
			Path:    "docs/my file's $(draft).md",
			PageEnd: 1,
		},
	).Apply(
		context.Background(),
		agentproxy.TransformInput{Text: "one\ntwo\n"},
	)
	if err != nil {
		t.Fatalf("apply transform: %v", err)
	}

	expected := "next page: sed -n '2,2p' 'docs/my file'\\''s $(draft).md'"
	if !strings.Contains(output.Text, expected) {
		t.Fatalf("paginated output missing quoted path %q: %s", expected, output.Text)
	}
}

func TestFileReadPaginationTransformAllowsSmallOutput(t *testing.T) {
	t.Parallel()

	input := "one\ntwo\n"
	output, err := agentproxy.NewPipeline(
		nil,
		agentproxy.FileReadPaginationTransform{
			Path:    "pkg/app.py",
			PageEnd: 3,
		},
	).Apply(context.Background(), agentproxy.TransformInput{Text: input})
	if err != nil {
		t.Fatalf("apply transform: %v", err)
	}

	if output.Text != input {
		t.Fatalf("text changed: got %q, want %q", output.Text, input)
	}

	if len(output.Records) != 1 ||
		output.Records[0].Decision != "allow" ||
		output.Records[0].PolicyID != agentproxy.FileReadPaginationPolicyID {
		t.Fatalf("records = %#v", output.Records)
	}
}

func TestOutputLineCountMatchesProxyTransforms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want int
	}{
		{name: "empty", text: "", want: 0},
		{name: "no trailing newline", text: "one\ntwo", want: 2},
		{name: "trailing newline", text: "one\ntwo\n", want: 2},
		{name: "crlf", text: "one\r\ntwo\r\n", want: 2},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := agentproxy.OutputLineCount(test.text); got != test.want {
				t.Fatalf("line count = %d, want %d", got, test.want)
			}
		})
	}
}
