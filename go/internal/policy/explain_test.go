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
		"Category: `expression`",
		"Principles: `one-path-for-critical-operations`, `no-rationalized-shortcuts`",
		"## CEL Expression",
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
				Suggestion:      "Run ordinary git commands without bypass flags or shell indirection.",
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
		"`command_fact: {raw, lower, tool, argv, has_inline_env}`",
		"`event: {name, provider, tool, scope, mode, source, matcher, session_id, transcript_path, tool_input_keys, tool_response_keys, return_code, has_tool_input, has_tool_response, is_claude, is_codex, is_gemini}`",
		"`diff: {files, changed_files, staged_files, has_changes, hunks, added_lines, removed_lines, changed_symbols}`",
		"`changed_symbols: list({file, dir, base, ext, language, node_kind, symbol_kind, symbol_name, symbol_path, action, changed_lines, is_generated, is_test, original_line_count, current_line_count, line_delta, line_count_grows, line_count_shrinks, original_start_line, original_end_line, current_start_line, current_end_line, original_content_hash, current_content_hash})`",
		"`diff.hunks[]: {file, old_start, old_lines, new_start, new_lines, header, added_lines, removed_lines}`",
		"`source: {path, language, symbol_name, symbol_kind, chunk_hash, line_count, changed_lines, prior_failures, recent_remediations}`",
		"`findings: list({tool, code, message, file, language, symbol_name, symbol_kind, chunk_hash, line, line_count, changed_lines, severity, policy_id, skill_id, principle_ids})`",
		"`repo: {root, source_roots, python_version, config_candidates, protected_paths, protected_branches}`",
		"`referenced_files: list({file, dir, base, lower, exists, is_regular, in_agent_workspace, size_bytes})`",
		"Reviewed helpers:",
		"`glob_match(pattern, value)`",
		"`command_invokes(command, tool)`",
		"`repo_config_present(files, candidates)`",
		"`any_contains(values, value)`",
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
