// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

func (store *Store) CodeContext(
	ctx context.Context,
	query CodeContextQuery,
) (CodeContext, error) {
	chunk, err := store.findCodeContextChunk(ctx, query)
	if err != nil {
		return CodeContext{}, err
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}
	context := CodeContext{Chunk: chunk}
	if chunk.ParentChunkID != "" {
		parent, err := store.codeChunkByID(ctx, chunk.ParentChunkID)
		if err != nil {
			return CodeContext{}, err
		}
		context.Parent = &parent
	}
	children, err := store.childCodeChunks(ctx, chunk.ID, limit)
	if err != nil {
		return CodeContext{}, err
	}
	context.Children = children
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

	return CodeChunk{}, fmt.Errorf("code chunk context not found")
}

func (store *Store) codeChunkByPathLine(ctx context.Context, path string, line int) (CodeChunk, error) {
	rows, err := store.db.QueryContext(
		ctx,
		`SELECT
			chunk_id, path, language, node_kind, symbol_kind, symbol_name,
			symbol_path, COALESCE(parent_symbol_path, ''), parent_chunk_id, start_byte, end_byte,
			start_line, end_line, content_hash, search_text, raw_text
		FROM code_chunks
		WHERE path = ?
			AND start_line <= ?
			AND end_line >= ?
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
		return CodeChunk{}, fmt.Errorf("code chunk context not found at %s:%d", path, line)
	}

	return chunks[0], nil
}

func (store *Store) codeChunkByID(ctx context.Context, id string) (CodeChunk, error) {
	rows, err := store.db.QueryContext(
		ctx,
		`SELECT
			chunk_id, path, language, node_kind, symbol_kind, symbol_name,
			symbol_path, COALESCE(parent_symbol_path, ''), parent_chunk_id, start_byte, end_byte,
			start_line, end_line, content_hash, search_text, raw_text
		FROM code_chunks
		WHERE chunk_id = ?`,
		id,
	)
	if err != nil {
		return CodeChunk{}, fmt.Errorf("query code chunk %q: %w", id, err)
	}
	defer rows.Close()
	chunks, err := scanCodeChunks(rows)
	if err != nil {
		return CodeChunk{}, err
	}
	if len(chunks) != 1 {
		return CodeChunk{}, fmt.Errorf("code chunk %q not found", id)
	}

	return chunks[0], nil
}

func (store *Store) childCodeChunks(
	ctx context.Context,
	parentID string,
	limit int,
) ([]CodeChunk, error) {
	rows, err := store.db.QueryContext(
		ctx,
		`SELECT
			chunk_id, path, language, node_kind, symbol_kind, symbol_name,
			symbol_path, COALESCE(parent_symbol_path, ''), parent_chunk_id, start_byte, end_byte,
			start_line, end_line, content_hash, search_text, raw_text
		FROM code_chunks
		WHERE parent_chunk_id = ?
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

func scanCodeChunks(rows *sql.Rows) ([]CodeChunk, error) {
	results := []CodeChunk{}
	for rows.Next() {
		var result CodeChunk
		if err := rows.Scan(
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
		); err != nil {
			return nil, fmt.Errorf("scan code chunk: %w", err)
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
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

func (store *Store) outgoingCodeEdges(
	ctx context.Context,
	chunkID string,
	limit int,
) ([]CodeEdge, error) {
	rows, err := store.db.QueryContext(
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
	rows, err := store.db.QueryContext(
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
		if err := rows.Scan(
			&result.ID,
			&result.Kind,
			&result.Path,
			&result.SourceChunkID,
			&result.TargetPath,
			&result.TargetChunkID,
			&result.TargetSymbolPath,
			&result.TargetName,
			&result.RawText,
		); err != nil {
			return nil, fmt.Errorf("scan code edge: %w", err)
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate code edges: %w", err)
	}

	return results, nil
}

func (store *Store) astFindingLinksForChunk(
	ctx context.Context,
	chunkID string,
	limit int,
) ([]ASTFindingLink, error) {
	rows, err := store.db.QueryContext(
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
		var result ASTFindingLink
		var stale int
		if err := rows.Scan(
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
		); err != nil {
			return nil, fmt.Errorf("scan AST finding link: %w", err)
		}
		result.Stale = stale != 0
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate AST finding links: %w", err)
	}

	return results, nil
}
