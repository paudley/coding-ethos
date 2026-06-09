// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkspaceScanRegistersGitRepos(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	createWorkspaceGitRepo(t, root, "api", "initial api")
	createWorkspaceGitRepo(t, root, "web", "initial web")

	registry, warnings, err := ScanWorkspaceRepos(root)
	if err != nil {
		t.Fatalf("scan workspace repos: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}
	if len(registry.Repos) != 2 {
		t.Fatalf("repos = %#v, want 2", registry.Repos)
	}
	if registry.Repos[0].Alias != "api" || registry.Repos[1].Alias != "web" {
		t.Fatalf("aliases = %#v, want api/web", registry.Repos)
	}
	if registry.Repos[0].CodeIntelDB != DefaultDBPath(filepath.Join(root, "api")) {
		t.Fatalf("db path = %q, want repo-local default", registry.Repos[0].CodeIntelDB)
	}
}

func TestIsGitRepoAcceptsGitFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".git"), []byte("gitdir: ../actual.git\n"))

	if !isGitRepo(root) {
		t.Fatal("isGitRepo rejected a worktree-style .git file")
	}
}

func TestWorkspaceRefreshReportsStaleAndCochangeCandidates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	api := createWorkspaceGitRepo(t, root, "api", "shared change")
	web := createWorkspaceGitRepo(t, root, "web", "shared change")

	createWorkspaceIndexedStore(t, ctx, api)
	createWorkspaceIndexedStore(t, ctx, web)

	if _, err := AddWorkspaceRepo(root, "api", api); err != nil {
		t.Fatalf("add api repo: %v", err)
	}
	if _, err := AddWorkspaceRepo(root, "web", web); err != nil {
		t.Fatalf("add web repo: %v", err)
	}

	status, err := RefreshWorkspaceStatus(ctx, root)
	if err != nil {
		t.Fatalf("refresh workspace status: %v", err)
	}
	if status.Stats.Repos != 2 || status.Stats.Available != 2 {
		t.Fatalf("stats = %#v, want 2 available repos", status.Stats)
	}
	if len(status.CoChanges) == 0 {
		t.Fatalf("cochanges = %#v, want at least one candidate", status.CoChanges)
	}

	writeFile(t, filepath.Join(api, "changed.go"), []byte("package main\n"))
	runWorkspaceGit(t, api, "add", ".")
	runWorkspaceGit(t, api, "commit", "-m", "api followup")

	stale, err := WorkspaceStatusForRegistry(ctx, mustLoadWorkspaceRegistry(t, root))
	if err != nil {
		t.Fatalf("workspace status: %v", err)
	}
	if stale.Stats.Stale == 0 {
		t.Fatalf("stats = %#v, want stale repo after HEAD change", stale.Stats)
	}
}

func createWorkspaceIndexedStore(t *testing.T, ctx context.Context, root string) {
	t.Helper()

	store, err := Open(ctx, DefaultDBPath(root))
	if err != nil {
		t.Fatalf("open workspace store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close workspace store: %v", err)
		}
	})

	sourcePath := filepath.Join(root, "main.go")
	writeFile(t, sourcePath, []byte("package main\nfunc main() {}\n"))
	if _, err := NewASTIndexer(
		store,
	).IndexPaths(ctx, root, []string{"main.go"}); err != nil {
		t.Fatalf("index code paths: %v", err)
	}
}

func createWorkspaceGitRepo(t *testing.T, workspaceRoot, name, message string) string {
	t.Helper()

	root := filepath.Join(workspaceRoot, name)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	runWorkspaceGit(t, root, "init")
	runWorkspaceGit(t, root, "config", "user.name", "Workspace Test")
	runWorkspaceGit(t, root, "config", "user.email", "workspace@example.test")
	writeFile(t, filepath.Join(root, "README.md"), []byte("# "+name+"\n"))
	runWorkspaceGit(t, root, "add", ".")
	runWorkspaceGit(
		t,
		root,
		"commit",
		"-m",
		message,
		"--date",
		time.Now().UTC().Format(time.RFC3339),
	)

	return root
}

func runWorkspaceGit(t *testing.T, root string, args ...string) {
	t.Helper()

	if _, err := workspaceGitOutput(context.Background(), root, args...); err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
}

func mustLoadWorkspaceRegistry(t *testing.T, root string) WorkspaceRegistry {
	t.Helper()

	registry, err := LoadWorkspaceRegistry(root)
	if err != nil {
		t.Fatalf("load workspace registry: %v", err)
	}

	return registry
}

func writeFile(t *testing.T, path string, content []byte) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
