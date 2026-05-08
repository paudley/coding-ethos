// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package astfacts

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

const (
	symbolKindConfigEntry = "config_entry"
	symbolKindFunction    = "function"
)

type File struct {
	ContentHash string
	Language    string
	Symbols     []Symbol
	Imports     []Import
	LineCount   int
}

type Import struct {
	Target  string
	RawText string
}

type Symbol struct {
	SymbolKind      string
	SymbolName      string
	Language        string
	NodeKind        string
	Path            string
	SymbolPath      string
	RawText         string
	ContentHash     string
	ReferencedNames []string
	StartByte       int
	EndLine         int
	LineCount       int
	EndByte         int
	StartLine       int
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
			symbolPath := strings.Join(
				append(append([]string{}, parents...), name),
				".",
			)

			symbols = append(
				symbols,
				SymbolFromNode(
					path,
					language,
					contents,
					node,
					symbolKind,
					name,
					symbolPath,
					lineCount,
				),
			)

			if name != "" {
				parents = append(parents, name)
			}
		}

		childCount := node.NamedChildCount()
		for index := range childCount {
			visit(node.NamedChild(index), parents)
		}
	}
	visit(root, nil)

	return symbols
}

func nearestSymbolForLine(symbols []Symbol, line int) (Symbol, bool) {
	var best Symbol

	found := false

	for _, symbol := range symbols {
		if symbol.StartLine > line || symbol.EndLine < line {
			continue
		}

		if !found ||
			symbol.StartLine > best.StartLine ||
			(symbol.StartLine == best.StartLine && symbol.LineCount < best.LineCount) {
			best = symbol
			found = true
		}
	}

	return best, found
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

	endByte := max(boundedUintToInt(node.EndByte(), len(contents)), startByte)

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

		childCount := node.NamedChildCount()
		for index := range childCount {
			visit(node.NamedChild(index))
		}
	}
	visit(root)

	return imports
}

func importNodeKind(language, nodeKind string) bool {
	switch language {
	case LanguageGo:
		return nodeKind == "import_spec"
	case LanguagePython:
		return nodeKind == "import_statement" || nodeKind == "import_from_statement"
	case LanguageJavaScript:
		return nodeKind == "import_statement"
	default:
		return false
	}
}

func ImportTarget(language string, contents []byte, node *tree_sitter.Node) string {
	switch language {
	case LanguageGo, LanguageJavaScript:
		return cleanImportTarget(
			firstDescendantText(contents, node, stringLikeNodeKind),
		)
	case LanguagePython:
		if module := node.ChildByFieldName("module_name"); module != nil {
			return cleanImportTarget(module.Utf8Text(contents))
		}

		if name := firstDescendantText(contents, node, pythonImportNameNodeKind); name != "" {
			return cleanImportTarget(name)
		}
	}

	return ""
}

func ReferencedNames(
	language string,
	contents []byte,
	node *tree_sitter.Node,
) []string {
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

		childCount := candidate.NamedChildCount()
		for index := range childCount {
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

	childCount := node.NamedChildCount()
	for index := range childCount {
		if value := firstDescendantText(
			contents,
			node.NamedChild(index),
			matches,
		); value != "" {
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

func referenceIdentifierKind(language, nodeKind string) bool {
	switch language {
	case LanguageGo, LanguagePython, LanguageJavaScript:
		return nodeKind == "identifier"
	case LanguageShell:
		return nodeKind == "word" || nodeKind == "command_name"
	default:
		return false
	}
}

func SymbolKindForNode(language, nodeKind string) (string, bool) {
	for _, entry := range nodeSymbolKindEntries() {
		if entry.Language == language && entry.NodeKind == nodeKind {
			return entry.SymbolKind, true
		}
	}

	return "", false
}

type nodeSymbolKindEntry struct {
	Language   string
	NodeKind   string
	SymbolKind string
}

func nodeSymbolKindEntries() []nodeSymbolKindEntry {
	return []nodeSymbolKindEntry{
		{
			Language:   LanguageGo,
			NodeKind:   "function_declaration",
			SymbolKind: symbolKindFunction,
		},
		{
			Language:   LanguageGo,
			NodeKind:   "method_declaration",
			SymbolKind: symbolKindFunction,
		},
		{Language: LanguageGo, NodeKind: "type_declaration", SymbolKind: "type"},
		{
			Language:   LanguageJavaScript,
			NodeKind:   "class_declaration",
			SymbolKind: "class",
		},
		{
			Language:   LanguageJavaScript,
			NodeKind:   "function_declaration",
			SymbolKind: symbolKindFunction,
		},
		{
			Language:   LanguageJavaScript,
			NodeKind:   "generator_function_declaration",
			SymbolKind: symbolKindFunction,
		},
		{
			Language:   LanguageJavaScript,
			NodeKind:   "method_definition",
			SymbolKind: symbolKindFunction,
		},
		{Language: LanguageJSON, NodeKind: "pair", SymbolKind: symbolKindConfigEntry},
		{Language: LanguagePython, NodeKind: "class_definition", SymbolKind: "class"},
		{
			Language:   LanguagePython,
			NodeKind:   "function_definition",
			SymbolKind: symbolKindFunction,
		},
		{
			Language:   LanguageShell,
			NodeKind:   "function_definition",
			SymbolKind: symbolKindFunction,
		},
		{Language: LanguageTOML, NodeKind: "pair", SymbolKind: symbolKindConfigEntry},
		{Language: LanguageTOML, NodeKind: "table", SymbolKind: "config_section"},
		{
			Language:   LanguageTOML,
			NodeKind:   "table_array_element",
			SymbolKind: "config_section",
		},
		{
			Language:   "yaml",
			NodeKind:   "block_mapping_pair",
			SymbolKind: symbolKindConfigEntry,
		},
	}
}

func SymbolName(node *tree_sitter.Node, contents []byte) string {
	name := node.ChildByFieldName("name")
	if name == nil {
		name = node.ChildByFieldName("key")
	}

	if name == nil {
		nameText := firstDescendantText(contents, node, keyNodeKind)
		if nameText == "" {
			return ""
		}

		return cleanSymbolName(nameText)
	}

	return cleanSymbolName(name.Utf8Text(contents))
}

func cleanMarkdownHeading(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimLeft(text, "#")
	text = strings.TrimSpace(text)

	if lines := strings.Split(text, "\n"); len(lines) > 0 {
		text = lines[0]
	}

	return cleanSymbolName(text)
}

func AnalyzeMarkdown(contents []byte) File {
	md := goldmark.New()
	reader := text.NewReader(contents)
	doc := md.Parser().Parse(reader)

	lineCount := LineCount(contents)
	symbols := []Symbol{}
	lineIndexer := newLineMap(contents)

	err := ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		symbol, analyzedOK := symbolFromMarkdownNode(node, contents, lineIndexer)
		if analyzedOK {
			symbols = append(symbols, symbol)
		}

		return ast.WalkContinue, nil
	})

	if err != nil {
		return File{
			ContentHash: ContentHash(contents),
			Language:    LanguageMarkdown,
			LineCount:   lineCount,
		}
	}

	return File{
		Symbols:     symbols,
		ContentHash: ContentHash(contents),
		Language:    LanguageMarkdown,
		LineCount:   lineCount,
	}
}

func symbolFromMarkdownNode(
	node ast.Node,
	contents []byte,
	lineIndexer lineMap,
) (Symbol, bool) {
	kind := node.Kind()

	var symbolKind string

	var name string

	headingLevel := 0

	switch kind {
	case ast.KindHeading:
		symbolKind = "heading"

		heading, analyzedOK := node.(*ast.Heading)
		if !analyzedOK {
			return Symbol{}, false
		}

		headingLevel = heading.Level

		var builder strings.Builder

		for i := range heading.Lines().Len() {
			line := heading.Lines().At(i)
			_, _ = builder.Write(line.Value(contents))
		}

		name = cleanMarkdownHeading(builder.String())
	case ast.KindCodeBlock, ast.KindFencedCodeBlock:
		symbolKind = "code_block"
		// Generate a block name based on the line number
		lines := node.Lines()
		if lines.Len() > 0 {
			startByte := lines.At(0).Start
			startLine := lineIndexer.lineForByte(startByte)
			name = fmt.Sprintf("block_%d", startLine)
		} else {
			name = "block_unknown"
		}
	default:
		return Symbol{}, false
	}

	lines := node.Lines()
	if lines.Len() == 0 {
		return Symbol{}, false
	}

	startByte := lines.At(0).Start
	endByte := lines.At(lines.Len() - 1).Stop
	startLine := lineIndexer.lineForByte(startByte)

	// Build a unique SymbolPath for headings by incorporating heading level and
	// start line. Duplicate headings (valid Markdown) share the same name but
	// differ in level or position, so a plain name would collide in stableID.
	symbolPath := name
	if headingLevel > 0 {
		symbolPath = fmt.Sprintf("h%d:%d:%s", headingLevel, startLine, name)
	}

	return Symbol{
		Language:   LanguageMarkdown,
		NodeKind:   kind.String(),
		SymbolKind: symbolKind,
		SymbolName: name,
		SymbolPath: symbolPath,
		StartByte:  startByte,
		EndByte:    endByte,
		StartLine:  startLine,
		EndLine:    lineIndexer.lineForByte(endByte),
	}, true
}

type lineMap struct {
	offsets []int
}

func newLineMap(contents []byte) lineMap {
	offsets := []int{0}

	for i, b := range contents {
		if b == '\n' {
			offsets = append(offsets, i+1)
		}
	}

	return lineMap{offsets: offsets}
}

func (lm lineMap) lineForByte(offset int) int {
	index, found := slices.BinarySearch(lm.offsets, offset)

	if found {
		return index + 1
	}

	return index
}

func keyNodeKind(nodeKind string) bool {
	switch nodeKind {
	case "bare_key", "dotted_key", "quoted_key", "string":
		return true
	default:
		return false
	}
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
	const maxIntValue = int(^uint(0) >> 1)

	if value > uint(maxIntValue) {
		return maxValue
	}

	converted := int(value)
	if converted > maxValue {
		return maxValue
	}

	return converted
}
