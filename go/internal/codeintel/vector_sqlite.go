// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	// Register sqlite-vec functions for SQLiteVectorIndex connections.
	_ "modernc.org/sqlite/vec"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/evidence"
)

const (
	sqliteVectorMetaTable = "vector_embeddings"
	sqliteVectorStoreMode = 0o700
	vectorBackendName     = "sqlite-vec"
	filteredSearchFactor  = 20
)

type SQLiteVectorIndex struct {
	database *sql.DB
}

type existingVectorMetadata struct {
	Found        bool
	OldDimension int
	RowID        int64
}

func NewSQLiteVectorIndex(
	ctx context.Context,
	path string,
) (*SQLiteVectorIndex, error) {
	if strings.TrimSpace(path) == "" {
		return nil, apperror.StaticError("SQLite vector path is required")
	}

	inlineErr0 := os.MkdirAll(filepath.Dir(path), sqliteVectorStoreMode)
	if inlineErr0 != nil {
		return nil, fmt.Errorf("create SQLite vector store dir: %w", inlineErr0)
	}

	database, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open SQLite vector store: %w", err)
	}

	index := &SQLiteVectorIndex{database: database}

	inlineErr1 := index.migrate(ctx)
	if inlineErr1 != nil {
		_ = database.Close()

		return nil, inlineErr1
	}

	return index, nil
}

func (index *SQLiteVectorIndex) UpsertEmbedding(
	ctx context.Context,
	record evidence.VectorRecord,
) error {
	record = normalizeVectorRecord(record)

	inlineErr2 := validateVectorRecord(record)
	if inlineErr2 != nil {
		return inlineErr2
	}

	metadata, err := json.Marshal(record.Metadata)
	if err != nil {
		return fmt.Errorf("marshal vector metadata %q: %w", record.ID, err)
	}

	inlineErr3 := index.ensureDimensionTable(ctx, record.Dimension)
	if inlineErr3 != nil {
		return inlineErr3
	}

	transaction, err := index.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("start SQLite vector transaction: %w", err)
	}

	defer func() {
		rollbackSQLiteVectorTx(transaction)
	}()

	rowID, oldDimension, found, err := existingVectorRow(
		ctx,
		transaction,
		record.ID,
		record.ModelID,
	)
	if err != nil {
		return err
	}

	rowID, err = upsertVectorMetadata(
		ctx,
		transaction,
		existingVectorMetadata{
			Found:        found,
			OldDimension: oldDimension,
			RowID:        rowID,
		},
		record,
		string(metadata),
	)
	if err != nil {
		return err
	}

	inlineErr4 := insertVecRow(ctx, transaction, rowID, record.Dimension, record.Vector)
	if inlineErr4 != nil {
		return inlineErr4
	}

	inlineErr5 := transaction.Commit()
	if inlineErr5 != nil {
		return fmt.Errorf("commit SQLite vector upsert %q: %w", record.ID, inlineErr5)
	}

	return nil
}

func updateVectorMetadata(
	ctx context.Context,
	transaction *sql.Tx,
	rowID int64,
	oldDimension int,
	record evidence.VectorRecord,
	metadata string,
) error {
	err := deleteVectorRow(ctx, transaction, rowID, oldDimension)
	if err != nil {
		return err
	}

	_, err = transaction.ExecContext(
		ctx,
		`UPDATE vector_embeddings
			SET collection = ?, input_kind = ?, text = ?, dimension = ?,
				metadata_json = ?, schema_version = ?
			WHERE rowid = ?`,
		record.Collection,
		record.InputKind,
		record.Text,
		record.Dimension,
		metadata,
		record.SchemaVersion,
		rowID,
	)
	if err != nil {
		return fmt.Errorf("update SQLite vector metadata %q: %w", record.ID, err)
	}

	return nil
}

func upsertVectorMetadata(
	ctx context.Context,
	transaction *sql.Tx,
	existing existingVectorMetadata,
	record evidence.VectorRecord,
	metadata string,
) (int64, error) {
	if existing.Found {
		err := updateVectorMetadata(
			ctx,
			transaction,
			existing.RowID,
			existing.OldDimension,
			record,
			metadata,
		)
		if err != nil {
			return 0, err
		}

		return existing.RowID, nil
	}

	result, err := transaction.ExecContext(
		ctx,
		`INSERT INTO vector_embeddings(
			id, collection, model_id, input_kind, text, dimension,
			metadata_json, schema_version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID,
		record.Collection,
		record.ModelID,
		record.InputKind,
		record.Text,
		record.Dimension,
		metadata,
		record.SchemaVersion,
	)
	if err != nil {
		return 0, fmt.Errorf("insert SQLite vector metadata %q: %w", record.ID, err)
	}

	rowID, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read SQLite vector rowid %q: %w", record.ID, err)
	}

	return rowID, nil
}

func (index *SQLiteVectorIndex) DeleteEmbedding(
	ctx context.Context,
	recordID string,
	modelID string,
) error {
	transaction, err := index.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("start SQLite vector delete transaction: %w", err)
	}

	defer func() {
		rollbackSQLiteVectorTx(transaction)
	}()

	rowID, dimension, found, err := existingVectorRow(
		ctx,
		transaction,
		strings.TrimSpace(recordID),
		strings.TrimSpace(modelID),
	)
	if err != nil {
		return err
	}

	if !found {
		return nil
	}

	inlineErr6 := deleteVectorRow(ctx, transaction, rowID, dimension)
	if inlineErr6 != nil {
		return inlineErr6
	}

	_, inlineErrB := transaction.ExecContext(
		ctx,
		"DELETE FROM vector_embeddings WHERE rowid = ?",
		rowID,
	)
	if inlineErrB != nil {
		return fmt.Errorf("delete SQLite vector metadata %q: %w", recordID, inlineErrB)
	}

	inlineErr7 := transaction.Commit()
	if inlineErr7 != nil {
		return fmt.Errorf("commit SQLite vector delete %q: %w", recordID, inlineErr7)
	}

	return nil
}

func (index *SQLiteVectorIndex) Search(
	ctx context.Context,
	query evidence.VectorQuery,
) ([]evidence.VectorMatch, error) {
	query.Collection = strings.TrimSpace(query.Collection)

	query.ModelID = strings.TrimSpace(query.ModelID)
	if query.Collection == "" || query.ModelID == "" {
		return nil, apperror.StaticError(
			"vector search requires collection and model id",
		)
	}

	if len(query.Vector) == 0 {
		return nil, apperror.StaticError("vector search requires query vector")
	}

	inlineErr8 := index.ensureDimensionTable(ctx, len(query.Vector))
	if inlineErr8 != nil {
		return nil, inlineErr8
	}

	limit := query.Limit
	if limit <= 0 {
		limit = 10
	}

	rows, err := index.database.QueryContext(
		ctx,
		fmt.Sprintf(
			`SELECT m.id, m.metadata_json,
				vec_distance_cosine(v.embedding, vec_f32(?)) AS distance
			FROM %s AS v
			JOIN vector_embeddings AS m ON m.rowid = v.rowid
			WHERE m.collection = ? AND m.model_id = ? AND m.dimension = ?
			ORDER BY distance
			LIMIT ?`,
			vectorTableName(len(query.Vector)),
		),
		vectorLiteral(query.Vector),
		query.Collection,
		query.ModelID,
		len(query.Vector),
		searchCandidateLimit(limit, query.Filters),
	)
	if err != nil {
		return nil, fmt.Errorf("query sqlite-vec vectors: %w", err)
	}
	defer rows.Close()

	matches := []evidence.VectorMatch{}

	for rows.Next() {
		match, keep, err := scanVecMatch(rows, query.Filters)
		if err != nil {
			return nil, err
		}

		if keep {
			matches = append(matches, match)
		}

		if len(matches) == limit {
			break
		}
	}

	inlineErr9 := rows.Err()
	if inlineErr9 != nil {
		return nil, fmt.Errorf("iterate sqlite-vec vectors: %w", inlineErr9)
	}

	return matches, nil
}

func (index *SQLiteVectorIndex) Stats(
	ctx context.Context,
) (evidence.VectorStats, error) {
	rows, err := index.database.QueryContext(
		ctx,
		`SELECT collection, COUNT(*) FROM vector_embeddings GROUP BY collection`,
	)
	if err != nil {
		return evidence.VectorStats{}, fmt.Errorf("query sqlite-vec stats: %w", err)
	}
	defer rows.Close()

	stats := evidence.VectorStats{
		Backend:     vectorBackendName,
		Collections: map[string]int{},
	}

	for rows.Next() {
		var (
			collection string
			count      int
		)

		err := rows.Scan(&collection, &count)
		if err != nil {
			return evidence.VectorStats{}, fmt.Errorf("scan sqlite-vec stats: %w", err)
		}

		stats.Collections[collection] = count
		stats.Rows += count
	}

	inlineErr10 := rows.Err()
	if inlineErr10 != nil {
		return evidence.VectorStats{}, fmt.Errorf(
			"iterate sqlite-vec stats: %w",
			inlineErr10,
		)
	}

	return stats, nil
}

func (index *SQLiteVectorIndex) Rebuild(ctx context.Context, collection string) error {
	collection = strings.TrimSpace(collection)

	transaction, err := index.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("start sqlite-vec rebuild transaction: %w", err)
	}

	defer func() {
		rollbackSQLiteVectorTx(transaction)
	}()

	vectorRows, err := vectorRowsForRebuild(ctx, transaction, collection)
	if err != nil {
		return err
	}

	for _, row := range vectorRows {
		err = deleteVectorRow(ctx, transaction, row.rowID, row.dimension)
		if err != nil {
			return err
		}
	}

	err = clearVectorMetadata(ctx, transaction, collection)
	if err != nil {
		return err
	}

	inlineErr13 := transaction.Commit()
	if inlineErr13 != nil {
		return fmt.Errorf(
			"commit sqlite-vec rebuild collection %q: %w",
			collection,
			inlineErr13,
		)
	}

	return nil
}

type vectorRow struct {
	rowID     int64
	dimension int
}

func vectorRowsForRebuild(
	ctx context.Context,
	transaction *sql.Tx,
	collection string,
) ([]vectorRow, error) {
	rows, err := transaction.QueryContext(
		ctx,
		"SELECT rowid, dimension FROM vector_embeddings WHERE ? = '' OR collection = ?",
		collection,
		collection,
	)
	if err != nil {
		return nil, fmt.Errorf("query sqlite-vec rebuild rows: %w", err)
	}

	return scanVectorRowsForRebuild(rows)
}

func scanVectorRowsForRebuild(rows *sql.Rows) ([]vectorRow, error) {
	defer func() {
		_ = rows.Close()
	}()

	vectorRows := []vectorRow{}

	for rows.Next() {
		var row vectorRow

		err := rows.Scan(&row.rowID, &row.dimension)
		if err != nil {
			return nil, fmt.Errorf("scan sqlite-vec rebuild row: %w", err)
		}

		vectorRows = append(vectorRows, row)
	}

	err := rows.Close()
	if err != nil {
		return nil, fmt.Errorf("close sqlite-vec rebuild rows: %w", err)
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate sqlite-vec rebuild rows: %w", err)
	}

	return vectorRows, nil
}

func clearVectorMetadata(
	ctx context.Context,
	transaction *sql.Tx,
	collection string,
) error {
	_, err := transaction.ExecContext(
		ctx,
		"DELETE FROM vector_embeddings WHERE ? = '' OR collection = ?",
		collection,
		collection,
	)
	if err != nil {
		return fmt.Errorf(
			"clear sqlite-vec metadata collection %q: %w",
			collection,
			err,
		)
	}

	return nil
}

func (index *SQLiteVectorIndex) Close() error {
	err := index.database.Close()
	if err != nil {
		return fmt.Errorf("close SQLite vector index: %w", err)
	}

	return nil
}

func (index *SQLiteVectorIndex) migrate(ctx context.Context) error {
	var version string

	inlineErr14 := index.database.QueryRowContext(ctx, "SELECT vec_version()").
		Scan(&version)
	if inlineErr14 != nil {
		return fmt.Errorf("load sqlite-vec extension: %w", inlineErr14)
	}

	_, err := index.database.ExecContext(
		ctx,
		`CREATE TABLE IF NOT EXISTS vector_embeddings (
		id TEXT NOT NULL,
		collection TEXT NOT NULL,
		model_id TEXT NOT NULL,
		input_kind TEXT,
		text TEXT,
		dimension INTEGER NOT NULL,
		metadata_json TEXT NOT NULL,
		schema_version INTEGER NOT NULL,
		UNIQUE(id, model_id)
	)`,
	)
	if err != nil {
		return fmt.Errorf("migrate sqlite-vec metadata store: %w", err)
	}

	_, err = index.database.ExecContext(
		ctx,
		`CREATE INDEX IF NOT EXISTS idx_vector_embeddings_lookup
		ON vector_embeddings(collection, model_id, dimension)`,
	)
	if err != nil {
		return fmt.Errorf("index sqlite-vec metadata store: %w", err)
	}

	return nil
}

func (index *SQLiteVectorIndex) ensureDimensionTable(
	ctx context.Context,
	dimension int,
) error {
	if dimension <= 0 {
		return apperror.StaticError("sqlite-vec dimension must be positive")
	}

	_, err := index.database.ExecContext(
		ctx,
		fmt.Sprintf(
			"CREATE VIRTUAL TABLE IF NOT EXISTS %s USING vec0(embedding float[%d])",
			vectorTableName(dimension),
			dimension,
		),
	)
	if err != nil {
		return fmt.Errorf(
			"create sqlite-vec table for dimension %d: %w",
			dimension,
			err,
		)
	}

	return nil
}

func existingVectorRow(
	ctx context.Context,
	transaction *sql.Tx,
	recordID string,
	modelID string,
) (int64, int, bool, error) {
	var (
		rowID     int64
		dimension int
	)

	err := transaction.QueryRowContext(
		ctx,
		"SELECT rowid, dimension FROM vector_embeddings WHERE id = ? AND model_id = ?",
		recordID,
		modelID,
	).Scan(&rowID, &dimension)
	if err == nil {
		return rowID, dimension, true, nil
	}

	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, false, nil
	}

	return 0, 0, false, fmt.Errorf("query SQLite vector metadata %q: %w", recordID, err)
}

func insertVecRow(
	ctx context.Context,
	transaction *sql.Tx,
	rowID int64,
	dimension int,
	vector []float32,
) error {
	_, err := transaction.ExecContext(
		ctx,
		fmt.Sprintf(
			"INSERT INTO %s(rowid, embedding) VALUES (?, vec_f32(?))",
			vectorTableName(dimension),
		),
		rowID,
		vectorLiteral(vector),
	)
	if err != nil {
		return fmt.Errorf("insert sqlite-vec row %d: %w", rowID, err)
	}

	return nil
}

func deleteVectorRow(
	ctx context.Context,
	transaction *sql.Tx,
	rowID int64,
	dimension int,
) error {
	_, err := transaction.ExecContext(
		ctx,
		fmt.Sprintf("DELETE FROM %s WHERE rowid = ?", vectorTableName(dimension)),
		rowID,
	)
	if err != nil {
		return fmt.Errorf("delete sqlite-vec row %d: %w", rowID, err)
	}

	return nil
}

func scanVecMatch(
	rows *sql.Rows,
	filters map[string]string,
) (evidence.VectorMatch, bool, error) {
	var (
		recordID string
		metadata string
		distance float64
	)

	err := rows.Scan(&recordID, &metadata, &distance)
	if err != nil {
		return evidence.VectorMatch{}, false, fmt.Errorf(
			"scan sqlite-vec match: %w",
			err,
		)
	}

	recordMetadata := map[string]string{}

	err = json.Unmarshal([]byte(metadata), &recordMetadata)
	if err != nil {
		return evidence.VectorMatch{}, false, fmt.Errorf(
			"decode sqlite-vec metadata %q: %w",
			recordID,
			err,
		)
	}

	if !metadataMatches(recordMetadata, filters) {
		return evidence.VectorMatch{}, false, nil
	}

	return evidence.VectorMatch{
		ID:       recordID,
		Score:    1 - distance,
		Metadata: recordMetadata,
	}, true, nil
}

func normalizeVectorRecord(record evidence.VectorRecord) evidence.VectorRecord {
	record.ID = strings.TrimSpace(record.ID)
	record.Collection = strings.TrimSpace(record.Collection)
	record.ModelID = strings.TrimSpace(record.ModelID)

	record.InputKind = strings.TrimSpace(record.InputKind)
	if record.Dimension == 0 {
		record.Dimension = len(record.Vector)
	}

	if record.SchemaVersion == 0 {
		record.SchemaVersion = evidence.SchemaVersion
	}

	if record.Metadata == nil {
		record.Metadata = map[string]string{}
	}

	return record
}

func validateVectorRecord(record evidence.VectorRecord) error {
	if record.ID == "" || record.Collection == "" || record.ModelID == "" {
		return apperror.StaticError("vector id, collection, and model id are required")
	}

	if record.Dimension <= 0 {
		return apperror.StaticError("vector dimension must be positive")
	}

	if len(record.Vector) != record.Dimension {
		return apperror.Wrapf(
			apperror.StaticError("vector dimension mismatch for %q"),
			"vector dimension mismatch for %q",
			record.ID,
		)
	}

	return nil
}

func vectorLiteral(vector []float32) string {
	values := make([]string, 0, len(vector))
	for _, value := range vector {
		values = append(values, fmt.Sprintf("%g", value))
	}

	return "[" + strings.Join(values, ",") + "]"
}

func vectorTableName(dimension int) string {
	return fmt.Sprintf("vector_embeddings_vec_%d", dimension)
}

func searchCandidateLimit(limit int, filters map[string]string) int {
	if len(filters) == 0 {
		return limit
	}

	return limit * filteredSearchFactor
}

func metadataMatches(metadata, filters map[string]string) bool {
	for key, value := range filters {
		if strings.TrimSpace(value) == "" {
			continue
		}

		if metadata[key] != value {
			return false
		}
	}

	return true
}

func rollbackSQLiteVectorTx(transaction *sql.Tx) {
	err := transaction.Rollback()
	if err != nil {
		return
	}
}

var _ evidence.VectorIndex = (*SQLiteVectorIndex)(nil)
