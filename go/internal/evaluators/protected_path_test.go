// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators_test

import (
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestEvaluateProtectedPathBlocksCommandReference(t *testing.T) {
	t.Parallel()

	policyDef := protectedPathPolicy()

	decisions, err := EvaluateProtectedPath(
		policyDef,
		Context{Command: "rm /repo/.git/coding-ethos-hooks/coding-ethos-git-hook"},
	)
	if err != nil {
		t.Fatalf("evaluate protected path: %v", err)
	}

	if len(decisions) != 1 || decisions[0].Decision != blockDecision {
		t.Fatalf("expected block decision, got %#v", decisions)
	}
}

func TestEvaluateProtectedPathBlocksRelativeCommandReference(t *testing.T) {
	t.Parallel()

	policyDef := protectedPathPolicy()

	decisions, err := EvaluateProtectedPath(
		policyDef,
		Context{Command: "rm .git/coding-ethos-hooks/coding-ethos-git-hook"},
	)
	if err != nil {
		t.Fatalf("evaluate protected path: %v", err)
	}

	if len(decisions) != 1 || decisions[0].Decision != blockDecision {
		t.Fatalf("expected block decision, got %#v", decisions)
	}
}

func TestEvaluateProtectedPathBlocksFileTarget(t *testing.T) {
	t.Parallel()

	policyDef := protectedPathPolicy()

	decisions, err := EvaluateProtectedPath(
		policyDef,
		Context{Files: []string{"/repo/.git/coding-ethos-hooks/coding-ethos-git-hook"}},
	)
	if err != nil {
		t.Fatalf("evaluate protected path: %v", err)
	}

	if len(decisions) != 1 || decisions[0].Decision != blockDecision {
		t.Fatalf("expected block decision, got %#v", decisions)
	}
}

func TestEvaluateProtectedPathBlocksDirectoryChildren(t *testing.T) {
	t.Parallel()

	policyDef := protectedPathPolicy()

	decisions, err := EvaluateProtectedPath(
		policyDef,
		Context{
			Files: []string{"/opt/blocked/child"},
			EvaluatorOptions: map[string]any{
				"paths": []string{"/opt/blocked/"},
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate protected path: %v", err)
	}

	if len(decisions) != 1 || decisions[0].Decision != blockDecision {
		t.Fatalf("expected block decision, got %#v", decisions)
	}
}

func TestEvaluateProtectedPathUsesConfiguredPaths(t *testing.T) {
	t.Parallel()

	policyDef := protectedPathPolicy()

	decisions, err := EvaluateProtectedPath(
		policyDef,
		Context{
			Files: []string{"/opt/blocked"},
			EvaluatorOptions: map[string]any{
				"paths": []any{"/opt/blocked"},
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate protected path: %v", err)
	}

	if len(decisions) != 1 || decisions[0].Decision != blockDecision {
		t.Fatalf("expected block decision, got %#v", decisions)
	}
}

func TestEvaluateProtectedPathAllowsAgentWorkspaceTargets(t *testing.T) {
	t.Parallel()

	policyDef := protectedPathPolicy()

	for _, file := range []string{
		".claude/projects/repo/memory/project.md",
		".claude/plans/adaptive-exploring-toast.md",
		".codex/projects/repo/memories/project.md",
		".gemini/MEMORY.md",
		"/workspace/.claude/projects/repo/memory/project.md",
		"/workspace/.claude/plans/adaptive-exploring-toast.md",
		"/workspace/.codex/projects/repo/memories/project.md",
		"/workspace/.gemini/projects/repo/MEMORY.md",
	} {
		decisions, err := EvaluateProtectedPath(
			policyDef,
			Context{
				Files: []string{file},
				EvaluatorOptions: map[string]any{
					"paths": []string{"/workspace/.claude", "/workspace/.codex", "/workspace/.gemini"},
				},
			},
		)
		if err != nil {
			t.Fatalf("evaluate protected path: %v", err)
		}

		if len(decisions) != 0 {
			t.Fatalf("expected allowed agent workspace target %q, got %#v", file, decisions)
		}
	}
}

func protectedPathPolicy() policy.Policy {
	return policy.Policy{
		ID:       "filesystem.protected_path",
		Category: "filesystem",
		Source: policy.SourceRef{
			File: "config.yaml",
			Path: "filesystem.protected_path",
		},
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		Message:         "Protected coding-ethos hook paths must not be modified.",
		DefenseLayers:   policy.GitDefenseLayers("block", "", "block", "", ""),
		Evaluators: []policy.Evaluator{{
			Kind: "path",
			Name: "filesystem.protected_path",
		}},
	}
}
