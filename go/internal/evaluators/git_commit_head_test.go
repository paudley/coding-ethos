// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators_test

import (
	. "blackcat.ca/coding-ethos/go/internal/evaluators"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestEvaluateGitCommitHeadAdvancedRecordsUnchangedHead(t *testing.T) {
	t.Parallel()

	repo := initCommitHeadRepo(t)
	policyDef := policy.ExampleBundle().Policies["git.commit_head_advanced"]

	context := Context{
		Scope:   "PreToolUse",
		Argv:    []string{"git", "commit", "-m", "test"},
		Command: "git commit -m test",
		Cwd:     repo,
	}

	_, err := EvaluateGitCommitHeadAdvanced(policyDef, context)
	if err != nil {
		t.Fatalf("record head: %v", err)
	}

	context.Scope = "PostToolUse"
	context.HasToolResponse = true
	context.ReturnCode = 0

	decisions, err := EvaluateGitCommitHeadAdvanced(policyDef, context)
	if err != nil {
		t.Fatalf("verify head: %v", err)
	}

	if len(decisions) != 1 {
		t.Fatalf("decision count mismatch: %#v", decisions)
	}

	if decisions[0].Decision != "record" || decisions[0].Severity != "record" {
		t.Fatalf("decision mismatch: %#v", decisions[0])
	}

	ok, err := ReadCommitHeadState(repo)
	if err != nil || ok {
		t.Fatalf("expected consumed commit-head state, ok=%v err=%v", ok, err)
	}
}

func TestEvaluateGitCommitHeadAdvancedRecordsAdvancedHead(t *testing.T) {
	t.Parallel()

	repo := initCommitHeadRepo(t)
	policyDef := policy.ExampleBundle().Policies["git.commit_head_advanced"]

	context := Context{
		Scope:   "PreToolUse",
		Argv:    []string{"git", "commit", "-m", "test"},
		Command: "git commit -m test",
		Cwd:     repo,
	}

	_, err := EvaluateGitCommitHeadAdvanced(policyDef, context)
	if err != nil {
		t.Fatalf("record head: %v", err)
	}

	err = os.WriteFile(filepath.Join(repo, "file.txt"), []byte("changed\n"), 0o600)
	if err != nil {
		t.Fatalf("write change: %v", err)
	}

	runGit(t, repo, "add", "file.txt")
	runGit(t, repo, "commit", "-m", "second")

	context.Scope = "PostToolUse"
	context.HasToolResponse = true
	context.ReturnCode = 0

	decisions, err := EvaluateGitCommitHeadAdvanced(policyDef, context)
	if err != nil {
		t.Fatalf("verify head: %v", err)
	}

	if len(decisions) != 1 {
		t.Fatalf("decision count mismatch: %#v", decisions)
	}

	if decisions[0].Decision != "record" {
		t.Fatalf("decision mismatch: %#v", decisions[0])
	}

	ok, err := ReadCommitHeadState(repo)
	if err != nil || ok {
		t.Fatalf("expected consumed commit-head state, ok=%v err=%v", ok, err)
	}
}

func TestEvaluateGitCommitHeadAdvancedDoesNotBlockFailedCommit(t *testing.T) {
	t.Parallel()

	repo := initCommitHeadRepo(t)
	policyDef := policy.ExampleBundle().Policies["git.commit_head_advanced"]

	context := Context{
		Scope:   "PreToolUse",
		Argv:    []string{"git", "commit", "-m", "test"},
		Command: "git commit -m test",
		Cwd:     repo,
	}

	_, err := EvaluateGitCommitHeadAdvanced(policyDef, context)
	if err != nil {
		t.Fatalf("record head: %v", err)
	}

	context.Scope = "PostToolUse"
	context.HasToolResponse = true
	context.ReturnCode = 1

	decisions, err := EvaluateGitCommitHeadAdvanced(policyDef, context)
	if err != nil {
		t.Fatalf("verify failed commit: %v", err)
	}

	if len(decisions) != 1 {
		t.Fatalf("decision count mismatch: %#v", decisions)
	}

	if decisions[0].Decision != "record" || decisions[0].Severity != "record" {
		t.Fatalf("decision mismatch: %#v", decisions[0])
	}

	ok, err := ReadCommitHeadState(repo)
	if err != nil || ok {
		t.Fatalf("expected consumed commit-head state, ok=%v err=%v", ok, err)
	}
}

func TestEvaluateGitCommitHeadAdvancedDoesNotBlockMissingToolResponse(t *testing.T) {
	t.Parallel()

	repo := initCommitHeadRepo(t)
	policyDef := policy.ExampleBundle().Policies["git.commit_head_advanced"]

	context := Context{
		Scope:   "PreToolUse",
		Argv:    []string{"git", "commit", "-m", "test"},
		Command: "git commit -m test",
		Cwd:     repo,
	}

	_, err := EvaluateGitCommitHeadAdvanced(policyDef, context)
	if err != nil {
		t.Fatalf("record head: %v", err)
	}

	context.Scope = "PostToolUse"

	decisions, err := EvaluateGitCommitHeadAdvanced(policyDef, context)
	if err != nil {
		t.Fatalf("verify missing tool response: %v", err)
	}

	if len(decisions) != 1 {
		t.Fatalf("decision count mismatch: %#v", decisions)
	}

	if decisions[0].Decision != "record" || decisions[0].Severity != "record" {
		t.Fatalf("decision mismatch: %#v", decisions[0])
	}

	ok, err := ReadCommitHeadState(repo)
	if err != nil || ok {
		t.Fatalf("expected consumed commit-head state, ok=%v err=%v", ok, err)
	}
}

func initCommitHeadRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")

	err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("initial\n"), 0o600)
	if err != nil {
		t.Fatalf("write file: %v", err)
	}

	runGit(t, repo, "add", "file.txt")
	runGit(t, repo, "commit", "-m", "initial")

	return repo
}

func runGit(t *testing.T, repo string, args ...string) {
	t.Helper()

	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = repo
	cmd.Env = cleanGitTestEnv()

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func cleanGitTestEnv() []string {
	env := make([]string, 0, len(os.Environ()))
	for _, item := range os.Environ() {
		name, _, found := strings.Cut(item, "=")
		if found && gitLocalEnvName(name) {
			continue
		}

		env = append(env, item)
	}

	return append(
		env,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"XDG_CONFIG_HOME="+os.DevNull,
	)
}

func gitLocalEnvName(name string) bool {
	switch name {
	case "GIT_ALTERNATE_OBJECT_DIRECTORIES",
		"GIT_COMMON_DIR",
		"GIT_CONFIG_COUNT",
		"GIT_CONFIG_GLOBAL",
		"GIT_CONFIG_NOSYSTEM",
		"GIT_CONFIG_PARAMETERS",
		"GIT_CONFIG_SYSTEM",
		"GIT_DIR",
		"GIT_INDEX_FILE",
		"GIT_NAMESPACE",
		"GIT_OBJECT_DIRECTORY",
		"GIT_PREFIX",
		"GIT_QUARANTINE_PATH",
		"GIT_WORK_TREE",
		"XDG_CONFIG_HOME":
		return true
	default:
		return strings.HasPrefix(name, "GIT_CONFIG_KEY_") ||
			strings.HasPrefix(name, "GIT_CONFIG_VALUE_")
	}
}
