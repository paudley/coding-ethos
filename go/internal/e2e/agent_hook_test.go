// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package e2e_test

import (
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
	t.Parallel()

	repo := preparedAgentHookRepo(t)

	for _, testCase := range agentHookFixtureCases() {
		payload := loadAgentHookPayload(t, repo, testCase.fixture)
		result := repo.CodingEthosRunWithInput(t, payload, "agent-hook")

		result.RequireExit(t, testCase.wantExit)
		requireStdout(t, result, testCase)
		requireStderr(t, result, testCase)
	}
}

func agentHookFixtureCases() []agentHookFixtureCase {
	return []agentHookFixtureCase{
		{
			name:    "claude pretool git rewrite",
			fixture: "claude-pretool-git-status.json",
			wantStdout: []string{
				`"hookSpecificOutput"`,
				`"permissionDecision": "allow"`,
				`"updatedInput"`,
				`policy-git`,
			},
		},
		{
			name:    "gemini pretool git rewrite",
			fixture: "gemini-pretool-git-status.json",
			wantStdout: []string{
				`"decision": "allow"`,
				`"updatedInput"`,
				`policy-git`,
			},
		},
		{
			name:            "codex pretool git rewrite suppression",
			fixture:         "codex-pretool-git-status.json",
			wantStdoutExact: "{}",
			wantStdoutMissing: []string{
				`"updatedInput"`,
				`policy-git`,
			},
		},
		{
			name:     "codex pretool git bypass denial",
			fixture:  "codex-pretool-git-bypass.json",
			wantExit: 2,
			wantStdout: []string{
				`"decision": "block"`,
				`"permissionDecision": "deny"`,
				`"trackingID": "hook-`,
				`git.hook_bypass`,
			},
			wantStderr: []string{
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
			name:            "codex posttool write file quiet path",
			fixture:         "codex-posttool-write-file.json",
			wantStdoutExact: "{}",
			wantStdoutMissing: []string{
				`"additionalContext"`,
				`python: run ruff/mypy/pyright`,
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
	runtimeRoot := e2e.InstrumentedEthosRoot(t, sourceRoot)
	repo := e2e.FromReference(t, sourceRoot, "policy-lint-basic")
	repo.EthosRoot = runtimeRoot
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
