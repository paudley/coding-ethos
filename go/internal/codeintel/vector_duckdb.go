// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/evidence"
)

const (
	duckDBVectorSearchFixedArgs = 4
	duckDBVectorStoreMode       = 0o700
)

type DuckDBVectorIndex struct {
	database  *sql.DB
	vssLoaded bool
}

func NewDuckDBVectorIndex(
	ctx context.Context,
	path string,
) (*DuckDBVectorIndex, error) {
	if strings.TrimSpace(path) == "" {
		return nil, apperror.StaticError("DuckDB vector path is required")
	}

	err := os.MkdirAll(filepath.Dir(path), duckDBVectorStoreMode)
	if err != nil {
		return nil, fmt.Errorf("create DuckDB vector store dir: %w", err)
	}

	database, err := sql.Open("duckdb", path)
	if err != nil {
		return nil, fmt.Errorf("open DuckDB vector store: %w", err)
	}

	index := &DuckDBVectorIndex{database: database}

	err = index.migrate(ctx)
	if err != nil {
		_ = database.Close()

		return nil, err
	}

	return index, nil
}

func (index *DuckDBVectorIndex) Close() error {
	err := index.database.Close()
	if err != nil {
		return fmt.Errorf("close DuckDB vector index: %w", err)
	}

	return nil
}

func (index *DuckDBVectorIndex) UpsertEmbedding(
	ctx context.Context,
	record evidence.VectorRecord,
) error {
	record = normalizeVectorRecord(record)

	err := validateVectorRecord(record)
	if err != nil {
		return err
	}

	metadata, err := json.Marshal(record.Metadata)
	if err != nil {
		return fmt.Errorf("marshal vector metadata %q: %w", record.ID, err)
	}

	err = index.ensureDimensionTable(ctx, record.Dimension)
	if err != nil {
		return err
	}

	transaction, err := index.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("start DuckDB vector transaction: %w", err)
	}

	defer rollbackDuckDBVectorTx(transaction)

	err = deleteDuckDBVectorRecord(ctx, transaction, record.ID, record.ModelID)
	if err != nil {
		return err
	}

	_, err = transaction.ExecContext(
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
		return fmt.Errorf("insert DuckDB vector metadata %q: %w", record.ID, err)
	}

	err = insertDuckDBVectorRow(ctx, transaction, record)
	if err != nil {
		return err
	}

	err = transaction.Commit()
	if err != nil {
		return fmt.Errorf("commit DuckDB vector upsert %q: %w", record.ID, err)
	}

	return nil
}

func (index *DuckDBVectorIndex) DeleteEmbedding(
	ctx context.Context,
	recordID string,
	modelID string,
) error {
	transaction, err := index.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("start DuckDB vector delete transaction: %w", err)
	}

	defer rollbackDuckDBVectorTx(transaction)

	err = deleteDuckDBVectorRecord(ctx, transaction, recordID, modelID)
	if err != nil {
		return err
	}

	err = transaction.Commit()
	if err != nil {
		return fmt.Errorf("commit DuckDB vector delete %q: %w", recordID, err)
	}

	return nil
}

func (index *DuckDBVectorIndex) Search(
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

	err := index.ensureDimensionTable(ctx, len(query.Vector))
	if err != nil {
		return nil, err
	}

	limit := query.Limit
	if limit <= 0 {
		limit = 10
	}

	args := make([]any, 0, len(query.Vector)+duckDBVectorSearchFixedArgs)
	args = append(args, vectorArgs(query.Vector)...)
	args = append(args, query.Collection, query.ModelID, len(query.Vector))
	args = append(args, searchCandidateLimit(limit, query.Filters))

	rows, err := index.database.QueryContext(
		ctx,
		fmt.Sprintf(
			`SELECT m.id, m.metadata_json,
				array_cosine_distance(v.embedding, %s) AS distance
			FROM %s AS v
			JOIN vector_embeddings AS m
				ON m.id = v.id AND m.model_id = v.model_id
			WHERE m.collection = ? AND m.model_id = ? AND m.dimension = ?
			ORDER BY distance
			LIMIT ?`,
			duckDBArrayValue(len(query.Vector)),
			vectorTableName(len(query.Vector)),
		),
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("query DuckDB VSS vectors: %w", err)
	}
	defer rows.Close()

	matches := []evidence.VectorMatch{}

	for rows.Next() {
		match, keep, scanErr := scanDuckDBVectorMatch(rows, query.Filters)
		if scanErr != nil {
			return nil, scanErr
		}

		if keep {
			matches = append(matches, match)
		}

		if len(matches) == limit {
			break
		}
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate DuckDB VSS vectors: %w", err)
	}

	return matches, nil
}

func (index *DuckDBVectorIndex) Stats(
	ctx context.Context,
) (evidence.VectorStats, error) {
	rows, err := index.database.QueryContext(
		ctx,
		`SELECT collection, COUNT(*) FROM vector_embeddings GROUP BY collection`,
	)
	if err != nil {
		return evidence.VectorStats{}, fmt.Errorf("query DuckDB VSS stats: %w", err)
	}
	defer rows.Close()

	stats := evidence.VectorStats{
		Backend:     VectorBackendDuckDBVSS,
		Collections: map[string]int{},
	}

	for rows.Next() {
		var (
			collection string
			count      int
		)

		scanErr := rows.Scan(&collection, &count)
		if scanErr != nil {
			return evidence.VectorStats{}, fmt.Errorf("scan DuckDB VSS stats: %w", scanErr)
		}

		stats.Collections[collection] = count
		stats.Rows += count
	}

	err = rows.Err()
	if err != nil {
		return evidence.VectorStats{}, fmt.Errorf("iterate DuckDB VSS stats: %w", err)
	}

	return stats, nil
}

func (index *DuckDBVectorIndex) Rebuild(ctx context.Context, collection string) error {
	collection = strings.TrimSpace(collection)

	transaction, err := index.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("start DuckDB VSS rebuild transaction: %w", err)
	}

	defer rollbackDuckDBVectorTx(transaction)

	err = clearDuckDBVectorRows(ctx, transaction, collection)
	if err != nil {
		return err
	}

	err = transaction.Commit()
	if err != nil {
		return fmt.Errorf("commit DuckDB VSS rebuild collection %q: %w", collection, err)
	}

	return nil
}

func (index *DuckDBVectorIndex) migrate(ctx context.Context) error {
	vssLoaded, err := loadDuckDBVSSExtension(ctx, index.database)
	if err != nil {
		return err
	}

	index.vssLoaded = vssLoaded

	_, err = index.database.ExecContext(
		ctx,
		`CREATE TABLE IF NOT EXISTS vector_embeddings (
			id VARCHAR,
			collection VARCHAR,
			model_id VARCHAR,
			input_kind VARCHAR,
			text VARCHAR,
			dimension BIGINT,
			metadata_json VARCHAR,
			schema_version BIGINT,
			PRIMARY KEY(id, model_id)
		)`,
	)
	if err != nil {
		return fmt.Errorf("migrate DuckDB VSS metadata store: %w", err)
	}

	_, err = index.database.ExecContext(
		ctx,
		`CREATE INDEX IF NOT EXISTS idx_vector_embeddings_lookup
		ON vector_embeddings(collection, model_id, dimension)`,
	)
	if err != nil {
		return fmt.Errorf("index DuckDB VSS metadata store: %w", err)
	}

	return nil
}

func loadDuckDBVSSExtension(ctx context.Context, database *sql.DB) (bool, error) {
	_, err := database.ExecContext(ctx, "LOAD vss")
	if err != nil {
		if duckDBVSSExtensionUnavailable(err) {
			return false, nil
		}

		return false, fmt.Errorf("load DuckDB VSS extension: %w", err)
	}

	err = setDuckDBVSSPersistence(ctx, database)
	if err != nil {
		return false, err
	}

	return true, nil
}

func duckDBVSSExtensionUnavailable(err error) bool {
	message := strings.ToLower(err.Error())

	return strings.Contains(message, "extension") &&
		strings.Contains(message, "install it first")
}

func setDuckDBVSSPersistence(ctx context.Context, database *sql.DB) error {
	_, err := database.ExecContext(ctx, "SET hnsw_enable_experimental_persistence = true")
	if err != nil {
		return fmt.Errorf("enable DuckDB VSS persistence: %w", err)
	}

	return nil
}

func (index *DuckDBVectorIndex) ensureDimensionTable(
	ctx context.Context,
	dimension int,
) error {
	if dimension <= 0 {
		return apperror.StaticError("DuckDB VSS dimension must be positive")
	}

	tableName := vectorTableName(dimension)

	_, err := index.database.ExecContext(
		ctx,
		fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s (
				id VARCHAR,
				model_id VARCHAR,
				embedding FLOAT[%d],
				PRIMARY KEY(id, model_id)
			)`,
			tableName,
			dimension,
		),
	)
	if err != nil {
		return fmt.Errorf("create DuckDB VSS table for dimension %d: %w", dimension, err)
	}

	if !index.vssLoaded {
		return nil
	}

	_, err = index.database.ExecContext(
		ctx,
		fmt.Sprintf(
			`CREATE INDEX IF NOT EXISTS idx_%s_hnsw
			ON %s USING HNSW (embedding)
			WITH (metric = 'cosine')`,
			tableName,
			tableName,
		),
	)
	if err != nil {
		return fmt.Errorf("create DuckDB VSS HNSW index for dimension %d: %w", dimension, err)
	}

	return nil
}

func insertDuckDBVectorRow(
	ctx context.Context,
	transaction *sql.Tx,
	record evidence.VectorRecord,
) error {
	args := append([]any{record.ID, record.ModelID}, vectorArgs(record.Vector)...)

	_, err := transaction.ExecContext(
		ctx,
		fmt.Sprintf(
			"INSERT INTO %s(id, model_id, embedding) VALUES (?, ?, %s)",
			vectorTableName(record.Dimension),
			duckDBArrayValue(record.Dimension),
		),
		args...,
	)
	if err != nil {
		return fmt.Errorf("insert DuckDB VSS row %q: %w", record.ID, err)
	}

	return nil
}

func deleteDuckDBVectorRecord(
	ctx context.Context,
	transaction *sql.Tx,
	recordID string,
	modelID string,
) error {
	rows, err := transaction.QueryContext(
		ctx,
		`SELECT model_id, dimension
		FROM vector_embeddings
		WHERE id = ? AND model_id = ?`,
		strings.TrimSpace(recordID),
		strings.TrimSpace(modelID),
	)
	if err != nil {
		return fmt.Errorf("query DuckDB VSS dimensions for %q: %w", recordID, err)
	}
	defer rows.Close()

	type vectorIdentity struct {
		modelID   string
		dimension int
	}

	identities := []vectorIdentity{}

	for rows.Next() {
		var identity vectorIdentity

		err = rows.Scan(&identity.modelID, &identity.dimension)
		if err != nil {
			return fmt.Errorf("scan DuckDB VSS dimension for %q: %w", recordID, err)
		}

		identities = append(identities, identity)
	}

	err = rows.Err()
	if err != nil {
		return fmt.Errorf("iterate DuckDB VSS dimensions for %q: %w", recordID, err)
	}

	for _, identity := range identities {
		_, err = transaction.ExecContext(
			ctx,
			fmt.Sprintf(
				"DELETE FROM %s WHERE id = ? AND model_id = ?",
				vectorTableName(identity.dimension),
			),
			recordID,
			identity.modelID,
		)
		if err != nil {
			return fmt.Errorf("delete DuckDB VSS row %q: %w", recordID, err)
		}
	}

	_, err = transaction.ExecContext(
		ctx,
		"DELETE FROM vector_embeddings WHERE id = ? AND model_id = ?",
		strings.TrimSpace(recordID),
		strings.TrimSpace(modelID),
	)
	if err != nil {
		return fmt.Errorf("delete DuckDB VSS metadata %q: %w", recordID, err)
	}

	return nil
}

func clearDuckDBVectorRows(
	ctx context.Context,
	transaction *sql.Tx,
	collection string,
) error {
	rows, err := transaction.QueryContext(
		ctx,
		`SELECT DISTINCT dimension FROM vector_embeddings
		WHERE ? = '' OR collection = ?`,
		collection,
		collection,
	)
	if err != nil {
		return fmt.Errorf("query DuckDB VSS rebuild dimensions: %w", err)
	}
	defer rows.Close()

	dimensions := []int{}

	for rows.Next() {
		var dimension int

		err = rows.Scan(&dimension)
		if err != nil {
			return fmt.Errorf("scan DuckDB VSS rebuild dimension: %w", err)
		}

		dimensions = append(dimensions, dimension)
	}

	err = rows.Err()
	if err != nil {
		return fmt.Errorf("iterate DuckDB VSS rebuild dimensions: %w", err)
	}

	for _, dimension := range dimensions {
		_, err = transaction.ExecContext(
			ctx,
			fmt.Sprintf(
				`DELETE FROM %s
				WHERE EXISTS (
					SELECT 1 FROM vector_embeddings AS m
					WHERE m.id = %s.id
						AND m.model_id = %s.model_id
						AND (? = '' OR m.collection = ?)
				)`,
				vectorTableName(dimension),
				vectorTableName(dimension),
				vectorTableName(dimension),
			),
			collection,
			collection,
		)
		if err != nil {
			return fmt.Errorf("clear DuckDB VSS rows for dimension %d: %w", dimension, err)
		}
	}

	_, err = transaction.ExecContext(
		ctx,
		"DELETE FROM vector_embeddings WHERE ? = '' OR collection = ?",
		collection,
		collection,
	)
	if err != nil {
		return fmt.Errorf("clear DuckDB VSS metadata collection %q: %w", collection, err)
	}

	return nil
}

func scanDuckDBVectorMatch(
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
		return evidence.VectorMatch{}, false, fmt.Errorf("scan DuckDB VSS match: %w", err)
	}

	recordMetadata := map[string]string{}

	err = json.Unmarshal([]byte(metadata), &recordMetadata)
	if err != nil {
		return evidence.VectorMatch{}, false, fmt.Errorf(
			"decode DuckDB VSS metadata %q: %w",
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

func duckDBArrayValue(dimension int) string {
	placeholders := make([]string, 0, dimension)
	for range dimension {
		placeholders = append(placeholders, "?")
	}

	return fmt.Sprintf(
		"array_value(%s)::FLOAT[%d]",
		strings.Join(placeholders, ","),
		dimension,
	)
}

func vectorArgs(vector []float32) []any {
	args := make([]any, 0, len(vector))
	for _, value := range vector {
		args = append(args, value)
	}

	return args
}

func rollbackDuckDBVectorTx(transaction *sql.Tx) {
	err := transaction.Rollback()
	if err != nil {
		return
	}
}

var _ evidence.VectorIndex = (*DuckDBVectorIndex)(nil)
