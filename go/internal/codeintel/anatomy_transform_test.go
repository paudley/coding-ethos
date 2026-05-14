// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel_test

import (
	"context"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	. "blackcat.ca/coding-ethos/go/internal/codeintel"
)

func TestDirectoryAnatomyTransformAppendsCompactMap(t *testing.T) {
	t.Parallel()

	input := "app.go\nworker.py\n"
	anatomy := DirectoryAnatomy{
		Path: "pkg",
		Files: []DirectoryAnatomyFile{{
			Path:            "pkg/app.go",
			Language:        "go",
			LineCount:       8,
			EstimatedTokens: 24,
			Symbols: []DirectoryAnatomySymbol{{
				SymbolPath: "BuildMessage",
				StartLine:  3,
			}},
		}},
	}

	output, err := agentproxy.NewPipeline(
		agentproxy.WhitespaceTokenizer{},
		DirectoryAnatomyTransform{Anatomy: anatomy},
	).Apply(
		context.Background(),
		agentproxy.TransformInput{
			Metadata: map[string]string{"provider": "codex"},
			Text:     input,
		},
	)
	if err != nil {
		t.Fatalf("apply anatomy transform: %v", err)
	}

	if !strings.HasPrefix(output.Text, input) ||
		!strings.Contains(output.Text, "coding_ethos_anatomy:\npath: pkg") ||
		!strings.Contains(output.Text, "files[1]{path,language,lines,tokens,symbols}:") ||
		!strings.Contains(output.Text, "  pkg/app.go,go,8,24,BuildMessage@3") {
		t.Fatalf("output text = %q", output.Text)
	}

	if output.Metadata["provider"] != "codex" ||
		output.Metadata["coding_ethos.directory_anatomy"] != "true" {
		t.Fatalf("metadata = %#v", output.Metadata)
	}

	if output.Record.Name != DirectoryAnatomyTransformName ||
		output.Record.Decision != "inject" ||
		output.Record.FindingsCount != 1 ||
		output.Record.InputHash == "" ||
		output.Record.OutputHash == "" ||
		output.Record.InputHash == output.Record.OutputHash ||
		output.Record.OutputTokens <= output.Record.InputTokens {
		t.Fatalf("record = %#v", output.Record)
	}
}

func TestDirectoryAnatomyTransformSkipsEmptyMap(t *testing.T) {
	t.Parallel()

	output, err := agentproxy.NewPipeline(
		nil,
		DirectoryAnatomyTransform{Anatomy: DirectoryAnatomy{Path: "pkg"}},
	).Apply(
		context.Background(),
		agentproxy.TransformInput{Text: "app.go\n"},
	)
	if err != nil {
		t.Fatalf("apply anatomy transform: %v", err)
	}

	if output.Text != "app.go\n" ||
		output.Record.Name != DirectoryAnatomyTransformName ||
		output.Record.Decision != "skip" {
		t.Fatalf("output = %#v", output)
	}
}
