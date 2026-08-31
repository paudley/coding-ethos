// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/realgit"
)

func TestRefreshLintFilesIndexesOnlyExplicitFilesAndTombstonesDeletion(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	appPath := filepath.Join(root, "pkg", "app.py")
	otherPath := filepath.Join(root, "pkg", "other.py")

	writeMaintenanceTestFile(t, appPath, []byte("VALUE = 1\n"))
	writeMaintenanceTestFile(t, otherPath, []byte("OTHER = 2\n"))
	runMaintenanceTestGit(t, root, "init", "--initial-branch", "main")
	runMaintenanceTestGit(t, root, "config", "user.email", "test@example.com")
	runMaintenanceTestGit(t, root, "config", "user.name", "Test User")
	runMaintenanceTestGit(t, root, "add", "pkg/app.py", "pkg/other.py")
	runMaintenanceTestGit(t, root, "commit", "-m", "initial")

	summary, err := RefreshLintFiles(ctx, root, []string{"pkg/app.py"})
	if err != nil {
		t.Fatalf("initial incremental refresh: %v", err)
	}
	if summary.FilesIndexed != 1 {
		t.Fatalf("files indexed = %d, want 1", summary.FilesIndexed)
	}

	store, err := Open(ctx, DefaultDBPath(root))
	if err != nil {
		t.Fatalf("open code-intel store: %v", err)
	}

	if _, found, getErr := store.GetCodeFile(ctx, "pkg/app.py"); getErr != nil || !found {
		t.Fatalf("requested file found=%v err=%v, want true nil", found, getErr)
	}
	if file, found, getErr := store.GetCodeFile(
		ctx,
		"pkg/other.py",
	); getErr != nil ||
		found {
		t.Fatalf("unrequested file=%#v found=%v err=%v, want absent", file, found, getErr)
	}
	if err = store.Close(); err != nil {
		t.Fatalf("close code-intel store: %v", err)
	}

	if err = os.Remove(appPath); err != nil {
		t.Fatalf("delete indexed file: %v", err)
	}
	runMaintenanceTestGit(t, root, "add", "--update", "pkg/app.py")

	summary, err = RefreshLintFiles(ctx, root, []string{"pkg/app.py"})
	if err != nil {
		t.Fatalf("deleted-file incremental refresh: %v", err)
	}
	if !slices.Contains(summary.Deleted, "pkg/app.py") {
		t.Fatalf("deleted summary = %#v, want pkg/app.py", summary.Deleted)
	}

	store, err = Open(ctx, DefaultDBPath(root))
	if err != nil {
		t.Fatalf("reopen code-intel store: %v", err)
	}
	defer store.Close()

	file, found, err := store.GetCodeFile(ctx, "pkg/app.py")
	if err != nil {
		t.Fatalf("get deleted code file: %v", err)
	}
	if !found || file.DeletedAtUTC == "" || file.StaleReason != "deleted_by_intent" {
		t.Fatalf("deleted file = %#v, found=%v", file, found)
	}
}

func TestRefreshLintFilesRejectsDirectoryAndOutsidePaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.py")
	writeMaintenanceTestFile(t, filepath.Join(root, "app.py"), []byte("VALUE = 1\n"))
	writeMaintenanceTestFile(t, outside, []byte("OUTSIDE = 1\n"))
	symlink := filepath.Join(root, "outside-link.py")
	if err := os.Symlink(outside, symlink); err != nil {
		t.Fatalf("create outside symlink: %v", err)
	}

	summary, err := RefreshLintFiles(
		context.Background(),
		root,
		[]string{".", outside, "outside-link.py"},
	)
	if err != nil {
		t.Fatalf("reject broad lint paths: %v", err)
	}
	if summary.FilesIndexed != 0 || summary.ChunksIndexed != 0 ||
		len(summary.Skipped) != 0 || len(summary.Deleted) != 0 {
		t.Fatalf("summary = %#v, want empty", summary)
	}

	if _, err = os.Stat(DefaultDBPath(root)); !os.IsNotExist(err) {
		t.Fatalf("ordinary broad paths created code-intel DB: %v", err)
	}
}

func TestRefreshLintFilesDoesNotReconcileUnrelatedIndexedFiles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	appPath := filepath.Join(root, "app.py")
	generatedPath := filepath.Join(root, "generated.py")

	writeMaintenanceTestFile(t, appPath, []byte("VALUE = 1\n"))
	writeMaintenanceTestFile(t, generatedPath, []byte("GENERATED = 1\n"))
	runMaintenanceTestGit(t, root, "init", "--initial-branch", "main")
	runMaintenanceTestGit(t, root, "config", "user.email", "test@example.com")
	runMaintenanceTestGit(t, root, "config", "user.name", "Test User")
	runMaintenanceTestGit(t, root, "add", "app.py")
	runMaintenanceTestGit(t, root, "commit", "-m", "initial")

	store, err := Open(ctx, DefaultDBPath(root))
	if err != nil {
		t.Fatalf("open code-intel store: %v", err)
	}

	_, err = NewASTIndexer(store).IndexPaths(
		ctx,
		root,
		[]string{"app.py", "generated.py"},
	)
	if err != nil {
		t.Fatalf("seed code-intel index: %v", err)
	}
	if err = store.Close(); err != nil {
		t.Fatalf("close seeded code-intel store: %v", err)
	}

	writeMaintenanceTestFile(
		t,
		filepath.Join(root, ".gitignore"),
		[]byte("generated.py\n"),
	)
	writeMaintenanceTestFile(t, appPath, []byte("VALUE = 2\n"))

	if _, err = RefreshLintFiles(ctx, root, []string{"app.py"}); err != nil {
		t.Fatalf("incremental refresh: %v", err)
	}

	store, err = Open(ctx, DefaultDBPath(root))
	if err != nil {
		t.Fatalf("reopen code-intel store: %v", err)
	}
	defer store.Close()

	generated, found, err := store.GetCodeFile(ctx, "generated.py")
	if err != nil {
		t.Fatalf("get unrelated indexed file: %v", err)
	}
	if !found || generated.DeletedAtUTC != "" || generated.StaleReason != "" {
		t.Fatalf("unrelated indexed file = %#v, found=%v, want active", generated, found)
	}
}

func writeMaintenanceTestFile(t *testing.T, path string, content []byte) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create test file parent: %v", err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write test file: %v", err)
	}
}

func runMaintenanceTestGit(t *testing.T, root string, args ...string) {
	t.Helper()

	command := realgit.Command(context.Background(), false, args...)
	command.Dir = root
	command.Env = realgit.CleanGitLocalEnv(os.Environ())

	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}
