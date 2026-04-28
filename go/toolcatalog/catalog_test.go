// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package toolcatalog_test

import (
	"slices"
	"testing"

	"blackcat.ca/coding-ethos/go/toolcatalog"
)

func TestPythonStaticToolsExposeExpectedTools(t *testing.T) {
	t.Parallel()

	tools := toolcatalog.PythonStaticTools()

	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		names[tool.Name] = true
	}

	for _, name := range []string{"ruff", "pyright", "mypy", "pylint"} {
		if !names[name] {
			t.Fatalf("PythonStaticTools() missing %q: %#v", name, tools)
		}
	}
}

func TestPythonStaticToolsRequestStructuredOutput(t *testing.T) {
	t.Parallel()

	for name, token := range map[string]string{
		"ruff":    "--output-format",
		"pyright": "--outputjson",
		"mypy":    "--output",
		"pylint":  "--output-format=json",
	} {
		tool, ok := toolcatalog.PythonStaticTool(name)
		if !ok {
			t.Fatalf("PythonStaticTool(%q) missing", name)
		}

		if !slices.Contains(tool.Command, token) {
			t.Fatalf("%s command missing %q: %#v", name, token, tool.Command)
		}
	}
}

func TestPythonStaticToolReturnsDefensiveCopy(t *testing.T) {
	t.Parallel()

	tool, found := toolcatalog.PythonStaticTool("ruff")
	if !found {
		t.Fatal("missing ruff tool")
	}

	tool.Command[0] = "changed"

	again, found := toolcatalog.PythonStaticTool("ruff")
	if !found {
		t.Fatal("missing ruff tool on second lookup")
	}

	if again.Command[0] != "ruff" {
		t.Fatalf("catalog command mutated: %#v", again.Command)
	}
}
