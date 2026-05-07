// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package agentproxy_test

import (
	"context"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
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
