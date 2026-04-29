// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

//nolint:lll // Uses process-global fixtures.
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseHadolintFindings(t *testing.T) {
	t.Parallel()

	findings := parseHadolintFindings(toolOutputFixture(t, "hadolint.json"))
	if len(findings) != 1 {
		t.Fatalf("parseHadolintFindings() = %#v, want one finding", findings)
	}

	got := findings[0]
	if got.File != "Dockerfile" || got.Line != 3 || got.Code != "DL3008" ||
		got.Severity != "warning" || got.Message != "Pin versions in apt get install." {
		t.Fatalf("unexpected finding: %#v", got)
	}

	textFindings := parseHadolintFindings("Dockerfile:3 DL3008 warning: Pin versions in apt get install.")
	if len(textFindings) != 1 {
		t.Fatalf("parseHadolintFindings(text) = %#v, want one finding", textFindings)
	}
}

func TestParseActionlintFindings(t *testing.T) {
	t.Parallel()

	findings := parseActionlintFindings(toolOutputFixture(t, "actionlint.jsonl"))
	if len(findings) != 1 {
		t.Fatalf("parseActionlintFindings() = %#v, want one finding", findings)
	}

	got := findings[0]
	if got.File != ".github/workflows/ci.yml" || got.Line != 12 || got.Column != 5 ||
		got.Code != "syntax-check" || got.Message != "property \"run\" is not defined" {
		t.Fatalf("unexpected finding: %#v", got)
	}

	textFindings := parseActionlintFindings(".github/workflows/ci.yml:12:5: property \"run\" is not defined [syntax-check]")
	if len(textFindings) != 1 {
		t.Fatalf("parseActionlintFindings(text) = %#v, want one finding", textFindings)
	}
}

func TestParseGolangciFindings(t *testing.T) {
	t.Parallel()

	findings := parseGolangciFindings(
		"level=warning msg=\"runner warning\"\n" + toolOutputFixture(t, "golangci.json"),
	)
	if len(findings) != 1 {
		t.Fatalf("parseGolangciFindings() = %#v, want one finding", findings)
	}

	got := findings[0]
	if got.File != "pkg/app.go" || got.Line != 8 || got.Column != 2 ||
		got.Code != "ineffassign" || got.Message != "ineffectual assignment to err" {
		t.Fatalf("unexpected finding: %#v", got)
	}

	textFindings := parseGolangciFindings("pkg/app.go:8:2: ineffectual assignment to err (ineffassign)")
	if len(textFindings) != 1 {
		t.Fatalf("parseGolangciFindings(text) = %#v, want one finding", textFindings)
	}
}

func TestParseGofmtCheckFindings(t *testing.T) {
	t.Parallel()

	findings := parseGofmtCheckFindings("pkg/app.go\ncmd/main.go\n")
	if len(findings) != 2 {
		t.Fatalf("parseGofmtCheckFindings() = %#v, want two findings", findings)
	}

	got := findings[0]
	if got.Tool != "gofmt-check" ||
		got.File != toolCatalogGoFile ||
		got.Severity != "error" ||
		got.Message != "Go file is not gofmt-formatted." {
		t.Fatalf("unexpected finding: %#v", got)
	}
}

func TestParsePythonQualityFindings(t *testing.T) {
	t.Parallel()

	assertComplexityFinding(t)
	assertMaintainabilityFinding(t)
	assertMaintainabilityTimeoutFinding(t)
	assertVultureFinding(t)
}

func assertComplexityFinding(t *testing.T) {
	t.Helper()

	complexity := parseComplexityFindings("  pkg/app.py:42 build_payload (complexity: 19)")
	if len(complexity) != 1 || complexity[0].Code != "cyclomatic-complexity" ||
		complexity[0].Line != 42 {
		t.Fatalf("parseComplexityFindings() = %#v", complexity)
	}

	radon := parseRadonComplexityFindings(
		`{"pkg/app.py":[{"type":"function","rank":"C","lineno":42,"name":"build_payload","complexity":19}]}`,
		15,
	)
	if len(radon) != 1 ||
		radon[0].Code != "cyclomatic-complexity" ||
		radon[0].Message != "build_payload" ||
		radon[0].Detail != "complexity: 19" {
		t.Fatalf("parseRadonComplexityFindings() = %#v", radon)
	}
}

func assertMaintainabilityFinding(t *testing.T) {
	t.Helper()

	maintainability := parseMaintainabilityFindings("  pkg/app.py (MI: 42.50)")
	if len(maintainability) != 1 || maintainability[0].Code != "maintainability-index" {
		t.Fatalf("parseMaintainabilityFindings() = %#v", maintainability)
	}

	radon := parseRadonMaintainabilityFindings(
		`{"pkg/app.py":{"mi":42.5,"rank":"C"}}`,
		50,
	)
	if len(radon) != 1 ||
		radon[0].Code != "maintainability-index" ||
		radon[0].Detail != "MI: 42.50" {
		t.Fatalf("parseRadonMaintainabilityFindings() = %#v", radon)
	}
}

func assertMaintainabilityTimeoutFinding(t *testing.T) {
	t.Helper()

	timeout := parseMaintainabilityFindings("Error: radon timed out after 60s")
	if len(timeout) != 1 ||
		timeout[0].Code != "timeout" ||
		timeout[0].Message != "radon timed out after 60s" ||
		timeout[0].Advice == "" {
		t.Fatalf("parseMaintainabilityFindings(timeout) = %#v", timeout)
	}
}

func assertVultureFinding(t *testing.T) {
	t.Helper()

	vulture := parseVultureFindings("pkg/app.py:17: unused function 'helper' (60% confidence)")
	if len(vulture) != 1 || vulture[0].Code != "unused-code" || vulture[0].Line != 17 {
		t.Fatalf("parseVultureFindings() = %#v", vulture)
	}
}

func toolOutputFixture(t *testing.T, name string) string {
	t.Helper()

	content, err := os.ReadFile(filepath.Join("testdata", "tool_outputs", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}

	return string(content)
}
