// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/internal/celexpr"
	"blackcat.ca/coding-ethos/go/internal/realgit"
)

func (store *Store) RefreshDiffEditPatterns(
	ctx context.Context,
	root string,
) (int, error) {
	patterns, err := diffEditPatterns(ctx, root)
	if err != nil {
		return 0, err
	}

	if len(patterns) == 0 {
		return 0, nil
	}

	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin diff edit pattern write: %w", err)
	}
	defer rollbackUnlessCommitted(transaction)

	now := time.Now().UTC().Format(time.RFC3339)
	for _, pattern := range patterns {
		enriched, err := enrichDiffEditPattern(ctx, transaction, pattern)
		if err != nil {
			return 0, err
		}

		err = upsertDiffEditPattern(ctx, transaction, enriched, now)
		if err != nil {
			return 0, err
		}
	}

	err = transaction.Commit()
	if err != nil {
		return 0, fmt.Errorf("commit diff edit pattern write: %w", err)
	}

	return len(patterns), nil
}

func (store *Store) RepeatedDiffEditPatterns(
	ctx context.Context,
	query DiffEditPatternQuery,
) ([]DiffEditPattern, error) {
	rows, err := store.database.QueryContext(
		ctx,
		`SELECT diff_source, COALESCE(first_git_head, ''), COALESCE(last_git_head, ''),
			target_path, pattern_hash, COALESCE(removed_sha256, ''),
			COALESCE(added_sha256, ''), COALESCE(ast_chunk_id, ''),
			COALESCE(ast_language, ''), COALESCE(ast_node_kind, ''),
			COALESCE(ast_symbol_kind, ''), COALESCE(ast_symbol_name, ''),
			COALESCE(ast_symbol_path, ''), COALESCE(last_seen_utc, ''),
			COALESCE(hunk_header, ''), old_start, old_lines, new_start, new_lines,
			seen_count
		FROM diff_edit_patterns
		JOIN code_files ON code_files.path = diff_edit_patterns.target_path
		WHERE (? = '' OR diff_source = ?)
			AND (? = '' OR target_path = ?)
			AND COALESCE(code_files.deleted_at_utc, '') = ''
		ORDER BY seen_count DESC, last_seen_utc DESC, target_path
		LIMIT ?`,
		query.DiffSource,
		query.DiffSource,
		query.Path,
		query.Path,
		defaultQueryLimit(query.Limit),
	)
	if err != nil {
		return nil, fmt.Errorf("query repeated diff edit patterns: %w", err)
	}
	defer rows.Close()

	results := []DiffEditPattern{}
	for rows.Next() {
		var result DiffEditPattern

		err = rows.Scan(
			&result.DiffSource,
			&result.FirstGitHead,
			&result.LastGitHead,
			&result.TargetPath,
			&result.PatternHash,
			&result.RemovedSHA256,
			&result.AddedSHA256,
			&result.ASTChunkID,
			&result.ASTLanguage,
			&result.ASTNodeKind,
			&result.ASTSymbolKind,
			&result.ASTSymbolName,
			&result.ASTSymbolPath,
			&result.LastSeenUTC,
			&result.HunkHeader,
			&result.OldStart,
			&result.OldLines,
			&result.NewStart,
			&result.NewLines,
			&result.SeenCount,
		)
		if err != nil {
			return nil, fmt.Errorf("scan repeated diff edit pattern: %w", err)
		}

		results = append(results, result)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate repeated diff edit patterns: %w", err)
	}

	return results, nil
}

func diffEditPatterns(ctx context.Context, root string) ([]DiffEditPattern, error) {
	patterns := []DiffEditPattern{}
	gitHead, err := gitDiffOutput(ctx, root, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return patterns, nil
	}
	gitHead = strings.TrimSpace(gitHead)

	for _, source := range []struct {
		name string
		args []string
	}{
		{name: "worktree", args: []string{"diff", "--no-ext-diff", "-U0"}},
		{name: "staged", args: []string{"diff", "--cached", "--no-ext-diff", "-U0"}},
	} {
		diff, err := gitDiffOutput(ctx, root, source.args...)
		if err != nil {
			return nil, err
		}

		for _, hunk := range celexpr.ParseDiffHunks(diff, nil) {
			patterns = append(patterns, diffEditPattern(source.name, gitHead, hunk))
		}
	}

	return patterns, nil
}

func gitDiffOutput(ctx context.Context, root string, args ...string) (string, error) {
	command := realgit.Command(ctx, false, args...)
	command.Dir = root
	command.Env = realgit.CleanGitLocalEnv(os.Environ())
	command.Env = append(command.Env, "GIT_OPTIONAL_LOCKS=0")

	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}

	return string(output), nil
}

func diffEditPattern(
	source string,
	gitHead string,
	hunk celexpr.DiffHunkInput,
) DiffEditPattern {
	removedHash := diffLineHash(removedTexts(hunk))
	addedHash := diffLineHash(addedTexts(hunk))
	patternHash := diffLineHash([]string{
		source,
		hunk.File,
		fmt.Sprintf("%d", hunk.OldStart),
		fmt.Sprintf("%d", hunk.NewStart),
		removedHash,
	})

	return DiffEditPattern{
		DiffSource:    source,
		GitHead:       gitHead,
		TargetPath:    hunk.File,
		PatternHash:   patternHash,
		RemovedSHA256: removedHash,
		AddedSHA256:   addedHash,
		HunkHeader:    hunk.Header,
		OldStart:      hunk.OldStart,
		OldLines:      hunk.OldLines,
		NewStart:      hunk.NewStart,
		NewLines:      hunk.NewLines,
	}
}

func removedTexts(hunk celexpr.DiffHunkInput) []string {
	values := make([]string, 0, len(hunk.RemovedLines))
	for _, line := range hunk.RemovedLines {
		values = append(values, line.Text)
	}

	return values
}

func addedTexts(hunk celexpr.DiffHunkInput) []string {
	values := make([]string, 0, len(hunk.AddedLines))
	for _, line := range hunk.AddedLines {
		values = append(values, line.Text)
	}

	return values
}

func diffLineHash(lines []string) string {
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))

	return hex.EncodeToString(sum[:])
}

func enrichDiffEditPattern(
	ctx context.Context,
	transaction *sql.Tx,
	pattern DiffEditPattern,
) (DiffEditPattern, error) {
	line := pattern.NewStart
	if line <= 0 {
		line = pattern.OldStart
	}

	row := transaction.QueryRowContext(
		ctx,
		`SELECT chunk_id, code_chunks.language, node_kind, COALESCE(symbol_kind, ''),
			COALESCE(symbol_name, ''), COALESCE(symbol_path, '')
		FROM code_chunks
		JOIN code_files ON code_files.path = code_chunks.path
		WHERE code_chunks.path = ? AND start_line <= ? AND end_line >= ?
			AND COALESCE(code_files.deleted_at_utc, '') = ''
		ORDER BY (end_line - start_line) ASC, start_line DESC, chunk_id
		LIMIT 1`,
		pattern.TargetPath,
		line,
		line,
	)

	err := row.Scan(
		&pattern.ASTChunkID,
		&pattern.ASTLanguage,
		&pattern.ASTNodeKind,
		&pattern.ASTSymbolKind,
		&pattern.ASTSymbolName,
		&pattern.ASTSymbolPath,
	)
	if err == nil {
		return pattern, nil
	}

	if err == sql.ErrNoRows {
		return pattern, nil
	}

	return DiffEditPattern{}, fmt.Errorf("lookup AST chunk for diff edit pattern: %w", err)
}

func upsertDiffEditPattern(
	ctx context.Context,
	transaction *sql.Tx,
	pattern DiffEditPattern,
	now string,
) error {
	_, err := transaction.ExecContext(
		ctx,
		`INSERT INTO diff_edit_patterns(
			pattern_hash, diff_source, first_git_head, last_git_head, target_path,
			hunk_header, removed_sha256, added_sha256, old_start, old_lines,
			new_start, new_lines, ast_chunk_id, ast_language, ast_node_kind,
			ast_symbol_kind, ast_symbol_name, ast_symbol_path, first_seen_utc,
			last_seen_utc, seen_count
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?, 1)
		ON CONFLICT(pattern_hash) DO UPDATE SET
			last_git_head = excluded.last_git_head,
			added_sha256 = excluded.added_sha256,
			ast_chunk_id = excluded.ast_chunk_id,
			ast_language = excluded.ast_language,
			ast_node_kind = excluded.ast_node_kind,
			ast_symbol_kind = excluded.ast_symbol_kind,
			ast_symbol_name = excluded.ast_symbol_name,
			ast_symbol_path = excluded.ast_symbol_path,
			last_seen_utc = excluded.last_seen_utc,
			seen_count = seen_count + 1`,
		pattern.PatternHash,
		pattern.DiffSource,
		pattern.GitHead,
		pattern.GitHead,
		pattern.TargetPath,
		pattern.HunkHeader,
		pattern.RemovedSHA256,
		pattern.AddedSHA256,
		pattern.OldStart,
		pattern.OldLines,
		pattern.NewStart,
		pattern.NewLines,
		pattern.ASTChunkID,
		pattern.ASTLanguage,
		pattern.ASTNodeKind,
		pattern.ASTSymbolKind,
		pattern.ASTSymbolName,
		pattern.ASTSymbolPath,
		now,
		now,
	)
	if err != nil {
		return fmt.Errorf("upsert diff edit pattern %s: %w", pattern.PatternHash, err)
	}

	return nil
}
