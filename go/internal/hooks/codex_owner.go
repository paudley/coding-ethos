// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import (
	"os"
	"path/filepath"
	"strings"
)

func shouldSkipNestedCodexHook(event Event) bool {
	if event.Provider() != providerCodex {
		return false
	}

	consumerRoot := cleanAbsPath(os.Getenv("CODE_ETHOS_CONSUMER_ROOT"))
	if consumerRoot == "" {
		return false
	}

	cwd := event.Cwd
	if strings.TrimSpace(cwd) == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return false
		}
	}

	nearestRoot := cleanAbsPath(gitRootFromPath(cwd))
	if nearestRoot == "" {
		return false
	}

	return nearestRoot != consumerRoot
}

func cleanAbsPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}

	if !filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err == nil {
			path = abs
		}
	}

	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		path = resolved
	}

	return filepath.Clean(path)
}
