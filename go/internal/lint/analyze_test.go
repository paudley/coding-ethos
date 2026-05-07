// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package lint_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/lint"
)

func TestAnalyzeTracesRanksFailuresAndGuidanceCandidates(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTraceFixture(t, root, "run-a.json", []Finding{
		{
			CheckID:    "python.import_order",
			PolicyID:   "python.import_order",
			SourceTool: "ruff",
			Code:       "E402",
			File:       "lib/python/app.py",
			Status:     "fail",
			Severity:   "error",
			Message:    "Module import not at top",
			Advice:     "Move imports to module scope.",
			EthosIDs:   []string{"no-conditional-imports"},
			SkillID:    "conditional-imports",
			Blocking:   true,
		},
		{
			CheckID:  "record.only",
			Status:   "pass",
			Message:  "record",
			Blocking: false,
		},
	})
	writeTraceFixture(t, root, "run-b.json", []Finding{
		{
			CheckID:    "python.import_order",
			PolicyID:   "python.import_order",
			SourceTool: "ruff",
			Code:       "E402",
			File:       "lib/python/other.py",
			Status:     "fail",
			Severity:   "error",
			Message:    "Module import not at top",
			Advice:     "Move imports to module scope.",
			EthosIDs:   []string{"no-conditional-imports"},
			SkillID:    "conditional-imports",
			Blocking:   true,
		},
	})
	writeTraceFixture(t, root, "run-c.json", []Finding{
		{
			CheckID:    "repo.license_header",
			SourceTool: "license_header",
			File:       "go/internal/app.go",
			Status:     "fail",
			Severity:   "block",
			Message:    "missing required license header text",
			EthosIDs:   []string{"documentation-as-contract"},
			Blocking:   true,
		},
		{
			CheckID:    "tool.pylint",
			SourceTool: "pylint",
			Code:       "no-member",
			File:       "lib/python/app.py",
			Status:     "fail",
			Severity:   "error",
			Message:    "Instance has no member",
			Blocking:   true,
		},
	})

	analysis, err := AnalyzeTraces(root)
	if err != nil {
		t.Fatalf("AnalyzeTraces() returned error: %v", err)
	}

	assertTraceAnalysisCounts(t, analysis)
	assertTraceAnalysisTopCounts(t, analysis)
	assertTraceAnalysisGuidance(t, analysis)
	assertTraceAnalysisOutput(t, analysis)
}

func assertTraceAnalysisCounts(t *testing.T, analysis Analysis) {
	t.Helper()

	if analysis.RunsAnalyzed != 3 || analysis.Findings != 4 {
		t.Fatalf("analysis counts = %#v", analysis)
	}
}

func assertTraceAnalysisTopCounts(t *testing.T, analysis Analysis) {
	t.Helper()

	assertCount(
		t,
		"top checks",
		analysis.TopChecks,
		Count{Key: "python.import_order", Count: 2},
	)
	assertCount(t, "top codes", analysis.TopCodes, Count{Key: "ruff:E402", Count: 2})
	assertCount(
		t,
		"patterns",
		analysis.RepeatedPatterns,
		Count{Key: "python.import_order|lib/python/...", Count: 2},
	)
	assertCount(
		t,
		"ethos IDs",
		analysis.TopEthosIDs,
		Count{Key: "no-conditional-imports", Count: 2},
	)
	assertCount(
		t,
		"skill IDs",
		analysis.TopSkillIDs,
		Count{Key: "conditional-imports", Count: 2},
	)
	assertCount(
		t,
		"skill hints",
		analysis.TopSkillHints,
		Count{Key: "conditional-imports", Count: 2},
	)
	assertCount(
		t,
		"unmapped codes",
		analysis.UnmappedCodes,
		Count{Key: "pylint:no-member", Count: 1},
	)
}

func assertCount(t *testing.T, label string, counts []Count, want Count) {
	t.Helper()

	if len(counts) == 0 || counts[0] != want {
		t.Fatalf("%s = %#v", label, counts)
	}
}

func assertTraceAnalysisGuidance(t *testing.T, analysis Analysis) {
	t.Helper()

	if len(analysis.GuidanceCandidates) == 0 ||
		analysis.GuidanceCandidates[0].Advice != "Move imports to module scope." {
		t.Fatalf("guidance candidates = %#v", analysis.GuidanceCandidates)
	}
}

func assertTraceAnalysisOutput(t *testing.T, analysis Analysis) {
	t.Helper()

	assertAnalysisOutputContains(t, "analysis", FormatAnalysisHuman(analysis), []string{
		"Top checks: python.import_order=2",
		"Top tool codes: ruff:E402=2",
		"Unmapped tool codes: pylint:no-member=1",
		"Top skill IDs: conditional-imports=2",
		"Top emitted skill hints: conditional-imports=2",
		"Guidance candidates:",
	})
	assertAnalysisOutputContains(
		t,
		"TOON analysis",
		FormatAnalysisTOON(analysis),
		[]string{
			"format: toon",
			"operation: analyze-log",
			"top_codes[",
			"unmapped_codes[1]{key,count}:",
			"pylint:no-member,1",
			"top_skill_ids[1]{key,count}:",
			"top_skill_hints[1]{key,count}:",
			"guidance_candidates[",
		},
	)
}

func assertAnalysisOutputContains(
	t *testing.T,
	label string,
	output string,
	expected []string,
) {
	t.Helper()

	for _, want := range expected {
		if !strings.Contains(output, want) {
			t.Fatalf("%s output missing %q:\n%s", label, want, output)
		}
	}
}

func TestEncodeAnalysisHonorsFormat(t *testing.T) {
	t.Parallel()

	analysis := Analysis{
		Path:          "lint-runs",
		TopCodes:      []Count{{Key: "ruff:E402", Count: 2}},
		UnmappedCodes: []Count{{Key: "pylint:no-member", Count: 1}},
		RunsAnalyzed:  1,
		RunsAvailable: 1,
		Findings:      3,
	}

	var buffer bytes.Buffer

	err := EncodeAnalysis(&buffer, analysis, "toon")
	if err != nil {
		t.Fatalf("EncodeAnalysis() returned error: %v", err)
	}

	if !strings.Contains(buffer.String(), "format: toon") ||
		!strings.Contains(buffer.String(), "unmapped_codes[1]{key,count}:") {
		t.Fatalf("TOON analysis missing expected content:\n%s", buffer.String())
	}

	buffer.Reset()

	err = EncodeAnalysis(&buffer, analysis, "json")
	if err != nil {
		t.Fatalf("EncodeAnalysis(json) returned error: %v", err)
	}

	if !strings.Contains(buffer.String(), `"unmapped_codes"`) {
		t.Fatalf("JSON analysis missing expected content:\n%s", buffer.String())
	}
}

func TestAnalyzeTracesFiltersByFilePattern(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTraceFixture(t, root, "python.json", []Finding{
		{
			CheckID:    "python.import_order",
			SourceTool: "ruff",
			Code:       "E402",
			File:       filepath.Join(root, "lib", "python", "app.py"),
			Status:     "fail",
			Message:    "Module import not at top",
			Advice:     "Move imports to module scope.",
			SkillID:    "conditional-imports",
			Blocking:   true,
		},
	})
	writeTraceFixture(t, root, "go.json", []Finding{
		{
			CheckID:    "repo.license_header",
			SourceTool: "license_header",
			File:       "go/internal/app.go",
			Status:     "fail",
			Message:    "missing required license header text",
			SkillID:    "managed-toolchain",
			Blocking:   true,
		},
	})

	analysis, err := AnalyzeTracesWithOptions(root, AnalysisOptions{
		Files:                 []string{"lib/python/new_module.py"},
		MaxCounts:             3,
		MaxGuidanceCandidates: 2,
	})
	if err != nil {
		t.Fatalf("AnalyzeTracesWithOptions() returned error: %v", err)
	}

	if analysis.Findings != 1 {
		t.Fatalf("analysis should only include relevant Python finding: %#v", analysis)
	}

	if len(analysis.TopCodes) != 1 ||
		analysis.TopCodes[0] != (Count{Key: "ruff:E402", Count: 1}) {
		t.Fatalf("top codes = %#v", analysis.TopCodes)
	}

	if len(analysis.GuidanceCandidates) != 1 ||
		analysis.GuidanceCandidates[0].CheckID != "python.import_order" {
		t.Fatalf("guidance candidates = %#v", analysis.GuidanceCandidates)
	}

	if len(analysis.TopSkillHints) != 1 ||
		analysis.TopSkillHints[0] != (Count{Key: "conditional-imports", Count: 1}) {
		t.Fatalf("scoped skill hints = %#v", analysis.TopSkillHints)
	}
}

func TestAnalyzeTracesAllowsMissingTraceDirectory(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "missing", "lint-runs")

	analysis, err := AnalyzeTraces(path)
	if err != nil {
		t.Fatalf("AnalyzeTraces() returned error: %v", err)
	}

	if analysis.Path != path ||
		analysis.RunsAvailable != 0 ||
		analysis.RunsAnalyzed != 0 ||
		analysis.Findings != 0 {
		t.Fatalf("analysis = %#v", analysis)
	}
}

func TestReplayTraceReturnsSavedResult(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTraceFixture(t, root, "tool-mypy.json", []Finding{{
		RawOutcome: map[string]any{
			"category":  "configuration_error",
			"exit_code": float64(2),
			"output":    "mypy: error: cannot read file '<repo>/pkg/app.py'",
		},
		CheckID:    "tool.mypy",
		SourceTool: "mypy",
		Status:     "fail",
		Severity:   "error",
		Message:    "mypy configuration or usage failed with status 2",
		Blocking:   true,
	}})

	result, err := ReplayTrace(filepath.Join(root, "tool-mypy.json"))
	if err != nil {
		t.Fatalf("ReplayTrace() returned error: %v", err)
	}

	if !result.Blocked() || len(result.Findings) != 1 {
		t.Fatalf("replayed result = %#v", result)
	}

	if result.Findings[0].CheckID != "tool.mypy" {
		t.Fatalf("replayed finding = %#v", result.Findings[0])
	}
}

func writeTraceFixture(
	t *testing.T,
	root string,
	name string,
	findings []Finding,
) {
	t.Helper()

	path := filepath.Join(root, name)

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	defer file.Close()

	err = json.NewEncoder(file).Encode(TraceRecord{
		RecordedAtUTC: "20260429T000000Z",
		RepoRoot:      root,
		Result: Result{
			Scope:      ScopeStaged,
			Status:     "blocked",
			Findings:   findings,
			SkillHints: skillHintsForFixture(findings),
		},
	})
	if err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
}

func skillHintsForFixture(findings []Finding) []SkillHint {
	hints := []SkillHint{}

	seen := map[string]bool{}
	for _, finding := range findings {
		if finding.SkillID == "" || seen[finding.SkillID] {
			continue
		}

		hints = append(hints, SkillHint{
			SkillID: finding.SkillID,
			Message: "fixture skill hint",
		})
		seen[finding.SkillID] = true
	}

	return hints
}
