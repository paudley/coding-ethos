// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evidence

import (
	"testing"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/agentmsg"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestFromDiagnosticBuildsStableFindingWithSourceSpan(t *testing.T) {
	t.Parallel()

	diagnostic := diagnostics.Diagnostic{
		Tool:     "ruff",
		Code:     "F401",
		File:     "./pkg/app.py",
		Function: "main",
		Line:     4,
		Column:   8,
		Message:  "unused import",
		PolicyID: "python.unused_imports",
		SkillID:  "lint-remediation",
		Severity: "error",
		Advice:   "Remove unused imports.",
		Metadata: map[string]any{
			"implementation": "cel",
			"language":       "python",
			"symbol_kind":    "function",
			"end_line":       6,
			"content_hash":   "sha256:abc",
		},
		PrincipleIDs: []string{"static-analysis-is-the-first-line-of-defense"},
	}

	first := FromDiagnostic(diagnostic)
	second := FromDiagnostic(diagnostic)

	if first.ID == "" || first.ID != second.ID {
		t.Fatalf("unstable finding ID: first=%q second=%q", first.ID, second.ID)
	}
	if first.SourceSpan.Path != "pkg/app.py" ||
		first.SourceSpan.Language != "python" ||
		first.SourceSpan.SymbolName != "main" ||
		first.SourceSpan.EndLine != 6 {
		t.Fatalf("source span = %#v", first.SourceSpan)
	}
	if first.EvaluatorKind != "cel" ||
		first.SchemaVersion != SchemaVersion ||
		first.SearchText == "" {
		t.Fatalf("finding = %#v", first)
	}
}

func TestFromDecisionKeepsEvidenceKeysAndActionContext(t *testing.T) {
	t.Parallel()

	finding := FromDecision(policy.Decision{
		PolicyID:   "git.hook_bypass",
		Severity:   "block",
		Message:    "Hook bypass is forbidden.",
		Suggestion: "Use the managed wrapper.",
		Evidence: map[string]any{
			"command":        "git commit --no-verify -m test",
			"implementation": "cel",
			"skill_id":       "safe-git-workflow",
		},
		PrincipleIDs: []string{"one-path-for-critical-operations"},
	})

	if finding.PolicyID != "git.hook_bypass" ||
		finding.SkillID != "safe-git-workflow" ||
		finding.EvaluatorKind != "cel" {
		t.Fatalf("finding = %#v", finding)
	}
	if len(finding.EvidenceKeys) != 3 || finding.SearchText == "" {
		t.Fatalf("evidence/search = %#v", finding)
	}
}

func TestFromDiagnosticsAndDecisionsFilterEmptyRecords(t *testing.T) {
	t.Parallel()

	findings := FromDiagnostics([]diagnostics.Diagnostic{
		{},
		{
			Tool:     "ruff",
			Code:     "F401",
			File:     "pkg/app.py",
			Message:  "unused import",
			PolicyID: "python.unused_imports",
		},
	})
	if len(findings) != 1 || findings[0].PolicyID != "python.unused_imports" {
		t.Fatalf("findings = %#v", findings)
	}

	decisionFindings := FromDecisions([]policy.Decision{
		{},
		{
			PolicyID: "git.hook_bypass",
			Message:  "blocked",
			Evidence: map[string]any{"tool": "Bash"},
		},
		{
			PolicyID: "diagnostic.policy",
			Diagnostics: []diagnostics.Diagnostic{{
				File:     "pkg/diag.py",
				Message:  "diagnostic message",
				PolicyID: "diagnostic.policy",
			}},
		},
	})
	if len(decisionFindings) != 2 {
		t.Fatalf("decision findings = %#v", decisionFindings)
	}
	if decisionFindings[0].PolicyID != "git.hook_bypass" ||
		decisionFindings[1].SourceSpan.Path != "pkg/diag.py" {
		t.Fatalf("decision findings = %#v", decisionFindings)
	}
}

func TestEnvelopeFromFindingPreservesMetadataCopies(t *testing.T) {
	t.Parallel()

	finding := Finding{
		ID:            "finding-1",
		PolicyID:      "policy.a",
		SkillID:       "skill-a",
		EvaluatorKind: "cel",
		EvidenceKeys:  []string{"command"},
		SchemaVersion: SchemaVersion,
	}
	envelope := EnvelopeFromFinding(finding)
	finding.EvidenceKeys[0] = "mutated"

	if envelope.ID != "finding-1" ||
		envelope.PolicyID != "policy.a" ||
		envelope.EvidenceKeys[0] != "command" ||
		envelope.SchemaVersion != SchemaVersion {
		t.Fatalf("envelope = %#v", envelope)
	}
}

func TestRemediationEventsLinkRemediationToFinding(t *testing.T) {
	t.Parallel()

	events := RemediationEvents(
		[]agentmsg.Remediation{{
			ID:       "rem-1",
			PolicyID: "policy.a",
			SkillID:  "skill-a",
			Message:  "Fix it.",
		}},
		[]Finding{{ID: "finding-1"}},
		"trace-a.json",
		"suggested",
	)

	if len(events) != 1 {
		t.Fatalf("events = %#v", events)
	}
	event := events[0]
	if event.ID == "" ||
		event.RemediationID != "rem-1" ||
		event.FindingID != "finding-1" ||
		event.TraceID != "trace-a.json" ||
		event.SchemaVersion != SchemaVersion {
		t.Fatalf("event = %#v", event)
	}
}

func TestStableIDPreservesEmptyFieldPositions(t *testing.T) {
	t.Parallel()

	left := stableID("finding", "rule", "", "code")
	right := stableID("finding", "rule", "code", "")

	if left == right {
		t.Fatalf("stable IDs collided when empty fields moved: %q", left)
	}
}
