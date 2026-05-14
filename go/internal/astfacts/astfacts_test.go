// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package astfacts_test

import (
	"slices"
	"testing"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"

	. "blackcat.ca/coding-ethos/go/internal/astfacts"
)

func TestAnalyzeIndexesJSONAndTOMLConfigEntries(t *testing.T) {
	t.Parallel()

	jsonFile, found, err := Analyze("config/settings.json", []byte(`{
  "tools": {
    "ruff": {"enabled": true}
  }
}`))
	if err != nil {
		t.Fatalf("analyze json: %v", err)
	}

	if !found || jsonFile.Language != "json" {
		t.Fatalf("json language = %q, ok=%v", jsonFile.Language, found)
	}

	if !hasSymbol(jsonFile.Symbols, "tools.ruff", "config_entry") {
		t.Fatalf("json symbols missing tools.ruff: %#v", jsonFile.Symbols)
	}

	tomlFile, found, err := Analyze("pyproject.toml", []byte(`[tool.ruff]
line-length = 100

[tool.pyright]
typeCheckingMode = "strict"
`))
	if err != nil {
		t.Fatalf("analyze toml: %v", err)
	}

	if !found || tomlFile.Language != "toml" {
		t.Fatalf("toml language = %q, ok=%v", tomlFile.Language, found)
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

	context, found, err := ContextForLine("pkg/app.py", []byte(`class Worker:
    def run(self):
        return "ok"
`), 3)
	if err != nil {
		t.Fatalf("context: %v", err)
	}

	if !found {
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
			path: "pkg/app.py",
			source: "import pathlib\n\nclass Worker:\n" +
				"    def run(self):\n        return pathlib.Path('.')\n",
			language:   "python",
			symbolPath: "Worker.run",
			symbolKind: "function",
			importName: "pathlib",
			reference:  "pathlib",
		},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			t.Parallel()

			file, found, err := Analyze(test.path, []byte(test.source))
			if err != nil {
				t.Fatalf("analyze: %v", err)
			}

			if !found || file.Language != test.language {
				t.Fatalf("language = %q ok=%v", file.Language, found)
			}

			if !hasSymbol(file.Symbols, test.symbolPath, test.symbolKind) {
				t.Fatalf(
					"missing symbol %s/%s: %#v",
					test.symbolPath,
					test.symbolKind,
					file.Symbols,
				)
			}

			if !hasImport(file.Imports, test.importName) {
				t.Fatalf("missing import %q: %#v", test.importName, file.Imports)
			}

			if !symbolReferences(file.Symbols, test.symbolPath, test.reference) {
				t.Fatalf(
					"symbol %q missing reference %q: %#v",
					test.symbolPath,
					test.reference,
					file.Symbols,
				)
			}
		})
	}
}

func TestAnalyzeIndexesJavaScriptAndTypeScriptFacts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path     string
		source   string
		language string
	}{
		{
			path:     "web/app.js",
			source:   "import tool from 'pkg';\nclass Worker { run() { return tool(); } }\n",
			language: "javascript",
		},
		{
			path: "web/app.ts",
			source: "import tool from 'pkg';\n" +
				"class Worker { run(): string { return tool(); } }\n",
			language: "typescript",
		},
		{
			path: "web/app.mts",
			source: "import tool from 'pkg';\n" +
				"class Worker { run(): string { return tool(); } }\n",
			language: "typescript",
		},
		{
			path: "web/app.cts",
			source: "import tool from 'pkg';\n" +
				"class Worker { run(): string { return tool(); } }\n",
			language: "typescript",
		},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			t.Parallel()

			file, found, err := Analyze(test.path, []byte(test.source))
			if err != nil {
				t.Fatalf("analyze: %v", err)
			}

			assertCodeFacts(t, file, found, test.language, "pkg", "tool")
		})
	}
}

func assertCodeFacts(
	t *testing.T,
	file File,
	found bool,
	language string,
	importName string,
	reference string,
) {
	t.Helper()

	if !found || file.Language != language {
		t.Fatalf("language = %q ok=%v", file.Language, found)
	}

	if !hasSymbol(file.Symbols, "Worker.run", "function") {
		t.Fatalf("missing Worker.run symbol: %#v", file.Symbols)
	}

	if !hasImport(file.Imports, importName) {
		t.Fatalf("missing import %q: %#v", importName, file.Imports)
	}

	if !symbolReferences(file.Symbols, "Worker.run", reference) {
		t.Fatalf("Worker.run missing reference %q: %#v", reference, file.Symbols)
	}
}

func TestAnalyzeHandlesShellAndYAMLFacts(t *testing.T) {
	t.Parallel()

	shellFile, found, err := Analyze(
		"scripts/build.sh",
		[]byte("build() {\n  echo ok\n}\n"),
	)
	if err != nil {
		t.Fatalf("analyze shell: %v", err)
	}

	if !found || !hasSymbol(shellFile.Symbols, "build", "function") {
		t.Fatalf("shell symbols = %#v ok=%v", shellFile.Symbols, found)
	}

	yamlFile, found, err := Analyze(
		"deploy/pod.yaml",
		[]byte("apiVersion: v1\nkind: Pod\nmetadata:\n  name: unsafe-pod\n"),
	)
	if err != nil {
		t.Fatalf("analyze yaml: %v", err)
	}

	if !found || yamlFile.Language != "yaml" || len(yamlFile.Symbols) == 0 {
		t.Fatalf("yaml facts = %#v ok=%v", yamlFile, found)
	}

	if !hasSymbol(yamlFile.Symbols, "kind", "config_entry") ||
		!hasSymbol(yamlFile.Symbols, "metadata.name", "config_entry") {
		t.Fatalf("kubernetes yaml facts missing expected entries: %#v", yamlFile.Symbols)
	}
}

func TestUnsupportedPathsAndInvalidLinesReturnNoContext(t *testing.T) {
	t.Parallel()

	if language, found := LanguageForPath("README.txt"); found || language != "" {
		t.Fatalf("unsupported language = %q ok=%v", language, found)
	}

	_, found, inlineErrAutoA := Analyze("README.txt", []byte("# docs\n"))
	if inlineErrAutoA != nil || found {
		t.Fatalf("unsupported analyze ok=%v err=%v", found, inlineErrAutoA)
	}

	_, found, inlineErrAutoB := ContextForLine(
		"pkg/app.py",
		[]byte("def run():\n    pass\n"),
		0,
	)
	if inlineErrAutoB != nil ||
		found {
		t.Fatalf("invalid line context ok=%v err=%v", found, inlineErrAutoB)
	}
}

func TestParseAndWalkExposeTreeTraversalHelpers(t *testing.T) {
	t.Parallel()

	tree, found, err := Parse("pkg/app.py", []byte("def run():\n    return 1\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if !found {
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

func hasSymbol(symbols []Symbol, path, kind string) bool {
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

func symbolReferences(symbols []Symbol, path, reference string) bool {
	for _, symbol := range symbols {
		if symbol.SymbolPath != path {
			continue
		}

		if slices.Contains(symbol.ReferencedNames, reference) {
			return true
		}
	}

	return false
}

func hasCall(symbols []Symbol, path, call string) bool {
	for _, symbol := range symbols {
		if symbol.SymbolPath != path {
			continue
		}

		if slices.Contains(symbol.CallNames, call) {
			return true
		}
	}

	return false
}

func hasBase(symbols []Symbol, path, base string) bool {
	for _, symbol := range symbols {
		if symbol.SymbolPath != path {
			continue
		}

		if slices.Contains(symbol.BaseNames, base) {
			return true
		}
	}

	return false
}

func TestAnalyzeExtractsCallsAndBases(t *testing.T) {
	t.Parallel()

	// Python
	pyFile, found, err := Analyze("app.py", []byte(`
class Base:
    pass

class Sub(Base):
    def run(self):
        other()
`))

	if err != nil || !found {
		t.Fatalf("analyze python: %v %v", err, found)
	}

	if !hasBase(pyFile.Symbols, "Sub", "Base") {
		t.Errorf("Sub missing base Base: %#v", pyFile.Symbols)
	}

	if !hasCall(pyFile.Symbols, "Sub.run", "other") {
		t.Errorf("Sub.run missing call other: %#v", pyFile.Symbols)
	}

	// Go
	goFile, found, err := Analyze("app.go", []byte(`
package main

func main() {
    execute()
}
`))

	if err != nil || !found {
		t.Fatalf("analyze go: %v %v", err, found)
	}

	if !hasCall(goFile.Symbols, "main", "execute") {
		t.Errorf("main missing call execute: %#v", goFile.Symbols)
	}
}
