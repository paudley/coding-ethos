// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package lint_test

import (
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
	})

	analysis, err := AnalyzeTraces(root)
	if err != nil {
		t.Fatalf("AnalyzeTraces() returned error: %v", err)
	}

	if analysis.RunsAnalyzed != 3 || analysis.Findings != 3 {
		t.Fatalf("analysis counts = %#v", analysis)
	}
	if analysis.TopChecks[0] != (Count{Key: "python.import_order", Count: 2}) {
		t.Fatalf("top checks = %#v", analysis.TopChecks)
	}
	if analysis.TopCodes[0] != (Count{Key: "ruff:E402", Count: 2}) {
		t.Fatalf("top codes = %#v", analysis.TopCodes)
	}
	if analysis.RepeatedPatterns[0] != (Count{Key: "python.import_order|lib/python/...", Count: 2}) {
		t.Fatalf("patterns = %#v", analysis.RepeatedPatterns)
	}
	if analysis.TopEthosIDs[0] != (Count{Key: "no-conditional-imports", Count: 2}) {
		t.Fatalf("ethos IDs = %#v", analysis.TopEthosIDs)
	}
	if len(analysis.GuidanceCandidates) == 0 ||
		analysis.GuidanceCandidates[0].Advice != "Move imports to module scope." {
		t.Fatalf("guidance candidates = %#v", analysis.GuidanceCandidates)
	}

	output := FormatAnalysisHuman(analysis)
	for _, want := range []string{
		"Top checks: python.import_order=2",
		"Top tool codes: ruff:E402=2",
		"Guidance candidates:",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("analysis output missing %q:\n%s", want, output)
		}
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
			Scope:    ScopeStaged,
			Status:   "blocked",
			Findings: findings,
		},
	})
	if err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
}
