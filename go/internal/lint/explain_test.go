// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package lint_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestExplainReportsSelectedScopeChecks(t *testing.T) {
	t.Parallel()

	result, err := Explain(policy.ExampleBundle(), ScopeStaged)
	if err != nil {
		t.Fatalf("Explain() returned error: %v", err)
	}

	if result.Scope != ScopeStaged || result.Selected == 0 {
		t.Fatalf("explain result = %#v", result)
	}
	if result.SelectedEvidenceMaps == 0 || len(result.EvidenceMaps) == 0 {
		t.Fatalf("explain omitted evidence maps: %#v", result)
	}

	var found bool
	for _, check := range result.Checks {
		if check.CheckID == "git.hook_bypass" {
			found = true
			if check.Status != "selected" || check.Reason == "" {
				t.Fatalf("check = %#v", check)
			}
		}
	}
	if !found {
		t.Fatalf("missing git.hook_bypass in %#v", result.Checks)
	}

	output := FormatExplainResultHuman(result)
	if !strings.Contains(output, "lint scope: staged") ||
		!strings.Contains(output, "git.hook_bypass") ||
		!strings.Contains(output, "selected tools:") ||
		!strings.Contains(output, "evidence maps:") {
		t.Fatalf("human output missing expected details:\n%s", output)
	}
}

func TestExplainReportsToolSelectionForFiles(t *testing.T) {
	t.Parallel()

	result, err := ExplainWithOptions(policy.ExampleBundle(), ExplainOptions{
		Scope: ScopeFiles,
		Files: []string{
			"pkg/app.py",
			"go/internal/app.go",
			"scripts/run.sh",
			"config.yaml",
			".github/workflows/ci.yml",
			"Dockerfile",
		},
	})
	if err != nil {
		t.Fatalf("ExplainWithOptions() returned error: %v", err)
	}

	selected := explainToolStatusByName(result)
	for _, name := range []string{
		"ruff",
		"pyright",
		"mypy",
		"shellcheck",
		"golangci-lint",
		"actionlint",
		"yamllint",
		"hadolint",
	} {
		if selected[name] != "selected" {
			t.Fatalf("%s status = %q, tools = %#v", name, selected[name], result.Tools)
		}
	}
	if selected["pylint"] != "skipped" {
		t.Fatalf("pylint should remain disabled by default: %#v", result.Tools)
	}

	output := FormatExplainResultTOON(result)
	for _, want := range []string{
		"format: toon",
		"operation: explain",
		"files[6]{path}:",
		"ruff,selected,python-static,ruff,json,file selector matched pkg/app.py",
		"pylint,skipped,python-static,pylint,json,tool is disabled by default",
		"evidence_maps[",
		"ruff,codes=" + "PLC" + "0415,python.conditional_imports,conditional-imports,high",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("TOON output missing %q:\n%s", want, output)
		}
	}
	conditionalImportEvidence := "ruff,codes=" +
		"PLC" + "0415,python.conditional_imports,conditional-imports,high"
	if strings.Count(output, conditionalImportEvidence) != 1 {
		t.Fatalf("TOON output should dedupe repeated evidence maps:\n%s", output)
	}
}

func TestEncodeExplainResultJSONIncludesTools(t *testing.T) {
	t.Parallel()

	result, err := ExplainWithOptions(policy.ExampleBundle(), ExplainOptions{
		Scope: ScopeFiles,
		Files: []string{"pkg/app.py"},
	})
	if err != nil {
		t.Fatalf("ExplainWithOptions() returned error: %v", err)
	}

	var buffer bytes.Buffer
	if err := EncodeExplainResult(&buffer, result, "json"); err != nil {
		t.Fatalf("EncodeExplainResult() returned error: %v", err)
	}

	var decoded ExplainResult
	if err := json.Unmarshal(buffer.Bytes(), &decoded); err != nil {
		t.Fatalf("decode explain JSON: %v\n%s", err, buffer.String())
	}
	if decoded.SelectedTools == 0 || len(decoded.Tools) == 0 {
		t.Fatalf("JSON explain omitted tools: %#v", decoded)
	}
	if decoded.SelectedEvidenceMaps == 0 || len(decoded.EvidenceMaps) == 0 {
		t.Fatalf("JSON explain omitted evidence maps: %#v", decoded)
	}
}

func explainToolStatusByName(result ExplainResult) map[string]string {
	status := map[string]string{}
	for _, tool := range result.Tools {
		status[tool.Name] = tool.Status
	}

	return status
}
