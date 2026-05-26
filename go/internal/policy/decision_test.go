// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package policy_test

import (
	"testing"

	"blackcat.ca/coding-ethos/go/diagnostics"
	. "blackcat.ca/coding-ethos/go/internal/policy"
)

const blockDecision = "block"

func TestNewDecisionCopiesPolicyContext(t *testing.T) {
	t.Parallel()

	policyDef := ExampleBundle().Policies["git.hook_bypass"]
	decision := NewDecision(blockDecision, policyDef)

	if decision.Decision != blockDecision {
		t.Fatalf("decision mismatch: got %q", decision.Decision)
	}

	if decision.PolicyID != "git.hook_bypass" {
		t.Fatalf("policy id mismatch: got %q", decision.PolicyID)
	}

	if decision.Severity != policyDef.DefaultSeverity {
		t.Fatalf("severity mismatch: got %q", decision.Severity)
	}

	if decision.Message != policyDef.Message {
		t.Fatalf("message mismatch: got %q", decision.Message)
	}

	if decision.Suggestion != policyDef.Suggestion {
		t.Fatalf("suggestion mismatch: got %q", decision.Suggestion)
	}

	if len(decision.PrincipleIDs) != len(policyDef.PrincipleIDs) {
		t.Fatalf("principle ids mismatch: %#v", decision.PrincipleIDs)
	}
}

func TestDecisionEvidenceFilesPrefersCanonicalFiles(t *testing.T) {
	t.Parallel()

	decision := Decision{
		Evidence: map[string]any{
			"files":        []string{" pyproject.toml ", ""},
			"staged_files": []string{"bin/coding-ethos-run"},
		},
	}

	files := decision.EvidenceFiles()
	if len(files) != 1 || files[0] != "pyproject.toml" {
		t.Fatalf("files mismatch: %#v", files)
	}
}

func TestDecisionEvidenceFilesFallsBackToStagedFiles(t *testing.T) {
	t.Parallel()

	decision := Decision{
		Evidence: map[string]any{
			"staged_files": []any{"bin/coding-ethos-run", ""},
		},
	}

	files := decision.EvidenceFiles()
	if len(files) != 1 || files[0] != "bin/coding-ethos-run" {
		t.Fatalf("files mismatch: %#v", files)
	}
}

func TestDecisionEvidenceFilesFallsBackToSingularPath(t *testing.T) {
	t.Parallel()

	decision := Decision{
		Evidence: map[string]any{
			"path": " Makefile ",
		},
	}

	files := decision.EvidenceFiles()
	if len(files) != 1 || files[0] != "Makefile" {
		t.Fatalf("files mismatch: %#v", files)
	}
}

func TestDecisionEvidenceCommandsNormalizeCommandShapes(t *testing.T) {
	t.Parallel()

	decision := Decision{
		Evidence: map[string]any{
			"shell_commands": []any{
				map[string]any{"argv": []any{"git", "status", "--short"}},
				map[string]any{"name": " python -m pytest "},
			},
		},
	}

	commands := decision.EvidenceCommands()
	if len(commands) != 2 ||
		commands[0] != "git status --short" ||
		commands[1] != "python -m pytest" {
		t.Fatalf("commands mismatch: %#v", commands)
	}
}

func TestDecisionEvidenceStringsNormalizeScalarAndListValues(t *testing.T) {
	t.Parallel()

	decision := Decision{
		Evidence: map[string]any{
			"tool":     " ruff ",
			"skill_id": []any{" lint-remediation ", ""},
		},
	}

	if decision.EvidenceTool() != "ruff" {
		t.Fatalf("tool mismatch: %q", decision.EvidenceTool())
	}
	if decision.EvidenceSkillID() != "lint-remediation" {
		t.Fatalf("skill mismatch: %q", decision.EvidenceSkillID())
	}
}

func TestDecisionEvidenceDiagnosticsFillCanonicalContext(t *testing.T) {
	t.Parallel()

	decision := Decision{
		PolicyID:     "git.staged_admin_files",
		Severity:     blockDecision,
		Message:      "Administrative staged files require explicit handling.",
		Suggestion:   "Confirm the policy change is intentional.",
		PrincipleIDs: []string{"feedback-as-a-first-class-citizen"},
		Evidence: map[string]any{
			"files":    []any{" coding_ethos.yml ", ""},
			"skill_id": "safe-git-workflow",
			"tool":     "policy-lint",
		},
		Diagnostics: []diagnostics.Diagnostic{{
			Message: "specific diagnostic",
		}},
	}

	diagnostics := decision.EvidenceDiagnostics()
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}

	item := diagnostics[0]
	if item.File != "coding_ethos.yml" ||
		item.Tool != "policy-lint" ||
		item.PolicyID != "git.staged_admin_files" ||
		item.Severity != blockDecision ||
		item.SkillID != "safe-git-workflow" ||
		item.Advice != "Confirm the policy change is intentional." ||
		len(item.PrincipleIDs) != 1 {
		t.Fatalf("diagnostic = %#v", item)
	}
}

func TestDecisionEvidenceDiagnosticsRepresentMultipleFilesInDetail(t *testing.T) {
	t.Parallel()

	decision := Decision{
		PolicyID: "repo.multi_file",
		Severity: blockDecision,
		Evidence: map[string]any{
			"staged_files": []string{"a.go", "b.go"},
		},
		Diagnostics: []diagnostics.Diagnostic{{
			Detail: "matched staged policy",
		}},
	}

	diagnostics := decision.EvidenceDiagnostics()
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}

	if diagnostics[0].File != "" ||
		diagnostics[0].Detail != "matched staged policy; files=a.go,b.go" {
		t.Fatalf("diagnostic = %#v", diagnostics[0])
	}
}
