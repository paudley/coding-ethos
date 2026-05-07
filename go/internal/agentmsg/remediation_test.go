// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package agentmsg_test

import (
	"testing"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/agentmsg"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestFromDiagnosticsBuildsMCPBackedRemediation(t *testing.T) {
	t.Parallel()

	items := agentmsg.FromDiagnostics([]diagnostics.Diagnostic{{
		Tool:         "ruff",
		File:         "pkg/app.py",
		Line:         12,
		Column:       4,
		Severity:     "error",
		Code:         "PLC0415",
		PolicyID:     "python.conditional_imports",
		SkillID:      "conditional-imports",
		Message:      "import outside top-level",
		Advice:       "Move imports to module scope.",
		AdviceSteps:  []string{"Move the import to the top of the file."},
		PrincipleIDs: []string{"no-conditional-imports"},
		Rerun:        []string{"ruff check pkg/app.py"},
	}})

	if len(items) != 1 {
		t.Fatalf("got %d remediation items, want 1", len(items))
	}

	item := items[0]
	if item.PolicyID != "python.conditional_imports" {
		t.Fatalf("policy id = %q", item.PolicyID)
	}

	if item.ID == "" {
		t.Fatalf("missing stable remediation id: %#v", item)
	}

	if item.MCP == nil || item.MCP.Tool != "policy_explain" {
		t.Fatalf("MCP call = %#v", item.MCP)
	}

	if item.MCP.Arguments["policy_id"] != "python.conditional_imports" {
		t.Fatalf("MCP args = %#v", item.MCP.Arguments)
	}

	if item.NextSteps[0] != "Move the import to the top of the file." {
		t.Fatalf("next steps = %#v", item.NextSteps)
	}

	if item.NextSteps[1] != policyExplainStep("python.conditional_imports") {
		t.Fatalf("missing policy MCP step: %#v", item.NextSteps)
	}

	if item.NextSteps[2] != skillLookupStep("conditional-imports") {
		t.Fatalf("missing skill MCP step: %#v", item.NextSteps)
	}
}

func TestFromDecisionsUsesFailedActionAndFallbackStep(t *testing.T) {
	t.Parallel()

	items := agentmsg.FromDecisions([]policy.Decision{{
		Decision: "block",
		PolicyID: "git.wrapper_required",
		Severity: "block",
		Message:  "Use the coding-ethos git wrapper.",
		Evidence: map[string]any{
			"command":  "git commit --no-verify -m test",
			"skill_id": "safe-git-workflow",
		},
		PrincipleIDs: []string{"one-path-for-critical-operations"},
	}}, "Bash")

	if len(items) != 1 {
		t.Fatalf("got %d remediation items, want 1", len(items))
	}

	item := items[0]
	if item.FailedAction != "Bash" {
		t.Fatalf("failed action = %q", item.FailedAction)
	}

	if item.Command != "git commit --no-verify -m test" {
		t.Fatalf("command = %q", item.Command)
	}

	if item.SkillUse != "Load the safe-git-workflow skill before editing or retrying." {
		t.Fatalf("skill use = %q", item.SkillUse)
	}

	if item.NextSteps[0] != policyExplainStep("git.wrapper_required") {
		t.Fatalf("next steps = %#v", item.NextSteps)
	}

	if item.NextSteps[1] != skillLookupStep("safe-git-workflow") {
		t.Fatalf("next steps = %#v", item.NextSteps)
	}
}

func policyExplainStep(policyID string) string {
	return "Call MCP policy_explain with policy_id=" + policyID + " before retrying."
}

func skillLookupStep(skillID string) string {
	return "Call MCP skill_lookup with skill_id=" + skillID + " for the repair playbook."
}

func TestFromDiagnosticsKeepsPolicyOnlyItemsAlignedWithFindings(t *testing.T) {
	t.Parallel()

	items := agentmsg.FromDiagnostics([]diagnostics.Diagnostic{{
		PolicyID: "python.unused_imports",
	}})

	if len(items) != 1 {
		t.Fatalf("got %d remediation items, want 1", len(items))
	}

	if items[0].PolicyID != "python.unused_imports" {
		t.Fatalf("policy id = %q", items[0].PolicyID)
	}
}

func TestFromDecisionsKeepsPolicyOnlyItemsAlignedWithFindings(t *testing.T) {
	t.Parallel()

	items := agentmsg.FromDecisions([]policy.Decision{{
		PolicyID: "git.wrapper_required",
	}}, "Bash")

	if len(items) != 1 {
		t.Fatalf("got %d remediation items, want 1", len(items))
	}

	if items[0].PolicyID != "git.wrapper_required" || items[0].FailedAction != "Bash" {
		t.Fatalf("remediation = %#v", items[0])
	}
}

func TestFromDecisionsUsesDiagnosticItemsAndFileEvidenceFallbacks(t *testing.T) {
	t.Parallel()

	items := agentmsg.FromDecisions([]policy.Decision{
		{
			Diagnostics: []diagnostics.Diagnostic{{
				Tool:     "policy",
				PolicyID: "filesystem.line_limits",
				File:     "pkg/large.py",
				Message:  "Large source files must not keep growing.",
			}},
		},
		{
			PolicyID: "filesystem.protected_path",
			Message:  "Protected path",
			Evidence: map[string]any{
				"files": []any{"coding-ethos-hooks/bin/coding-ethos-run"},
			},
		},
		{
			Message: "",
		},
	}, "Edit")

	if len(items) != 2 {
		t.Fatalf("got %d remediation items, want 2: %#v", len(items), items)
	}

	if items[0].FailedAction != "Edit" ||
		items[0].PolicyID != "filesystem.line_limits" ||
		items[0].File != "pkg/large.py" {
		t.Fatalf("diagnostic remediation mismatch: %#v", items[0])
	}

	if items[1].Path != "coding-ethos-hooks/bin/coding-ethos-run" {
		t.Fatalf("file evidence fallback missing: %#v", items[1])
	}
}

func TestFromDiagnosticsUsesSkillOnlyMCPWhenPolicyMissing(t *testing.T) {
	t.Parallel()

	items := agentmsg.FromDiagnostics([]diagnostics.Diagnostic{{
		SkillID: "lint-remediation",
		Message: "Fix the lint finding structurally.",
	}})

	if len(items) != 1 {
		t.Fatalf("got %d remediation items, want 1", len(items))
	}

	if items[0].MCP == nil || items[0].MCP.Tool != "skill_lookup" ||
		items[0].MCP.Arguments["skill_id"] != "lint-remediation" {
		t.Fatalf("skill-only MCP mismatch: %#v", items[0].MCP)
	}
}

func TestSummarizeReportsRepeatedPolicies(t *testing.T) {
	t.Parallel()

	summary := agentmsg.Summarize([]agentmsg.Remediation{
		{ID: "rem-1", PolicyID: "policy.a", SkillID: "skill-a"},
		{ID: "rem-2", PolicyID: "policy.a", SkillID: "skill-a"},
		{ID: "rem-3", PolicyID: "policy.b"},
	})

	if summary.RemediationCount != 3 {
		t.Fatalf("remediation count = %d", summary.RemediationCount)
	}

	if len(summary.PolicyIDs) != 2 || len(summary.SkillIDs) != 1 {
		t.Fatalf("summary ids = %#v", summary)
	}

	if len(summary.RepeatedPolicy) != 1 ||
		summary.RepeatedPolicy[0].PolicyID != "policy.a" ||
		summary.RepeatedPolicy[0].Count != 2 {
		t.Fatalf("repeat summary = %#v", summary.RepeatedPolicy)
	}
}
