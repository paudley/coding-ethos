// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestRunBlocksGitHookBypass(t *testing.T) {
	result, err := Run(policy.ExampleBundle(), Options{
		Event: Event{
			HookEventName: "PreToolUse",
			ToolName:      "Bash",
			ToolInput: map[string]any{
				"command": "git commit --no-verify -m test",
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}
	if result.Status != "blocked" {
		t.Fatalf("status mismatch: got %q", result.Status)
	}
	if len(result.Decisions) != 1 {
		t.Fatalf("decision count mismatch: %#v", result.Decisions)
	}
	if result.Decisions[0].PolicyID != "git.hook_bypass" {
		t.Fatalf("policy mismatch: %#v", result.Decisions[0])
	}
}

func TestRunAllowsNormalGitCommit(t *testing.T) {
	repo := initHookRepo(t)
	result, err := Run(policy.ExampleBundle(), Options{
		Event: Event{
			HookEventName: "PreToolUse",
			ToolName:      "Bash",
			Cwd:           repo,
			ToolInput: map[string]any{
				"command": "git commit -m test",
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}
	if result.Status != "allowed" {
		t.Fatalf("status mismatch: got %q", result.Status)
	}
	if len(result.Decisions) != 1 {
		t.Fatalf("decision count mismatch: %#v", result.Decisions)
	}
	if result.Decisions[0].PolicyID != "git.commit_head_advanced" || result.Decisions[0].Decision != "record" {
		t.Fatalf("decision mismatch: %#v", result.Decisions[0])
	}
}

func TestRunAllowsNormalNonCommitGitCommand(t *testing.T) {
	result, err := Run(policy.ExampleBundle(), Options{
		Event: Event{
			HookEventName: "PreToolUse",
			ToolName:      "Bash",
			ToolInput: map[string]any{
				"command": "git status",
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}
	if result.Status != "allowed" {
		t.Fatalf("status mismatch: got %q", result.Status)
	}
	if len(result.Decisions) != 0 {
		t.Fatalf("expected no decisions, got %#v", result.Decisions)
	}
}

func TestDecodeEventReadsClaudeLikePayload(t *testing.T) {
	event, err := DecodeEvent(strings.NewReader(`{
		"hook_event_name": "PreToolUse",
		"tool_name": "Bash",
		"tool_input": {"command": "git status"}
	}`))
	if err != nil {
		t.Fatalf("decode event: %v", err)
	}
	if event.HookEventName != "PreToolUse" || event.ToolName != "Bash" {
		t.Fatalf("event mismatch: %#v", event)
	}
	if event.Command() != "git status" {
		t.Fatalf("command mismatch: %q", event.Command())
	}
}

func initHookRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runHookGit(t, repo, "init")
	runHookGit(t, repo, "config", "user.email", "test@example.com")
	runHookGit(t, repo, "config", "user.name", "Test User")
	runHookGit(t, repo, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("initial\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runHookGit(t, repo, "add", "file.txt")
	runHookGit(t, repo, "commit", "-m", "initial")
	return repo
}

func runHookGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}
