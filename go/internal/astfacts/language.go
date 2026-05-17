// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package astfacts

import (
	"path/filepath"
	"strings"
	"unsafe"

	tree_sitter_toml "github.com/tree-sitter-grammars/tree-sitter-toml/bindings/go"
	tree_sitter_yaml "github.com/tree-sitter-grammars/tree-sitter-yaml/bindings/go"
	tree_sitter_bash "github.com/tree-sitter/tree-sitter-bash/bindings/go"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tree_sitter_json "github.com/tree-sitter/tree-sitter-json/bindings/go"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

const (
	LanguageGo         = "go"
	LanguagePython     = "python"
	LanguageJavaScript = "javascript"
	LanguageJSON       = "json"
	LanguageMarkdown   = "markdown"
	LanguageShell      = "shell"
	LanguageTOML       = "toml"
	LanguageTypeScript = "typescript"
	LanguageYAML       = "yaml"
)

func LanguageForPath(path string) (string, bool) {
	language, _, ok := languageForPath(path)

	return language, ok
}

func languageForPath(path string) (string, unsafe.Pointer, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return LanguageGo, tree_sitter_go.Language(), true
	case ".py":
		return LanguagePython, tree_sitter_python.Language(), true
	case ".js", ".jsx", ".mjs", ".cjs":
		return LanguageJavaScript, tree_sitter_javascript.Language(), true
	case ".ts", ".mts", ".cts":
		return LanguageTypeScript, tree_sitter_typescript.LanguageTypescript(), true
	case ".tsx":
		return LanguageTypeScript, tree_sitter_typescript.LanguageTSX(), true
	case ".json", ".jsonc":
		return LanguageJSON, tree_sitter_json.Language(), true
	case ".md":
		return LanguageMarkdown, nil, true
	case ".sh", ".bash", ".zsh":
		return LanguageShell, tree_sitter_bash.Language(), true
	case ".toml":
		return LanguageTOML, tree_sitter_toml.Language(), true
	case ".yaml", ".yml":
		return LanguageYAML, tree_sitter_yaml.Language(), true
	default:
		return "", nil, false
	}
}
