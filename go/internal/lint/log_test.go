// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package lint_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/lint"
)

const conditionalImportsPolicyID = "python.conditional_imports"

func TestLogResultWritesNormalizedTrace(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	result := Result{
		Scope:  ScopeStaged,
		Status: "blocked",
		Findings: []Finding{{
			CheckID:    "python.direct_imports",
			PolicyID:   conditionalImportsPolicyID,
			SkillID:    "conditional-imports",
			Severity:   "block",
			Status:     "fail",
			Message:    "direct import violation",
			SourceTool: "ruff",
			EthosIDs:   []string{"no-conditional-imports"},
			Blocking:   true,
		}},
		SkillHints: []SkillHint{{
			PrincipleID: "linting-as-code-quality-enforcement",
			SkillID:     "lint-remediation",
			Message:     "Fix lint structurally.",
			Next:        "Load the lint-remediation skill for the remediation playbook.",
		}},
	}

	path, err := LogResult(repo, result)
	if err != nil {
		t.Fatalf("LogResult() returned error: %v", err)
	}

	if filepath.Dir(path) != filepath.Join(repo, ".coding-ethos", "lint-runs") {
		t.Fatalf("trace path = %q", path)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}

	var record TraceRecord

	inlineErr0 := json.Unmarshal(content, &record)
	if inlineErr0 != nil {
		t.Fatalf("decode trace: %v\n%s", inlineErr0, content)
	}

	assertNormalizedTraceRecord(t, repo, record)
	assertNormalizedFinding(t, record)
	assertNormalizedRemediation(t, record)
}

func assertNormalizedTraceRecord(t *testing.T, repo string, record TraceRecord) {
	t.Helper()

	if record.RepoRoot != repo ||
		record.SchemaVersion != 1 ||
		record.TraceID == "" ||
		record.Result.TraceID != record.TraceID ||
		record.Result.Scope != ScopeStaged ||
		len(record.Result.Findings) != 1 ||
		len(record.Result.SkillHints) != 1 ||
		len(record.AgentRemediation) != 1 ||
		len(record.Findings) != 1 ||
		len(record.RemediationEvents) != 1 {
		t.Fatalf("trace record = %#v", record)
	}
}

func assertNormalizedFinding(t *testing.T, record TraceRecord) {
	t.Helper()

	if record.Findings[0].PolicyID != conditionalImportsPolicyID ||
		record.Findings[0].ID == "" ||
		record.Findings[0].SearchText == "" {
		t.Fatalf("normalized findings = %#v", record.Findings)
	}
}

func assertNormalizedRemediation(t *testing.T, record TraceRecord) {
	t.Helper()

	if record.RemediationEvents[0].RemediationID != record.AgentRemediation[0].ID ||
		record.RemediationEvents[0].FindingID != record.Findings[0].ID {
		t.Fatalf("remediation events = %#v", record.RemediationEvents)
	}

	if record.AgentRemediation[0].PolicyID != conditionalImportsPolicyID ||
		record.AgentRemediation[0].MCP == nil ||
		record.AgentRemediation[0].MCP.Tool != "policy_explain" {
		t.Fatalf("remediation trace = %#v", record.AgentRemediation)
	}
}

func TestLogResultSanitizesTraceScopeFilename(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	result := Result{
		Scope:  "tool:ruff/json output",
		Status: "blocked",
	}

	path, err := LogResult(repo, result)
	if err != nil {
		t.Fatalf("LogResult() returned error: %v", err)
	}

	name := filepath.Base(path)
	if !strings.Contains(name, "-tool_ruff_json_output.json") {
		t.Fatalf("trace filename not sanitized: %q", name)
	}

	if strings.ContainsAny(name, `: /\`) {
		t.Fatalf("trace filename contains unsafe separator: %q", name)
	}
}

func TestTracePathForIDStaysInsideLintTraceDir(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()

	path, err := TracePathForID(repo, "20260101T000000.000000000Z-123-tool_ruff.json")
	if err != nil {
		t.Fatalf("TracePathForID() returned error: %v", err)
	}

	want := filepath.Join(
		repo,
		".coding-ethos",
		"lint-runs",
		"20260101T000000.000000000Z-123-tool_ruff.json",
	)
	if path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
}

func TestTracePathForIDRejectsPathTraversal(t *testing.T) {
	t.Parallel()

	_, inlineErrAutoA := TracePathForID(t.TempDir(), "../secret.json")
	if inlineErrAutoA == nil {
		t.Fatalf("TracePathForID() accepted path traversal")
	}
}
