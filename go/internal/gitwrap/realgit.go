// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package gitwrap

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const realGitEnv = "CODING_ETHOS_REAL_GIT"

const gitExecutableName = "git"

var errRealGitUnresolved = errors.New("real git executable could not be resolved")

func ResolveRealGit(requested string) (string, error) {
	if requested != "" && requested != gitExecutableName {
		return requested, nil
	}

	if envValue := strings.TrimSpace(os.Getenv(realGitEnv)); envValue != "" {
		return envValue, nil
	}

	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve current executable: %w", err)
	}

	resolvedSelf, err := filepath.EvalSymlinks(self)
	if err != nil {
		resolvedSelf = self
	}

	for _, candidate := range realGitCandidates(resolvedSelf) {
		if candidate == "" {
			continue
		}

		resolvedCandidate, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			resolvedCandidate = candidate
		}

		if sameExecutable(resolvedSelf, resolvedCandidate) {
			continue
		}

		return candidate, nil
	}

	return "", fmt.Errorf(
		"%w: set %s to the system git executable",
		errRealGitUnresolved,
		realGitEnv,
	)
}

func realGitCandidates(self string) []string {
	candidates := []string{}

	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" || samePath(dir, filepath.Dir(self)) {
			continue
		}

		candidates = append(candidates, filepath.Join(dir, gitExecutableName))
	}

	candidates = append(
		candidates,
		[]string{
			"/usr/bin/git",
			"/bin/git",
			"/usr/local/bin/git",
			"/opt/homebrew/bin/git",
		}...)

	lookedUp, err := exec.LookPath(gitExecutableName)
	if err == nil {
		candidates = append(candidates, lookedUp)
	}

	return executableFiles(candidates)
}

func executableFiles(paths []string) []string {
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

func samePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)

	rightAbs, rightErr := filepath.Abs(right)
	if leftErr != nil || rightErr != nil {
		return left == right
	}

	return leftAbs == rightAbs
}

func sameExecutable(left, right string) bool {
	leftInfo, leftErr := os.Stat(left)

	rightInfo, rightErr := os.Stat(right)
	if leftErr == nil && rightErr == nil {
		return os.SameFile(leftInfo, rightInfo)
	}

	return samePath(left, right)
}
