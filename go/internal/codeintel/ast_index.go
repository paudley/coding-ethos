// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
			return CodeIndexSummary{}, fmt.Errorf("stat index path %q: %w", inputPath, err)
		}
		if info.IsDir() {
			if err := indexer.indexDir(ctx, root, path, &summary); err != nil {
				return CodeIndexSummary{}, err
			}
			continue
		}
		if err := indexer.indexFile(ctx, root, path, &summary); err != nil {
			return CodeIndexSummary{}, err
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
	return filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if shouldSkipDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}

		return indexer.indexFile(ctx, root, path, summary)
	})
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
	parsed, _, err := astfacts.Analyze(relativePath, contents)
	if err != nil {
		return err
	}

	chunks := codeChunksFromSymbols(parsed.Symbols)
	file := CodeFile{
		Path:         relativePath,
		Language:     language,
		ContentHash:  parsed.ContentHash,
		SizeBytes:    len(contents),
		LineCount:    parsed.LineCount,
		IndexedAtUTC: time.Now().UTC().Format(time.RFC3339),
	}
	if err := indexer.store.ReplaceCodeFileChunks(ctx, file, chunks); err != nil {
		return err
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

	return CodeChunk{
		ID:          stableID("code-chunk", symbol.Path, symbol.Language, symbol.NodeKind, symbol.SymbolPath, symbol.ContentHash),
		Path:        symbol.Path,
		Language:    symbol.Language,
		NodeKind:    symbol.NodeKind,
		SymbolKind:  symbol.SymbolKind,
		SymbolName:  symbol.SymbolName,
		SymbolPath:  symbol.SymbolPath,
		StartByte:   symbol.StartByte,
		EndByte:     symbol.EndByte,
		StartLine:   symbol.StartLine,
		EndLine:     symbol.EndLine,
		ContentHash: symbol.ContentHash,
		SearchText:  search,
		RawText:     symbol.RawText,
	}
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", ".hg", ".svn", ".tox", ".venv", "node_modules", "build", "dist", ".cache":
		return true
	default:
		return false
	}
}
