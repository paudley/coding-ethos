// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package gitwrap_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/gitwrap"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/realgit"
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

func TestExecuteDoesNotLeakRunnerStackToRealGit(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "git-env.log")
	fakeGit := fakeEnvGit(t, logPath)

	t.Setenv("CODING_ETHOS_EXEC_STACK", "coding-ethos-run")

	err := Execute(fakeGit, Options{Argv: []string{"status"}, AdminApproved: true})
	if err != nil {
		t.Fatalf("execute fake git: %v", err)
	}

	log := readText(t, logPath)
	if strings.Contains(log, "CODING_ETHOS_EXEC_STACK=coding-ethos-run") {
		t.Fatalf("runner stack leaked into real git env:\n%s", log)
	}

	if !strings.Contains(log, "CODE_ETHOS_ADMIN_APPROVED=1") {
		t.Fatalf("admin approval env missing from real git env:\n%s", log)
	}
}

func initGitwrapRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGitwrapGit(t, repo, "init")
	runGitwrapGit(t, repo, "config", "user.email", "test@example.com")
	runGitwrapGit(t, repo, "config", "user.name", "Test User")

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

func fakeEnvGit(t *testing.T, logPath string) string {
	t.Helper()

	scriptPath := filepath.Join(t.TempDir(), "git")
	script := `#!/usr/bin/env bash
set -euo pipefail
log_path=` + strconv.Quote(logPath) + `
printf 'CODING_ETHOS_EXEC_STACK=%s\n' "${CODING_ETHOS_EXEC_STACK:-}" >> "$log_path"
printf 'CODE_ETHOS_ADMIN_APPROVED=%s\n' "${CODE_ETHOS_ADMIN_APPROVED:-}" >> "$log_path"
`

	err := os.WriteFile(scriptPath, []byte(script), 0o600)
	if err != nil {
		t.Fatalf("write fake git: %v", err)
	}

	err = os.Chmod(scriptPath, 0o700)
	if err != nil {
		t.Fatalf("chmod fake git: %v", err)
	}

	return scriptPath
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
