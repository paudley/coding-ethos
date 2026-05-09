// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

// Package realgit resolves the host git executable without using coding-ethos
// shims or runtime binaries.
package realgit

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/safeexec"
)

const (
	// Env names the validated environment setting for the host git binary.
	Env = "CODING_ETHOS_REAL_GIT"

	executableName = "git"
)

var errUnresolved = apperror.StaticError(
	"real git executable could not be resolved",
)

// Resolve returns a host git executable path for internal coding-ethos use.
func Resolve(ctx context.Context, requested string) (string, error) {
	if requested != "" && requested != executableName {
		return requested, nil
	}

	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve current executable: %w", err)
	}

	resolvedSelf, err := filepath.EvalSymlinks(self)
	if err != nil {
		resolvedSelf = self
	}

	if envValue := strings.TrimSpace(os.Getenv(Env)); envValue != "" &&
		UsableCandidate(resolvedSelf, envValue) &&
		reportsGitVersion(ctx, envValue) {
		return envValue, nil
	}

	for _, candidate := range Candidates(resolvedSelf) {
		if !UsableCandidate(resolvedSelf, candidate) {
			continue
		}

		return candidate, nil
	}

	return "", fmt.Errorf("%w: set %s to the system git executable", errUnresolved, Env)
}

// Candidates returns executable git candidates, excluding the current binary's
// directory from PATH-derived candidates.
func Candidates(self string) []string {
	candidates := []string{}

	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" || SamePath(dir, filepath.Dir(self)) {
			continue
		}

		candidates = append(candidates, filepath.Join(dir, executableName))
	}

	candidates = append(
		candidates,
		[]string{
			"/usr/bin/git",
			"/bin/git",
			"/usr/local/bin/git",
			"/opt/homebrew/bin/git",
		}...)

	lookedUp, err := exec.LookPath(executableName)
	if err == nil {
		candidates = append(candidates, lookedUp)
	}

	return ExecutableFiles(candidates)
}

// UsableCandidate reports whether candidate is not the current executable and
// does not look like a coding-ethos shim.
func UsableCandidate(self, candidate string) bool {
	if candidate == "" {
		return false
	}

	executable, err := exec.LookPath(candidate)
	if err != nil {
		return false
	}

	resolvedCandidate, err := filepath.EvalSymlinks(executable)
	if err != nil {
		resolvedCandidate = executable
	}

	if SameExecutable(self, resolvedCandidate) {
		return false
	}

	if LooksLikeCodingEthosShim(executable, self) ||
		LooksLikeCodingEthosShim(resolvedCandidate, self) {
		return false
	}

	return true
}

// LooksLikeCodingEthosShim identifies agent-facing git shims and runtime stubs.
func LooksLikeCodingEthosShim(path, self string) bool {
	if filepath.Base(path) == "coding-ethos-run" {
		return true
	}

	if SamePath(filepath.Dir(path), filepath.Dir(self)) {
		return true
	}

	return dirContainsCodingEthosRuntime(filepath.Dir(path))
}

func dirContainsCodingEthosRuntime(dir string) bool {
	cleaned := filepath.Clean(dir)
	target := filepath.Join(cleaned, "coding-ethos-run")

	if filepath.Dir(target) != cleaned {
		return false
	}

	info, err := os.Stat(target)

	return err == nil && !info.IsDir()
}

func reportsGitVersion(ctx context.Context, path string) bool {
	const versionTimeout = time.Second

	ctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()

	output, err := safeexec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return false
	}

	return strings.HasPrefix(strings.TrimSpace(string(output)), "git version ")
}

// Executable returns the git binary path. When wantsShim is false, Resolve
// is used to find the real host git, skipping coding-ethos shims. When
// wantsShim is true, the bare name "git" is returned for standard PATH
// resolution (which may find a shim).
func Executable(ctx context.Context, wantsShim bool) string {
	if wantsShim {
		return executableName
	}

	resolved, err := Resolve(ctx, executableName)
	if err != nil {
		return executableName
	}

	return resolved
}

// Command builds an *exec.Cmd for a git operation. When wantsShim is false,
// the real host git is resolved and shims are skipped. When wantsShim is
// true, standard PATH resolution is used.
func Command(ctx context.Context, wantsShim bool, args ...string) *exec.Cmd {
	return CommandFor(ctx, executableName, wantsShim, args...)
}

// CommandFor builds an *exec.Cmd resolving the specified git binary. If
// requested is a specific path (not "git" or ""), it is used directly.
// Otherwise, standard resolution applies with the wantsShim flag.
func CommandFor(
	ctx context.Context,
	requested string,
	wantsShim bool,
	args ...string,
) *exec.Cmd {
	if wantsShim {
		return safeexec.CommandContext(ctx, executableName, args...)
	}

	resolved, err := Resolve(ctx, requested)
	if err != nil {
		return safeexec.CommandContext(ctx, executableName, args...)
	}

	return safeexec.CommandContext(ctx, resolved, args...)
}

// CleanGitLocalEnv removes git hook-local environment variables from a
// command environment slice so that child git processes do not inherit the
// caller's hook context.
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

// ExecutableFiles filters paths to executable files.
func ExecutableFiles(paths []string) []string {
	seen := map[string]bool{}
	files := []string{}

	for _, path := range paths {
		if seen[path] {
			continue
		}

		seen[path] = true

		resolved, err := exec.LookPath(path)
		if err != nil {
			continue
		}

		files = append(files, resolved)
	}

	return files
}

// SamePath compares paths after making them absolute when possible.
func SamePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)

	rightAbs, rightErr := filepath.Abs(right)
	if leftErr != nil || rightErr != nil {
		return left == right
	}

	return leftAbs == rightAbs
}

// SameExecutable compares executable identity when stat information is available.
func SameExecutable(left, right string) bool {
	resolvedLeft, leftErr := filepath.EvalSymlinks(left)
	if leftErr != nil {
		resolvedLeft = left
	}

	resolvedRight, rightErr := filepath.EvalSymlinks(right)
	if rightErr != nil {
		resolvedRight = right
	}

	return SamePath(resolvedLeft, resolvedRight)
}
