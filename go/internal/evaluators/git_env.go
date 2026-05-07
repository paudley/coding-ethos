// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/realgit"
	"blackcat.ca/coding-ethos/go/internal/safeexec"
)

const (
	gitBinaryName = "git"
	gitShimDirEnv = "CODING_ETHOS_GIT_SHIM_DIR"

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
	resolved, err := realgit.Resolve(gitBinaryName)
	if err == nil && strings.TrimSpace(resolved) != "" {
		return resolved
	}

	for _, candidate := range realGitCandidates() {
		resolved, ok := realGitCandidate(candidate)
		if ok {
			return resolved
		}
	}

	return gitBinaryName
}

func realGitCandidate(candidate string) (string, bool) {
	path, err := exec.LookPath(candidate)
	if err != nil || pathInGitShimDir(path) || sameExecutableAsSelf(path) {
		return "", false
	}

	return path, true
}

func pathInGitShimDir(path string) bool {
	shimDir := strings.TrimSpace(os.Getenv(gitShimDirEnv))
	if shimDir == "" {
		return false
	}

	return samePath(filepath.Dir(path), shimDir)
}

func sameExecutableAsSelf(path string) bool {
	self, err := os.Executable()
	if err != nil {
		return false
	}

	selfInfo, selfErr := os.Stat(resolveSymlink(self))
	pathInfo, pathErr := os.Stat(resolveSymlink(path))

	if selfErr == nil && pathErr == nil {
		return os.SameFile(selfInfo, pathInfo)
	}

	return samePath(self, path)
}

func resolveSymlink(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}

	return resolved
}

func samePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)

	if leftErr != nil || rightErr != nil {
		return left == right
	}

	return leftAbs == rightAbs
}

func realGitCandidates() []string {
	candidates := []string{"/usr/bin/git", "/bin/git", "/usr/local/bin/git"}

	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}

		candidates = append(candidates, filepath.Join(dir, gitBinaryName))
	}

	return candidates
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
