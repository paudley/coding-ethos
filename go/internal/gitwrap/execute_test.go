// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package gitwrap_test

import (
	. "blackcat.ca/coding-ethos/go/internal/gitwrap"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

const statusBlocked = "blocked"

func TestVerifyPostBlocksFalseSuccessfulCommit(t *testing.T) {
	t.Parallel()

	repo := initGitwrapRepo(t)

	options := Options{Argv: []string{"commit", "-m", "noop"}, Cwd: repo}

	err := PreparePost(policy.ExampleBundle(), options)
	if err != nil {
		t.Fatalf("prepare post: %v", err)
	}

	result, err := VerifyPost(policy.ExampleBundle(), options)
	if err != nil {
		t.Fatalf("verify post: %v", err)
	}

	if result.Status != statusBlocked {
		t.Fatalf("status mismatch: got %q", result.Status)
	}
}

func initGitwrapRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGitwrapGit(t, repo, "init")
	runGitwrapGit(t, repo, "config", "user.email", "test@example.com")
	runGitwrapGit(t, repo, "config", "user.name", "Test User")
	runGitwrapGit(t, repo, "config", "commit.gpgsign", "false")

	err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("initial\n"), 0o600)
	if err != nil {
		t.Fatalf("write file: %v", err)
	}

	runGitwrapGit(t, repo, "add", "file.txt")
	runGitwrapGit(t, repo, "commit", "-m", "initial")

	return repo
}

func runGitwrapGit(t *testing.T, repo string, args ...string) {
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

	return env
}

func gitLocalEnvName(name string) bool {
	switch name {
	case "GIT_ALTERNATE_OBJECT_DIRECTORIES",
		"GIT_COMMON_DIR",
		"GIT_CONFIG_COUNT",
		"GIT_CONFIG_PARAMETERS",
		"GIT_DIR",
		"GIT_INDEX_FILE",
		"GIT_NAMESPACE",
		"GIT_OBJECT_DIRECTORY",
		"GIT_PREFIX",
		"GIT_QUARANTINE_PATH",
		"GIT_WORK_TREE":
		return true
	default:
		return strings.HasPrefix(name, "GIT_CONFIG_KEY_") ||
			strings.HasPrefix(name, "GIT_CONFIG_VALUE_")
	}
}
