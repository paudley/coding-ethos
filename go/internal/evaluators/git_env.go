// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package evaluators

import (
	"context"
	"os"
	"os/exec"

	"blackcat.ca/coding-ethos/go/internal/realgit"
)

const (
	// RealGitEnv names the validated environment setting for the real git binary.
	RealGitEnv = "CODING_ETHOS_REAL_GIT"
)

func gitCommand(cwd string, args ...string) *exec.Cmd {
	return GitCommand(cwd, args...)
}

// GitCommand builds a git command with hook-local git environment removed.
func GitCommand(cwd string, args ...string) *exec.Cmd {
	cmd := realgit.Command(context.Background(), false, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}

	cmd.Env = realgit.CleanGitLocalEnv(os.Environ())

	return cmd
}
