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
