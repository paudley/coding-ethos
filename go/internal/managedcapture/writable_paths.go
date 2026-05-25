// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package managedcapture

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/sandbox"
)

func prepareManagedWritablePaths(root string, evidence sandbox.Evidence) error {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil
	}

	for _, path := range evidence.WritePaths {
		if !managedWritableDir(path) {
			continue
		}

		err := prepareManagedWritablePath(root, path)
		if err != nil {
			return err
		}
	}

	return nil
}

func prepareManagedWritablePath(root, path string) error {
	target := filepath.Join(root, filepath.FromSlash(strings.Trim(path, "/")))

	err := os.MkdirAll(target, capturedPrivateDirMode)
	if err != nil {
		return fmt.Errorf(
			"coding-ethos bug: managed writable path %q could not be prepared: %w",
			path,
			err,
		)
	}

	return nil
}

func managedWritableDir(path string) bool {
	path = strings.TrimSpace(filepath.ToSlash(path))
	if path == "" || filepath.IsAbs(filepath.FromSlash(path)) {
		return false
	}

	path = strings.Trim(path, "/")
	switch path {
	case ".coding-ethos/cache",
		sandbox.SandboxTempWritePath,
		sandbox.SandboxGoCachePath,
		sandbox.SandboxGolangCIPath,
		".pytest_cache",
		".mypy_cache",
		".ruff_cache",
		".uv-cache":
		return true
	default:
		return strings.HasPrefix(path, ".coding-ethos/cache/")
	}
}
