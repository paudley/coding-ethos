// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"context"
	"os"
	"os/exec"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/safeexec"
)

const (
	gitBinaryName = "git"

	// RealGitEnv names the validated environment setting for the real git binary.
	RealGitEnv = "CODING_ETHOS_REAL_GIT"
)

func gitCommand(cwd string, args ...string) *exec.Cmd {
	return GitCommand(cwd, args...)
}

// GitCommand builds a git command with hook-local git environment removed.
func GitCommand(cwd string, args ...string) *exec.Cmd {
	cmd := safeexec.CommandContext(context.Background(), gitExecutable(), args...)
	if cwd != "" {
		cmd.Dir = cwd
	}

	cmd.Env = CleanGitLocalEnv(os.Environ())

	return cmd
}

func gitExecutable() string {
	if value := strings.TrimSpace(os.Getenv(RealGitEnv)); value != "" {
		return value
	}

	return gitBinaryName
}

// CleanGitLocalEnv removes git-local environment variables from command env.
func CleanGitLocalEnv(source []string) []string {
	env := make([]string, 0, len(source))
	for _, item := range source {
		name, _, found := strings.Cut(item, "=")
		if found && gitLocalEnvName(name) {
			continue
		}

		env = append(env, item)
	}

	env = append(env, "GIT_OPTIONAL_LOCKS=0")

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
