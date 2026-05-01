// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package policy_test

import (
	. "blackcat.ca/coding-ethos/go/internal/policy"
	"bytes"
	"strings"
	"testing"
)

func TestExplainPolicyWritesPolicyDetails(t *testing.T) {
	t.Parallel()

	var buffer bytes.Buffer

	err := ExplainPolicy(&buffer, ExampleBundle(), "git.hook_bypass")
	if err != nil {
		t.Fatalf("explain policy: %v", err)
	}

	output := buffer.String()
	for _, expected := range []string{
		"# git.hook_bypass",
		"Category: `git`",
		"Principles: `one-path-for-critical-operations`",
		"Hook bypass is forbidden.",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("explanation missing %q:\n%s", expected, output)
		}
	}
}

func TestExplainPolicyWritesCELExpressionDetails(t *testing.T) {
	t.Parallel()

	bundle := Bundle{
		BundleID: "test",
		Principles: map[string]Principle{
			"one-path-for-critical-operations": {
				ID:    "one-path-for-critical-operations",
				Title: "One Path for Critical Operations",
			},
		},
		Skills: map[string]Skill{
			"safe-git-workflow": {
				ID:        "safe-git-workflow",
				ShortHint: "Use the protected Git workflow.",
			},
		},
		Policies: map[string]Policy{
			"custom.no_subprocess_git": {
				ID:              "custom.no_subprocess_git",
				Category:        "expression",
				DefaultSeverity: "block",
				Source:          SourceRef{File: "config.yaml", Path: "policy.expressions"},
				PrincipleIDs:    []string{"one-path-for-critical-operations"},
				Message:         "Git subprocesses are forbidden.",
				Suggestion:      "Use the protected Git wrapper.",
				Evaluators: []Evaluator{{
					Kind: "cel",
					Name: "cel.expression",
					Options: map[string]any{
						"dispatch_scopes": []string{"files", "staged"},
						"scope":           "command",
						"skill_id":        "safe-git-workflow",
						"when":            `command.contains("subprocess")`,
					},
				}},
			},
		},
	}

	var buffer bytes.Buffer
	err := ExplainPolicy(&buffer, bundle, "custom.no_subprocess_git")
	if err != nil {
		t.Fatalf("explain policy: %v", err)
	}

	output := buffer.String()
	for _, expected := range []string{
		"## CEL Expression",
		"command.contains(\"subprocess\")",
		"Evidence fields:",
		"`when`",
		"Input schema:",
		"`path: {file, dir, base, ext, is_test, is_generated, in_source_root}`",
		"Reviewed helpers:",
		"`glob_match(pattern, value)`",
		"Skill: `safe-git-workflow` - Use the protected Git workflow.",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("expression explanation missing %q:\n%s", expected, output)
		}
	}
}

func TestExplainPolicyRejectsUnknownPolicy(t *testing.T) {
	t.Parallel()

	var buffer bytes.Buffer

	err := ExplainPolicy(&buffer, ExampleBundle(), "missing.policy")
	if err == nil {
		t.Fatal("expected error")
	}

	if !strings.Contains(err.Error(), `unknown policy "missing.policy"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}
