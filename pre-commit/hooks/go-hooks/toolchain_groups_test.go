// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import "testing"

func TestParseHadolintFindings(t *testing.T) {
	findings := parseHadolintFindings("Dockerfile:3 DL3008 warning: Pin versions in apt get install.")
	if len(findings) != 1 {
		t.Fatalf("parseHadolintFindings() = %#v, want one finding", findings)
	}
	got := findings[0]
	if got.File != "Dockerfile" || got.Line != 3 || got.Code != "DL3008" ||
		got.Severity != "warning" || got.Message != "Pin versions in apt get install." {
		t.Fatalf("unexpected finding: %#v", got)
	}
}

func TestParseActionlintFindings(t *testing.T) {
	findings := parseActionlintFindings(".github/workflows/ci.yml:12:5: property \"run\" is not defined [syntax-check]")
	if len(findings) != 1 {
		t.Fatalf("parseActionlintFindings() = %#v, want one finding", findings)
	}
	got := findings[0]
	if got.File != ".github/workflows/ci.yml" || got.Line != 12 || got.Column != 5 ||
		got.Code != "syntax-check" || got.Message != "property \"run\" is not defined" {
		t.Fatalf("unexpected finding: %#v", got)
	}
}

func TestParseGolangciFindings(t *testing.T) {
	findings := parseGolangciFindings("pkg/app.go:8:2: ineffectual assignment to err (ineffassign)")
	if len(findings) != 1 {
		t.Fatalf("parseGolangciFindings() = %#v, want one finding", findings)
	}
	got := findings[0]
	if got.File != "pkg/app.go" || got.Line != 8 || got.Column != 2 ||
		got.Code != "ineffassign" || got.Message != "ineffectual assignment to err" {
		t.Fatalf("unexpected finding: %#v", got)
	}
}

func TestParsePythonQualityFindings(t *testing.T) {
	complexity := parseComplexityFindings("  pkg/app.py:42 build_payload (complexity: 19)")
	if len(complexity) != 1 || complexity[0].Code != "cyclomatic-complexity" ||
		complexity[0].Line != 42 {
		t.Fatalf("parseComplexityFindings() = %#v", complexity)
	}
	maintainability := parseMaintainabilityFindings("  pkg/app.py (MI: 42.50)")
	if len(maintainability) != 1 || maintainability[0].Code != "maintainability-index" {
		t.Fatalf("parseMaintainabilityFindings() = %#v", maintainability)
	}
	vulture := parseVultureFindings("pkg/app.py:17: unused function 'helper' (60% confidence)")
	if len(vulture) != 1 || vulture[0].Code != "unused-code" || vulture[0].Line != 17 {
		t.Fatalf("parseVultureFindings() = %#v", vulture)
	}
}
