// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package astfacts

import "testing"

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

func hasSymbol(symbols []Symbol, path string, kind string) bool {
	for _, symbol := range symbols {
		if symbol.SymbolPath == path && symbol.SymbolKind == kind {
			return true
		}
	}

	return false
}
