// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks

import (
	"os"
	"path/filepath"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestRunBlocksWriteToExistingFile(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	writeProxySearchReplaceFixture(t, repo, "pkg/app.py", "value = 1\n")

	result, err := Run(policy.ExampleBundle(), Options{
		Event: Event{
			HookEventName: eventPreToolUse,
			ToolName:      "Write",
			Cwd:           repo,
			ToolInput: map[string]any{
				"file_path": "pkg/app.py",
				"content":   "value = 2\n",
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}

	assertProxySearchReplaceBlock(t, result, "write_existing_file")
}

func TestRunBlocksWriteToExistingSymlink(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	target := filepath.Join(t.TempDir(), "target.py")
	err := os.WriteFile(target, []byte("value = 1\n"), 0o600)
	if err != nil {
		t.Fatalf("write symlink target: %v", err)
	}
	link := filepath.Join(repo, "pkg", "link.py")
	err = os.MkdirAll(filepath.Dir(link), 0o700)
	if err != nil {
		t.Fatalf("create symlink dir: %v", err)
	}
	err = os.Symlink(target, link)
	if err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	result, err := Run(policy.ExampleBundle(), Options{
		Event: Event{
			HookEventName: eventPreToolUse,
			ToolName:      "Write",
			Cwd:           repo,
			ToolInput: map[string]any{
				"file_path": "pkg/link.py",
				"content":   "value = 2\n",
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}

	assertProxySearchReplaceBlock(t, result, "write_existing_file")
}

func TestRunAllowsWriteToNewFile(t *testing.T) {
	t.Parallel()

	result, err := Run(policy.ExampleBundle(), Options{
		Event: Event{
			HookEventName: eventPreToolUse,
			ToolName:      "Write",
			Cwd:           t.TempDir(),
			ToolInput: map[string]any{
				"file_path": "pkg/new.py",
				"content":   "value = 2\n",
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}

	if result.Blocked() {
		t.Fatalf("new-file Write should be allowed, got %#v", result.Decisions)
	}
}

func TestRunAllowsExactEdit(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	writeProxySearchReplaceFixture(t, repo, "pkg/app.py", "value = 1\n")

	result, err := Run(policy.ExampleBundle(), Options{
		Event: Event{
			HookEventName: eventPreToolUse,
			ToolName:      "Edit",
			Cwd:           repo,
			ToolInput: map[string]any{
				"file_path":  "pkg/app.py",
				"old_string": "value = 1\n",
				"new_string": "value = 2\n",
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}

	if result.Blocked() {
		t.Fatalf("exact Edit should be allowed, got %#v", result.Decisions)
	}
}

func TestRunBlocksEditWithoutSearchReplaceBlock(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	writeProxySearchReplaceFixture(t, repo, "pkg/app.py", "value = 1\n")

	result, err := Run(policy.ExampleBundle(), Options{
		Event: Event{
			HookEventName: eventPreToolUse,
			ToolName:      "Edit",
			Cwd:           repo,
			ToolInput: map[string]any{
				"file_path": "pkg/app.py",
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}

	assertProxySearchReplaceBlock(t, result, "missing")
}

func TestRunBlocksEditWithAmbiguousTargetFiles(t *testing.T) {
	t.Parallel()

	result, err := Run(policy.ExampleBundle(), Options{
		Event: Event{
			HookEventName: eventPreToolUse,
			ToolName:      "Edit",
			Cwd:           t.TempDir(),
			ToolInput: map[string]any{
				"file_path":  "pkg/app.py",
				"old_string": "value = 1\n",
				"new_string": "value = 2\n",
				"paths":      []any{"pkg/other.py"},
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}

	assertProxySearchReplaceBlock(t, result, "invalid_edit_target")
}

func TestRunBlocksEditToSymlinkTarget(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	target := filepath.Join(t.TempDir(), "target.py")
	err := os.WriteFile(target, []byte("value = 1\n"), 0o600)
	if err != nil {
		t.Fatalf("write symlink target: %v", err)
	}
	link := filepath.Join(repo, "pkg", "link.py")
	err = os.MkdirAll(filepath.Dir(link), 0o700)
	if err != nil {
		t.Fatalf("create symlink dir: %v", err)
	}
	err = os.Symlink(target, link)
	if err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	result, err := Run(policy.ExampleBundle(), Options{
		Event: Event{
			HookEventName: eventPreToolUse,
			ToolName:      "Edit",
			Cwd:           repo,
			ToolInput: map[string]any{
				"file_path":  "pkg/link.py",
				"old_string": "value = 1\n",
				"new_string": "value = 2\n",
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}

	assertProxySearchReplaceBlock(t, result, "invalid_edit_target")
}

func TestRunUsesBundleProxySearchReplacePolicy(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	writeProxySearchReplaceFixture(t, repo, "pkg/app.py", "value = 1\n")

	bundle := policy.ExampleBundle()
	policyDef := bundle.Policies[policy.ProxySearchReplaceEditPolicyID]
	policyDef.Message = "custom bundle policy message"
	bundle.Policies[policy.ProxySearchReplaceEditPolicyID] = policyDef

	result, err := Run(bundle, Options{
		Event: Event{
			HookEventName: eventPreToolUse,
			ToolName:      "Edit",
			Cwd:           repo,
			ToolInput: map[string]any{
				"file_path":  "pkg/app.py",
				"old_string": "missing = 1\n",
				"new_string": "value = 2\n",
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}
	if !result.Blocked() || len(result.Decisions) != 1 {
		t.Fatalf("result = %#v", result)
	}
	if result.Decisions[0].Message != "custom bundle policy message" {
		t.Fatalf("decision did not use bundle policy: %#v", result.Decisions[0])
	}
}

func TestRunBlocksEditMissingSearch(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	writeProxySearchReplaceFixture(t, repo, "pkg/app.py", "value = 1\n")

	result, err := Run(policy.ExampleBundle(), Options{
		Event: Event{
			HookEventName: eventPreToolUse,
			ToolName:      "Edit",
			Cwd:           repo,
			ToolInput: map[string]any{
				"file_path":  "pkg/app.py",
				"old_string": "missing = 1\n",
				"new_string": "value = 2\n",
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}

	assertProxySearchReplaceBlock(t, result, "missing")
}

func TestRunBlocksEditAmbiguousSearch(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	writeProxySearchReplaceFixture(t, repo, "pkg/app.py", "value = 1\nvalue = 1\n")

	result, err := Run(policy.ExampleBundle(), Options{
		Event: Event{
			HookEventName: eventPreToolUse,
			ToolName:      "Edit",
			Cwd:           repo,
			ToolInput: map[string]any{
				"file_path":  "pkg/app.py",
				"old_string": "value = 1\n",
				"new_string": "value = 2\n",
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}

	assertProxySearchReplaceBlock(t, result, "ambiguous")
}

func TestRunBlocksEditEmptySearch(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	writeProxySearchReplaceFixture(t, repo, "pkg/app.py", "value = 1\n")

	result, err := Run(policy.ExampleBundle(), Options{
		Event: Event{
			HookEventName: eventPreToolUse,
			ToolName:      "Edit",
			Cwd:           repo,
			ToolInput: map[string]any{
				"file_path":  "pkg/app.py",
				"old_string": "",
				"new_string": "value = 2\n",
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}

	assertProxySearchReplaceBlock(t, result, "empty_search")
}

func TestRunBlocksMultiEditSecondMissingSearch(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	writeProxySearchReplaceFixture(t, repo, "pkg/app.py", "alpha\nbeta\n")

	result, err := Run(policy.ExampleBundle(), Options{
		Event: Event{
			HookEventName: eventPreToolUse,
			ToolName:      "MultiEdit",
			Cwd:           repo,
			ToolInput: map[string]any{
				"file_path": "pkg/app.py",
				"edits": []any{
					map[string]any{"old_string": "alpha\n", "new_string": "gamma\n"},
					map[string]any{"old_string": "missing\n", "new_string": "delta\n"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}

	assertProxySearchReplaceBlock(t, result, "missing")
}

func TestRunBlocksMalformedMultiEditInputs(t *testing.T) {
	t.Parallel()

	cases := map[string]any{
		"missing edits": nil,
		"empty edits":   []any{},
		"invalid edit":  []any{"not an edit object"},
	}

	for name, edits := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			repo := t.TempDir()
			writeProxySearchReplaceFixture(t, repo, "pkg/app.py", "value = 1\n")

			toolInput := map[string]any{
				"file_path": "pkg/app.py",
			}
			if edits != nil {
				toolInput["edits"] = edits
			}

			result, err := Run(policy.ExampleBundle(), Options{
				Event: Event{
					HookEventName: eventPreToolUse,
					ToolName:      "MultiEdit",
					Cwd:           repo,
					ToolInput:     toolInput,
				},
			})
			if err != nil {
				t.Fatalf("run hook: %v", err)
			}

			assertProxySearchReplaceBlock(t, result, "malformed_multiedit")
		})
	}
}

func assertProxySearchReplaceBlock(t *testing.T, result Result, reason string) {
	t.Helper()

	if !result.Blocked() {
		t.Fatalf("result was not blocked: %#v", result)
	}
	if len(result.Decisions) != 1 {
		t.Fatalf("decisions = %#v", result.Decisions)
	}
	decision := result.Decisions[0]
	if decision.PolicyID != policy.ProxySearchReplaceEditPolicyID {
		t.Fatalf("policy = %q", decision.PolicyID)
	}
	if got := decision.Evidence["reason"]; got != reason {
		t.Fatalf("reason = %#v, want %q; decision %#v", got, reason, decision)
	}
}

func writeProxySearchReplaceFixture(t *testing.T, root, file, content string) {
	t.Helper()

	path := filepath.Join(root, file)
	err := os.MkdirAll(filepath.Dir(path), 0o700)
	if err != nil {
		t.Fatalf("create fixture dir: %v", err)
	}
	err = os.WriteFile(path, []byte(content), 0o600)
	if err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}
