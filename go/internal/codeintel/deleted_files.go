// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
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

	rows, err := store.database.QueryContext(
		ctx,
		`SELECT path
		FROM code_files
		WHERE COALESCE(deleted_at_utc, '') = ''
		ORDER BY path`,
	)
	if err != nil {
		return nil, fmt.Errorf("query active code files for deletion refresh: %w", err)
	}
	defer rows.Close()

	deleted := []string{}
	for rows.Next() {
		var path string

		err = rows.Scan(&path)
		if err != nil {
			return nil, fmt.Errorf("scan active code file for deletion refresh: %w", err)
		}

		if !pathInDeletionScopes(path, scopes) {
			continue
		}

		_, err = os.Stat(filepath.Join(root, filepath.FromSlash(path)))
		if err == nil {
			continue
		}

		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("stat indexed code file %q: %w", path, err)
		}

		deleted = append(deleted, path)
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate active code files for deletion refresh: %w", err)
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

func (store *Store) MarkIgnoredCodeFilesDeleted(
	ctx context.Context,
	ignoredPath func(path string) bool,
) ([]string, error) {
	rows, err := store.database.QueryContext(
		ctx,
		`SELECT path
		FROM code_files
		WHERE COALESCE(deleted_at_utc, '') = ''
		ORDER BY path`,
	)
	if err != nil {
		return nil, fmt.Errorf("query active code files for ignored refresh: %w", err)
	}
	defer rows.Close()

	ignored := []string{}
	for rows.Next() {
		var path string

		err = rows.Scan(&path)
		if err != nil {
			return nil, fmt.Errorf("scan active code file for ignored refresh: %w", err)
		}

		if ignoredPath(path) {
			ignored = append(ignored, path)
		}
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate active code files for ignored refresh: %w", err)
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

	return nil
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

func deletionScope(root string, inputPath string) (string, error) {
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
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if shouldSkipDir(part) {
			return true
		}
	}

	return false
}
