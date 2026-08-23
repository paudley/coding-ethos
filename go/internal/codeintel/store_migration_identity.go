// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type migrationStoreRole string

const (
	migrationSourceStore      migrationStoreRole = "source"
	migrationDestinationStore migrationStoreRole = "destination"
)

func canonicalMigrationRepositoryRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", fmt.Errorf("%w: repository root is required", errStoreMigrationInvalid)
	}

	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve store migration repository root: %w", err)
	}

	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("canonicalize store migration repository root: %w", err)
	}

	info, err := os.Stat(canonical)
	if err != nil {
		return "", fmt.Errorf("stat store migration repository root: %w", err)
	}

	if !info.IsDir() {
		return "", fmt.Errorf(
			"%w: repository root is not a directory: %s",
			errStoreMigrationInvalid,
			canonical,
		)
	}

	return filepath.Clean(canonical), nil
}

func canonicalMigrationFilePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("%w: database path is required", errStoreMigrationInvalid)
	}

	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve store migration database path: %w", err)
	}

	canonical, err := filepath.EvalSymlinks(absolute)
	if err == nil {
		return filepath.Clean(canonical), nil
	}

	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("canonicalize store migration database path: %w", err)
	}

	parent, parentErr := filepath.EvalSymlinks(filepath.Dir(absolute))
	if parentErr != nil {
		if !errors.Is(parentErr, os.ErrNotExist) {
			return "", fmt.Errorf("canonicalize migration database directory: %w", parentErr)
		}

		parent = filepath.Dir(absolute)
	}

	return filepath.Join(parent, filepath.Base(absolute)), nil
}

func validateDistinctMigrationPaths(sourcePath, destinationPath string) error {
	if sourcePath == destinationPath {
		return fmt.Errorf(
			"%w: source and destination are the same path",
			errStoreMigrationInvalid,
		)
	}

	sourceInfo, sourceErr := os.Stat(sourcePath)

	destinationInfo, destinationErr := os.Stat(destinationPath)
	if sourceErr == nil &&
		destinationErr == nil &&
		os.SameFile(sourceInfo, destinationInfo) {
		return fmt.Errorf(
			"%w: source and destination are the same file",
			errStoreMigrationInvalid,
		)
	}

	return nil
}

func validateMigrationRepositoryIdentity(
	ctx context.Context,
	database *sql.DB,
	schema migrationSchema,
	repositoryRoot string,
	databasePath string,
	role migrationStoreRole,
) error {
	identities, err := migrationRepositoryIdentities(ctx, database, schema)
	if err != nil {
		return err
	}

	metadataIdentity, found, err := migrationMetadataIdentity(ctx, database)
	if err != nil {
		return err
	}

	if found {
		identities = append(identities, metadataIdentity)
	}

	identities, err = canonicalMigrationIdentities(identities, repositoryRoot)
	if err != nil {
		return err
	}

	if len(identities) > 0 {
		return validateObservedMigrationIdentities(
			identities,
			repositoryRoot,
			role,
		)
	}

	return validateUnobservedMigrationIdentity(
		ctx,
		database,
		repositoryRoot,
		databasePath,
		role,
	)
}

func validateObservedMigrationIdentities(
	identities []string,
	repositoryRoot string,
	role migrationStoreRole,
) error {
	for _, identity := range identities {
		if identity != repositoryRoot {
			return fmt.Errorf(
				"%w: %s store repository identity %s does not match %s",
				errStoreMigrationIntegrity,
				role,
				identity,
				repositoryRoot,
			)
		}
	}

	return nil
}

func validateUnobservedMigrationIdentity(
	ctx context.Context,
	database *sql.DB,
	repositoryRoot string,
	databasePath string,
	role migrationStoreRole,
) error {
	rows, err := migrationDataRowCount(ctx, database)
	if err != nil {
		return err
	}

	if rows == 0 && role == migrationDestinationStore {
		return nil
	}

	legacyPath, err := canonicalMigrationFilePath(DefaultDBPath(repositoryRoot))
	if err != nil {
		return err
	}

	if role == migrationSourceStore && databasePath == legacyPath {
		return nil
	}

	return fmt.Errorf(
		"%w: cannot verify %s store repository identity from %d data rows",
		errStoreMigrationIntegrity,
		role,
		rows,
	)
}

func migrationRepositoryIdentities(
	ctx context.Context,
	database *sql.DB,
	schema migrationSchema,
) ([]string, error) {
	identities := []string{}

	for _, table := range []string{
		"traces",
		"proxy_sessions",
		"proxy_events",
		"code_health_snapshots",
	} {
		if !migrationSchemaHasColumn(schema, table, "repo_root") {
			continue
		}

		tableIdentities, err := migrationTableRepositoryIdentities(ctx, database, table)
		if err != nil {
			return nil, err
		}

		identities = append(identities, tableIdentities...)
	}

	return identities, nil
}

func migrationTableRepositoryIdentities(
	ctx context.Context,
	database *sql.DB,
	table string,
) ([]string, error) {
	// #nosec G201 -- table is selected from the fixed migration inventory.
	query := fmt.Sprintf(
		"SELECT DISTINCT repo_root FROM %s WHERE COALESCE(TRIM(repo_root), '') != ''",
		quoteMigrationIdentifier(table),
	)

	rows, err := database.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query repository identities from %s: %w", table, err)
	}
	defer rows.Close()

	identities := []string{}

	for rows.Next() {
		var identity string

		scanErr := rows.Scan(&identity)
		if scanErr != nil {
			return nil, fmt.Errorf("scan repository identity from %s: %w", table, scanErr)
		}

		identities = append(identities, identity)
	}

	rowsErr := rows.Err()
	if rowsErr != nil {
		return nil, fmt.Errorf("iterate repository identities from %s: %w", table, rowsErr)
	}

	return identities, nil
}

func migrationMetadataIdentity(
	ctx context.Context,
	database *sql.DB,
) (string, bool, error) {
	var identity string

	err := database.QueryRowContext(
		ctx,
		"SELECT value FROM schema_metadata WHERE key = 'repository_identity'",
	).Scan(&identity)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}

	if err != nil {
		return "", false, fmt.Errorf("query store repository identity: %w", err)
	}

	return identity, true, nil
}

func canonicalMigrationIdentities(
	identities []string,
	repositoryRoot string,
) ([]string, error) {
	canonical := []string{}

	for _, identity := range identities {
		identity = strings.TrimSpace(identity)
		if identity == "" {
			continue
		}

		if !filepath.IsAbs(identity) {
			identity = filepath.Join(repositoryRoot, identity)
		}

		resolved, err := filepath.Abs(identity)
		if err != nil {
			return nil, fmt.Errorf("resolve stored repository identity: %w", err)
		}

		evaluated, evalErr := filepath.EvalSymlinks(resolved)
		if evalErr == nil {
			resolved = evaluated
		}

		resolved = filepath.Clean(resolved)
		if !slices.Contains(canonical, resolved) {
			canonical = append(canonical, resolved)
		}
	}

	return canonical, nil
}

func migrationDataRowCount(ctx context.Context, database *sql.DB) (int64, error) {
	var total int64

	for _, spec := range migrationTableSpecs() {
		if spec.name == "schema_metadata" {
			continue
		}

		query := "SELECT COUNT(*) FROM " + quoteMigrationIdentifier(spec.name)

		var count int64

		err := database.QueryRowContext(ctx, query).Scan(&count)
		if err != nil {
			return 0, fmt.Errorf("count repository identity rows in %s: %w", spec.name, err)
		}

		total += count
	}

	return total, nil
}

func migrationSchemaHasColumn(
	schema migrationSchema,
	table string,
	column string,
) bool {
	for _, candidate := range schema[table] {
		if candidate.name == column {
			return true
		}
	}

	return false
}
