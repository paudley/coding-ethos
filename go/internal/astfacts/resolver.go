// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package astfacts

import (
	"fmt"
	"sync"
	"unsafe"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
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
	mu      sync.Mutex
	parsers map[string]*parserEntry
}

type parserEntry struct {
	mu     sync.Mutex
	parser *tree_sitter.Parser
}

var defaultResolver = NewResolver()

func NewResolver() *Resolver {
	return &Resolver{parsers: map[string]*parserEntry{}}
}

func Analyze(path string, contents []byte) (File, bool, error) {
	return defaultResolver.Analyze(path, contents)
}

func ContextForLine(path string, contents []byte, line int) (Context, bool, error) {
	return defaultResolver.ContextForLine(path, contents, line)
}

func Parse(path string, contents []byte) (*tree_sitter.Tree, bool, error) {
	return defaultResolver.Parse(path, contents)
}

func (resolver *Resolver) Analyze(path string, contents []byte) (File, bool, error) {
	language, parserLanguage, ok := languageForPath(path)
	if !ok {
		return File{}, false, nil
	}
	tree, err := resolver.parse(path, language, parserLanguage, contents)
	if err != nil {
		return File{}, false, err
	}
	defer tree.Close()

	lineCount := LineCount(contents)
	root := tree.RootNode()

	return File{
		Symbols:     CollectSymbols(path, language, contents, root, lineCount),
		Imports:     CollectImports(language, contents, root),
		ContentHash: ContentHash(contents),
		Language:    language,
		LineCount:   lineCount,
	}, true, nil
}

func (resolver *Resolver) ContextForLine(path string, contents []byte, line int) (Context, bool, error) {
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

func (resolver *Resolver) Parse(path string, contents []byte) (*tree_sitter.Tree, bool, error) {
	language, parserLanguage, ok := languageForPath(path)
	if !ok {
		return nil, false, nil
	}
	tree, err := resolver.parse(path, language, parserLanguage, contents)
	if err != nil {
		return nil, true, err
	}

	return tree, true, nil
}

func (resolver *Resolver) parse(
	path string,
	language string,
	parserLanguage unsafe.Pointer,
	contents []byte,
) (*tree_sitter.Tree, error) {
	entry, err := resolver.parserEntry(path, language, parserLanguage)
	if err != nil {
		return nil, err
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()

	tree := entry.parser.Parse(contents, nil)
	if tree == nil {
		return nil, fmt.Errorf("parse %q with tree-sitter returned nil tree", path)
	}

	return tree, nil
}

func (resolver *Resolver) parserEntry(
	path string,
	language string,
	parserLanguage unsafe.Pointer,
) (*parserEntry, error) {
	resolver.mu.Lock()
	defer resolver.mu.Unlock()

	entry := resolver.parsers[language]
	if entry == nil {
		parser := tree_sitter.NewParser()
		if err := parser.SetLanguage(tree_sitter.NewLanguage(parserLanguage)); err != nil {
			parser.Close()
			return nil, fmt.Errorf("set tree-sitter language for %q: %w", path, err)
		}
		entry = &parserEntry{parser: parser}
		resolver.parsers[language] = entry
	}

	return entry, nil
}
