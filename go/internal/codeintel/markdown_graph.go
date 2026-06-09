// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const (
	codeEdgeKindDocuments    = "documents"
	codeEdgeKindMentions     = "mentions"
	codeEdgeKindRationaleFor = "rationale_for"
)

// markdownPathReferencePattern matches Markdown file references with an
// optional ./ or ../ prefix, slash-separated path segments, a supported source
// or documentation extension, and an optional #fragment for symbol mentions.
const markdownPathReferencePattern = `(?:\.{1,2}/)?[A-Za-z0-9_.-]+` +
	`(?:/[A-Za-z0-9_.-]+)*\.` +
	`(?:go|py|md|yaml|yml|toml|json|jsonc|sh|bash|zsh|ts|tsx|js|jsx|mjs|cjs)` +
	`(?:#[A-Za-z_][A-Za-z0-9_.-]*)?`

var markdownPathReferenceRE = regexp.MustCompile(markdownPathReferencePattern)

func markdownDocumentEdges(
	path string,
	contents []byte,
	chunks []CodeChunk,
) []CodeEdge {
	if !strings.HasSuffix(strings.ToLower(path), ".md") {
		return nil
	}

	headings := markdownHeadingChunks(chunks)
	if len(headings) == 0 {
		return nil
	}

	edges := []CodeEdge{}
	lineNumber := 0
	headingIndex := 0

	var source CodeChunk

	for line := range strings.SplitSeq(string(contents), "\n") {
		lineNumber++
		for headingIndex < len(headings) &&
			headings[headingIndex].StartLine <= lineNumber {
			source = headings[headingIndex]
			headingIndex++
		}

		if source.ID == "" {
			continue
		}

		for _, match := range markdownPathReferenceRE.FindAllString(line, -1) {
			targetPath, targetSymbol := markdownTargetReference(path, match)
			if targetPath == "" || targetPath == path {
				continue
			}

			edgeKind := markdownReferenceEdgeKind(source.SymbolName, targetSymbol)
			edges = append(edges, CodeEdge{
				ID: stableID(
					"code-edge",
					edgeKind,
					path,
					source.ID,
					targetPath,
					targetSymbol,
				),
				Kind:             edgeKind,
				Path:             path,
				SourceChunkID:    source.ID,
				TargetPath:       targetPath,
				TargetSymbolPath: targetSymbol,
				TargetName:       markdownTargetName(targetPath, targetSymbol),
				ProvenanceClass:  markdownReferenceProvenance(edgeKind),
				RawText:          strings.TrimSpace(line),
			})
		}
	}

	return dedupeCodeEdges(edges)
}

func markdownHeadingChunks(chunks []CodeChunk) []CodeChunk {
	headings := []CodeChunk{}

	for _, chunk := range chunks {
		if chunk.SymbolKind == "heading" {
			headings = append(headings, chunk)
		}
	}

	slices.SortFunc(headings, func(left, right CodeChunk) int {
		return left.StartLine - right.StartLine
	})

	return headings
}

func markdownTargetReference(sourcePath, raw string) (string, string) {
	targetPath, targetSymbol, _ := strings.Cut(raw, "#")
	targetPath = strings.Trim(targetPath, "`'\"()[]{}<>,;:")

	targetPath = strings.TrimRight(targetPath, ".")
	if strings.HasPrefix(targetPath, "./") || strings.HasPrefix(targetPath, "../") {
		targetPath = filepath.Join(filepath.Dir(sourcePath), targetPath)
	}

	targetPath = filepath.ToSlash(filepath.Clean(targetPath))
	if targetPath == "." {
		return "", ""
	}

	targetSymbol = strings.Trim(strings.TrimSpace(targetSymbol), "`'\"()[]{}<>,.;:")

	return targetPath, targetSymbol
}

func markdownReferenceEdgeKind(heading, targetSymbol string) string {
	if markdownHeadingIsRationale(heading) {
		return codeEdgeKindRationaleFor
	}

	if targetSymbol != "" {
		return codeEdgeKindMentions
	}

	return codeEdgeKindDocuments
}

func markdownHeadingIsRationale(heading string) bool {
	normalized := strings.ToLower(strings.TrimSpace(heading))

	return strings.Contains(normalized, "rationale") ||
		strings.Contains(normalized, "reasoning") ||
		strings.Contains(normalized, "decision") ||
		strings.HasPrefix(normalized, "why ") ||
		normalized == "why" ||
		normalized == "reason" ||
		strings.HasPrefix(normalized, "reason ") ||
		strings.Contains(normalized, " reason")
}

func markdownReferenceProvenance(edgeKind string) string {
	if edgeKind == codeEdgeKindRationaleFor {
		return ProvenanceInferred
	}

	return ProvenanceDocDerived
}

func markdownTargetName(targetPath, targetSymbol string) string {
	if targetSymbol != "" {
		return targetSymbol
	}

	return filepath.Base(targetPath)
}
