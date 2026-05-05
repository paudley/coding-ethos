// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package astfacts

import (
	"testing"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func TestAnalyzeIndexesJSONAndTOMLConfigEntries(t *testing.T) {
	t.Parallel()

	jsonFile, ok, err := Analyze("config/settings.json", []byte(`{
  "tools": {
    "ruff": {"enabled": true}
  }
}`))
	if err != nil {
		t.Fatalf("analyze json: %v", err)
	}
	if !ok || jsonFile.Language != "json" {
		t.Fatalf("json language = %q, ok=%v", jsonFile.Language, ok)
	}
	if !hasSymbol(jsonFile.Symbols, "tools.ruff", "config_entry") {
		t.Fatalf("json symbols missing tools.ruff: %#v", jsonFile.Symbols)
	}

	tomlFile, ok, err := Analyze("pyproject.toml", []byte(`[tool.ruff]
line-length = 100

[tool.pyright]
typeCheckingMode = "strict"
`))
	if err != nil {
		t.Fatalf("analyze toml: %v", err)
	}
	if !ok || tomlFile.Language != "toml" {
		t.Fatalf("toml language = %q, ok=%v", tomlFile.Language, ok)
	}
	if !hasSymbol(tomlFile.Symbols, "tool.ruff.line-length", "config_entry") {
		t.Fatalf("toml symbols missing tool.ruff.line-length: %#v", tomlFile.Symbols)
	}
	if !hasSymbol(tomlFile.Symbols, "tool.pyright", "config_section") {
		t.Fatalf("toml symbols missing tool.pyright section: %#v", tomlFile.Symbols)
	}
}

func TestContextForLineReturnsNearestSymbol(t *testing.T) {
	t.Parallel()

	context, ok, err := ContextForLine("pkg/app.py", []byte(`class Worker:
    def run(self):
        return "ok"
`), 3)
	if err != nil {
		t.Fatalf("context: %v", err)
	}
	if !ok {
		t.Fatalf("context not found")
	}
	if context.SymbolPath != "Worker.run" || context.SymbolKind != "function" {
		t.Fatalf("context = %#v", context)
	}
}

func TestAnalyzeIndexesCodeSymbolsImportsAndReferences(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path       string
		source     string
		language   string
		symbolPath string
		symbolKind string
		importName string
		reference  string
	}{
		{
			path:       "pkg/app.go",
			source:     "package pkg\n\nimport \"fmt\"\n\nfunc Run() { fmt.Println(\"ok\") }\n",
			language:   "go",
			symbolPath: "Run",
			symbolKind: "function",
			importName: "fmt",
			reference:  "fmt",
		},
		{
			path:       "pkg/app.py",
			source:     "import pathlib\n\nclass Worker:\n    def run(self):\n        return pathlib.Path('.')\n",
			language:   "python",
			symbolPath: "Worker.run",
			symbolKind: "function",
			importName: "pathlib",
			reference:  "pathlib",
		},
		{
			path:       "web/app.js",
			source:     "import tool from 'pkg';\nclass Worker { run() { return tool(); } }\n",
			language:   "javascript",
			symbolPath: "Worker.run",
			symbolKind: "function",
			importName: "pkg",
			reference:  "tool",
		},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			t.Parallel()

			file, ok, err := Analyze(test.path, []byte(test.source))
			if err != nil {
				t.Fatalf("analyze: %v", err)
			}
			if !ok || file.Language != test.language {
				t.Fatalf("language = %q ok=%v", file.Language, ok)
			}
			if !hasSymbol(file.Symbols, test.symbolPath, test.symbolKind) {
				t.Fatalf("missing symbol %s/%s: %#v", test.symbolPath, test.symbolKind, file.Symbols)
			}
			if !hasImport(file.Imports, test.importName) {
				t.Fatalf("missing import %q: %#v", test.importName, file.Imports)
			}
			if !symbolReferences(file.Symbols, test.symbolPath, test.reference) {
				t.Fatalf("symbol %q missing reference %q: %#v", test.symbolPath, test.reference, file.Symbols)
			}
		})
	}
}

func TestAnalyzeHandlesShellAndYAMLFacts(t *testing.T) {
	t.Parallel()

	shellFile, ok, err := Analyze("scripts/build.sh", []byte("build() {\n  echo ok\n}\n"))
	if err != nil {
		t.Fatalf("analyze shell: %v", err)
	}
	if !ok || !hasSymbol(shellFile.Symbols, "build", "function") {
		t.Fatalf("shell symbols = %#v ok=%v", shellFile.Symbols, ok)
	}

	yamlFile, ok, err := Analyze("config.yaml", []byte("tooling:\n  enabled: true\n"))
	if err != nil {
		t.Fatalf("analyze yaml: %v", err)
	}
	if !ok || yamlFile.Language != "yaml" || len(yamlFile.Symbols) == 0 {
		t.Fatalf("yaml facts = %#v ok=%v", yamlFile, ok)
	}
}

func TestUnsupportedPathsAndInvalidLinesReturnNoContext(t *testing.T) {
	t.Parallel()

	if language, ok := LanguageForPath("README.md"); ok || language != "" {
		t.Fatalf("markdown language = %q ok=%v", language, ok)
	}
	if _, ok, err := Analyze("README.md", []byte("# docs\n")); err != nil || ok {
		t.Fatalf("unsupported analyze ok=%v err=%v", ok, err)
	}
	if _, ok, err := ContextForLine("pkg/app.py", []byte("def run():\n    pass\n"), 0); err != nil || ok {
		t.Fatalf("invalid line context ok=%v err=%v", ok, err)
	}
}

func TestParseAndWalkExposeTreeTraversalHelpers(t *testing.T) {
	t.Parallel()

	tree, ok, err := Parse("pkg/app.py", []byte("def run():\n    return 1\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !ok {
		t.Fatal("parse should support Python")
	}
	defer tree.Close()

	visited := 0
	Walk(tree.RootNode(), func(_ *tree_sitter.Node) {
		visited++
	})
	if visited == 0 {
		t.Fatal("walk should visit nodes")
	}
	deepest := 0
	WalkWithDepth(tree.RootNode(), func(_ *tree_sitter.Node, depth int) {
		if depth > deepest {
			deepest = depth
		}
	})
	if deepest == 0 {
		t.Fatal("walk with depth should report nested nodes")
	}
	if !NodeContainsLine(tree.RootNode(), 1) {
		t.Fatal("root should contain first line")
	}
}

func hasSymbol(symbols []Symbol, path string, kind string) bool {
	for _, symbol := range symbols {
		if symbol.SymbolPath == path && symbol.SymbolKind == kind {
			return true
		}
	}

	return false
}

func hasImport(imports []Import, target string) bool {
	for _, item := range imports {
		if item.Target == target {
			return true
		}
	}

	return false
}

func symbolReferences(symbols []Symbol, path string, reference string) bool {
	for _, symbol := range symbols {
		if symbol.SymbolPath != path {
			continue
		}
		for _, name := range symbol.ReferencedNames {
			if name == reference {
				return true
			}
		}
	}

	return false
}
