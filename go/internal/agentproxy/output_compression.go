// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package agentproxy

import (
	"context"
	"strconv"
	"strings"
)

const (
	defaultToolOutputMaxLines = 80
	defaultToolOutputHead     = 32
	defaultToolOutputTail     = 32
	minPreservedLineBudget    = 2
	minToolOutputMaxLines     = 3
)

// ToolOutputCompressionTransform caps verbose tool output while preserving the
// beginning and ending context where command identity and terminal failures
// usually appear.
type ToolOutputCompressionTransform struct {
	MaxLines int
	Head     int
	Tail     int
}

func (transform ToolOutputCompressionTransform) Name() string {
	return "tool-output-compression"
}

func (transform ToolOutputCompressionTransform) Apply(
	_ context.Context,
	input TransformInput,
) (TransformOutput, error) {
	limits := transform.normalizedLimits()
	lines := splitOutputLines(input.Text)

	if len(lines) <= limits.MaxLines {
		return TransformOutput{
			Text:     input.Text,
			Metadata: cloneMetadata(input.Metadata),
			Record: TransformRecord{
				Reason: "tool output within line budget",
			},
		}, nil
	}

	omitted := len(lines) - limits.Head - limits.Tail
	compressed := make([]string, 0, limits.Head+limits.Tail+1)
	compressed = append(compressed, lines[:limits.Head]...)
	compressed = append(
		compressed,
		strings.Join([]string{
			"[coding-ethos: compressed tool output; ",
			strconv.Itoa(omitted),
			" of ",
			strconv.Itoa(len(lines)),
			" lines omitted to save tokens]",
		}, ""),
	)
	compressed = append(compressed, lines[len(lines)-limits.Tail:]...)

	metadata := cloneMetadata(input.Metadata)
	metadata["coding_ethos.compressed"] = "true"
	metadata["coding_ethos.compressed_lines_omitted"] = strconv.Itoa(omitted)

	return TransformOutput{
		Text:     joinOutputLines(compressed, strings.HasSuffix(input.Text, "\n")),
		Metadata: metadata,
		Record: TransformRecord{
			Reason: "tool output exceeded line budget",
		},
	}, nil
}

type toolOutputCompressionLimits struct {
	MaxLines int
	Head     int
	Tail     int
}

func (
	transform ToolOutputCompressionTransform,
) normalizedLimits() toolOutputCompressionLimits {
	limits := toolOutputCompressionLimits(transform)

	if limits.MaxLines <= 0 {
		limits.MaxLines = defaultToolOutputMaxLines
	}

	if limits.Head <= 0 {
		limits.Head = defaultToolOutputHead
	}

	if limits.Tail <= 0 {
		limits.Tail = defaultToolOutputTail
	}

	if limits.MaxLines < minToolOutputMaxLines {
		limits.MaxLines = minToolOutputMaxLines
	}

	if limits.Head+limits.Tail >= limits.MaxLines {
		preservedLineBudget := max(limits.MaxLines-1, minPreservedLineBudget)
		head := preservedLineBudget / minPreservedLineBudget
		tail := preservedLineBudget - head

		if limits.Head < head {
			head = limits.Head
			tail = preservedLineBudget - head
		}

		if limits.Tail < tail {
			tail = limits.Tail
			head = preservedLineBudget - tail
		}

		limits.Head = head
		limits.Tail = tail
	}

	return limits
}

func splitOutputLines(text string) []string {
	trimmed := strings.TrimSuffix(text, "\n")
	if trimmed == "" {
		return nil
	}

	return strings.Split(trimmed, "\n")
}

func joinOutputLines(lines []string, trailingNewline bool) string {
	if len(lines) == 0 {
		return ""
	}

	output := strings.Join(lines, "\n")
	if trailingNewline {
		output += "\n"
	}

	return output
}
