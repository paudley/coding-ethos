// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package evaluators_test

import (
	"os"
	"path/filepath"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

func compiledRepoBundle(tb testing.TB) policy.Bundle {
	tb.Helper()

	root := repoRoot(tb)

	bundle, _, err := policy.Compile(policy.CompileOptions{
		Primary: filepath.Join(root, "coding_ethos.yml"),
		Config:  filepath.Join(root, "config.yaml"),
	})
	if err != nil {
		tb.Fatalf("compile repo policy bundle: %v", err)
	}

	return bundle
}

func repoRoot(tb testing.TB) string {
	tb.Helper()

	dir, err := filepath.Abs(".")
	if err != nil {
		tb.Fatalf("resolve cwd: %v", err)
	}

	for {
		if fileExists(filepath.Join(dir, "coding_ethos.yml")) &&
			fileExists(filepath.Join(dir, "config.yaml")) {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			tb.Fatalf("repository root not found from %s", dir)
		}

		dir = parent
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}
