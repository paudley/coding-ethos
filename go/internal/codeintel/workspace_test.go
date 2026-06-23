// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"encoding/json"
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
	gitDir := filepath.Join(root, "..", "actual.git")
	if err := os.MkdirAll(gitDir, 0o700); err != nil {
		t.Fatalf("mkdir gitdir: %v", err)
	}
	writeFile(t, filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"))

	writeFile(t, filepath.Join(root, ".git"), []byte("gitdir: ../actual.git\n"))

	if !isGitRepo(root) {
		t.Fatal("isGitRepo rejected a worktree-style .git file")
	}
}

func TestIsGitRepoRejectsBrokenGitFileTarget(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	gitDir := filepath.Join(root, "..", "actual.git")
	if err := os.MkdirAll(gitDir, 0o700); err != nil {
		t.Fatalf("mkdir gitdir: %v", err)
	}

	writeFile(t, filepath.Join(root, ".git"), []byte("gitdir: ../actual.git\n"))

	if isGitRepo(root) {
		t.Fatal("isGitRepo accepted a gitdir without a HEAD marker")
	}
}

func TestAddWorkspaceRepoRejectsDuplicateCanonicalPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	repo := createWorkspaceGitRepo(t, root, "api", "initial api")

	if _, err := AddWorkspaceRepo(root, "api", repo); err != nil {
		t.Fatalf("add api repo: %v", err)
	}
	if _, err := AddWorkspaceRepo(root, "api-copy", repo); err == nil {
		t.Fatal("AddWorkspaceRepo allowed duplicate canonical path")
	}
}

func TestAddWorkspaceRepoRejectsDuplicateSymlinkPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	repo := createWorkspaceGitRepo(t, root, "api", "initial api")
	link := filepath.Join(root, "api-link")
	if err := os.Symlink(repo, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	if _, err := AddWorkspaceRepo(root, "api", repo); err != nil {
		t.Fatalf("add api repo: %v", err)
	}
	if _, err := AddWorkspaceRepo(root, "api-link", link); err == nil {
		t.Fatal("AddWorkspaceRepo allowed duplicate symlink path")
	}
}

func TestLoadWorkspaceRegistryToleratesMissingRepoPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	missingPath := filepath.Join(root, "missing")
	registry := WorkspaceRegistry{
		SchemaVersion: workspaceSchemaVersion,
		WorkspaceRoot: root,
		Repos: []WorkspaceRepo{
			{Alias: "missing", Path: missingPath},
		},
	}

	data, err := json.Marshal(registry)
	if err != nil {
		t.Fatalf("marshal registry: %v", err)
	}
	writeFile(t, DefaultWorkspaceConfigPath(root), append(data, '\n'))

	loaded, err := LoadWorkspaceRegistry(root)
	if err != nil {
		t.Fatalf("load registry with missing repo path: %v", err)
	}
	if len(loaded.Repos) != 1 || loaded.Repos[0].Path != missingPath {
		t.Fatalf("loaded registry = %#v, want missing path preserved", loaded)
	}
	if loaded.Repos[0].CodeIntelDB != DefaultDBPath(missingPath) {
		t.Fatalf("db path = %q, want default for missing repo", loaded.Repos[0].CodeIntelDB)
	}

	updated, err := RemoveWorkspaceRepo(root, "missing")
	if err != nil {
		t.Fatalf("remove missing repo: %v", err)
	}
	if len(updated.Repos) != 0 {
		t.Fatalf("repos after remove = %#v, want none", updated.Repos)
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

func TestWorkspaceRepoStatusReportsMissingStore(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	repoPath := createWorkspaceGitRepo(t, root, "api", "initial api")
	repo := WorkspaceRepo{
		Alias:       "api",
		Path:        repoPath,
		CodeIntelDB: DefaultDBPath(repoPath),
	}

	status, err := workspaceRepoStatus(ctx, repo)
	if err != nil {
		t.Fatalf("workspace repo status: %v", err)
	}
	if !status.Stale || status.StaleWarning != "code-intel store is missing" {
		t.Fatalf("status = %#v, want missing-store stale warning", status)
	}
	if status.StoreAvailable {
		t.Fatalf("status = %#v, want unavailable store", status)
	}
}

func TestWorkspaceOpenRepoStoreStatusReadsIndexStats(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	repoPath := createWorkspaceGitRepo(t, root, "api", "initial api")

	store, err := Open(ctx, DefaultDBPath(repoPath))
	if err != nil {
		t.Fatalf("open workspace store: %v", err)
	}
	sourcePath := filepath.Join(repoPath, "main.go")
	writeFile(t, sourcePath, []byte("package main\nfunc main() {}\n"))
	if _, err := NewASTIndexer(
		store,
	).IndexPaths(ctx, repoPath, []string{"main.go"}); err != nil {
		t.Fatalf("index code paths: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close workspace store: %v", err)
	}

	store, err = OpenReadOnly(ctx, DefaultDBPath(repoPath))
	if err != nil {
		t.Fatalf("open read-only store: %v", err)
	}

	status, err := workspaceOpenRepoStoreStatus(ctx, store, WorkspaceRepoStatus{
		Alias:       "api",
		Path:        repoPath,
		CodeIntelDB: DefaultDBPath(repoPath),
	})
	if err != nil {
		t.Fatalf("workspace open repo store status: %v", err)
	}
	if status.LastIndexedAtUTC == "" {
		t.Fatalf("status = %#v, want indexed timestamp", status)
	}
	if status.Stale {
		t.Fatalf("status = %#v, want active indexed store", status)
	}
}

func TestWorkspaceRepoByAliasTrimsInput(t *testing.T) {
	t.Parallel()

	registry := WorkspaceRegistry{
		Repos: []WorkspaceRepo{
			{Alias: "api", Path: "/workspace/api"},
		},
	}

	repo, ok := WorkspaceRepoByAlias(registry, " api ")
	if !ok {
		t.Fatal("WorkspaceRepoByAlias did not find trimmed alias")
	}
	if repo.Path != "/workspace/api" {
		t.Fatalf("repo = %#v, want api path", repo)
	}

	if _, ok := WorkspaceRepoByAlias(registry, "web"); ok {
		t.Fatal("WorkspaceRepoByAlias found unknown alias")
	}
}

func TestWorkspaceContractsFromEdgesMapsProviderRepos(t *testing.T) {
	t.Parallel()

	registry := WorkspaceRegistry{
		Repos: []WorkspaceRepo{
			{Alias: "api", Path: "/workspace/api"},
			{Alias: "web", Path: "/workspace/web"},
		},
	}
	edges := []CodeEdge{
		{
			Kind:       "imports",
			Path:       "client.go",
			TargetPath: "/workspace/web/pkg/server.go",
			RawText:    "import web",
		},
		{
			Kind:       "package_dependency",
			Path:       "package.json",
			TargetPath: "web/package.json",
			RawText:    `"@workspace/web": "workspace:*"`,
		},
		{
			Kind:       "http_client",
			Path:       "client.go",
			TargetPath: "/workspace/web/routes/users.go",
			RawText:    "GET /users",
		},
		{Kind: "calls", TargetPath: "/workspace/web/pkg/ignored.go"},
		{Kind: "imports"},
		{Kind: "imports", TargetPath: "/workspace/api/internal/self.go"},
	}

	contracts := workspaceContractsFromEdges(registry.Repos[0], registry, edges)

	if len(contracts) != 3 {
		t.Fatalf("contracts = %#v, want three cross-repo contracts", contracts)
	}

	for _, want := range []struct {
		evidence string
		kind     string
	}{
		{kind: "imports", evidence: "import web"},
		{kind: "package_dependency", evidence: `"@workspace/web": "workspace:*"`},
		{kind: "http_client", evidence: "GET /users"},
	} {
		if !workspaceContractsContain(contracts, want.kind, want.evidence) {
			t.Fatalf("contracts = %#v, missing %s contract", contracts, want.kind)
		}
	}
}

func workspaceContractsContain(
	contracts []WorkspaceContract,
	kind string,
	evidence string,
) bool {
	for _, contract := range contracts {
		if contract.ConsumerRepo == "api" &&
			contract.ProviderRepo == "web" &&
			contract.Kind == kind &&
			contract.Evidence == evidence {
			return true
		}
	}

	return false
}

func TestWorkspaceRepoForTargetPathSupportsAliasPrefixes(t *testing.T) {
	t.Parallel()

	registry := WorkspaceRegistry{
		Repos: []WorkspaceRepo{
			{Alias: "api", Path: "/workspace/api"},
			{Alias: "web", Path: "/workspace/web"},
		},
	}

	repo, ok := workspaceRepoForTargetPath(
		registry,
		registry.Repos[0],
		"web/pkg/server.go",
	)
	if !ok || repo.Alias != "web" {
		t.Fatalf("repo = %#v ok=%v, want web alias target", repo, ok)
	}

	if _, ok := workspaceRepoForTargetPath(
		registry,
		registry.Repos[0],
		"api/self.go",
	); ok {
		t.Fatal("workspaceRepoForTargetPath matched current repo alias")
	}
}

func TestWorkspaceCoChangeReasonRequiresSubjectOrCloseAuthorTime(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	left := workspaceGitCommit{
		AuthorEmail: "dev@example.test",
		AuthorTime:  now,
		Subject:     "shared",
	}
	right := workspaceGitCommit{
		AuthorEmail: "other@example.test",
		AuthorTime:  now.Add(10 * time.Hour),
		Subject:     "shared",
	}
	if got := workspaceCoChangeReason(left, right); got != "matching commit subject" {
		t.Fatalf("reason = %q, want subject match", got)
	}

	right.Subject = "different"
	right.AuthorEmail = left.AuthorEmail
	right.AuthorTime = now.Add(workspaceCoChangeWindow)
	if got := workspaceCoChangeReason(
		left,
		right,
	); got != "matching author and close commit time" {
		t.Fatalf("reason = %q, want close author-time match", got)
	}

	right.AuthorTime = now.Add(workspaceCoChangeWindow + time.Second)
	if got := workspaceCoChangeReason(left, right); got != "" {
		t.Fatalf("reason = %q, want no match", got)
	}
}

func TestParseWorkspaceGitCommitRejectsMalformedRecords(t *testing.T) {
	t.Parallel()

	if _, ok := parseWorkspaceGitCommit("api", ""); ok {
		t.Fatal("parseWorkspaceGitCommit accepted empty record")
	}
	if _, ok := parseWorkspaceGitCommit(
		"api",
		"hash\x1femail\x1fbad-time\x1fsubject",
	); ok {
		t.Fatal("parseWorkspaceGitCommit accepted bad timestamp")
	}

	record := "hash\x1fdev@example.test\x1f2026-06-09T18:00:00Z\x1fsubject\nmain.go\n\n"
	commit, ok := parseWorkspaceGitCommit("api", record)
	if !ok {
		t.Fatal("parseWorkspaceGitCommit rejected valid record")
	}
	if commit.RepoAlias != "api" || commit.Hash != "hash" ||
		len(commit.Paths) != 1 || commit.Paths[0] != "main.go" {
		t.Fatalf("commit = %#v, want parsed record", commit)
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
