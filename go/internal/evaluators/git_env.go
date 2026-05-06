// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"context"
	"os"
	"os/exec"
	"strings"
)

const realGitEnv = "CODING_ETHOS_REAL_GIT"

func gitCommand(cwd string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(context.Background(), gitExecutable(), args...)
	if cwd != "" {
		cmd.Dir = cwd
	}

	cmd.Env = cleanGitLocalEnv(os.Environ())

	return cmd
}

func gitExecutable() string {
	if value := strings.TrimSpace(os.Getenv(realGitEnv)); value != "" {
		return value
	}

	return "git"
}

func cleanGitLocalEnv(source []string) []string {
	env := make([]string, 0, len(source))
	for _, item := range source {
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
