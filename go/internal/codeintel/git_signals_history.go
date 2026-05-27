// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//nolint:wsl_v5 // Git log parsing keeps adjacent parse state updates together.
package codeintel

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/internal/realgit"
)

var errMalformedGitSignalCommitHeader = errors.New("malformed git signal commit header")

func currentGitSignalHead(ctx context.Context, root string) (string, error) {
	gitPath, err := realgit.Resolve(ctx, "git")
	if err != nil {
		return "", fmt.Errorf("resolve git for git signals: %w", err)
	}

	headCommit, err := gitOutput(ctx, root, gitPath, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(headCommit), nil
}

func loadGitSignalCommits(
	ctx context.Context,
	root string,
	limit int,
) ([]gitCommitSignal, error) {
	gitPath, err := realgit.Resolve(ctx, "git")
	if err != nil {
		return nil, fmt.Errorf("resolve git for git signals: %w", err)
	}

	if limit <= 0 {
		limit = defaultGitSignalCommitLimit
	}

	args := []string{
		"log",
		"--date=iso-strict",
		"--numstat",
		"--format=format:" + gitSignalCommitPrefix +
			"%H" + gitSignalFieldSeparator +
			"%aN" + gitSignalFieldSeparator +
			"%aE" + gitSignalFieldSeparator +
			"%aI",
		"-n",
		strconv.Itoa(limit),
	}
	output, err := gitOutput(ctx, root, gitPath, args...)
	if err != nil {
		return nil, err
	}

	commits, err := parseGitSignalLog(output)
	if err != nil {
		return nil, err
	}

	return commits, nil
}

func loadGitSignalCommitsAfter(
	ctx context.Context,
	root string,
	indexedHead string,
) ([]gitCommitSignal, error) {
	gitPath, err := realgit.Resolve(ctx, "git")
	if err != nil {
		return nil, fmt.Errorf("resolve git for git signals: %w", err)
	}

	args := gitSignalLogArgs(indexedHead + "..HEAD")
	output, err := gitOutput(ctx, root, gitPath, args...)
	if err != nil {
		return nil, err
	}

	commits, err := parseGitSignalLog(output)
	if err != nil {
		return nil, err
	}

	return commits, nil
}

func gitSignalHeadIsAncestor(
	ctx context.Context,
	root string,
	ancestorHead string,
) (bool, error) {
	gitPath, err := realgit.Resolve(ctx, "git")
	if err != nil {
		return false, fmt.Errorf("resolve git for git signals: %w", err)
	}

	command := exec.CommandContext(
		ctx,
		gitPath,
		"merge-base",
		"--is-ancestor",
		ancestorHead,
		"HEAD",
	)
	command.Dir = root

	output, err := command.CombinedOutput()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}

	return false, fmt.Errorf(
		"git merge-base --is-ancestor %s HEAD: %w\n%s",
		ancestorHead,
		err,
		output,
	)
}

func gitSignalLogArgs(revision string) []string {
	args := []string{
		"log",
		"--date=iso-strict",
		"--numstat",
		"--format=format:" + gitSignalCommitPrefix +
			"%H" + gitSignalFieldSeparator +
			"%aN" + gitSignalFieldSeparator +
			"%aE" + gitSignalFieldSeparator +
			"%aI",
	}
	if revision != "" {
		args = append(args, revision)
	}

	return args
}

func gitOutput(
	ctx context.Context,
	root, gitPath string,
	args ...string,
) (string, error) {
	command := exec.CommandContext(ctx, gitPath, args...)
	command.Dir = root

	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, output)
	}

	return string(output), nil
}

func parseGitSignalLog(output string) ([]gitCommitSignal, error) {
	commits := []gitCommitSignal{}
	current := (*gitCommitSignal)(nil)

	for rawLine := range strings.Lines(output) {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		if strings.HasPrefix(rawLine, gitSignalCommitPrefix) {
			commit, err := parseGitSignalCommitHeader(rawLine)
			if err != nil {
				return nil, err
			}
			commits = append(commits, commit)
			current = &commits[len(commits)-1]

			continue
		}
		if current == nil {
			continue
		}

		change, ok := parseGitSignalNumstat(line)
		if ok {
			current.Changes = append(current.Changes, change)
		}
	}

	return commits, nil
}

func parseGitSignalCommitHeader(line string) (gitCommitSignal, error) {
	fields := strings.Split(
		strings.TrimPrefix(line, gitSignalCommitPrefix),
		gitSignalFieldSeparator,
	)
	if len(fields) != gitSignalHeaderFieldCount {
		return gitCommitSignal{}, fmt.Errorf(
			"%w: %q",
			errMalformedGitSignalCommitHeader,
			line,
		)
	}

	return gitCommitSignal{
		Hash:        fields[0],
		AuthorName:  fields[1],
		AuthorEmail: strings.ToLower(strings.TrimSpace(fields[2])),
		WhenUTC:     normalizeGitSignalTimestamp(fields[3]),
	}, nil
}

func parseGitSignalNumstat(line string) (gitCommitChange, bool) {
	fields := strings.Split(line, "\t")
	if len(fields) < 3 || fields[0] == "-" || fields[1] == "-" {
		return gitCommitChange{}, false
	}

	additions, addErr := strconv.Atoi(fields[0])
	deletions, delErr := strconv.Atoi(fields[1])
	if addErr != nil || delErr != nil {
		return gitCommitChange{}, false
	}

	path := normalizeGitSignalPath(fields[len(fields)-1])
	if path == "" {
		return gitCommitChange{}, false
	}

	return gitCommitChange{Path: path, Additions: additions, Deletions: deletions}, true
}

func normalizeGitSignalPath(path string) string {
	path = strings.TrimSpace(path)
	if strings.Contains(path, " => ") {
		path = normalizeGitSignalRenamePath(path)
	}

	return filepath.ToSlash(strings.Trim(path, "{}"))
}

func normalizeGitSignalRenamePath(path string) string {
	open := strings.Index(path, "{")
	closeIndex := strings.Index(path, "}")
	if open >= 0 && closeIndex > open {
		inside := path[open+1 : closeIndex]
		parts := strings.Split(inside, " => ")
		if len(parts) == gitSignalRenamePartCount {
			return path[:open] + parts[1] + path[closeIndex+1:]
		}
	}

	parts := strings.Split(path, " => ")
	if len(parts) == gitSignalRenamePartCount {
		return parts[1]
	}

	return path
}

func normalizeGitSignalTimestamp(value string) string {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return strings.TrimSpace(value)
	}

	return parsed.UTC().Format(time.RFC3339)
}

func buildGitSignalAggregates(
	commits []gitCommitSignal,
) (map[string]*gitFileAccumulator, map[string]*gitCoChangeAccumulator) {
	files := map[string]*gitFileAccumulator{}
	cochanges := map[string]*gitCoChangeAccumulator{}

	for _, commit := range commits {
		changedPaths := uniqueCommitPaths(commit.Changes)
		for _, change := range commit.Changes {
			file := gitSignalFile(files, change.Path)
			file.commits[commit.Hash] = true
			file.additions += change.Additions
			file.deletions += change.Deletions
			file.firstSeen = minNonEmptyTime(file.firstSeen, commit.WhenUTC)
			file.lastSeen = maxNonEmptyTime(file.lastSeen, commit.WhenUTC)

			author := gitSignalAuthor(file, commit.AuthorEmail, commit.AuthorName)
			author.commits++
			author.additions += change.Additions
			author.deletions += change.Deletions
			author.lastSeen = maxNonEmptyTime(author.lastSeen, commit.WhenUTC)
		}

		if len(changedPaths) <= gitSignalCoChangePathLimit {
			recordGitCoChanges(cochanges, changedPaths, commit.WhenUTC)
		}
	}

	return files, cochanges
}

func uniqueCommitPaths(changes []gitCommitChange) []string {
	seen := map[string]bool{}
	paths := []string{}
	for _, change := range changes {
		if seen[change.Path] {
			continue
		}
		seen[change.Path] = true
		paths = append(paths, change.Path)
	}
	slices.Sort(paths)

	return paths
}

func gitSignalFile(
	files map[string]*gitFileAccumulator,
	path string,
) *gitFileAccumulator {
	file := files[path]
	if file == nil {
		file = &gitFileAccumulator{
			authors: map[string]*gitAuthorAccumulator{},
			commits: map[string]bool{},
			path:    path,
		}
		files[path] = file
	}

	return file
}

func gitSignalAuthor(
	file *gitFileAccumulator,
	email string,
	name string,
) *gitAuthorAccumulator {
	if email == "" {
		email = "unknown"
	}

	author := file.authors[email]
	if author == nil {
		author = &gitAuthorAccumulator{name: name, email: email}
		file.authors[email] = author
	}

	return author
}

func recordGitCoChanges(
	cochanges map[string]*gitCoChangeAccumulator,
	paths []string,
	whenUTC string,
) {
	for leftIndex, left := range paths {
		for _, right := range paths[leftIndex+1:] {
			incrementGitCoChange(cochanges, left, right, whenUTC)
			incrementGitCoChange(cochanges, right, left, whenUTC)
		}
	}
}

func incrementGitCoChange(
	cochanges map[string]*gitCoChangeAccumulator,
	path string,
	relatedPath string,
	whenUTC string,
) {
	key := path + "\x00" + relatedPath
	cochange := cochanges[key]
	if cochange == nil {
		cochange = &gitCoChangeAccumulator{path: path, relatedPath: relatedPath}
		cochanges[key] = cochange
	}
	cochange.count++
	cochange.lastSeen = maxNonEmptyTime(cochange.lastSeen, whenUTC)
}

func sortedGitSignalKeys(files map[string]*gitFileAccumulator) []string {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	slices.Sort(paths)

	return paths
}

func sortedGitCoChangeKeys(cochanges map[string]*gitCoChangeAccumulator) []string {
	keys := make([]string, 0, len(cochanges))
	for key := range cochanges {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	return keys
}

func sortedAuthorAccumulators(
	authors map[string]*gitAuthorAccumulator,
) []*gitAuthorAccumulator {
	result := make([]*gitAuthorAccumulator, 0, len(authors))
	for _, author := range authors {
		result = append(result, author)
	}
	slices.SortFunc(result, func(left, right *gitAuthorAccumulator) int {
		if left.commits != right.commits {
			return right.commits - left.commits
		}
		if left.additions+left.deletions != right.additions+right.deletions {
			return (right.additions + right.deletions) -
				(left.additions + left.deletions)
		}

		return strings.Compare(left.email, right.email)
	})

	return result
}
