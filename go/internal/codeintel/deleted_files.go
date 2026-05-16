// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/internal/realgit"
)

func (store *Store) MarkMissingCodeFilesDeleted(
	ctx context.Context,
	root string,
	paths []string,
) ([]string, error) {
	scopes, err := deletionScopes(root, paths)
	if err != nil {
		return nil, err
	}

	activePaths, _ := gitTrackedAndUnignoredPaths(ctx, root)
	deletedPaths := gitDeletedPaths(ctx, root)

	indexedPaths, err := store.activeCodeFilePaths(ctx, "deletion")
	if err != nil {
		return nil, err
	}

	deleted, err := missingIndexedCodeFiles(
		root,
		indexedPaths,
		scopes,
		activePaths,
		deletedPaths,
	)
	if err != nil {
		return nil, err
	}

	if len(deleted) == 0 {
		return deleted, nil
	}

	deletedAt := time.Now().UTC().Format(time.RFC3339)

	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin deleted code file refresh: %w", err)
	}
	defer rollbackUnlessCommitted(transaction)

	for _, path := range deleted {
		err = markCodeFileDeleted(ctx, transaction, path, deletedAt)
		if err != nil {
			return nil, err
		}
	}

	err = transaction.Commit()
	if err != nil {
		return nil, fmt.Errorf("commit deleted code file refresh: %w", err)
	}

	return deleted, nil
}

func (store *Store) activeCodeFilePaths(
	ctx context.Context,
	operation string,
) ([]string, error) {
	rows, err := store.database.QueryContext(
		ctx,
		`SELECT path
		FROM code_files
		WHERE COALESCE(deleted_at_utc, '') = ''
		ORDER BY path`,
	)
	if err != nil {
		return nil, fmt.Errorf("query active code files for %s refresh: %w", operation, err)
	}
	defer rows.Close()

	paths := []string{}

	for rows.Next() {
		var path string

		err = rows.Scan(&path)
		if err != nil {
			return nil, fmt.Errorf("scan active code file for %s refresh: %w", operation, err)
		}

		paths = append(paths, path)
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate active code files for %s refresh: %w", operation, err)
	}

	return paths, nil
}

func missingIndexedCodeFiles(
	root string,
	indexedPaths []string,
	scopes []string,
	activePaths map[string]bool,
	deletedPaths map[string]bool,
) ([]string, error) {
	deleted := []string{}

	for _, path := range indexedPaths {
		if !pathInDeletionScopes(path, scopes) {
			continue
		}

		missing, missingErr := indexedPathMissing(
			root,
			path,
			activePaths,
			deletedPaths,
		)
		if missingErr != nil {
			return nil, missingErr
		}

		if missing {
			deleted = append(deleted, path)
		}
	}

	return deleted, nil
}

func indexedPathMissing(
	root string,
	path string,
	activePaths map[string]bool,
	deletedPaths map[string]bool,
) (bool, error) {
	if activePaths[path] && !deletedPaths[path] {
		return false, nil
	}

	_, err := os.Stat(filepath.Join(root, filepath.FromSlash(path)))
	if err == nil {
		return false, nil
	}

	if !os.IsNotExist(err) {
		return false, fmt.Errorf("stat indexed code file %q: %w", path, err)
	}

	return true, nil
}

func gitDeletedPaths(ctx context.Context, root string) map[string]bool {
	command := realgit.Command(ctx, false, "-C", root, "ls-files", "-d", "-z")
	command.Env = realgit.CleanGitLocalEnv(os.Environ())

	output, err := command.Output()
	if err != nil {
		return nil
	}

	deleted := map[string]bool{}

	for rawPath := range bytes.SplitSeq(output, []byte{0}) {
		if len(rawPath) == 0 {
			continue
		}

		deleted[filepath.ToSlash(string(rawPath))] = true
	}

	return deleted
}

func (store *Store) MarkIgnoredCodeFilesDeleted(
	ctx context.Context,
	ignoredPath func(path string) bool,
) ([]string, error) {
	paths, err := store.activeCodeFilePaths(ctx, "ignored")
	if err != nil {
		return nil, err
	}

	ignored := []string{}

	for _, path := range paths {
		if ignoredPath(path) {
			ignored = append(ignored, path)
		}
	}

	if len(ignored) == 0 {
		return ignored, nil
	}

	deletedAt := time.Now().UTC().Format(time.RFC3339)

	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin ignored code file refresh: %w", err)
	}
	defer rollbackUnlessCommitted(transaction)

	for _, path := range ignored {
		err = markCodeFileInactive(ctx, transaction, path, deletedAt, "ignored")
		if err != nil {
			return nil, err
		}
	}

	err = transaction.Commit()
	if err != nil {
		return nil, fmt.Errorf("commit ignored code file refresh: %w", err)
	}

	return ignored, nil
}

func markCodeFileDeleted(
	ctx context.Context,
	transaction *sql.Tx,
	path string,
	deletedAt string,
) error {
	return markCodeFileInactive(ctx, transaction, path, deletedAt, "deleted")
}

func markCodeFileInactive(
	ctx context.Context,
	transaction *sql.Tx,
	path string,
	inactiveAt string,
	reason string,
) error {
	_, err := transaction.ExecContext(
		ctx,
		`UPDATE code_files
		SET deleted_at_utc = ?, stale_reason = ?
		WHERE path = ?`,
		inactiveAt,
		reason,
		path,
	)
	if err != nil {
		return fmt.Errorf("mark code file %s %q: %w", reason, path, err)
	}

	chunkIDs, err := codeChunkIDsForPath(ctx, transaction, path)
	if err != nil {
		return err
	}

	err = deleteEmbeddingRecordsForCodeChunks(ctx, transaction, chunkIDs)
	if err != nil {
		return err
	}

	_, err = transaction.ExecContext(
		ctx,
		`DELETE FROM code_intel_fts
		WHERE kind = 'code_chunk' AND path = ?`,
		path,
	)
	if err != nil {
		return fmt.Errorf("remove inactive code file FTS rows %q: %w", path, err)
	}

	return nil
}

func codeChunkIDsForPath(
	ctx context.Context,
	transaction *sql.Tx,
	path string,
) ([]any, error) {
	rows, err := transaction.QueryContext(
		ctx,
		`SELECT chunk_id
		FROM code_chunks
		WHERE path = ?`,
		path,
	)
	if err != nil {
		return nil, fmt.Errorf("query code chunk IDs for inactive file %q: %w", path, err)
	}
	defer rows.Close()

	chunkIDs := []any{}

	for rows.Next() {
		var chunkID string

		err = rows.Scan(&chunkID)
		if err != nil {
			return nil, fmt.Errorf("scan code chunk ID for inactive file %q: %w", path, err)
		}

		chunkIDs = append(chunkIDs, chunkID)
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate code chunk IDs for inactive file %q: %w", path, err)
	}

	return chunkIDs, nil
}

func deletionScopes(root string, paths []string) ([]string, error) {
	if len(paths) == 0 {
		paths = []string{"."}
	}

	scopes := make([]string, 0, len(paths))
	for _, inputPath := range paths {
		scope, err := deletionScope(root, inputPath)
		if err != nil {
			return nil, err
		}

		scopes = append(scopes, scope)
	}

	return scopes, nil
}

func deletionScope(root, inputPath string) (string, error) {
	path := strings.TrimSpace(inputPath)
	if path == "" {
		path = "."
	}

	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}

	relative, err := filepath.Rel(root, path)
	if err != nil {
		return "", fmt.Errorf("relativize deletion refresh scope %q: %w", inputPath, err)
	}

	scope := filepath.ToSlash(filepath.Clean(relative))
	if scope == "." {
		return "", nil
	}

	return strings.TrimSuffix(scope, "/"), nil
}

func pathInDeletionScopes(path string, scopes []string) bool {
	for _, scope := range scopes {
		if scope == "" || path == scope || strings.HasPrefix(path, scope+"/") {
			return true
		}
	}

	return false
}

func pathHasSkippedDir(path string) bool {
	return slices.ContainsFunc(
		strings.Split(filepath.ToSlash(path), "/"),
		func(segment string) bool {
			return shouldSkipDir(segment) || segment == "coding-ethos"
		},
	)
}
