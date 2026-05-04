// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/evidence"
)

const (
	sqliteVectorMetaTable = "vector_embeddings"
	vectorBackendName     = "sqlite-vec"
)

type SQLiteVectorIndex struct {
	db *sql.DB
}

func NewSQLiteVectorIndex(ctx context.Context, path string) (*SQLiteVectorIndex, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("SQLite vector path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), storeDirMode); err != nil {
		return nil, fmt.Errorf("create SQLite vector store dir: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open SQLite vector store: %w", err)
	}
	index := &SQLiteVectorIndex{db: db}
	if err := index.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	return index, nil
}

func (index *SQLiteVectorIndex) UpsertEmbedding(
	ctx context.Context,
	record evidence.VectorRecord,
) error {
	record = normalizeVectorRecord(record)
	if err := validateVectorRecord(record); err != nil {
		return err
	}
	metadata, err := json.Marshal(record.Metadata)
	if err != nil {
		return fmt.Errorf("marshal vector metadata %q: %w", record.ID, err)
	}
	if err := index.ensureDimensionTable(ctx, record.Dimension); err != nil {
		return err
	}

	tx, err := index.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("start SQLite vector transaction: %w", err)
	}
	defer tx.Rollback()

	rowID, oldDimension, found, err := existingVectorRow(ctx, tx, record.ID, record.ModelID)
	if err != nil {
		return err
	}
	if found {
		if err := deleteVectorRow(ctx, tx, rowID, oldDimension); err != nil {
			return err
		}
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE vector_embeddings
			SET collection = ?, input_kind = ?, text = ?, dimension = ?,
				metadata_json = ?, schema_version = ?
			WHERE rowid = ?`,
			record.Collection,
			record.InputKind,
			record.Text,
			record.Dimension,
			string(metadata),
			record.SchemaVersion,
			rowID,
		); err != nil {
			return fmt.Errorf("update SQLite vector metadata %q: %w", record.ID, err)
		}
	} else {
		result, err := tx.ExecContext(
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
			string(metadata),
			record.SchemaVersion,
		)
		if err != nil {
			return fmt.Errorf("insert SQLite vector metadata %q: %w", record.ID, err)
		}
		rowID, err = result.LastInsertId()
		if err != nil {
			return fmt.Errorf("read SQLite vector rowid %q: %w", record.ID, err)
		}
	}
	if err := insertVecRow(ctx, tx, rowID, record.Dimension, record.Vector); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit SQLite vector upsert %q: %w", record.ID, err)
	}

	return nil
}

func (index *SQLiteVectorIndex) DeleteEmbedding(
	ctx context.Context,
	id string,
	modelID string,
) error {
	tx, err := index.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("start SQLite vector delete transaction: %w", err)
	}
	defer tx.Rollback()

	rowID, dimension, found, err := existingVectorRow(
		ctx,
		tx,
		strings.TrimSpace(id),
		strings.TrimSpace(modelID),
	)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	if err := deleteVectorRow(ctx, tx, rowID, dimension); err != nil {
		return err
	}
	if _, err := tx.ExecContext(
		ctx,
		"DELETE FROM vector_embeddings WHERE rowid = ?",
		rowID,
	); err != nil {
		return fmt.Errorf("delete SQLite vector metadata %q: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit SQLite vector delete %q: %w", id, err)
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
		return nil, fmt.Errorf("vector search requires collection and model id")
	}
	if len(query.Vector) == 0 {
		return nil, fmt.Errorf("vector search requires query vector")
	}
	if err := index.ensureDimensionTable(ctx, len(query.Vector)); err != nil {
		return nil, err
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 10
	}

	rows, err := index.db.QueryContext(
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
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sqlite-vec vectors: %w", err)
	}

	return matches, nil
}

func (index *SQLiteVectorIndex) Stats(ctx context.Context) (evidence.VectorStats, error) {
	rows, err := index.db.QueryContext(
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
		var collection string
		var count int
		if err := rows.Scan(&collection, &count); err != nil {
			return evidence.VectorStats{}, fmt.Errorf("scan sqlite-vec stats: %w", err)
		}
		stats.Collections[collection] = count
		stats.Rows += count
	}
	if err := rows.Err(); err != nil {
		return evidence.VectorStats{}, fmt.Errorf("iterate sqlite-vec stats: %w", err)
	}

	return stats, nil
}

func (index *SQLiteVectorIndex) Rebuild(ctx context.Context, collection string) error {
	collection = strings.TrimSpace(collection)
	tx, err := index.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("start sqlite-vec rebuild transaction: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(
		ctx,
		"SELECT rowid, dimension FROM vector_embeddings WHERE ? = '' OR collection = ?",
		collection,
		collection,
	)
	if err != nil {
		return fmt.Errorf("query sqlite-vec rebuild rows: %w", err)
	}
	type vectorRow struct {
		rowID     int64
		dimension int
	}
	vectorRows := []vectorRow{}
	for rows.Next() {
		var row vectorRow
		if err := rows.Scan(&row.rowID, &row.dimension); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan sqlite-vec rebuild row: %w", err)
		}
		vectorRows = append(vectorRows, row)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close sqlite-vec rebuild rows: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate sqlite-vec rebuild rows: %w", err)
	}

	for _, row := range vectorRows {
		if err := deleteVectorRow(ctx, tx, row.rowID, row.dimension); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(
		ctx,
		"DELETE FROM vector_embeddings WHERE ? = '' OR collection = ?",
		collection,
		collection,
	); err != nil {
		return fmt.Errorf("clear sqlite-vec metadata collection %q: %w", collection, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit sqlite-vec rebuild collection %q: %w", collection, err)
	}

	return nil
}

func (index *SQLiteVectorIndex) Close() error {
	return index.db.Close()
}

func (index *SQLiteVectorIndex) migrate(ctx context.Context) error {
	var version string
	if err := index.db.QueryRowContext(ctx, "SELECT vec_version()").Scan(&version); err != nil {
		return fmt.Errorf("load sqlite-vec extension: %w", err)
	}
	_, err := index.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS vector_embeddings (
		id TEXT NOT NULL,
		collection TEXT NOT NULL,
		model_id TEXT NOT NULL,
		input_kind TEXT,
		text TEXT,
		dimension INTEGER NOT NULL,
		metadata_json TEXT NOT NULL,
		schema_version INTEGER NOT NULL,
		UNIQUE(id, model_id)
	)`)
	if err != nil {
		return fmt.Errorf("migrate sqlite-vec metadata store: %w", err)
	}
	_, err = index.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_vector_embeddings_lookup
		ON vector_embeddings(collection, model_id, dimension)`)
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
		return fmt.Errorf("sqlite-vec dimension must be positive")
	}
	_, err := index.db.ExecContext(
		ctx,
		fmt.Sprintf(
			"CREATE VIRTUAL TABLE IF NOT EXISTS %s USING vec0(embedding float[%d])",
			vectorTableName(dimension),
			dimension,
		),
	)
	if err != nil {
		return fmt.Errorf("create sqlite-vec table for dimension %d: %w", dimension, err)
	}

	return nil
}

func existingVectorRow(
	ctx context.Context,
	tx *sql.Tx,
	id string,
	modelID string,
) (int64, int, bool, error) {
	var rowID int64
	var dimension int
	err := tx.QueryRowContext(
		ctx,
		"SELECT rowid, dimension FROM vector_embeddings WHERE id = ? AND model_id = ?",
		id,
		modelID,
	).Scan(&rowID, &dimension)
	if err == nil {
		return rowID, dimension, true, nil
	}
	if err == sql.ErrNoRows {
		return 0, 0, false, nil
	}

	return 0, 0, false, fmt.Errorf("query SQLite vector metadata %q: %w", id, err)
}

func insertVecRow(
	ctx context.Context,
	tx *sql.Tx,
	rowID int64,
	dimension int,
	vector []float32,
) error {
	_, err := tx.ExecContext(
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
	tx *sql.Tx,
	rowID int64,
	dimension int,
) error {
	_, err := tx.ExecContext(
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
		id       string
		metadata string
		distance float64
	)
	if err := rows.Scan(&id, &metadata, &distance); err != nil {
		return evidence.VectorMatch{}, false, fmt.Errorf("scan sqlite-vec match: %w", err)
	}
	recordMetadata := map[string]string{}
	if err := json.Unmarshal([]byte(metadata), &recordMetadata); err != nil {
		return evidence.VectorMatch{}, false, fmt.Errorf("decode sqlite-vec metadata %q: %w", id, err)
	}
	if !metadataMatches(recordMetadata, filters) {
		return evidence.VectorMatch{}, false, nil
	}

	return evidence.VectorMatch{
		ID:       id,
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
		return fmt.Errorf("vector id, collection, and model id are required")
	}
	if record.Dimension <= 0 {
		return fmt.Errorf("vector dimension must be positive")
	}
	if len(record.Vector) != record.Dimension {
		return fmt.Errorf("vector dimension mismatch for %q", record.ID)
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

	return limit * 20
}

func metadataMatches(metadata map[string]string, filters map[string]string) bool {
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

var _ evidence.VectorIndex = (*SQLiteVectorIndex)(nil)
