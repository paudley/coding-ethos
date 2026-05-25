// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/e2e"
)

type agentHookFixtureCase struct {
	name              string
	fixture           string
	wantStdoutExact   string
	wantStdout        []string
	wantStdoutMissing []string
	wantStderr        []string
	wantExit          int
}

func TestAgentHookProviderPayloadFixtures(t *testing.T) {
	for _, testCase := range agentHookFixtureCases() {
		t.Run(testCase.name, func(t *testing.T) {
			repo := preparedAgentHookRepo(t)
			payload := loadAgentHookPayload(t, repo, testCase.fixture)
			prepareAgentHookFixtureState(t, repo, payload)
			result := repo.CodingEthosRunWithInput(t, payload, "agent-hook")

			result.RequireExit(t, testCase.wantExit)
			requireStdout(t, result, testCase)
			requireStderr(t, result, testCase)
		})
	}
}

func agentHookFixtureCases() []agentHookFixtureCase {
	return []agentHookFixtureCase{
		{
			name:    "claude pretool read-only git routes through runner",
			fixture: "claude-pretool-git-status.json",
			wantStdout: []string{
				`"updatedInput"`,
				`agent-shell --`,
				`git status --short`,
				`Routed shell command through the approved runner path.`,
			},
		},
		{
			name:    "gemini pretool read-only git routes through runner",
			fixture: "gemini-pretool-git-status.json",
			wantStdout: []string{
				`"updatedInput"`,
				`agent-shell --`,
				`git status --short`,
				`Routed shell command through the approved runner path.`,
			},
		},
		{
			name:     "codex pretool read-only git denial",
			fixture:  "codex-pretool-git-status.json",
			wantExit: 1,
			wantStdout: []string{
				`"decision": "block"`,
				`"permissionDecision": "deny"`,
				`git.wrapper_required`,
				`cerun -- 'git status --short'`,
			},
		},
		{
			name:     "codex pretool git bypass denial",
			fixture:  "codex-pretool-git-bypass.json",
			wantExit: 1,
			wantStdout: []string{
				`"decision": "block"`,
				`"permissionDecision": "deny"`,
				`"trackingID": "hook-`,
				`git.hook_bypass`,
			},
		},
		{
			name:    "claude posttool write file context",
			fixture: "claude-posttool-write-file.json",
			wantStdout: []string{
				`"additionalContext"`,
				`tool: Write`,
				`src/app.py`,
				`python: run ruff/mypy/pyright`,
			},
		},
		{
			name:    "codex posttool write file compact context",
			fixture: "codex-posttool-write-file.json",
			wantStdout: []string{
				`"hookSpecificOutput"`,
				`"additionalContext"`,
				`coding-ethos: review the edited file; run focused formatting, ` +
					`lint, type, or tests; fix static-analysis findings structurally.`,
			},
		},
		{
			name:            "codex pretool apply patch quiet path",
			fixture:         "codex-pretool-apply-patch.json",
			wantStdoutExact: "{}",
			wantStdoutMissing: []string{
				`"decision": "block"`,
				`"updatedInput"`,
			},
		},
	}
}

func preparedAgentHookRepo(t *testing.T) e2e.Repo {
	t.Helper()

	if testing.Short() {
		t.Skip("real agent hook e2e is skipped in short mode")
	}

	if runtime.GOOS == windowsGOOS {
		t.Skip("real agent hook e2e uses POSIX paths")
	}

	sourceRoot := repoRootFromWorkingDirectory(t)
	e2e.RequireRuntime(t, sourceRoot)
	runtimeRoot := e2e.MutableBinEthosRoot(t, sourceRoot)
	repo := e2e.FromReference(t, sourceRoot, "policy-lint-basic")
	repo.EthosRoot = runtimeRoot
	repo.SyncHookPolicyBundle(t)
	repo.Git(t, "checkout", "-b", "e2e-agent-hooks")

	return repo
}

func loadAgentHookPayload(t *testing.T, repo e2e.Repo, fixture string) string {
	t.Helper()

	path := filepath.Join("testdata", "agent-hooks", fixture)

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read agent hook fixture %s: %v", path, err)
	}

	return strings.ReplaceAll(string(content), "${REPO_ROOT}", repo.Root)
}

func prepareAgentHookFixtureState(t *testing.T, repo e2e.Repo, payload string) {
	t.Helper()

	var event map[string]any

	err := json.Unmarshal([]byte(payload), &event)
	if err != nil {
		t.Fatalf("decode agent hook fixture state: %v", err)
	}

	if hookEventName(event) != "PostToolUse" || toolName(event) != "write_file" {
		return
	}

	input, ok := eventInput(event)
	if !ok {
		return
	}

	filePath, fileOK := input["file_path"].(string)
	content, contentOK := input["content"].(string)

	if !fileOK || !contentOK {
		return
	}

	repo.Touch(t, filePath, content)
}

func hookEventName(event map[string]any) string {
	for _, key := range []string{"hook_event_name", "hookEventName", "event"} {
		if value, ok := event[key].(string); ok {
			return value
		}
	}

	return ""
}

func toolName(event map[string]any) string {
	for _, key := range []string{"tool_name", "toolName", "tool"} {
		if value, ok := event[key].(string); ok {
			return value
		}
	}

	return ""
}

func eventInput(event map[string]any) (map[string]any, bool) {
	for _, key := range []string{"tool_input", "toolInput", "input"} {
		value, ok := event[key].(map[string]any)
		if ok {
			return value, true
		}
	}

	return nil, false
}

func requireStdout(
	t *testing.T,
	result e2e.CommandResult,
	testCase agentHookFixtureCase,
) {
	t.Helper()

	if testCase.wantStdoutExact != "" &&
		strings.TrimSpace(result.Stdout) != testCase.wantStdoutExact {
		t.Fatalf(
			"stdout = %q, want exactly %q\nstderr:\n%s",
			result.Stdout,
			testCase.wantStdoutExact,
			result.Stderr,
		)
	}

	for _, want := range testCase.wantStdout {
		if !strings.Contains(result.Stdout, want) {
			t.Fatalf(
				"stdout missing %q\nstdout:\n%s\nstderr:\n%s",
				want,
				result.Stdout,
				result.Stderr,
			)
		}
	}

	for _, unwanted := range testCase.wantStdoutMissing {
		if strings.Contains(result.Stdout, unwanted) {
			t.Fatalf(
				"stdout contains %q\nstdout:\n%s\nstderr:\n%s",
				unwanted,
				result.Stdout,
				result.Stderr,
			)
		}
	}
}

func requireStderr(
	t *testing.T,
	result e2e.CommandResult,
	testCase agentHookFixtureCase,
) {
	t.Helper()

	for _, want := range testCase.wantStderr {
		if !strings.Contains(result.Stderr, want) {
			t.Fatalf(
				"stderr missing %q\nstdout:\n%s\nstderr:\n%s",
				want,
				result.Stdout,
				result.Stderr,
			)
		}
	}
}
