// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/hooks"
	"blackcat.ca/coding-ethos/go/internal/memories"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestRunRewritesClaudeMemoryWriteToCentralStore(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	result, err := Run(policy.ExampleBundle(), Options{
		Event: Event{
			Cwd:           root,
			HookEventName: "PreToolUse",
			ProviderHint:  "claude",
			ToolName:      "Write",
			ToolInput: map[string]any{
				"file_path": "~/.claude/projects/acme/repo/memory/project.md",
				"content":   "remember",
			},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if result.Status != "allowed" || result.HookSpecificOutput == nil {
		t.Fatalf("result = %#v", result)
	}
	if result.HookSpecificOutput.UpdatedInput["file_path"] != memories.PrimaryFile {
		t.Fatalf("updated input = %#v", result.HookSpecificOutput.UpdatedInput)
	}
	if _, err := os.Stat(
		filepath.Join(root, ".coding-ethos", "memories", "index.yaml"),
	); err != nil {
		t.Fatalf("missing central memory index: %v", err)
	}
}

func TestRunRewritesClaudeMemoryWriteWithoutProviderHint(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	result, err := Run(policy.ExampleBundle(), Options{
		Event: Event{
			Cwd:           root,
			HookEventName: "PreToolUse",
			ToolName:      "Write",
			ToolInput: map[string]any{
				"file_path": "~/.claude/projects/acme/repo/memory/project.md",
				"content":   "remember",
			},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if result.Status != "allowed" || result.HookSpecificOutput == nil {
		t.Fatalf("result = %#v", result)
	}
	if result.HookSpecificOutput.UpdatedInput["file_path"] != memories.PrimaryFile {
		t.Fatalf("updated input = %#v", result.HookSpecificOutput.UpdatedInput)
	}
}

func TestRunRewritesMemoryPathListToConfiguredCentralStore(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	err := os.WriteFile(
		filepath.Join(root, "repo_config.toml"),
		[]byte("[memories]\nprimary_file = \".coding-ethos/memories/REPO.md\"\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write repo config: %v", err)
	}

	result, err := Run(policy.ExampleBundle(), Options{
		Event: Event{
			Cwd:           root,
			HookEventName: "PreToolUse",
			ProviderHint:  "claude",
			ToolName:      "Write",
			ToolInput: map[string]any{
				"files":   []any{"~/.claude/projects/acme/repo/memory/project.md"},
				"content": "remember",
			},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	updated, ok := result.HookSpecificOutput.UpdatedInput["files"].([]string)
	if !ok || len(updated) != 1 || updated[0] != ".coding-ethos/memories/REPO.md" {
		t.Fatalf("updated input = %#v", result.HookSpecificOutput.UpdatedInput)
	}
}

func TestRunBlocksCodexMemoryWriteWithCentralGuidance(t *testing.T) {
	t.Parallel()

	result, err := Run(policy.ExampleBundle(), Options{
		Event: Event{
			Cwd:           t.TempDir(),
			HookEventName: "PreToolUse",
			ProviderHint:  "codex",
			ToolName:      "Write",
			ToolInput: map[string]any{
				"file_path": ".codex/MEMORY.md",
				"content":   "remember",
			},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if result.Status != "blocked" {
		t.Fatalf("result = %#v", result)
	}
	if len(result.Decisions) != 1 || result.Decisions[0].PolicyID != "memory.centralized" {
		t.Fatalf("decisions = %#v", result.Decisions)
	}
	if !strings.Contains(result.Decisions[0].Message, memories.DeniedReason()) {
		t.Fatalf("missing guidance in %#v", result.Decisions[0])
	}
	if strings.Contains(result.Decisions[0].Message, "\n") {
		t.Fatalf("memory denial reason must stay single-line: %#v", result.Decisions[0])
	}
}

func TestRunAllowsCodexCentralMemoryFallbackWrite(t *testing.T) {
	t.Parallel()

	result, err := Run(policy.ExampleBundle(), Options{
		Event: Event{
			Cwd:           t.TempDir(),
			HookEventName: "PreToolUse",
			ProviderHint:  "codex",
			ToolName:      "Write",
			ToolInput: map[string]any{
				"file_path": memories.PrimaryFile,
				"content":   "remember",
			},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if result.Status != "allowed" {
		t.Fatalf("result = %#v", result)
	}
	if len(result.Decisions) != 0 {
		t.Fatalf("central fallback write should not be reblocked: %#v", result.Decisions)
	}
}
