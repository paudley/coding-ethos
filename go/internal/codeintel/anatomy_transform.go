// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel

import (
	"context"
	"fmt"
	"maps"
	"strconv"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
)

const (
	// DirectoryAnatomyTransformName is the stable transform identifier recorded
	// when proxy listing output receives a compact anatomy block.
	DirectoryAnatomyTransformName = "directory-anatomy-map"

	directoryAnatomyMetadataKey = "coding_ethos.directory_anatomy"
)

// DirectoryAnatomyTransform appends compact, AST-derived file anatomy to an
// existing directory listing while preserving the original listing text.
type DirectoryAnatomyTransform struct {
	Anatomy DirectoryAnatomy
}

// EnrichDirectoryListing appends AST-backed anatomy to raw directory listing
// output through the shared proxy transform pipeline.
func (store *Store) EnrichDirectoryListing(
	ctx context.Context,
	query DirectoryAnatomyQuery,
	listing string,
) (agentproxy.TransformOutput, error) {
	anatomy, err := store.DirectoryAnatomy(ctx, query)
	if err != nil {
		return agentproxy.TransformOutput{}, err
	}

	output, err := agentproxy.NewPipeline(
		nil,
		DirectoryAnatomyTransform{Anatomy: anatomy},
	).Apply(ctx, agentproxy.TransformInput{Text: listing})
	if err != nil {
		return agentproxy.TransformOutput{}, fmt.Errorf(
			"apply directory anatomy transform: %w",
			err,
		)
	}

	return output, nil
}

func (transform DirectoryAnatomyTransform) Name() string {
	return DirectoryAnatomyTransformName
}

func (transform DirectoryAnatomyTransform) Apply(
	_ context.Context,
	input agentproxy.TransformInput,
) (agentproxy.TransformOutput, error) {
	block := RenderDirectoryAnatomyTOON(transform.Anatomy)
	if block == "" {
		return agentproxy.TransformOutput{
			Text:     input.Text,
			Metadata: cloneTransformMetadata(input.Metadata),
			Record: agentproxy.TransformRecord{
				Reason:   "directory anatomy is empty",
				Decision: "skip",
			},
		}, nil
	}

	metadata := cloneTransformMetadata(input.Metadata)
	metadata[directoryAnatomyMetadataKey] = "true"

	return agentproxy.TransformOutput{
		Text:     appendDirectoryAnatomyBlock(input.Text, block),
		Metadata: metadata,
		Record: agentproxy.TransformRecord{
			Reason:        "append compact directory anatomy",
			Decision:      "inject",
			FindingsCount: len(transform.Anatomy.Files),
		},
	}, nil
}

// RenderDirectoryAnatomyTOON renders a compact directory-local anatomy map for
// agent context. The format is intentionally small and inspired by Aider's
// repomap while relying on coding-ethos' local AST index as the source of truth.
func RenderDirectoryAnatomyTOON(anatomy DirectoryAnatomy) string {
	if len(anatomy.Files) == 0 {
		return ""
	}

	lines := []string{
		"coding_ethos_anatomy:",
		"path: " + quoteAnatomyValue(anatomy.Path),
		"files[" + strconv.Itoa(len(anatomy.Files)) +
			"]{path,language,lines,tokens,symbols}:",
	}

	for _, file := range anatomy.Files {
		lines = append(lines, strings.Join([]string{
			"  " + quoteAnatomyValue(file.Path),
			quoteAnatomyValue(file.Language),
			strconv.Itoa(file.LineCount),
			strconv.Itoa(file.EstimatedTokens),
			quoteAnatomyValue(renderAnatomySymbols(file.Symbols)),
		}, ","))
	}

	return strings.Join(lines, "\n")
}

func appendDirectoryAnatomyBlock(text, block string) string {
	switch {
	case text == "":
		return block + "\n"
	case strings.HasSuffix(text, "\n"):
		return text + "\n" + block + "\n"
	default:
		return text + "\n\n" + block + "\n"
	}
}

func renderAnatomySymbols(symbols []DirectoryAnatomySymbol) string {
	if len(symbols) == 0 {
		return ""
	}

	parts := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		name := symbol.SymbolPath
		if name == "" {
			name = symbol.Name
		}

		if name == "" {
			continue
		}

		if symbol.StartLine > 0 {
			name += "@" + strconv.Itoa(symbol.StartLine)
		}

		parts = append(parts, name)
	}

	return strings.Join(parts, ";")
}

func quoteAnatomyValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return `""`
	}

	if strings.ContainsAny(value, ",;:\n\r\t ") {
		return strconv.Quote(value)
	}

	return value
}

func cloneTransformMetadata(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}

	output := make(map[string]string, len(input))
	maps.Copy(output, input)

	return output
}
