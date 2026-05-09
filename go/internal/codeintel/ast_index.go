// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/internal/astfacts"
)

type ASTIndexer struct {
	store *Store
}

func NewASTIndexer(store *Store) ASTIndexer {
	return ASTIndexer{store: store}
}

func (indexer ASTIndexer) IndexPaths(
	ctx context.Context,
	root string,
	paths []string,
) (CodeIndexSummary, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = "."
	}

	if len(paths) == 0 {
		paths = []string{"."}
	}

	summary := CodeIndexSummary{}

	for _, inputPath := range paths {
		path := inputPath
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}

		info, err := os.Stat(path)
		if err != nil {
			return CodeIndexSummary{}, fmt.Errorf(
				"stat index path %q: %w",
				inputPath,
				err,
			)
		}

		if info.IsDir() {
			err := indexer.indexDir(ctx, root, path, &summary)
			if err != nil {
				return CodeIndexSummary{}, err
			}

			continue
		}

		inlineErr0 := indexer.indexFile(ctx, root, path, &summary)
		if inlineErr0 != nil {
			return CodeIndexSummary{}, inlineErr0
		}
	}

	return summary, nil
}

func (indexer ASTIndexer) indexDir(
	ctx context.Context,
	root string,
	dir string,
	summary *CodeIndexSummary,
) error {
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walk code-intel AST source path %s: %w", path, err)
		}

		if entry.IsDir() {
			if shouldSkipDir(entry.Name()) {
				return filepath.SkipDir
			}

			return nil
		}

		return indexer.indexFile(ctx, root, path, summary)
	})
	if err != nil {
		return fmt.Errorf("walk code-intel AST directory %s: %w", dir, err)
	}

	return nil
}

func (indexer ASTIndexer) indexFile(
	ctx context.Context,
	root string,
	path string,
	summary *CodeIndexSummary,
) error {
	language, ok := astfacts.LanguageForPath(path)
	if !ok {
		return nil
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read indexed file %q: %w", path, err)
	}

	relativePath, err := filepath.Rel(root, path)
	if err != nil {
		return fmt.Errorf("relativize indexed file %q: %w", path, err)
	}

	relativePath = filepath.ToSlash(relativePath)
	hash := astfacts.ContentHash(contents)

	existing, found, err := indexer.store.GetCodeFile(ctx, relativePath)
	if err != nil {
		return fmt.Errorf("lookup existing code file %q: %w", relativePath, err)
	}

	parserName, parserVersion := astfacts.ParserMetadataForLanguage(language)

	if found &&
		existing.ContentHash == hash &&
		existing.ParserName == parserName &&
		existing.ParserVersion == parserVersion &&
		existing.StaleReason == "" {
		summary.Skipped = append(summary.Skipped, relativePath)

		return nil
	}

	parsed, _, err := astfacts.Analyze(relativePath, contents)
	if err != nil {
		return fmt.Errorf("analyze AST facts for %s: %w", relativePath, err)
	}

	chunks := codeChunksFromSymbols(parsed.Symbols)
	chunks = attachParentChunks(chunks)
	edges := codeEdgesFromParsedFile(relativePath, parsed, chunks)

	file := CodeFile{
		Path:          relativePath,
		Language:      language,
		ContentHash:   parsed.ContentHash,
		ParserName:    parserName,
		ParserVersion: parserVersion,
		SizeBytes:     len(contents),
		LineCount:     parsed.LineCount,
		IndexedAtUTC:  time.Now().UTC().Format(time.RFC3339),
	}

	inlineErr1 := indexer.store.ReplaceCodeFileIndex(ctx, file, chunks, edges)
	if inlineErr1 != nil {
		return inlineErr1
	}

	summary.FilesIndexed++
	summary.ChunksIndexed += len(chunks)

	return nil
}

func codeChunksFromSymbols(symbols []astfacts.Symbol) []CodeChunk {
	chunks := make([]CodeChunk, 0, len(symbols))
	for _, symbol := range symbols {
		chunks = append(chunks, codeChunkFromSymbol(symbol))
	}

	return chunks
}

func codeChunkFromSymbol(symbol astfacts.Symbol) CodeChunk {
	search := strings.Join(compactStrings([]string{
		symbol.Path,
		symbol.Language,
		symbol.NodeKind,
		symbol.SymbolKind,
		symbol.SymbolName,
		symbol.SymbolPath,
		symbol.RawText,
	}), "\n")

	parentSymbolPath := parentSymbolPath(symbol.SymbolPath)

	return CodeChunk{
		ID: stableID(
			"code-chunk",
			symbol.Path,
			symbol.Language,
			symbol.NodeKind,
			symbol.SymbolPath,
			symbol.ContentHash,
		),
		Path:             symbol.Path,
		Language:         symbol.Language,
		NodeKind:         symbol.NodeKind,
		SymbolKind:       symbol.SymbolKind,
		SymbolName:       symbol.SymbolName,
		SymbolPath:       symbol.SymbolPath,
		ParentSymbolPath: parentSymbolPath,
		StartByte:        symbol.StartByte,
		EndByte:          symbol.EndByte,
		StartLine:        symbol.StartLine,
		EndLine:          symbol.EndLine,
		ContentHash:      symbol.ContentHash,
		SearchText:       search,
		RawText:          symbol.RawText,
	}
}

func attachParentChunks(chunks []CodeChunk) []CodeChunk {
	bySymbolPath := map[string]CodeChunk{}

	for _, chunk := range chunks {
		if chunk.SymbolPath != "" {
			bySymbolPath[chunk.SymbolPath] = chunk
		}
	}

	for index := range chunks {
		if parent, ok := bySymbolPath[chunks[index].ParentSymbolPath]; ok {
			chunks[index].ParentChunkID = parent.ID
		}
	}

	return chunks
}

func parentSymbolPath(symbolPath string) string {
	parts := strings.Split(symbolPath, ".")
	if len(parts) <= 1 {
		return ""
	}

	return strings.Join(parts[:len(parts)-1], ".")
}

func codeEdgesFromParsedFile(
	path string,
	parsed astfacts.File,
	chunks []CodeChunk,
) []CodeEdge {
	edges := []CodeEdge{}

	for _, chunk := range chunks {
		if chunk.ParentChunkID != "" {
			edges = append(edges, CodeEdge{
				ID: stableID(
					"code-edge",
					"contains",
					path,
					chunk.ParentChunkID,
					chunk.ID,
				),
				Kind:             "contains",
				Path:             path,
				SourceChunkID:    chunk.ParentChunkID,
				TargetPath:       path,
				TargetChunkID:    chunk.ID,
				TargetSymbolPath: chunk.SymbolPath,
				TargetName:       chunk.SymbolName,
			})
		}
	}

	edges = append(edges, importEdges(path, parsed.Imports)...)
	edges = append(edges, referenceEdges(path, parsed.Symbols, chunks)...)

	return dedupeCodeEdges(edges)
}

func importEdges(path string, imports []astfacts.Import) []CodeEdge {
	edges := make([]CodeEdge, 0, len(imports))
	for _, imported := range imports {
		if imported.Target == "" {
			continue
		}

		edges = append(edges, CodeEdge{
			ID: stableID(
				"code-edge",
				"imports",
				path,
				imported.Target,
				imported.RawText,
			),
			Kind:       "imports",
			Path:       path,
			TargetPath: imported.Target,
			TargetName: imported.Target,
			RawText:    imported.RawText,
		})
	}

	return edges
}

func referenceEdges(
	path string,
	symbols []astfacts.Symbol,
	chunks []CodeChunk,
) []CodeEdge {
	chunksBySymbolPath := map[string]CodeChunk{}
	targetsByName := map[string][]CodeChunk{}

	for _, chunk := range chunks {
		chunksBySymbolPath[chunk.SymbolPath] = chunk
		if chunk.SymbolName != "" {
			targetsByName[chunk.SymbolName] = append(
				targetsByName[chunk.SymbolName],
				chunk,
			)
		}
	}

	edges := []CodeEdge{}

	for _, symbol := range symbols {
		source, ok := chunksBySymbolPath[symbol.SymbolPath]
		if !ok {
			continue
		}

		for _, name := range symbol.ReferencedNames {
			for _, target := range targetsByName[name] {
				if source.ID == target.ID {
					continue
				}

				edges = append(edges, CodeEdge{
					ID: stableID(
						"code-edge",
						"references",
						path,
						source.ID,
						target.ID,
					),
					Kind:             "references",
					Path:             path,
					SourceChunkID:    source.ID,
					TargetPath:       path,
					TargetChunkID:    target.ID,
					TargetSymbolPath: target.SymbolPath,
					TargetName:       target.SymbolName,
				})
			}
		}
	}

	return edges
}

func dedupeCodeEdges(edges []CodeEdge) []CodeEdge {
	seen := map[string]bool{}
	deduped := []CodeEdge{}

	for _, edge := range edges {
		if edge.ID == "" || seen[edge.ID] {
			continue
		}

		seen[edge.ID] = true
		deduped = append(deduped, edge)
	}

	slices.SortFunc(deduped, func(left, right CodeEdge) int {
		return strings.Compare(left.ID, right.ID)
	})

	return deduped
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git",
		".hg",
		".svn",
		".tox",
		".venv",
		"node_modules",
		"build",
		"dist",
		".cache":
		return true
	default:
		return false
	}
}
