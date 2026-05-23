// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

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
	t.Setenv(WrapperAuthorizedEnv, "spoofed")
	t.Setenv(WrapperPIDEnv, "999999")

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

	if !strings.Contains(log, "CODE_ETHOS_GIT_WRAPPER_AUTHORIZED=1") {
		t.Fatalf("wrapper authorization env missing from real git env:\n%s", log)
	}

	wantPID := "CODE_ETHOS_GIT_WRAPPER_PID=" + strconv.Itoa(os.Getpid())
	if !strings.Contains(log, wantPID) {
		t.Fatalf("wrapper pid env = log %q, want %q", log, wantPID)
	}
}

func TestExecuteForcesSignedCommit(t *testing.T) {
	t.Parallel()

	logPath := filepath.Join(t.TempDir(), "git-args.log")
	fakeGit := fakeArgsGit(t, logPath)

	err := Execute(fakeGit, Options{Argv: []string{"commit", "-m", "test"}})
	if err != nil {
		t.Fatalf("execute fake git: %v", err)
	}

	args := readText(t, logPath)
	if !strings.Contains(args, "commit\n-S\n-m\ntest\n") {
		t.Fatalf("commit signing args missing:\n%s", args)
	}
}

func TestExecuteLeavesPushCertificateOptional(t *testing.T) {
	t.Parallel()

	logPath := filepath.Join(t.TempDir(), "git-args.log")
	fakeGit := fakeArgsGit(t, logPath)

	err := Execute(fakeGit, Options{Argv: []string{"push", "origin", "main"}})
	if err != nil {
		t.Fatalf("execute fake git: %v", err)
	}

	args := readText(t, logPath)
	if strings.Contains(args, "--signed\n") {
		t.Fatalf("push certificate arg should not be forced:\n%s", args)
	}
}

func TestExecuteCreatesSignedCommitWithDisposableKey(t *testing.T) {
	if testing.Short() {
		t.Skip("GPG agent integration is excluded from short managed hook runs")
	}

	gitPath, err := realgit.Resolve(context.Background(), "git")
	if err != nil {
		t.Fatalf("resolve git: %v", err)
	}

	gpgPath, err := exec.LookPath("gpg")
	if err != nil {
		t.Fatalf("resolve gpg: %v", err)
	}

	gnupgHome := filepath.Join(t.TempDir(), "gnupg")
	err = os.Mkdir(gnupgHome, 0o700)
	if err != nil {
		t.Fatalf("create gnupg home: %v", err)
	}
	t.Setenv("GNUPGHOME", gnupgHome)
	fingerprint := generateGitwrapTestSigningKey(t, gpgPath, gnupgHome)

	repo := t.TempDir()
	runGitwrapGit(t, repo, "init")
	runGitwrapGit(t, repo, "config", "user.email", "test@example.com")
	runGitwrapGit(t, repo, "config", "user.name", "Test User")
	runGitwrapGit(t, repo, "config", "user.signingkey", fingerprint)
	runGitwrapGit(t, repo, "config", "gpg.program", gpgPath)
	runGitwrapGit(t, repo, "config", "commit.gpgsign", "true")

	err = os.WriteFile(filepath.Join(repo, "file.txt"), []byte("signed\n"), 0o600)
	if err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGitwrapGit(t, repo, "add", "file.txt")

	options := Options{
		Argv: []string{"commit", "-m", "signed test"},
		Cwd:  repo,
	}
	err = Execute(gitPath, options)
	if err != nil {
		t.Fatalf("execute signed commit: %v", err)
	}

	result, err := VerifyPost(policy.ExampleBundle(), options)
	if err != nil {
		t.Fatalf("verify signed commit: %v", err)
	}
	if result.Status != "allowed" {
		t.Fatalf("post status = %q decisions %#v", result.Status, result.Decisions)
	}

	status := strings.TrimSpace(gitwrapGitOutput(t, repo, "log", "-1", "--pretty=%G?"))
	if status != "G" {
		t.Fatalf("signature status = %q, want G", status)
	}
}

func initGitwrapRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGitwrapGit(t, repo, "init")
	runGitwrapGit(t, repo, "config", "user.email", "test@example.com")
	runGitwrapGit(t, repo, "config", "user.name", "Test User")

	hooksPath := filepath.Join(repo, "no-hooks")
	err := os.Mkdir(hooksPath, 0o700)
	if err != nil {
		t.Fatalf("create empty hooks path: %v", err)
	}
	runGitwrapGit(t, repo, "config", "core.hooksPath", hooksPath)

	err = os.WriteFile(
		filepath.Join(repo, ".gitignore"),
		[]byte(".code-ethos/cache/\n.coding-ethos/cache/\n"+
			".coding-ethos/code-intel.db\n.coding-ethos/hook-runs/\n"+
			".coding-ethos/lint-runs/\n.coding-ethos/prune-runs/\n"+
			".coding-ethos/state/\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}

	err = os.WriteFile(filepath.Join(repo, "file.txt"), []byte("initial\n"), 0o600)
	if err != nil {
		t.Fatalf("write file: %v", err)
	}

	runGitwrapGit(t, repo, "add", ".gitignore", "file.txt")
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

func gitwrapGitOutput(t *testing.T, repo string, args ...string) string {
	t.Helper()

	gitPath, err := realgit.Resolve(context.Background(), "git")
	if err != nil {
		t.Fatalf("resolve git: %v", err)
	}

	cmd := exec.CommandContext(context.Background(), gitPath, args...)
	cmd.Dir = repo
	cmd.Env = cleanGitTestEnv()

	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}

	return string(output)
}

func generateGitwrapTestSigningKey(t *testing.T, gpgPath, gnupgHome string) string {
	t.Helper()

	keyParams := filepath.Join(t.TempDir(), "key.params")
	err := os.WriteFile(keyParams, []byte(`%no-protection
Key-Type: eddsa
Key-Curve: ed25519
Key-Usage: sign
Name-Real: Coding Ethos Test
Name-Email: coding-ethos-test@example.invalid
Expire-Date: 1d
%commit
`), 0o600)
	if err != nil {
		t.Fatalf("write key params: %v", err)
	}

	cmd := exec.CommandContext(
		context.Background(),
		gpgPath,
		"--batch",
		"--homedir",
		gnupgHome,
		"--generate-key",
		keyParams,
	)
	if output, inlineErr := cmd.CombinedOutput(); inlineErr != nil {
		t.Fatalf("generate gpg key: %v\n%s", inlineErr, output)
	}

	cmd = exec.CommandContext(
		context.Background(),
		gpgPath,
		"--batch",
		"--homedir",
		gnupgHome,
		"--list-secret-keys",
		"--with-colons",
	)
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("list gpg key: %v", err)
	}

	for line := range strings.SplitSeq(string(output), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) > 9 && fields[0] == "fpr" && fields[9] != "" {
			return fields[9]
		}
	}

	t.Fatalf("no generated key fingerprint found:\n%s", output)
	return ""
}

func fakeEnvGit(t *testing.T, logPath string) string {
	t.Helper()

	scriptPath := filepath.Join(t.TempDir(), "git")
	script := `#!/usr/bin/env bash
set -euo pipefail
log_path=` + strconv.Quote(logPath) + `
printf 'CODING_ETHOS_EXEC_STACK=%s\n' "${CODING_ETHOS_EXEC_STACK:-}" >> "$log_path"
printf 'CODE_ETHOS_ADMIN_APPROVED=%s\n' "${CODE_ETHOS_ADMIN_APPROVED:-}" >> "$log_path"
printf 'CODE_ETHOS_GIT_WRAPPER_AUTHORIZED=%s\n' "${CODE_ETHOS_GIT_WRAPPER_AUTHORIZED:-}" >> "$log_path"
printf 'CODE_ETHOS_GIT_WRAPPER_PID=%s\n' "${CODE_ETHOS_GIT_WRAPPER_PID:-}" >> "$log_path"
`

	writeExecutableGitFixture(t, scriptPath, script)

	return scriptPath
}

func fakeArgsGit(t *testing.T, logPath string) string {
	t.Helper()

	scriptPath := filepath.Join(t.TempDir(), "git")
	script := `#!/usr/bin/env bash
set -euo pipefail
log_path=` + strconv.Quote(logPath) + `
printf '%s\n' "$@" > "$log_path"
`

	writeExecutableGitFixture(t, scriptPath, script)

	return scriptPath
}

func writeExecutableGitFixture(t *testing.T, scriptPath, script string) {
	t.Helper()

	file, err := os.OpenFile(scriptPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o700)
	if err != nil {
		t.Fatalf("create fake git: %v", err)
	}

	if _, err = file.WriteString(script); err != nil {
		_ = file.Close()
		t.Fatalf("write fake git: %v", err)
	}
	if err = file.Close(); err != nil {
		t.Fatalf("close fake git: %v", err)
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
