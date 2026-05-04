// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	tree_sitter_yaml "github.com/tree-sitter-grammars/tree-sitter-yaml/bindings/go"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_bash "github.com/tree-sitter/tree-sitter-bash/bindings/go"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
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
	language, parserLanguage, ok := languageForPath(path)
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
	parser := tree_sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(parserLanguage)); err != nil {
		return fmt.Errorf("set tree-sitter language for %q: %w", path, err)
	}
	tree := parser.Parse(contents, nil)
	if tree == nil {
		return fmt.Errorf("parse %q with tree-sitter returned nil tree", path)
	}
	defer tree.Close()

	chunks := collectCodeChunks(relativePath, language, contents, tree.RootNode())
	file := CodeFile{
		Path:         relativePath,
		Language:     language,
		ContentHash:  contentHash(contents),
		SizeBytes:    len(contents),
		LineCount:    lineCount(contents),
		IndexedAtUTC: time.Now().UTC().Format(time.RFC3339),
	}
	if err := indexer.store.ReplaceCodeFileChunks(ctx, file, chunks); err != nil {
		return err
	}
	summary.FilesIndexed++
	summary.ChunksIndexed += len(chunks)

	return nil
}

func collectCodeChunks(
	path string,
	language string,
	contents []byte,
	root *tree_sitter.Node,
) []CodeChunk {
	chunks := []CodeChunk{}
	var visit func(node *tree_sitter.Node, parents []string)
	visit = func(node *tree_sitter.Node, parents []string) {
		if node == nil {
			return
		}
		if symbolKind, ok := symbolKindForNode(language, node.Kind()); ok {
			name := symbolName(node, contents)
			symbolPath := strings.Join(append(append([]string{}, parents...), name), ".")
			chunk := codeChunkFromNode(path, language, contents, node, symbolKind, name, symbolPath)
			chunks = append(chunks, chunk)
			if name != "" {
				parents = append(parents, name)
			}
		}
		for index := uint(0); index < node.NamedChildCount(); index++ {
			visit(node.NamedChild(index), parents)
		}
	}
	visit(root, nil)

	return chunks
}

func codeChunkFromNode(
	path string,
	language string,
	contents []byte,
	node *tree_sitter.Node,
	symbolKind string,
	name string,
	symbolPath string,
) CodeChunk {
	startByte := boundedUintToInt(node.StartByte(), len(contents))
	endByte := boundedUintToInt(node.EndByte(), len(contents))
	if endByte < startByte {
		endByte = startByte
	}
	start := node.StartPosition()
	end := node.EndPosition()
	maxRow := max(lineCount(contents)-1, 0)
	raw := string(contents[startByte:endByte])
	search := strings.Join(compactStrings([]string{
		path,
		language,
		node.Kind(),
		symbolKind,
		name,
		symbolPath,
		raw,
	}), "\n")

	return CodeChunk{
		ID:          stableID("code-chunk", path, language, node.Kind(), symbolPath, contentHash([]byte(raw))),
		Path:        path,
		Language:    language,
		NodeKind:    node.Kind(),
		SymbolKind:  symbolKind,
		SymbolName:  name,
		SymbolPath:  symbolPath,
		StartByte:   startByte,
		EndByte:     endByte,
		StartLine:   boundedUintToInt(start.Row, maxRow) + 1,
		EndLine:     boundedUintToInt(end.Row, maxRow) + 1,
		ContentHash: contentHash([]byte(raw)),
		SearchText:  search,
		RawText:     raw,
	}
}

func boundedUintToInt(value uint, maxValue int) int {
	if value > uint(maxValue) {
		return maxValue
	}

	return int(value)
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

func symbolKindForNode(language string, nodeKind string) (string, bool) {
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

func symbolName(node *tree_sitter.Node, contents []byte) string {
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

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", ".hg", ".svn", ".tox", ".venv", "node_modules", "build", "dist", ".cache":
		return true
	default:
		return false
	}
}

func contentHash(contents []byte) string {
	hash := sha256.Sum256(contents)

	return hex.EncodeToString(hash[:])
}

func lineCount(contents []byte) int {
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
