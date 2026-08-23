// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"strconv"
	"strings"
	"time"
)

const migrationSourceCatalog = "migration_source"

type migrationQueryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func mergeAndVerifyMigrationTables(
	ctx context.Context,
	sourcePath string,
	destination *sql.DB,
	schema migrationSchema,
	repositoryRoot string,
) ([]StoreMigrationTable, error) {
	err := attachMigrationSource(ctx, destination, sourcePath)
	if err != nil {
		return nil, err
	}

	tables, migrationErr := mergeMigrationTables(
		ctx,
		destination,
		schema,
		repositoryRoot,
	)
	if migrationErr == nil {
		tables, migrationErr = verifyMigrationTables(ctx, destination, schema, tables)
	}

	err = errors.Join(migrationErr, detachMigrationSource(ctx, destination))
	if err != nil {
		return nil, err
	}

	return tables, nil
}

func attachMigrationSource(
	ctx context.Context,
	destination *sql.DB,
	sourcePath string,
) error {
	// #nosec G201 -- the canonical path is quoted as a DuckDB string literal.
	query := fmt.Sprintf(
		"ATTACH %s AS %s (READ_ONLY)",
		quoteMigrationLiteral(sourcePath),
		quoteMigrationIdentifier(migrationSourceCatalog),
	)

	_, err := destination.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("attach legacy migration source read-only: %w", err)
	}

	return nil
}

func detachMigrationSource(ctx context.Context, destination *sql.DB) error {
	const query = `DETACH "migration_source"`

	_, err := destination.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("detach legacy migration source: %w", err)
	}

	return nil
}

func mergeMigrationTables(
	ctx context.Context,
	destination *sql.DB,
	schema migrationSchema,
	repositoryRoot string,
) ([]StoreMigrationTable, error) {
	transaction, err := destination.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin code-intel store migration: %w", err)
	}
	defer rollbackUnlessCommitted(transaction)

	err = recordMigrationRepositoryIdentity(ctx, transaction, repositoryRoot)
	if err != nil {
		return nil, err
	}

	tables := make([]StoreMigrationTable, 0, len(migrationTableSpecs()))
	for _, spec := range migrationTableSpecs() {
		table, mergeErr := mergeMigrationTable(
			ctx,
			transaction,
			spec,
			schema[spec.name],
		)
		if mergeErr != nil {
			return nil, mergeErr
		}

		tables = append(tables, table)
	}

	err = transaction.Commit()
	if err != nil {
		return nil, fmt.Errorf("commit code-intel store migration: %w", err)
	}

	return tables, nil
}

func recordMigrationRepositoryIdentity(
	ctx context.Context,
	transaction *sql.Tx,
	repositoryRoot string,
) error {
	_, err := transaction.ExecContext(
		ctx,
		`INSERT OR REPLACE INTO schema_metadata(key, value)
		VALUES ('repository_identity', ?)`,
		repositoryRoot,
	)
	if err != nil {
		return fmt.Errorf("record migration repository identity: %w", err)
	}

	return nil
}

func mergeMigrationTable(
	ctx context.Context,
	destination *sql.Tx,
	spec migrationTableSpec,
	columns []migrationColumn,
) (StoreMigrationTable, error) {
	sourceTable := migrationSourceTable(spec.name)
	destinationTable := migrationDestinationTable(spec.name)

	err := validateMigrationTableKeys(
		ctx,
		destination,
		sourceTable,
		destinationTable,
		spec,
	)
	if err != nil {
		return StoreMigrationTable{}, err
	}

	baseline, err := inspectMigrationTable(
		ctx,
		destination,
		sourceTable,
		destinationTable,
		spec,
		columns,
	)
	if err != nil {
		return StoreMigrationTable{}, err
	}

	err = insertMissingMigrationRows(
		ctx,
		destination,
		sourceTable,
		destinationTable,
		spec,
		columns,
	)
	if err != nil {
		return StoreMigrationTable{}, err
	}

	return finishMigrationTable(
		ctx,
		destination,
		destinationTable,
		spec,
		columns,
		baseline,
	)
}

func validateMigrationTableKeys(
	ctx context.Context,
	destination *sql.Tx,
	sourceTable string,
	destinationTable string,
	spec migrationTableSpec,
) error {
	err := validateMigrationKeyUniqueness(ctx, destination, sourceTable, spec)
	if err != nil {
		return fmt.Errorf("validate source %s: %w", spec.name, err)
	}

	err = validateMigrationKeyUniqueness(ctx, destination, destinationTable, spec)
	if err != nil {
		return fmt.Errorf("validate destination %s: %w", spec.name, err)
	}

	return nil
}

type migrationTableBaseline struct {
	sourceDigest          string
	sourceRows            int64
	matchedRows           int64
	destinationRowsBefore int64
}

func inspectMigrationTable(
	ctx context.Context,
	destination *sql.Tx,
	sourceTable string,
	destinationTable string,
	spec migrationTableSpec,
	columns []migrationColumn,
) (migrationTableBaseline, error) {
	sourceRows, sourceDigest, err := migrationTableDigest(
		ctx,
		destination,
		sourceTable,
		spec,
		columns,
	)
	if err != nil {
		return migrationTableBaseline{}, err
	}

	destinationRowsBefore, err := migrationTableRowCount(
		ctx,
		destination,
		destinationTable,
	)
	if err != nil {
		return migrationTableBaseline{}, err
	}

	matchedRows, conflictRows, err := migrationDuplicateCounts(
		ctx,
		destination,
		sourceTable,
		destinationTable,
		spec,
		columns,
	)
	if err != nil {
		return migrationTableBaseline{}, err
	}

	if conflictRows != 0 {
		return migrationTableBaseline{}, fmt.Errorf(
			"%w: unequal duplicate row in %s",
			errStoreMigrationIntegrity,
			spec.name,
		)
	}

	return migrationTableBaseline{
		sourceDigest:          sourceDigest,
		sourceRows:            sourceRows,
		matchedRows:           matchedRows,
		destinationRowsBefore: destinationRowsBefore,
	}, nil
}

func finishMigrationTable(
	ctx context.Context,
	destination *sql.Tx,
	destinationTable string,
	spec migrationTableSpec,
	columns []migrationColumn,
	baseline migrationTableBaseline,
) (StoreMigrationTable, error) {
	destinationRows, destinationDigest, err := migrationTableDigest(
		ctx,
		destination,
		destinationTable,
		spec,
		columns,
	)
	if err != nil {
		return StoreMigrationTable{}, err
	}

	importedRows := destinationRows - baseline.destinationRowsBefore
	if baseline.sourceRows != importedRows+baseline.matchedRows {
		return StoreMigrationTable{}, fmt.Errorf(
			"%w: row accounting mismatch in %s",
			errStoreMigrationIntegrity,
			spec.name,
		)
	}

	return StoreMigrationTable{
		Table:              spec.name,
		SourceSHA256:       baseline.sourceDigest,
		DestinationSHA256:  destinationDigest,
		SourceRows:         baseline.sourceRows,
		ImportedRows:       importedRows,
		MatchedRows:        baseline.matchedRows,
		DestinationRows:    destinationRows,
		SourceRowsVerified: true,
	}, nil
}

func validateMigrationKeyUniqueness(
	ctx context.Context,
	database migrationQueryer,
	table string,
	spec migrationTableSpec,
) error {
	keys := quoteMigrationIdentifiers(spec.keyColumns)

	// #nosec G201 -- table and keys come from the validated schema inventory.
	query := fmt.Sprintf(
		"SELECT COUNT(*) FROM (SELECT %s FROM %s GROUP BY %s HAVING COUNT(*) > 1)",
		strings.Join(keys, ", "),
		table,
		strings.Join(keys, ", "),
	)

	var duplicates int64

	err := database.QueryRowContext(ctx, query).Scan(&duplicates)
	if err != nil {
		return fmt.Errorf("query duplicate migration keys: %w", err)
	}

	if duplicates != 0 {
		return fmt.Errorf(
			"%w: table has %d duplicate migration keys for %s",
			errStoreMigrationIntegrity,
			duplicates,
			strings.Join(spec.keyColumns, ","),
		)
	}

	return nil
}

func migrationDuplicateCounts(
	ctx context.Context,
	database migrationQueryer,
	sourceTable string,
	destinationTable string,
	spec migrationTableSpec,
	columns []migrationColumn,
) (int64, int64, error) {
	join := migrationKeyEquality("source", "destination", spec.keyColumns)
	equal := migrationColumnEquality("source", "destination", columns)

	// #nosec G201 -- identifiers come from the validated schema inventory.
	query := fmt.Sprintf(
		`SELECT COUNT(*),
		COALESCE(SUM(CASE WHEN %s THEN 0 ELSE 1 END), 0)
		FROM %s AS source
		JOIN %s AS destination ON %s`,
		equal,
		sourceTable,
		destinationTable,
		join,
	)

	var matchedRows, conflictRows int64

	err := database.QueryRowContext(ctx, query).Scan(&matchedRows, &conflictRows)
	if err != nil {
		return 0, 0, fmt.Errorf("compare duplicate migration rows: %w", err)
	}

	return matchedRows, conflictRows, nil
}

func insertMissingMigrationRows(
	ctx context.Context,
	destination *sql.Tx,
	sourceTable string,
	destinationTable string,
	spec migrationTableSpec,
	columns []migrationColumn,
) error {
	columnNames := migrationColumnIdentifiers(columns)
	selectedColumns := qualifiedMigrationIdentifiers("source", columnNames)
	join := migrationKeyEquality("source", "destination", spec.keyColumns)

	// #nosec G201 -- identifiers come from the validated schema inventory.
	query := fmt.Sprintf(
		`INSERT INTO %s(%s)
		SELECT %s FROM %s AS source
		WHERE NOT EXISTS (
			SELECT 1 FROM %s AS destination WHERE %s
		)`,
		destinationTable,
		strings.Join(columnNames, ", "),
		strings.Join(selectedColumns, ", "),
		sourceTable,
		destinationTable,
		join,
	)

	_, err := destination.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("insert migration rows into %s: %w", spec.name, err)
	}

	return nil
}

func verifyMigrationTables(
	ctx context.Context,
	destination *sql.DB,
	schema migrationSchema,
	tables []StoreMigrationTable,
) ([]StoreMigrationTable, error) {
	for index, spec := range migrationTableSpecs() {
		destinationRows, digest, err := migrationTableDigest(
			ctx,
			destination,
			migrationDestinationTable(spec.name),
			spec,
			schema[spec.name],
		)
		if err != nil {
			return nil, err
		}

		if destinationRows != tables[index].DestinationRows ||
			digest != tables[index].DestinationSHA256 {
			return nil, fmt.Errorf(
				"%w: destination table changed during verification: %s",
				errStoreMigrationIntegrity,
				spec.name,
			)
		}

		matchedRows, conflictRows, err := migrationDuplicateCounts(
			ctx,
			destination,
			migrationSourceTable(spec.name),
			migrationDestinationTable(spec.name),
			spec,
			schema[spec.name],
		)
		if err != nil {
			return nil, err
		}

		tables[index].SourceRowsVerified = conflictRows == 0 &&
			matchedRows == tables[index].SourceRows
		if !tables[index].SourceRowsVerified {
			return nil, fmt.Errorf(
				"%w: source rows were not preserved in %s",
				errStoreMigrationIntegrity,
				spec.name,
			)
		}
	}

	return tables, nil
}

func migrationTableRowCount(
	ctx context.Context,
	database migrationQueryer,
	table string,
) (int64, error) {
	// #nosec G201 -- table comes from the validated schema inventory.
	query := "SELECT COUNT(*) FROM " + table

	var count int64

	err := database.QueryRowContext(ctx, query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count migration table rows: %w", err)
	}

	return count, nil
}

func migrationTableDigest(
	ctx context.Context,
	database migrationQueryer,
	table string,
	spec migrationTableSpec,
	columns []migrationColumn,
) (int64, string, error) {
	rows, err := queryMigrationRows(ctx, database, table, spec, columns)
	if err != nil {
		return 0, "", err
	}
	defer rows.Close()

	digest := sha256.New()

	err = writeMigrationHash(digest, []byte(spec.name))
	if err != nil {
		return 0, "", err
	}

	count, err := hashMigrationRows(rows, columns, digest)
	if err != nil {
		return 0, "", err
	}

	return count, hex.EncodeToString(digest.Sum(nil)), nil
}

func queryMigrationRows(
	ctx context.Context,
	database migrationQueryer,
	table string,
	spec migrationTableSpec,
	columns []migrationColumn,
) (*sql.Rows, error) {
	// #nosec G201 -- table and columns come from the validated schema inventory.
	query := fmt.Sprintf(
		"SELECT %s FROM %s ORDER BY %s",
		strings.Join(migrationColumnIdentifiers(columns), ", "),
		table,
		strings.Join(quoteMigrationIdentifiers(spec.keyColumns), ", "),
	)

	rows, err := database.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query migration rows for %s: %w", spec.name, err)
	}

	return rows, nil
}

func hashMigrationRows(
	rows *sql.Rows,
	columns []migrationColumn,
	digest hash.Hash,
) (int64, error) {
	var count int64

	for rows.Next() {
		values, scanErr := scanMigrationValues(rows, len(columns))
		if scanErr != nil {
			return 0, fmt.Errorf("scan migration digest row: %w", scanErr)
		}

		count++

		err := writeMigrationHash(digest, migrationRowDigest(values))
		if err != nil {
			return 0, err
		}
	}

	rowsErr := rows.Err()
	if rowsErr != nil {
		return 0, fmt.Errorf("iterate migration digest rows: %w", rowsErr)
	}

	return count, nil
}

func migrationKeyEquality(left, right string, keys []string) string {
	equalities := make([]string, 0, len(keys))
	for _, key := range quoteMigrationIdentifiers(keys) {
		equalities = append(
			equalities,
			left+"."+key+" IS NOT DISTINCT FROM "+right+"."+key,
		)
	}

	return strings.Join(equalities, " AND ")
}

func migrationColumnEquality(
	left string,
	right string,
	columns []migrationColumn,
) string {
	return migrationKeyEquality(
		left,
		right,
		migrationColumnNames(columns),
	)
}

func migrationColumnNames(columns []migrationColumn) []string {
	names := make([]string, 0, len(columns))
	for _, column := range columns {
		names = append(names, column.name)
	}

	return names
}

func migrationSourceTable(name string) string {
	return strings.Join([]string{
		quoteMigrationIdentifier(migrationSourceCatalog),
		quoteMigrationIdentifier("main"),
		quoteMigrationIdentifier(name),
	}, ".")
}

func migrationDestinationTable(name string) string {
	return quoteMigrationIdentifier("main") + "." + quoteMigrationIdentifier(name)
}

func qualifiedMigrationIdentifiers(prefix string, identifiers []string) []string {
	qualified := make([]string, 0, len(identifiers))
	for _, identifier := range identifiers {
		qualified = append(qualified, prefix+"."+identifier)
	}

	return qualified
}

func scanMigrationValues(
	scanner interface{ Scan(dest ...any) error },
	count int,
) ([]any, error) {
	values := make([]any, count)
	targets := make([]any, count)

	for index := range values {
		targets[index] = &values[index]
	}

	err := scanner.Scan(targets...)
	if err != nil {
		return nil, fmt.Errorf("scan migration values: %w", err)
	}

	return values, nil
}

func migrationRowDigest(values []any) []byte {
	buffer := bytes.Buffer{}

	for _, value := range values {
		encoded := migrationValue(value)
		buffer.WriteString(strconv.Itoa(len(encoded)))
		buffer.WriteByte(':')
		buffer.WriteString(encoded)
		buffer.WriteByte(';')
	}

	digest := sha256.Sum256(buffer.Bytes())

	return digest[:]
}

func migrationValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case []byte:
		return "bytes:" + hex.EncodeToString(typed)
	case string:
		return "string:" + typed
	case bool:
		return "bool:" + strconv.FormatBool(typed)
	case int64:
		return "int64:" + strconv.FormatInt(typed, 10)
	case float64:
		return "float64:" + strconv.FormatFloat(typed, 'g', -1, 64)
	case time.Time:
		return "time:" + typed.UTC().Format(time.RFC3339Nano)
	default:
		return fmt.Sprintf("%T:%v", value, value)
	}
}

func migrationColumnIdentifiers(columns []migrationColumn) []string {
	return quoteMigrationIdentifiers(migrationColumnNames(columns))
}

func quoteMigrationIdentifiers(values []string) []string {
	identifiers := make([]string, 0, len(values))
	for _, value := range values {
		identifiers = append(identifiers, quoteMigrationIdentifier(value))
	}

	return identifiers
}

func writeMigrationHash(digest hash.Hash, payload []byte) error {
	_, err := digest.Write(payload)
	if err != nil {
		return fmt.Errorf("hash store migration rows: %w", err)
	}

	return nil
}
