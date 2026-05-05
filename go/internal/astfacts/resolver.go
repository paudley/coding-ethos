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
	parsers map[string]*tree_sitter.Parser
}

var defaultResolver = NewResolver()

func NewResolver() *Resolver {
	return &Resolver{parsers: map[string]*tree_sitter.Parser{}}
}

func Analyze(path string, contents []byte) (File, bool, error) {
	return defaultResolver.Analyze(path, contents)
}

func ContextForLine(path string, contents []byte, line int) (Context, bool, error) {
	return defaultResolver.ContextForLine(path, contents, line)
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

func (resolver *Resolver) parse(
	path string,
	language string,
	parserLanguage unsafe.Pointer,
	contents []byte,
) (*tree_sitter.Tree, error) {
	resolver.mu.Lock()
	defer resolver.mu.Unlock()

	parser := resolver.parsers[language]
	if parser == nil {
		parser = tree_sitter.NewParser()
		if err := parser.SetLanguage(tree_sitter.NewLanguage(parserLanguage)); err != nil {
			parser.Close()
			return nil, fmt.Errorf("set tree-sitter language for %q: %w", path, err)
		}
		resolver.parsers[language] = parser
	}
	tree := parser.Parse(contents, nil)
	if tree == nil {
		return nil, fmt.Errorf("parse %q with tree-sitter returned nil tree", path)
	}

	return tree, nil
}
