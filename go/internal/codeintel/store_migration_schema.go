// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

type migrationTableSpec struct {
	name              string
	keyColumns        []string
	logicalPrimaryKey bool
}

type migrationColumn struct {
	name       string
	columnType string
	notNull    bool
	primaryKey bool
}

type migrationSchema map[string][]migrationColumn

func migrationTableSpecs() []migrationTableSpec {
	return []migrationTableSpec{
		{name: "code_intel_events", keyColumns: []string{"event_id"}},
		{name: "schema_metadata", keyColumns: []string{"key"}},
		{name: "traces", keyColumns: []string{"trace_id"}},
		{name: "findings", keyColumns: []string{"finding_id"}},
		{name: "finding_occurrences", keyColumns: []string{"trace_id", "ordinal"}},
		{name: "hook_events", keyColumns: []string{"trace_id"}},
		{name: "hook_decisions", keyColumns: []string{"trace_id", "ordinal"}},
		{name: "hook_targets", keyColumns: []string{"trace_id", "ordinal"}},
		{name: "hook_reviews", keyColumns: []string{"review_id"}},
		{name: "proxy_sessions", keyColumns: []string{"session_id"}},
		{name: "proxy_events", keyColumns: []string{"event_id"}},
		{name: "proxy_transforms", keyColumns: []string{"event_id", "ordinal"}},
		{name: "code_files", keyColumns: []string{"path"}},
		{name: "code_delete_intents", keyColumns: []string{"intent_id"}},
		{name: "code_chunks", keyColumns: []string{"chunk_id"}},
		{name: "code_edges", keyColumns: []string{"edge_id"}},
		{name: "diff_edit_patterns", keyColumns: []string{"pattern_hash"}},
		{name: "ast_finding_links", keyColumns: []string{"link_id"}},
		{
			name:              "lsh_bands",
			keyColumns:        []string{"chunk_id", "band_index"},
			logicalPrimaryKey: true,
		},
		{name: "git_signal_metadata", keyColumns: []string{"key"}},
		{name: "git_signal_commits", keyColumns: []string{"commit_hash"}},
		{name: "git_file_signals", keyColumns: []string{"path"}},
		{name: "git_file_authors", keyColumns: []string{"path", "author_email"}},
		{name: "git_cochanges", keyColumns: []string{"path", "related_path"}},
		{name: "code_health_snapshots", keyColumns: []string{"snapshot_id"}},
		{name: "code_health_targets", keyColumns: []string{"snapshot_id", "path"}},
		{
			name:       "code_health_evidence",
			keyColumns: []string{"snapshot_id", "path", "ordinal"},
		},
		{name: "code_health_coverage", keyColumns: []string{"path"}},
		{name: "decisions", keyColumns: []string{"decision_id"}},
		{name: "decision_links", keyColumns: []string{"decision_id", "ordinal"}},
		{name: "sarif_runs", keyColumns: []string{"sarif_run_id"}},
		{name: "sarif_results", keyColumns: []string{"sarif_result_id"}},
		{name: "remediations", keyColumns: []string{"remediation_id"}},
		{
			name:       "remediation_occurrences",
			keyColumns: []string{"trace_id", "ordinal"},
		},
		{name: "remediation_events", keyColumns: []string{"event_id"}},
		{name: "remediation_outcomes", keyColumns: []string{"outcome_id"}},
		{name: "embedding_records", keyColumns: []string{"embedding_id"}},
		{
			name:              "code_intel_fts",
			keyColumns:        []string{"fts_id"},
			logicalPrimaryKey: true,
		},
		{
			name:              "code_intel_search_terms",
			keyColumns:        []string{"fts_id", "term"},
			logicalPrimaryKey: true,
		},
	}
}

func validateMigrationSchema(
	ctx context.Context,
	database *sql.DB,
) (migrationSchema, error) {
	err := validateMigrationSchemaVersion(ctx, database)
	if err != nil {
		return nil, err
	}

	err = validateMigrationTableInventory(ctx, database)
	if err != nil {
		return nil, err
	}

	schema := make(migrationSchema, len(migrationTableSpecs()))
	for _, spec := range migrationTableSpecs() {
		columns, err := readMigrationColumns(ctx, database, spec)
		if err != nil {
			return nil, err
		}

		schema[spec.name] = columns
	}

	return schema, nil
}

func validateMigrationSchemaVersion(ctx context.Context, database *sql.DB) error {
	var rawVersion string

	err := database.QueryRowContext(
		ctx,
		"SELECT value FROM schema_metadata WHERE key = 'schema_version'",
	).Scan(&rawVersion)
	if err != nil {
		return fmt.Errorf("read code-intel migration schema version: %w", err)
	}

	version, err := strconv.Atoi(rawVersion)
	if err != nil {
		return fmt.Errorf("parse code-intel migration schema version %q: %w", rawVersion, err)
	}

	if version != schemaVersion {
		return fmt.Errorf(
			"%w: schema version is %d, expected %d",
			errStoreMigrationIntegrity,
			version,
			schemaVersion,
		)
	}

	return nil
}

func validateMigrationTableInventory(ctx context.Context, database *sql.DB) error {
	rows, err := database.QueryContext(
		ctx,
		`SELECT table_name
		FROM information_schema.tables
		WHERE table_schema = 'main' AND table_type = 'BASE TABLE'
		ORDER BY table_name`,
	)
	if err != nil {
		return fmt.Errorf("query code-intel migration table inventory: %w", err)
	}
	defer rows.Close()

	actual := []string{}

	for rows.Next() {
		var name string

		scanErr := rows.Scan(&name)
		if scanErr != nil {
			return fmt.Errorf("scan code-intel migration table: %w", scanErr)
		}

		actual = append(actual, name)
	}

	rowsErr := rows.Err()
	if rowsErr != nil {
		return fmt.Errorf("iterate code-intel migration tables: %w", rowsErr)
	}

	expected := make([]string, 0, len(migrationTableSpecs()))
	for _, spec := range migrationTableSpecs() {
		expected = append(expected, spec.name)
	}

	slices.Sort(expected)

	if !slices.Equal(actual, expected) {
		return fmt.Errorf(
			"%w: table inventory mismatch: got %s, expected %s",
			errStoreMigrationIntegrity,
			strings.Join(actual, ","),
			strings.Join(expected, ","),
		)
	}

	return nil
}

func readMigrationColumns(
	ctx context.Context,
	database *sql.DB,
	spec migrationTableSpec,
) ([]migrationColumn, error) {
	query := fmt.Sprintf("PRAGMA table_info(%s)", quoteMigrationLiteral(spec.name))

	rows, err := database.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query migration schema for %s: %w", spec.name, err)
	}
	defer rows.Close()

	columns := []migrationColumn{}

	for rows.Next() {
		column, scanErr := scanMigrationColumn(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan migration schema for %s: %w", spec.name, scanErr)
		}

		columns = append(columns, column)
	}

	rowsErr := rows.Err()
	if rowsErr != nil {
		return nil, fmt.Errorf("iterate migration schema for %s: %w", spec.name, rowsErr)
	}

	err = validateMigrationKey(spec, columns)
	if err != nil {
		return nil, err
	}

	return columns, nil
}

func scanMigrationColumn(rows *sql.Rows) (migrationColumn, error) {
	var (
		column       migrationColumn
		ordinal      int
		defaultValue sql.NullString
	)

	err := rows.Scan(
		&ordinal,
		&column.name,
		&column.columnType,
		&column.notNull,
		&defaultValue,
		&column.primaryKey,
	)
	if err != nil {
		return migrationColumn{}, fmt.Errorf("scan migration column: %w", err)
	}

	return column, nil
}

func validateMigrationKey(spec migrationTableSpec, columns []migrationColumn) error {
	primaryKey := []string{}
	columnNames := make([]string, 0, len(columns))

	for _, column := range columns {
		columnNames = append(columnNames, column.name)
		if column.primaryKey {
			primaryKey = append(primaryKey, column.name)
		}
	}

	if spec.logicalPrimaryKey {
		if !keyColumnsExist(columnNames, spec.keyColumns) {
			return fmt.Errorf(
				"%w: logical key for %s is absent",
				errStoreMigrationIntegrity,
				spec.name,
			)
		}

		return nil
	}

	if !slices.Equal(primaryKey, spec.keyColumns) {
		return fmt.Errorf(
			"%w: primary key mismatch for %s: got %s, expected %s",
			errStoreMigrationIntegrity,
			spec.name,
			strings.Join(primaryKey, ","),
			strings.Join(spec.keyColumns, ","),
		)
	}

	return nil
}

func keyColumnsExist(columns, keys []string) bool {
	for _, key := range keys {
		if !slices.Contains(columns, key) {
			return false
		}
	}

	return true
}

func compareMigrationSchemas(source, destination migrationSchema) error {
	for _, spec := range migrationTableSpecs() {
		if !slices.Equal(source[spec.name], destination[spec.name]) {
			return fmt.Errorf(
				"%w: source and destination schema differ for %s",
				errStoreMigrationIntegrity,
				spec.name,
			)
		}
	}

	return nil
}

func quoteMigrationLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func quoteMigrationIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
