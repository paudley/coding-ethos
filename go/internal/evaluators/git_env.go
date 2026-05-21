// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package evaluators

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/realgit"
)

const (
	// RealGitEnv names the validated environment setting for the real git binary.
	RealGitEnv = "CODING_ETHOS_REAL_GIT"

	codingEthosRuntimeProbeBytes = 8192
)

func gitCommand(cwd string, args ...string) *exec.Cmd {
	return GitCommand(cwd, args...)
}

// GitCommand builds a git command with hook-local git environment removed.
func GitCommand(cwd string, args ...string) *exec.Cmd {
	realGit := configuredRealGitForEvaluator()

	cmd := realgit.CommandFor(context.Background(), realGit, false, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}

	cmd.Env = realgit.CleanGitLocalEnv(os.Environ())

	return cmd
}

func configuredRealGitForEvaluator() string {
	candidate := strings.TrimSpace(os.Getenv(RealGitEnv))
	if candidate == "" {
		return ""
	}

	if agentShellRealGitCandidate(candidate) {
		return candidate
	}

	self, err := os.Executable()
	if err != nil {
		return ""
	}

	resolvedSelf, err := filepath.EvalSymlinks(self)
	if err != nil {
		resolvedSelf = self
	}

	if !realgit.UsableCandidate(resolvedSelf, candidate) {
		return ""
	}

	if executableReferencesCodingEthosRuntime(candidate) {
		return ""
	}

	return candidate
}

func agentShellRealGitCandidate(path string) bool {
	cleaned := filepath.Clean(path)
	if filepath.Base(cleaned) != "real-git" {
		return false
	}

	if !strings.HasPrefix(filepath.Base(filepath.Dir(cleaned)), "run-") {
		return false
	}

	if !strings.HasSuffix(
		filepath.ToSlash(filepath.Dir(filepath.Dir(cleaned))),
		"/.coding-ethos/cache/agent-shell",
	) {
		return false
	}

	// #nosec G703 -- cleaned is a validated agent-shell real-git bind path.
	info, err := os.Stat(cleaned)

	return err == nil && !info.IsDir() && info.Mode().Perm()&0o111 != 0
}

func executableReferencesCodingEthosRuntime(path string) bool {
	// #nosec G304,G703 -- path is a real-git candidate validated by realgit.
	file, err := os.Open(path)
	if err != nil {
		return false
	}

	defer func() {
		_ = file.Close()
	}()

	buffer := make([]byte, codingEthosRuntimeProbeBytes)

	count, err := file.Read(buffer)
	if err != nil && count == 0 {
		return false
	}

	return bytes.Contains(buffer[:count], []byte("coding-ethos-run"))
}
