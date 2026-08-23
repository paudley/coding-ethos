// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/internal/apperror"
)

var (
	errStoreMigrationInvalid = apperror.StaticError(
		"invalid code-intel store migration",
	)
	errStoreMigrationIntegrity = apperror.StaticError(
		"code-intel store migration integrity check failed",
	)
)

// StoreMigrationOptions identifies one legacy source and private destination.
type StoreMigrationOptions struct {
	RepositoryRoot  string
	SourcePath      string
	DestinationPath string
	ManifestPath    string
}

type preparedStoreMigration struct {
	repositoryRoot  string
	sourcePath      string
	destinationPath string
	manifestPath    string
}

// MigrateStore merges a read-only legacy code-intel store into a private store
// and writes a verified audit manifest.
func MigrateStore(
	ctx context.Context,
	options StoreMigrationOptions,
) (StoreMigrationResult, error) {
	started := time.Now().UTC()

	prepared, err := prepareStoreMigration(options, started)
	if err != nil {
		return StoreMigrationResult{}, err
	}

	sourceHash, err := storeMigrationFileSHA256(prepared.sourcePath)
	if err != nil {
		return StoreMigrationResult{}, err
	}

	manifest := newStoreMigrationManifest(
		prepared.repositoryRoot,
		prepared.sourcePath,
		prepared.destinationPath,
		sourceHash,
		started,
	)

	tables, err := executeStoreMigration(ctx, prepared)
	if err != nil {
		return StoreMigrationResult{}, err
	}

	manifest.Tables = tables

	manifest.SourceSHA256After, err = storeMigrationFileSHA256(prepared.sourcePath)
	if err != nil {
		return StoreMigrationResult{}, err
	}

	manifest.SourceUnchanged = manifest.SourceSHA256Before == manifest.SourceSHA256After
	if !manifest.SourceUnchanged {
		return StoreMigrationResult{}, fmt.Errorf(
			"%w: legacy source changed during migration",
			errStoreMigrationIntegrity,
		)
	}

	manifest.DestinationSHA256, err = storeMigrationFileSHA256(
		prepared.destinationPath,
	)
	if err != nil {
		return StoreMigrationResult{}, err
	}

	manifest.DestinationRowsVerified = migrationTablesVerified(tables)
	if !manifest.DestinationRowsVerified {
		return StoreMigrationResult{}, fmt.Errorf(
			"%w: destination row verification failed",
			errStoreMigrationIntegrity,
		)
	}

	manifest.CompletedAtUTC = time.Now().UTC().Format(time.RFC3339Nano)

	return writeAndVerifyStoreMigrationManifest(manifest, prepared.manifestPath)
}

func prepareStoreMigration(
	options StoreMigrationOptions,
	started time.Time,
) (preparedStoreMigration, error) {
	repositoryRoot, err := canonicalMigrationRepositoryRoot(options.RepositoryRoot)
	if err != nil {
		return preparedStoreMigration{}, err
	}

	sourcePath := strings.TrimSpace(options.SourcePath)
	if sourcePath == "" {
		sourcePath = DefaultDBPath(repositoryRoot)
	}

	sourcePath, err = canonicalMigrationFilePath(sourcePath)
	if err != nil {
		return preparedStoreMigration{}, err
	}

	destinationPath := strings.TrimSpace(options.DestinationPath)
	if destinationPath == "" {
		destinationPath = DefaultDBPath(ResolveStateRoot(repositoryRoot))
	}

	destinationPath, err = canonicalMigrationFilePath(destinationPath)
	if err != nil {
		return preparedStoreMigration{}, err
	}

	err = validateDistinctMigrationPaths(sourcePath, destinationPath)
	if err != nil {
		return preparedStoreMigration{}, err
	}

	manifestPath := strings.TrimSpace(options.ManifestPath)
	if manifestPath == "" {
		manifestPath = defaultStoreMigrationManifestPath(destinationPath, started)
	}

	manifestPath, err = canonicalMigrationFilePath(manifestPath)
	if err != nil {
		return preparedStoreMigration{}, err
	}

	err = validateNewMigrationAuditPaths(sourcePath, destinationPath, manifestPath)
	if err != nil {
		return preparedStoreMigration{}, err
	}

	return preparedStoreMigration{
		repositoryRoot:  repositoryRoot,
		sourcePath:      sourcePath,
		destinationPath: destinationPath,
		manifestPath:    manifestPath,
	}, nil
}

func executeStoreMigration(
	ctx context.Context,
	prepared preparedStoreMigration,
) ([]StoreMigrationTable, error) {
	source, sourceSchema, err := openValidatedMigrationSource(ctx, prepared)
	if err != nil {
		return nil, err
	}

	err = source.Close()
	if err != nil {
		return nil, fmt.Errorf("close validated source store: %w", err)
	}

	err = inspectExistingMigrationDestination(ctx, prepared, sourceSchema)
	if err != nil {
		return nil, err
	}

	destination, err := openValidatedMigrationDestination(
		ctx,
		prepared,
		sourceSchema,
	)
	if err != nil {
		return nil, err
	}
	defer destination.Close()

	tables, err := mergeAndVerifyMigrationTables(
		ctx,
		prepared.sourcePath,
		destination.Database(),
		sourceSchema,
		prepared.repositoryRoot,
	)
	if err != nil {
		return nil, err
	}

	_, err = destination.Database().ExecContext(ctx, "CHECKPOINT")
	if err != nil {
		return nil, fmt.Errorf("checkpoint migration destination: %w", err)
	}

	return tables, nil
}

func openValidatedMigrationSource(
	ctx context.Context,
	prepared preparedStoreMigration,
) (*Store, migrationSchema, error) {
	source, err := OpenReadOnly(ctx, prepared.sourcePath)
	if err != nil {
		return nil, nil, fmt.Errorf("open legacy migration source read-only: %w", err)
	}

	schema, err := validateMigrationSchema(ctx, source.Database())
	if err != nil {
		_ = source.Close()

		return nil, nil, fmt.Errorf("validate source migration schema: %w", err)
	}

	err = validateMigrationRepositoryIdentity(
		ctx,
		source.Database(),
		schema,
		prepared.repositoryRoot,
		prepared.sourcePath,
		migrationSourceStore,
	)
	if err != nil {
		_ = source.Close()

		return nil, nil, err
	}

	return source, schema, nil
}

func openValidatedMigrationDestination(
	ctx context.Context,
	prepared preparedStoreMigration,
	sourceSchema migrationSchema,
) (*Store, error) {
	destination, err := Open(ctx, prepared.destinationPath)
	if err != nil {
		return nil, fmt.Errorf("open migration destination: %w", err)
	}

	destinationSchema, err := validateMigrationSchema(ctx, destination.Database())
	if err != nil {
		_ = destination.Close()

		return nil, fmt.Errorf("validate destination migration schema: %w", err)
	}

	err = compareMigrationSchemas(sourceSchema, destinationSchema)
	if err != nil {
		_ = destination.Close()

		return nil, err
	}

	err = validateMigrationRepositoryIdentity(
		ctx,
		destination.Database(),
		destinationSchema,
		prepared.repositoryRoot,
		prepared.destinationPath,
		migrationDestinationStore,
	)
	if err != nil {
		_ = destination.Close()

		return nil, err
	}

	return destination, nil
}

func inspectExistingMigrationDestination(
	ctx context.Context,
	prepared preparedStoreMigration,
	sourceSchema migrationSchema,
) error {
	_, err := os.Stat(prepared.destinationPath)
	if os.IsNotExist(err) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("stat migration destination: %w", err)
	}

	destination, err := OpenReadOnly(ctx, prepared.destinationPath)
	if err != nil {
		return fmt.Errorf("inspect migration destination read-only: %w", err)
	}
	defer destination.Close()

	destinationSchema, err := validateMigrationSchema(ctx, destination.Database())
	if err != nil {
		return fmt.Errorf("validate existing destination schema: %w", err)
	}

	err = compareMigrationSchemas(sourceSchema, destinationSchema)
	if err != nil {
		return err
	}

	return validateMigrationRepositoryIdentity(
		ctx,
		destination.Database(),
		destinationSchema,
		prepared.repositoryRoot,
		prepared.destinationPath,
		migrationDestinationStore,
	)
}

func migrationTablesVerified(tables []StoreMigrationTable) bool {
	if len(tables) != len(migrationTableSpecs()) {
		return false
	}

	for _, table := range tables {
		if !table.SourceRowsVerified ||
			table.SourceRows != table.ImportedRows+table.MatchedRows {
			return false
		}
	}

	return true
}
