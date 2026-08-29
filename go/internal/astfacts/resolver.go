// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package astfacts

import (
	"fmt"
	"sync"
	"unsafe"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"

	"blackcat.ca/coding-ethos/go/internal/apperror"
)

type Context struct {
	Language   string
	NodeKind   string
	SymbolKind string
	SymbolName string
	SymbolPath string
	StartLine  int
	EndLine    int
}

type Resolver struct {
	parsers map[string]*parserEntry
	mu      sync.Mutex
}

type parserEntry struct {
	parser *tree_sitter.Parser
	mu     sync.Mutex
}

func NewResolver() *Resolver {
	return &Resolver{parsers: map[string]*parserEntry{}}
}

func Analyze(path string, contents []byte) (File, bool, error) {
	return NewResolver().Analyze(path, contents)
}

func ContextForLine(path string, contents []byte, line int) (Context, bool, error) {
	return NewResolver().ContextForLine(path, contents, line)
}

func Parse(path string, contents []byte) (*tree_sitter.Tree, bool, error) {
	return NewResolver().Parse(path, contents)
}

func (resolver *Resolver) Analyze(path string, contents []byte) (File, bool, error) {
	language, parserKey, parserLanguage, ok := languageForPath(path)
	if !ok {
		return File{}, false, nil
	}

	if language == LanguageMarkdown {
		return resolver.analyzeMarkdown(path, contents), true, nil
	}

	tree, err := resolver.parse(path, parserKey, parserLanguage, contents)
	if err != nil {
		return File{}, false, err
	}
	defer tree.Close()

	lineCount := LineCount(contents)
	root := tree.RootNode()

	return File{
		Symbols:       CollectSymbols(path, language, contents, root, lineCount),
		Imports:       CollectImports(language, contents, root),
		ContentHash:   ContentHash(contents),
		Language:      language,
		LineCount:     lineCount,
		HasParseError: root.HasError(),
	}, true, nil
}

func (resolver *Resolver) ContextForLine(
	path string,
	contents []byte,
	line int,
) (Context, bool, error) {
	if line <= 0 {
		return Context{}, false, nil
	}

	parsed, ok, err := resolver.Analyze(path, contents)
	if err != nil || !ok {
		return Context{}, ok, err
	}

	symbol, found := nearestSymbolForLine(parsed.Symbols, line)
	if !found {
		return Context{}, false, nil
	}

	return Context{
		Language:   symbol.Language,
		NodeKind:   symbol.NodeKind,
		SymbolKind: symbol.SymbolKind,
		SymbolName: symbol.SymbolName,
		SymbolPath: symbol.SymbolPath,
		StartLine:  symbol.StartLine,
		EndLine:    symbol.EndLine,
	}, true, nil
}

func (resolver *Resolver) Parse(
	path string,
	contents []byte,
) (*tree_sitter.Tree, bool, error) {
	language, parserKey, parserLanguage, ok := languageForPath(path)
	if !ok || language == LanguageMarkdown {
		return nil, false, nil
	}

	tree, err := resolver.parse(path, parserKey, parserLanguage, contents)
	if err != nil {
		return nil, true, err
	}

	return tree, true, nil
}

func (resolver *Resolver) analyzeMarkdown(path string, contents []byte) File {
	return AnalyzeMarkdown(path, contents)
}

func (resolver *Resolver) parse(
	path string,
	parserKey string,
	parserLanguage unsafe.Pointer,
	contents []byte,
) (*tree_sitter.Tree, error) {
	entry, err := resolver.parserEntry(path, parserKey, parserLanguage)
	if err != nil {
		return nil, err
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()

	tree := entry.parser.Parse(contents, nil)
	if tree == nil {
		return nil, apperror.Wrapf(
			apperror.StaticError("parse %q with tree-sitter returned nil tree"),
			"parse %q with tree-sitter returned nil tree",
			path,
		)
	}

	return tree, nil
}

func (resolver *Resolver) parserEntry(
	path string,
	parserKey string,
	parserLanguage unsafe.Pointer,
) (*parserEntry, error) {
	resolver.mu.Lock()
	defer resolver.mu.Unlock()

	entry := resolver.parsers[parserKey]
	if entry == nil {
		parser := tree_sitter.NewParser()

		err := parser.SetLanguage(tree_sitter.NewLanguage(parserLanguage))
		if err != nil {
			parser.Close()

			return nil, fmt.Errorf("set tree-sitter language for %q: %w", path, err)
		}

		entry = &parserEntry{parser: parser}
		resolver.parsers[parserKey] = entry
	}

	return entry, nil
}
