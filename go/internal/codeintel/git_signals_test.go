// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"blackcat.ca/coding-ethos/go/internal/realgit"
)

func TestStoreRefreshesGitSignalsFromRealHistory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := newGitSignalRepo(t, ctx)

	writeGitSignalFile(t, repo, "pkg/a.go", "package pkg\n\nfunc A() {}\n")
	writeGitSignalFile(t, repo, "pkg/b.go", "package pkg\n\nfunc B() {}\n")
	runGitSignalGit(
		t,
		ctx,
		repo,
		gitSignalAuthorEnv("Alice", "alice@example.invalid"),
		"add",
		"pkg/a.go",
		"pkg/b.go",
	)
	runGitSignalGit(
		t,
		ctx,
		repo,
		gitSignalAuthorEnv("Alice", "alice@example.invalid"),
		"commit",
		"-m",
		"test(repo): add package files",
	)

	writeGitSignalFile(
		t,
		repo,
		"pkg/a.go",
		"package pkg\n\nfunc A() string { return \"a\" }\n",
	)
	writeGitSignalFile(
		t,
		repo,
		"pkg/b.go",
		"package pkg\n\nfunc B() string { return \"b\" }\n",
	)
	runGitSignalGit(
		t,
		ctx,
		repo,
		gitSignalAuthorEnv("Bob", "bob@example.invalid"),
		"add",
		"pkg/a.go",
		"pkg/b.go",
	)
	runGitSignalGit(
		t,
		ctx,
		repo,
		gitSignalAuthorEnv("Bob", "bob@example.invalid"),
		"commit",
		"-m",
		"test(repo): update package pair",
	)

	writeGitSignalFile(
		t,
		repo,
		"pkg/a.go",
		"package pkg\n\nfunc A() string { return \"aa\" }\n",
	)
	runGitSignalGit(
		t,
		ctx,
		repo,
		gitSignalAuthorEnv("Alice", "alice@example.invalid"),
		"add",
		"pkg/a.go",
	)
	runGitSignalGit(
		t,
		ctx,
		repo,
		gitSignalAuthorEnv("Alice", "alice@example.invalid"),
		"commit",
		"-m",
		"test(repo): update a alone",
	)

	store := openGitSignalTestStore(t, ctx)
	seedStaticGitSignalEdge(t, ctx, store)

	summary, err := store.RefreshGitSignals(ctx, repo, GitSignalRefreshOptions{
		CommitLimit: 10,
		Now:         time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("refresh git signals: %v", err)
	}
	if summary.Commits != 3 || summary.Files != 2 || summary.CoChanges != 2 {
		t.Fatalf("summary = %#v", summary)
	}

	signals, err := store.GitSignals(ctx, GitSignalQuery{Path: "pkg/a.go", Limit: 1})
	if err != nil {
		t.Fatalf("query git signals: %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("signals = %#v", signals)
	}

	signal := signals[0]
	if signal.CommitCount != 3 || signal.AuthorCount != 2 ||
		signal.PrimaryAuthorEmail != "alice@example.invalid" ||
		signal.PrimaryAuthorCommits != 2 {
		t.Fatalf("unexpected file signal: %#v", signal)
	}
	if len(signal.TopAuthors) != 2 ||
		signal.TopAuthors[0].OwnershipPercentage != 66.7 ||
		signal.TopAuthors[1].OwnershipPercentage != 33.3 {
		t.Fatalf("unexpected ownership percentages: %#v", signal.TopAuthors)
	}
	if len(signal.CoChanges) != 1 || signal.CoChanges[0].RelatedPath != "pkg/b.go" ||
		signal.CoChanges[0].Count != 2 || signal.CoChanges[0].HiddenCoupling {
		t.Fatalf("unexpected co-change signal: %#v", signal.CoChanges)
	}

	reviewers, err := store.GitReviewerSuggestions(ctx, GitReviewerSuggestionQuery{
		Paths: []string{"pkg/a.go"},
		Limit: 2,
	})
	if err != nil {
		t.Fatalf("query reviewers: %v", err)
	}
	if len(reviewers) != 2 || reviewers[0].Email != "alice@example.invalid" ||
		reviewers[0].Score <= reviewers[1].Score ||
		len(reviewers[0].ScoreExplanation) != 3 {
		t.Fatalf("unexpected reviewers: %#v", reviewers)
	}

	cached, err := store.RefreshGitSignals(
		ctx,
		repo,
		GitSignalRefreshOptions{CommitLimit: 10},
	)
	if err != nil {
		t.Fatalf("cached git signal refresh: %v", err)
	}
	if cached.Stale || cached.Refreshed || cached.HeadCommit != summary.HeadCommit {
		t.Fatalf("unexpected cached summary: %#v", cached)
	}

	writeGitSignalFile(t, repo, "pkg/c.go", "package pkg\n\nfunc C() {}\n")
	runGitSignalGit(
		t,
		ctx,
		repo,
		gitSignalAuthorEnv("Carol", "carol@example.invalid"),
		"add",
		"pkg/c.go",
	)
	runGitSignalGit(
		t,
		ctx,
		repo,
		gitSignalAuthorEnv("Carol", "carol@example.invalid"),
		"commit",
		"-m",
		"test(repo): add c without refresh",
	)

	stale, err := store.GitSignalSummary(ctx, repo)
	if err != nil {
		t.Fatalf("query stale git signal summary: %v", err)
	}
	if !stale.Stale || stale.HeadCommit != summary.HeadCommit {
		t.Fatalf("unexpected stale summary: %#v", stale)
	}
}

func TestNormalizeGitSignalPathKeepsRenameContext(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		input string
		want  string
	}{
		{input: "pkg/{old.go => new.go}", want: "pkg/new.go"},
		{input: "{pkg => src/pkg}/app.go", want: "src/pkg/app.go"},
		{input: "dir/{old => new}/file.go", want: "dir/new/file.go"},
		{input: "old.go => new.go", want: "new.go"},
	} {
		got := normalizeGitSignalPath(test.input)
		if got != test.want {
			t.Fatalf("normalizeGitSignalPath(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestBuildGitSignalAggregatesSkipsOversizedCoChanges(t *testing.T) {
	t.Parallel()

	changes := make([]gitCommitChange, gitSignalCoChangePathLimit+1)
	for index := range changes {
		changes[index] = gitCommitChange{
			Path:      "pkg/file-" + strconv.Itoa(index) + ".go",
			Additions: 1,
		}
	}

	files, cochanges := buildGitSignalAggregates([]gitCommitSignal{
		{
			Hash:        "commit-1",
			AuthorName:  "Bulk",
			AuthorEmail: "bulk@example.invalid",
			WhenUTC:     "2026-05-27T12:00:00Z",
			Changes:     changes,
		},
	})
	if len(files) != len(changes) {
		t.Fatalf("files = %d, want %d", len(files), len(changes))
	}
	if len(cochanges) != 0 {
		t.Fatalf("cochanges = %d, want 0 for oversized commit", len(cochanges))
	}
}

func openGitSignalTestStore(t *testing.T, ctx context.Context) *Store {
	t.Helper()

	store, err := Open(ctx, filepath.Join(t.TempDir(), "code-intel.duckdb"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Fatalf("close store: %v", closeErr)
		}
	})

	return store
}

func newGitSignalRepo(t *testing.T, ctx context.Context) string {
	t.Helper()

	repo := t.TempDir()
	runGitSignalGit(t, ctx, repo, nil, "init", "--initial-branch", "main")
	runGitSignalGit(t, ctx, repo, nil, "config", "user.name", "Test User")
	runGitSignalGit(t, ctx, repo, nil, "config", "user.email", "test@example.invalid")
	runGitSignalGit(t, ctx, repo, nil, "config", "commit.gpgsign", "false")

	return repo
}

func writeGitSignalFile(t *testing.T, root, path, content string) {
	t.Helper()

	fullPath := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o700); err != nil {
		t.Fatalf("create fixture dir: %v", err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
}

func runGitSignalGit(
	t *testing.T,
	ctx context.Context,
	root string,
	env []string,
	args ...string,
) {
	t.Helper()

	gitPath, err := realgit.Resolve(ctx, "git")
	if err != nil {
		t.Fatalf("resolve git: %v", err)
	}

	command := exec.CommandContext(ctx, gitPath, args...)
	command.Dir = root
	command.Env = append(os.Environ(), env...)

	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func gitSignalAuthorEnv(name, email string) []string {
	return []string{
		"GIT_AUTHOR_NAME=" + name,
		"GIT_AUTHOR_EMAIL=" + email,
		"GIT_COMMITTER_NAME=" + name,
		"GIT_COMMITTER_EMAIL=" + email,
	}
}

func seedStaticGitSignalEdge(t *testing.T, ctx context.Context, store *Store) {
	t.Helper()

	for _, path := range []string{"pkg/a.go", "pkg/b.go"} {
		_, err := store.Database().ExecContext(
			ctx,
			`INSERT INTO code_files(
				path, language, content_hash, size_bytes, line_count, indexed_at_utc
			) VALUES (?, 'go', 'hash', 1, 1, '2026-05-27T12:00:00Z')`,
			path,
		)
		if err != nil {
			t.Fatalf("insert code file %s: %v", path, err)
		}
	}

	_, err := store.Database().ExecContext(
		ctx,
		`INSERT INTO code_edges(edge_id, edge_kind, path, target_path)
		VALUES ('edge-a-b', 'import', 'pkg/a.go', 'pkg/b.go')`,
	)
	if err != nil {
		t.Fatalf("insert code edge: %v", err)
	}
}
