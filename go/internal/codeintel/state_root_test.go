// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"path/filepath"
	"testing"
)

func TestResolveStateRootUsesConfiguredPrivateRoot(t *testing.T) {
	repositoryRoot := filepath.Join("repository", "root")
	privateRoot := filepath.Join("private", "state")
	t.Setenv(StateRootEnvironment, "  "+privateRoot+"  ")

	if got := ResolveStateRoot(repositoryRoot); got != filepath.Clean(privateRoot) {
		t.Fatalf("ResolveStateRoot() = %q, want %q", got, privateRoot)
	}
}

func TestResolveStateRootFallsBackToRepositoryRoot(t *testing.T) {
	repositoryRoot := filepath.Join("repository", "root")
	t.Setenv(StateRootEnvironment, "  ")

	if got := ResolveStateRoot(repositoryRoot); got != filepath.Clean(repositoryRoot) {
		t.Fatalf("ResolveStateRoot() = %q, want %q", got, repositoryRoot)
	}
}
