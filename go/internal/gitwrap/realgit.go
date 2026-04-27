// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package gitwrap

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const realGitEnv = "CODING_ETHOS_REAL_GIT"

func ResolveRealGit(requested string) (string, error) {
	if requested != "" && requested != "git" {
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
	return "", fmt.Errorf("resolve real git: set %s to the system git executable", realGitEnv)
}

func realGitCandidates(self string) []string {
	candidates := []string{}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" || samePath(dir, filepath.Dir(self)) {
			continue
		}
		candidates = append(candidates, filepath.Join(dir, "git"))
	}
	for _, path := range []string{"/usr/bin/git", "/bin/git", "/usr/local/bin/git", "/opt/homebrew/bin/git"} {
		candidates = append(candidates, path)
	}
	lookedUp, err := exec.LookPath("git")
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
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			continue
		}
		files = append(files, path)
	}
	return files
}

func samePath(left string, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	if leftErr != nil || rightErr != nil {
		return left == right
	}
	return leftAbs == rightAbs
}

func sameExecutable(left string, right string) bool {
	leftInfo, leftErr := os.Stat(left)
	rightInfo, rightErr := os.Stat(right)
	if leftErr == nil && rightErr == nil {
		return os.SameFile(leftInfo, rightInfo)
	}
	return samePath(left, right)
}
