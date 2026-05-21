// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package managedcapture

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
)

func managedSubprocessPathPrefix(tempDir, realGit string) (string, error) {
	err := os.MkdirAll(tempDir, capturedPrivateDirMode)
	if err != nil {
		return "", fmt.Errorf("create managed subprocess temp dir: %w", err)
	}

	sum := sha256.Sum256([]byte(realGit))
	toolDir := filepath.Join(tempDir, fmt.Sprintf("path-%x", sum[:8]))

	err = os.MkdirAll(toolDir, capturedPrivateDirMode)
	if err != nil {
		return "", fmt.Errorf("create managed subprocess path: %w", err)
	}

	gitPath := filepath.Join(toolDir, "git")

	err = ensureManagedSubprocessGitLink(gitPath, realGit)
	if err != nil {
		return "", err
	}

	return toolDir, nil
}

func ensureManagedSubprocessGitLink(gitPath, realGit string) error {
	target, err := os.Readlink(gitPath)
	if err == nil {
		return replaceMismatchedManagedSubprocessGitLink(gitPath, realGit, target)
	}

	if !os.IsNotExist(err) {
		return replaceStaleManagedSubprocessGitFile(gitPath, realGit, err)
	}

	return createManagedSubprocessGitLink(gitPath, realGit)
}

func replaceMismatchedManagedSubprocessGitLink(gitPath, realGit, target string) error {
	if target == realGit {
		return nil
	}

	err := os.Remove(gitPath)
	if err != nil {
		return fmt.Errorf("replace managed subprocess git link: %w", err)
	}

	return createManagedSubprocessGitLink(gitPath, realGit)
}

func replaceStaleManagedSubprocessGitFile(
	gitPath, realGit string,
	readlinkErr error,
) error {
	info, statErr := os.Stat(gitPath)
	if statErr != nil || info.IsDir() || !info.Mode().IsRegular() {
		return fmt.Errorf("inspect managed subprocess git link: %w", readlinkErr)
	}

	err := os.Remove(gitPath)
	if err != nil {
		return fmt.Errorf("replace stale managed subprocess git file: %w", err)
	}

	return createManagedSubprocessGitLink(gitPath, realGit)
}

func createManagedSubprocessGitLink(gitPath, realGit string) error {
	err := os.Symlink(realGit, gitPath)
	if err != nil {
		if os.IsExist(err) {
			target, readErr := os.Readlink(gitPath)
			if readErr == nil && target == realGit {
				return nil
			}
		}

		return fmt.Errorf("link managed subprocess git: %w", err)
	}

	return nil
}
