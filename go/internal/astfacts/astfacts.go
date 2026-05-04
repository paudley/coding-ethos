// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package astfacts

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"unsafe"

	tree_sitter_yaml "github.com/tree-sitter-grammars/tree-sitter-yaml/bindings/go"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_bash "github.com/tree-sitter/tree-sitter-bash/bindings/go"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
)

type File struct {
	Symbols     []Symbol
	Imports     []Import
	ContentHash string
	Language    string
	LineCount   int
}

type Import struct {
	Target  string
	RawText string
}

type Symbol struct {
	RawText         string
	ContentHash     string
	Language        string
	NodeKind        string
	Path            string
	ReferencedNames []string
	SymbolKind      string
	SymbolName      string
	SymbolPath      string
	EndByte         int
	EndLine         int
	LineCount       int
	StartByte       int
	StartLine       int
}

func Analyze(path string, contents []byte) (File, bool, error) {
	language, parserLanguage, ok := languageForPath(path)
	if !ok {
		return File{}, false, nil
	}
	parser := tree_sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(parserLanguage)); err != nil {
		return File{}, false, fmt.Errorf("set tree-sitter language for %q: %w", path, err)
	}
	tree := parser.Parse(contents, nil)
	if tree == nil {
		return File{}, false, fmt.Errorf("parse %q with tree-sitter returned nil tree", path)
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

func CollectSymbols(
	path string,
	language string,
	contents []byte,
	root *tree_sitter.Node,
	lineCount int,
) []Symbol {
	symbols := []Symbol{}
	var visit func(node *tree_sitter.Node, parents []string)
	visit = func(node *tree_sitter.Node, parents []string) {
		if node == nil {
			return
		}
		if symbolKind, ok := SymbolKindForNode(language, node.Kind()); ok {
			name := SymbolName(node, contents)
			symbolPath := strings.Join(append(append([]string{}, parents...), name), ".")
			symbols = append(symbols, SymbolFromNode(path, language, contents, node, symbolKind, name, symbolPath, lineCount))
			if name != "" {
				parents = append(parents, name)
			}
		}
		for index := uint(0); index < node.NamedChildCount(); index++ {
			visit(node.NamedChild(index), parents)
		}
	}
	visit(root, nil)

	return symbols
}

func SymbolFromNode(
	path string,
	language string,
	contents []byte,
	node *tree_sitter.Node,
	symbolKind string,
	name string,
	symbolPath string,
	lineCount int,
) Symbol {
	startByte := boundedUintToInt(node.StartByte(), len(contents))
	endByte := boundedUintToInt(node.EndByte(), len(contents))
	if endByte < startByte {
		endByte = startByte
	}
	start := node.StartPosition()
	end := node.EndPosition()
	maxRow := max(lineCount-1, 0)
	raw := string(contents[startByte:endByte])
	startLine := boundedUintToInt(start.Row, maxRow) + 1
	endLine := boundedUintToInt(end.Row, maxRow) + 1

	return Symbol{
		RawText:         raw,
		ContentHash:     ContentHash([]byte(raw)),
		Language:        language,
		NodeKind:        node.Kind(),
		Path:            path,
		ReferencedNames: ReferencedNames(language, contents, node),
		SymbolKind:      symbolKind,
		SymbolName:      name,
		SymbolPath:      symbolPath,
		StartByte:       startByte,
		EndByte:         endByte,
		StartLine:       startLine,
		EndLine:         endLine,
		LineCount:       max(endLine-startLine+1, 0),
	}
}

func CollectImports(language string, contents []byte, root *tree_sitter.Node) []Import {
	imports := []Import{}
	var visit func(node *tree_sitter.Node)
	visit = func(node *tree_sitter.Node) {
		if node == nil {
			return
		}
		if importNodeKind(language, node.Kind()) {
			if target := ImportTarget(language, contents, node); target != "" {
				imports = append(imports, Import{
					Target:  target,
					RawText: strings.TrimSpace(node.Utf8Text(contents)),
				})
			}
		}
		for index := uint(0); index < node.NamedChildCount(); index++ {
			visit(node.NamedChild(index))
		}
	}
	visit(root)

	return imports
}

func importNodeKind(language string, nodeKind string) bool {
	switch language {
	case "go":
		return nodeKind == "import_spec"
	case "python":
		return nodeKind == "import_statement" || nodeKind == "import_from_statement"
	case "javascript":
		return nodeKind == "import_statement"
	default:
		return false
	}
}

func ImportTarget(language string, contents []byte, node *tree_sitter.Node) string {
	switch language {
	case "go", "javascript":
		return cleanImportTarget(firstDescendantText(contents, node, stringLikeNodeKind))
	case "python":
		if module := node.ChildByFieldName("module_name"); module != nil {
			return cleanImportTarget(module.Utf8Text(contents))
		}
		if name := firstDescendantText(contents, node, pythonImportNameNodeKind); name != "" {
			return cleanImportTarget(name)
		}
	}

	return ""
}

func ReferencedNames(language string, contents []byte, node *tree_sitter.Node) []string {
	names := map[string]bool{}
	var visit func(candidate *tree_sitter.Node)
	visit = func(candidate *tree_sitter.Node) {
		if candidate == nil {
			return
		}
		if referenceIdentifierKind(language, candidate.Kind()) {
			if name := cleanSymbolName(candidate.Utf8Text(contents)); name != "" {
				names[name] = true
			}
		}
		for index := uint(0); index < candidate.NamedChildCount(); index++ {
			visit(candidate.NamedChild(index))
		}
	}
	visit(node)

	return sortedMapKeys(names)
}

func firstDescendantText(
	contents []byte,
	node *tree_sitter.Node,
	matches func(string) bool,
) string {
	if node == nil {
		return ""
	}
	if matches(node.Kind()) {
		return node.Utf8Text(contents)
	}
	for index := uint(0); index < node.NamedChildCount(); index++ {
		if value := firstDescendantText(contents, node.NamedChild(index), matches); value != "" {
			return value
		}
	}

	return ""
}

func stringLikeNodeKind(nodeKind string) bool {
	return nodeKind == "interpreted_string_literal" ||
		nodeKind == "raw_string_literal" ||
		nodeKind == "string" ||
		nodeKind == "string_fragment"
}

func pythonImportNameNodeKind(nodeKind string) bool {
	return nodeKind == "dotted_name" || nodeKind == "identifier"
}

func referenceIdentifierKind(language string, nodeKind string) bool {
	switch language {
	case "go", "python", "javascript":
		return nodeKind == "identifier"
	case "shell":
		return nodeKind == "word" || nodeKind == "command_name"
	default:
		return false
	}
}

func LanguageForPath(path string) (string, bool) {
	language, _, ok := languageForPath(path)

	return language, ok
}

func languageForPath(path string) (string, unsafe.Pointer, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go", tree_sitter_go.Language(), true
	case ".py":
		return "python", tree_sitter_python.Language(), true
	case ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx":
		return "javascript", tree_sitter_javascript.Language(), true
	case ".sh", ".bash", ".zsh":
		return "shell", tree_sitter_bash.Language(), true
	case ".yaml", ".yml":
		return "yaml", tree_sitter_yaml.Language(), true
	default:
		return "", nil, false
	}
}

func SymbolKindForNode(language string, nodeKind string) (string, bool) {
	switch language {
	case "go":
		switch nodeKind {
		case "function_declaration", "method_declaration":
			return "function", true
		case "type_declaration":
			return "type", true
		}
	case "python":
		switch nodeKind {
		case "function_definition":
			return "function", true
		case "class_definition":
			return "class", true
		}
	case "javascript":
		switch nodeKind {
		case "function_declaration", "method_definition", "generator_function_declaration":
			return "function", true
		case "class_declaration":
			return "class", true
		}
	case "shell":
		if nodeKind == "function_definition" {
			return "function", true
		}
	case "yaml":
		if nodeKind == "block_mapping_pair" {
			return "config_entry", true
		}
	}

	return "", false
}

func SymbolName(node *tree_sitter.Node, contents []byte) string {
	name := node.ChildByFieldName("name")
	if name == nil {
		name = node.ChildByFieldName("key")
	}
	if name == nil {
		return ""
	}

	return cleanSymbolName(name.Utf8Text(contents))
}

func cleanSymbolName(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, `"'`)
	value = strings.TrimSuffix(value, ":")

	return strings.TrimSpace(value)
}

func cleanImportTarget(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, `"'`)
	value = strings.Trim(value, "`")
	value = strings.TrimSpace(value)

	return strings.Trim(value, ".")
}

func sortedMapKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for value := range values {
		keys = append(keys, value)
	}
	slices.Sort(keys)

	return keys
}

func ContentHash(contents []byte) string {
	hash := sha256.Sum256(contents)

	return hex.EncodeToString(hash[:])
}

func LineCount(contents []byte) int {
	if len(contents) == 0 {
		return 0
	}
	count := 1
	for _, value := range contents {
		if value == '\n' {
			count++
		}
	}

	return count
}

func boundedUintToInt(value uint, maxValue int) int {
	if value > uint(maxValue) {
		return maxValue
	}

	return int(value)
}
