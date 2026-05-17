// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package evaluators_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/realgit"
)

const (
	commitHeadBlockDecision = "block"
	postToolUseScope        = "PostToolUse"
	recordDecisionValue     = "record"
)

func TestEvaluateGitCommitHeadAdvancedBlocksUnchangedHead(t *testing.T) {
	t.Parallel()

	repo, decisions := evaluateRecordedCommitHead(t, 0)

	if len(decisions) != 1 {
		t.Fatalf("decision count mismatch: %#v", decisions)
	}

	if decisions[0].Decision != commitHeadBlockDecision ||
		decisions[0].Severity != commitHeadBlockDecision {
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

	context.Scope = postToolUseScope
	context.HasToolResponse = true
	context.HasReturnCode = true
	context.ReturnCode = 0

	decisions, err := EvaluateGitCommitHeadAdvanced(policyDef, context)
	if err != nil {
		t.Fatalf("verify head: %v", err)
	}

	if len(decisions) != 1 {
		t.Fatalf("decision count mismatch: %#v", decisions)
	}

	if decisions[0].Decision != recordDecisionValue {
		t.Fatalf("decision mismatch: %#v", decisions[0])
	}

	ok, err := ReadCommitHeadState(repo)
	if err != nil || ok {
		t.Fatalf("expected consumed commit-head state, ok=%v err=%v", ok, err)
	}
}

func TestEvaluateGitCommitHeadAdvancedDoesNotBlockFailedCommit(t *testing.T) {
	t.Parallel()

	repo, decisions := evaluateRecordedCommitHead(t, 1)

	if len(decisions) != 1 {
		t.Fatalf("decision count mismatch: %#v", decisions)
	}

	if decisions[0].Decision != recordDecisionValue ||
		decisions[0].Severity != recordDecisionValue {
		t.Fatalf("decision mismatch: %#v", decisions[0])
	}

	ok, err := ReadCommitHeadState(repo)
	if err != nil || ok {
		t.Fatalf("expected consumed commit-head state, ok=%v err=%v", ok, err)
	}
}

func evaluateRecordedCommitHead(
	t *testing.T,
	returnCode int,
) (string, []policy.Decision) {
	t.Helper()

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

	context.Scope = postToolUseScope
	context.HasToolResponse = true
	context.HasReturnCode = true
	context.ReturnCode = returnCode

	decisions, err := EvaluateGitCommitHeadAdvanced(policyDef, context)
	if err != nil {
		t.Fatalf("verify head: %v", err)
	}

	return repo, decisions
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

	context.Scope = postToolUseScope

	decisions, err := EvaluateGitCommitHeadAdvanced(policyDef, context)
	if err != nil {
		t.Fatalf("verify missing tool response: %v", err)
	}

	if len(decisions) != 1 {
		t.Fatalf("decision count mismatch: %#v", decisions)
	}

	if decisions[0].Decision != recordDecisionValue ||
		decisions[0].Severity != recordDecisionValue {
		t.Fatalf("decision mismatch: %#v", decisions[0])
	}

	ok, err := ReadCommitHeadState(repo)
	if err != nil || ok {
		t.Fatalf("expected consumed commit-head state, ok=%v err=%v", ok, err)
	}
}

func TestEvaluateGitCommitHeadAdvancedDoesNotBlockMissingReturnCode(t *testing.T) {
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

	context.Scope = postToolUseScope
	context.HasToolResponse = true

	decisions, err := EvaluateGitCommitHeadAdvanced(policyDef, context)
	if err != nil {
		t.Fatalf("verify missing return code: %v", err)
	}

	if len(decisions) != 1 {
		t.Fatalf("decision count mismatch: %#v", decisions)
	}

	if decisions[0].Decision != recordDecisionValue ||
		decisions[0].Severity != recordDecisionValue {
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

	gitPath, err := realgit.Resolve(context.Background(), "git")
	if err != nil {
		t.Fatalf("resolve git: %v", err)
	}

	cmd := exec.CommandContext(context.Background(), gitPath, args...)
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
