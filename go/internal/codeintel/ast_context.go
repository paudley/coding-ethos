// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/astfacts"
)

type sqlContextQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func (store *Store) CodeContext(
	ctx context.Context,
	query CodeContextQuery,
) (CodeContext, error) {
	chunk, err := store.findCodeContextChunk(ctx, query)
	if err != nil {
		return CodeContext{}, err
	}

	err = store.validateCodeContextFresh(ctx, query.Root, chunk.Path)
	if err != nil {
		return CodeContext{}, err
	}

	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}

	context := CodeContext{Chunk: chunk}
	if chunk.ParentChunkID != "" {
		parent, parentErr := store.codeChunkByID(ctx, chunk.ParentChunkID)
		if parentErr != nil {
			return CodeContext{}, parentErr
		}

		context.Parent = &parent
	}

	children, err := store.childCodeChunks(ctx, chunk.ID, limit)
	if err != nil {
		return CodeContext{}, err
	}

	context.Children = children

	siblings, err := store.siblingCodeChunks(ctx, chunk, limit)
	if err != nil {
		return CodeContext{}, err
	}

	context.Siblings = siblings

	outgoing, incoming, err := store.codeEdgesForChunk(ctx, chunk.ID, limit)
	if err != nil {
		return CodeContext{}, err
	}

	context.OutgoingEdges = outgoing
	context.IncomingEdges = incoming

	links, err := store.astFindingLinksForChunk(ctx, chunk.ID, limit)
	if err != nil {
		return CodeContext{}, err
	}

	context.FindingLinks = links

	return context, nil
}

func (store *Store) validateCodeContextFresh(
	ctx context.Context,
	root string,
	path string,
) error {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil
	}

	storedHash, storedSize, err := store.codeFileContentMetadata(ctx, path)
	if err != nil {
		return err
	}

	return validateCodeFileContentFresh(
		root,
		path,
		codeFileContentMetadata{hash: storedHash, size: storedSize},
	)
}

func validateCodeFileContentFresh(
	root string,
	path string,
	metadata codeFileContentMetadata,
) error {
	sourcePath := filepath.Join(root, filepath.FromSlash(path))

	info, err := os.Stat(sourcePath)
	if err != nil {
		return fmt.Errorf("stat current source for code context %s: %w", path, err)
	}

	if info.Size() != metadata.size || info.Size() > maxIndexedSourceBytes {
		return staleCodeContextError(path)
	}

	contents, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("read current source for code context %s: %w", path, err)
	}

	currentHash := astfacts.ContentHash(contents)
	if currentHash != metadata.hash {
		return staleCodeContextError(path)
	}

	return nil
}

func (store *Store) validateASTContextPathsFresh(
	ctx context.Context,
	root string,
	paths []string,
) error {
	return validateASTContextPathsFresh(
		ctx,
		root,
		paths,
		store.codeFileContentMetadataByPath,
	)
}

func (store *DuckDBStore) validateASTContextPathsFresh(
	ctx context.Context,
	root string,
	paths []string,
) error {
	return validateASTContextPathsFresh(
		ctx,
		root,
		paths,
		store.codeFileContentMetadataByPath,
	)
}

func validateASTContextPathsFresh(
	ctx context.Context,
	root string,
	paths []string,
	metadataByPath func(
		context.Context,
		[]string,
	) (map[string]codeFileContentMetadata, error),
) error {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil
	}

	paths = uniqueASTContextPaths(paths)

	metadata, err := metadataByPath(ctx, paths)
	if err != nil {
		return err
	}

	for _, path := range paths {
		fileMetadata, found := metadata[path]
		if !found {
			return fmt.Errorf("query code file metadata %s: %w", path, sql.ErrNoRows)
		}

		err := validateCodeFileContentFresh(root, path, fileMetadata)
		if err != nil {
			return err
		}
	}

	return nil
}

func (store *DuckDBStore) codeFileContentMetadataByPath(
	ctx context.Context,
	paths []string,
) (map[string]codeFileContentMetadata, error) {
	results := make(map[string]codeFileContentMetadata, len(paths))

	for offset := 0; offset < len(paths); offset += sqliteBatchSize {
		end := min(offset+sqliteBatchSize, len(paths))

		err := store.codeFileContentMetadataBatch(ctx, paths[offset:end], results)
		if err != nil {
			return nil, err
		}
	}

	return results, nil
}

func (store *DuckDBStore) codeFileContentMetadataBatch(
	ctx context.Context,
	paths []string,
	results map[string]codeFileContentMetadata,
) error {
	return codeFileContentMetadataBatch(
		ctx,
		store.database,
		paths,
		results,
		"DuckDB",
	)
}

func uniqueASTContextPaths(paths []string) []string {
	unique := make([]string, 0, len(paths))
	seen := map[string]bool{}

	for _, path := range paths {
		if path == "" || seen[path] {
			continue
		}

		seen[path] = true
		unique = append(unique, path)
	}

	slices.Sort(unique)

	return unique
}

func staleCodeContextError(path string) error {
	return apperror.Wrapf(
		apperror.StaticError("stale code context for %s; reindex before using AST context"),
		"stale code context for %s; reindex before using AST context",
		path,
	)
}

func (store *Store) codeFileContentMetadata(
	ctx context.Context,
	path string,
) (string, int64, error) {
	row := store.database.QueryRowContext(
		ctx,
		`SELECT content_hash, size_bytes
		FROM code_files
		WHERE path = ? AND COALESCE(deleted_at_utc, '') = ''`,
		path,
	)

	var (
		hash string
		size int64
	)

	err := row.Scan(&hash, &size)
	if err != nil {
		return "", 0, fmt.Errorf("query code file metadata %s: %w", path, err)
	}

	return hash, size, nil
}

type codeFileContentMetadata struct {
	hash string
	size int64
}

func (store *Store) codeFileContentMetadataByPath(
	ctx context.Context,
	paths []string,
) (map[string]codeFileContentMetadata, error) {
	results := make(map[string]codeFileContentMetadata, len(paths))

	for offset := 0; offset < len(paths); offset += sqliteBatchSize {
		end := min(offset+sqliteBatchSize, len(paths))

		err := store.codeFileContentMetadataBatch(ctx, paths[offset:end], results)
		if err != nil {
			return nil, err
		}
	}

	return results, nil
}

func (store *Store) codeFileContentMetadataBatch(
	ctx context.Context,
	paths []string,
	results map[string]codeFileContentMetadata,
) error {
	return codeFileContentMetadataBatch(ctx, store.database, paths, results, "SQLite")
}

func codeFileContentMetadataBatch(
	ctx context.Context,
	database sqlContextQuerier,
	paths []string,
	results map[string]codeFileContentMetadata,
	label string,
) error {
	if len(paths) == 0 {
		return nil
	}

	placeholders := make([]string, len(paths))
	args := make([]any, len(paths))

	for index, path := range paths {
		placeholders[index] = "?"
		args[index] = path
	}

	// #nosec G202 -- IN-list placeholders are generated for bound parameters.
	rows, err := database.QueryContext(
		ctx,
		`SELECT path, content_hash, size_bytes
		FROM code_files
		WHERE COALESCE(deleted_at_utc, '') = ''
			AND path IN (`+strings.Join(placeholders, ",")+`)`,
		args...,
	)
	if err != nil {
		return fmt.Errorf("query %s code file metadata batch: %w", label, err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			path     string
			metadata codeFileContentMetadata
		)

		err = rows.Scan(&path, &metadata.hash, &metadata.size)
		if err != nil {
			return fmt.Errorf("scan %s code file metadata batch: %w", label, err)
		}

		results[path] = metadata
	}

	err = rows.Err()
	if err != nil {
		return fmt.Errorf("iterate %s code file metadata batch: %w", label, err)
	}

	return nil
}

func (store *Store) findCodeContextChunk(
	ctx context.Context,
	query CodeContextQuery,
) (CodeChunk, error) {
	if strings.TrimSpace(query.ChunkID) != "" {
		return store.codeChunkByID(ctx, strings.TrimSpace(query.ChunkID))
	}

	if strings.TrimSpace(query.Path) != "" && query.Line > 0 {
		return store.codeChunkByPathLine(ctx, strings.TrimSpace(query.Path), query.Line)
	}

	chunks, err := store.CodeChunks(ctx, CodeChunkQuery{
		Path:       strings.TrimSpace(query.Path),
		SymbolPath: strings.TrimSpace(query.SymbolPath),
		Limit:      1,
	})
	if err != nil {
		return CodeChunk{}, err
	}

	if len(chunks) == 1 {
		return chunks[0], nil
	}

	return CodeChunk{}, apperror.StaticError("code chunk context not found")
}

func (store *Store) codeChunkByPathLine(
	ctx context.Context,
	path string,
	line int,
) (CodeChunk, error) {
	rows, err := store.database.QueryContext(
		ctx,
		`SELECT
			chunk_id, code_chunks.path, code_chunks.language, node_kind,
			symbol_kind, symbol_name, symbol_path,
			COALESCE(parent_symbol_path, ''), parent_chunk_id, start_byte, end_byte,
			start_line, end_line, code_chunks.content_hash, search_text, raw_text
		FROM code_chunks
		JOIN code_files ON code_files.path = code_chunks.path
		WHERE code_chunks.path = ?
			AND start_line <= ?
			AND end_line >= ?
			AND COALESCE(code_files.deleted_at_utc, '') = ''
		ORDER BY start_line DESC, (end_line - start_line) ASC, start_byte DESC
		LIMIT 1`,
		path,
		line,
		line,
	)
	if err != nil {
		return CodeChunk{}, fmt.Errorf("query code chunk at %s:%d: %w", path, line, err)
	}
	defer rows.Close()

	chunks, err := scanCodeChunks(rows)
	if err != nil {
		return CodeChunk{}, err
	}

	if len(chunks) != 1 {
		return CodeChunk{}, apperror.Wrapf(
			apperror.StaticError("code chunk context not found at %s:%d"),
			"code chunk context not found at %s:%d",
			path,
			line,
		)
	}

	return chunks[0], nil
}

func (store *Store) codeChunkByID(
	ctx context.Context,
	chunkID string,
) (CodeChunk, error) {
	rows, err := store.database.QueryContext(
		ctx,
		`SELECT
			chunk_id, code_chunks.path, code_chunks.language, node_kind,
			symbol_kind, symbol_name, symbol_path,
			COALESCE(parent_symbol_path, ''), parent_chunk_id, start_byte, end_byte,
			start_line, end_line, code_chunks.content_hash, search_text, raw_text
		FROM code_chunks
		JOIN code_files ON code_files.path = code_chunks.path
		WHERE chunk_id = ?
			AND COALESCE(code_files.deleted_at_utc, '') = ''`,
		chunkID,
	)
	if err != nil {
		return CodeChunk{}, fmt.Errorf("query code chunk %q: %w", chunkID, err)
	}
	defer rows.Close()

	chunks, err := scanCodeChunks(rows)
	if err != nil {
		return CodeChunk{}, err
	}

	if len(chunks) != 1 {
		return CodeChunk{}, apperror.Wrapf(
			apperror.StaticError("code chunk %q not found"),
			"code chunk %q not found",
			chunkID,
		)
	}

	return chunks[0], nil
}

func (store *Store) childCodeChunks(
	ctx context.Context,
	parentID string,
	limit int,
) ([]CodeChunk, error) {
	rows, err := store.database.QueryContext(
		ctx,
		`SELECT
			chunk_id, code_chunks.path, code_chunks.language, node_kind,
			symbol_kind, symbol_name, symbol_path,
			COALESCE(parent_symbol_path, ''), parent_chunk_id, start_byte, end_byte,
			start_line, end_line, code_chunks.content_hash, search_text, raw_text
		FROM code_chunks
		JOIN code_files ON code_files.path = code_chunks.path
		WHERE parent_chunk_id = ?
			AND COALESCE(code_files.deleted_at_utc, '') = ''
		ORDER BY start_line, start_byte
		LIMIT ?`,
		parentID,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query child code chunks: %w", err)
	}
	defer rows.Close()

	return scanCodeChunks(rows)
}

func (store *Store) siblingCodeChunks(
	ctx context.Context,
	chunk CodeChunk,
	limit int,
) ([]CodeChunk, error) {
	rows, err := store.database.QueryContext(
		ctx,
		`SELECT
			chunk_id, code_chunks.path, code_chunks.language, node_kind,
			symbol_kind, symbol_name, symbol_path,
			COALESCE(parent_symbol_path, ''), parent_chunk_id, start_byte, end_byte,
			start_line, end_line, code_chunks.content_hash, search_text, raw_text
		FROM code_chunks
		JOIN code_files ON code_files.path = code_chunks.path
		WHERE code_chunks.path = ?
			AND COALESCE(parent_chunk_id, '') = ?
			AND chunk_id != ?
			AND COALESCE(code_files.deleted_at_utc, '') = ''
		ORDER BY start_line, start_byte
		LIMIT ?`,
		chunk.Path,
		chunk.ParentChunkID,
		chunk.ID,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query sibling code chunks: %w", err)
	}
	defer rows.Close()

	return scanCodeChunks(rows)
}

func scanCodeChunks(rows *sql.Rows) ([]CodeChunk, error) {
	results := []CodeChunk{}

	for rows.Next() {
		var result CodeChunk

		err := rows.Scan(
			&result.ID,
			&result.Path,
			&result.Language,
			&result.NodeKind,
			&result.SymbolKind,
			&result.SymbolName,
			&result.SymbolPath,
			&result.ParentSymbolPath,
			&result.ParentChunkID,
			&result.StartByte,
			&result.EndByte,
			&result.StartLine,
			&result.EndLine,
			&result.ContentHash,
			&result.SearchText,
			&result.RawText,
		)
		if err != nil {
			return nil, fmt.Errorf("scan code chunk: %w", err)
		}

		results = append(results, result)
	}

	err := rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate code chunks: %w", err)
	}

	return results, nil
}

func (store *Store) codeEdgesForChunk(
	ctx context.Context,
	chunkID string,
	limit int,
) ([]CodeEdge, []CodeEdge, error) {
	outgoing, err := store.outgoingCodeEdges(ctx, chunkID, limit)
	if err != nil {
		return nil, nil, err
	}

	incoming, err := store.incomingCodeEdges(ctx, chunkID, limit)
	if err != nil {
		return nil, nil, err
	}

	return outgoing, incoming, nil
}

// CodeEdges returns indexed AST/code relationship edges that match the query.
func (store *Store) CodeEdges(
	ctx context.Context,
	query CodeEdgeQuery,
) ([]CodeEdge, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = 50
	}

	rows, err := store.database.QueryContext(
		ctx,
		`SELECT edge_id, edge_kind, path, COALESCE(source_chunk_id, ''),
			target_path, COALESCE(target_chunk_id, ''), target_symbol_path,
			target_name, raw_text
		FROM code_edges
		WHERE (? = '' OR path = ?)
			AND (? = '' OR edge_kind = ?)
			AND (? = '' OR target_name = ?)
		ORDER BY path, edge_kind, target_name
		LIMIT ?`,
		query.Path,
		query.Path,
		query.Kind,
		query.Kind,
		query.TargetName,
		query.TargetName,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query code edges: %w", err)
	}
	defer rows.Close()

	return scanCodeEdges(rows)
}

func (store *Store) outgoingCodeEdges(
	ctx context.Context,
	chunkID string,
	limit int,
) ([]CodeEdge, error) {
	rows, err := store.database.QueryContext(
		ctx,
		`SELECT edge_id, edge_kind, path, COALESCE(source_chunk_id, ''),
			target_path, COALESCE(target_chunk_id, ''), target_symbol_path,
			target_name, raw_text
		FROM code_edges
		WHERE source_chunk_id = ?
		ORDER BY edge_kind, target_name
		LIMIT ?`,
		chunkID,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query outgoing code edges: %w", err)
	}
	defer rows.Close()

	return scanCodeEdges(rows)
}

func (store *Store) incomingCodeEdges(
	ctx context.Context,
	chunkID string,
	limit int,
) ([]CodeEdge, error) {
	rows, err := store.database.QueryContext(
		ctx,
		`SELECT edge_id, edge_kind, path, COALESCE(source_chunk_id, ''),
			target_path, COALESCE(target_chunk_id, ''), target_symbol_path,
			target_name, raw_text
		FROM code_edges
		WHERE target_chunk_id = ?
		ORDER BY edge_kind, target_name
		LIMIT ?`,
		chunkID,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query incoming code edges: %w", err)
	}
	defer rows.Close()

	return scanCodeEdges(rows)
}

func scanCodeEdges(rows *sql.Rows) ([]CodeEdge, error) {
	results := []CodeEdge{}

	for rows.Next() {
		var result CodeEdge

		err := rows.Scan(
			&result.ID,
			&result.Kind,
			&result.Path,
			&result.SourceChunkID,
			&result.TargetPath,
			&result.TargetChunkID,
			&result.TargetSymbolPath,
			&result.TargetName,
			&result.RawText,
		)
		if err != nil {
			return nil, fmt.Errorf("scan code edge: %w", err)
		}

		results = append(results, result)
	}

	err := rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate code edges: %w", err)
	}

	return results, nil
}

func (store *Store) astFindingLinksForChunk(
	ctx context.Context,
	chunkID string,
	limit int,
) ([]ASTFindingLink, error) {
	rows, err := store.database.QueryContext(
		ctx,
		`SELECT link_id, finding_kind, finding_id, chunk_id, path, policy_id,
			skill_id, symbol_path, content_hash, stale
		FROM ast_finding_links
		WHERE chunk_id = ?
		ORDER BY finding_kind, finding_id
		LIMIT ?`,
		chunkID,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query AST finding links: %w", err)
	}
	defer rows.Close()

	results := []ASTFindingLink{}

	for rows.Next() {
		var (
			result ASTFindingLink
			stale  int
		)

		err := rows.Scan(
			&result.ID,
			&result.FindingKind,
			&result.FindingID,
			&result.ChunkID,
			&result.Path,
			&result.PolicyID,
			&result.SkillID,
			&result.SymbolPath,
			&result.ContentHash,
			&stale,
		)
		if err != nil {
			return nil, fmt.Errorf("scan AST finding link: %w", err)
		}

		result.Stale = stale != 0
		results = append(results, result)
	}

	inlineErr0 := rows.Err()
	if inlineErr0 != nil {
		return nil, fmt.Errorf("iterate AST finding links: %w", inlineErr0)
	}

	return results, nil
}
