// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agentproxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"strings"
	"unicode/utf8"

	"blackcat.ca/coding-ethos/go/internal/apperror"
)

const (
	errNilContentTransform      = apperror.StaticError("nil content transform")
	approximateTokenRuneDivisor = 4
)

type Tokenizer interface {
	Count(text string) int
}

type WhitespaceTokenizer struct{}

func (WhitespaceTokenizer) Count(text string) int {
	return len(strings.Fields(text))
}

type ApproximateTokenizer struct{}

func (ApproximateTokenizer) Count(text string) int {
	runes := utf8.RuneCountInString(text)
	if runes == 0 {
		return 0
	}

	return (runes + approximateTokenRuneDivisor - 1) / approximateTokenRuneDivisor
}

type TransformInput struct {
	Tokenizer Tokenizer
	Metadata  map[string]string
	Text      string
}

type TransformOutput struct {
	Text     string
	Records  []TransformRecord
	Metadata map[string]string
	Record   TransformRecord
}

type ContentTransform interface {
	Name() string
	Apply(ctx context.Context, input TransformInput) (TransformOutput, error)
}

type Pipeline struct {
	tokenizer  Tokenizer
	transforms []ContentTransform
}

func NewPipeline(tokenizer Tokenizer, transforms ...ContentTransform) Pipeline {
	if tokenizer == nil {
		tokenizer = ApproximateTokenizer{}
	}

	return Pipeline{tokenizer: tokenizer, transforms: transforms}
}

func (pipeline Pipeline) Apply(
	ctx context.Context,
	input TransformInput,
) (TransformOutput, error) {
	output := TransformOutput{
		Text:     input.Text,
		Metadata: cloneMetadata(input.Metadata),
	}

	for _, transform := range pipeline.transforms {
		if transform == nil {
			return TransformOutput{}, errNilContentTransform
		}

		next, err := transform.Apply(ctx, TransformInput{
			Text:      output.Text,
			Metadata:  output.Metadata,
			Tokenizer: pipeline.tokenizer,
		})
		if err != nil {
			return TransformOutput{}, fmt.Errorf("apply %s transform: %w", transform.Name(), err)
		}

		record := next.Record

		record.Name = transform.Name()
		if record.InputHash == "" {
			record.InputHash = HashText(output.Text)
		}

		if record.OutputHash == "" {
			record.OutputHash = HashText(next.Text)
		}

		if record.InputTokens == 0 {
			record.InputTokens = pipeline.tokenizer.Count(output.Text)
		}

		if record.OutputTokens == 0 {
			record.OutputTokens = pipeline.tokenizer.Count(next.Text)
		}

		if record.BytesRemoved == 0 && len(output.Text) > len(next.Text) {
			record.BytesRemoved = len(output.Text) - len(next.Text)
		}

		output.Text = next.Text
		output.Record = record
		output.Records = append(output.Records, record)
		output.Metadata = cloneMetadata(next.Metadata)
	}

	return output, nil
}

func HashText(text string) string {
	digest := sha256.Sum256([]byte(text))

	return hex.EncodeToString(digest[:])
}

func cloneMetadata(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}

	output := make(map[string]string, len(input))
	maps.Copy(output, input)

	return output
}
