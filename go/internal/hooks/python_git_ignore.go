// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/realgit"
)

func gitPathIgnored(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." || relative == "" ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) ||
		relative == ".." {
		return false
	}

	if !gitRootOwnsPath(root) {
		return false
	}

	command := realgit.Command(
		context.Background(),
		false,
		"-C",
		root,
		"check-ignore",
		"-q",
		"--",
		filepath.ToSlash(relative),
	)
	command.Env = realgit.CleanGitLocalEnv(os.Environ())

	return command.Run() == nil
}

func gitRootOwnsPath(root string) bool {
	command := realgit.Command(
		context.Background(),
		false,
		"-C",
		root,
		"rev-parse",
		"--show-toplevel",
	)
	command.Env = realgit.CleanGitLocalEnv(os.Environ())

	output, err := command.Output()
	if err != nil {
		return false
	}

	resolvedRoot, err := filepath.Abs(strings.TrimSpace(string(output)))
	if err != nil {
		return false
	}

	expectedRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}

	return resolvedRoot == expectedRoot
}
