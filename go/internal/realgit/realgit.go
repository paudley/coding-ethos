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
func Resolve(requested string) (string, error) {
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
		reportsGitVersion(envValue) {
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

	return SamePath(filepath.Dir(path), filepath.Dir(self))
}

func reportsGitVersion(path string) bool {
	const versionTimeout = time.Second

	ctx, cancel := context.WithTimeout(context.Background(), versionTimeout)
	defer cancel()

	output, err := safeexec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return false
	}

	return strings.HasPrefix(strings.TrimSpace(string(output)), "git version ")
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
