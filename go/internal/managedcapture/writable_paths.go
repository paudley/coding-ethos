// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package managedcapture

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/sandbox"
)

var (
	errManagedWritablePathTraversal = apperror.StaticError(
		"invalid sandbox write path contains parent traversal",
	)
	errManagedWritablePathEscapesRoot = apperror.StaticError(
		"invalid sandbox write path escapes repo root",
	)
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
	normalizedPath := strings.Trim(filepath.ToSlash(path), "/")
	if containsParentPathSegment(normalizedPath) {
		return fmt.Errorf(
			"%w: %q",
			errManagedWritablePathTraversal,
			path,
		)
	}

	root = filepath.Clean(root)
	target := filepath.Join(root, filepath.FromSlash(normalizedPath))

	if pathEscapesRoot(root, target) {
		return fmt.Errorf(
			"%w: path %q root %q",
			errManagedWritablePathEscapesRoot,
			path,
			root,
		)
	}

	err := os.MkdirAll(target, capturedPrivateDirMode)
	if err != nil {
		return fmt.Errorf(
			"prepare sandbox write path %q: %w",
			path,
			err,
		)
	}

	return nil
}

func pathEscapesRoot(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return true
	}

	return relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func managedWritableDir(path string) bool {
	path = strings.TrimSpace(filepath.ToSlash(path))
	if path == "" || filepath.IsAbs(filepath.FromSlash(path)) {
		return false
	}

	path = strings.Trim(path, "/")
	if containsParentPathSegment(path) {
		return false
	}

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

func containsParentPathSegment(path string) bool {
	for segment := range strings.SplitSeq(path, "/") {
		if segment == ".." {
			return true
		}
	}

	return false
}
