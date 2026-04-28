// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package toolcatalog_test

import (
	"reflect"
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

func TestToolchainToolsExposeCurrentHookCommands(t *testing.T) {
	t.Parallel()

	tools := mapByName(toolcatalog.ToolchainTools())

	assertToolCommand(t, tools["hadolint"], []string{"hadolint", "--format", "json"})
	assertToolCommand(
		t,
		tools["actionlint"],
		[]string{"actionlint", "-format", "{{json .}}"},
	)
	assertToolCommand(
		t,
		tools["shellcheck"],
		[]string{"shellcheck", "--severity=warning", "-x", "--format=json"},
	)
	assertToolCommand(t, tools["yamllint"], []string{"yamllint"})
	assertToolCommand(t, tools["golangci-lint"], []string{"golangci-lint", "run"})

	assertToolFileMetadata(t, tools["hadolint"], nil, nil, []string{"Dockerfile"})
	assertToolFileMetadata(
		t,
		tools["actionlint"],
		[]string{".yaml", ".yml"},
		[]string{".github/workflows/"},
		nil,
	)
	assertToolFileMetadata(
		t,
		tools["shellcheck"],
		[]string{".sh", ".bash", ".zsh", ".ksh"},
		nil,
		nil,
	)
	assertToolFileMetadata(t, tools["yamllint"], []string{".yaml", ".yml"}, nil, nil)
	assertToolFileMetadata(t, tools["golangci-lint"], []string{".go"}, nil, nil)

	if tools["yamllint"].RepoConfig != ".yamllint.yml" ||
		tools["yamllint"].ConfigFlags[0] != "-c" {
		t.Fatalf("yamllint config metadata = %#v", tools["yamllint"])
	}

	if !reflect.DeepEqual(
		tools["yamllint"].PostConfigArgs,
		[]string{"--strict", "-f", "parsable"},
	) {
		t.Fatalf("yamllint post-config args = %#v", tools["yamllint"].PostConfigArgs)
	}

	golangci := tools["golangci-lint"]
	if golangci.RepoConfig != ".golangci.yml" || golangci.ConfigFlags[0] != "--config" {
		t.Fatalf("golangci-lint config metadata = %#v", golangci)
	}

	wantPostConfig := []string{
		"--output.json.path",
		"stdout",
		"--output.text.path",
		"/dev/null",
	}
	if !reflect.DeepEqual(golangci.PostConfigArgs, wantPostConfig) {
		t.Fatalf(
			"golangci-lint post-config args = %#v, want %#v",
			golangci.PostConfigArgs,
			wantPostConfig,
		)
	}
}

func mapByName(tools []toolcatalog.Tool) map[string]toolcatalog.Tool {
	byName := make(map[string]toolcatalog.Tool, len(tools))
	for _, tool := range tools {
		byName[tool.Name] = tool
	}

	return byName
}

func assertToolCommand(t *testing.T, tool toolcatalog.Tool, want []string) {
	t.Helper()

	if !reflect.DeepEqual(tool.Command, want) {
		t.Fatalf("%s command = %#v, want %#v", tool.Name, tool.Command, want)
	}
}

func assertToolFileMetadata(
	t *testing.T,
	tool toolcatalog.Tool,
	extensions []string,
	prefixes []string,
	basePrefixes []string,
) {
	t.Helper()

	if !reflect.DeepEqual(tool.FileExtensions, extensions) ||
		!reflect.DeepEqual(tool.FilePrefixes, prefixes) ||
		!reflect.DeepEqual(tool.BaseNamePrefixes, basePrefixes) {
		t.Fatalf("file metadata for %s = %#v", tool.Name, tool)
	}
}
