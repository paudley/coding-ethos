// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"fmt"
	"strings"
)

func (store *Store) CompactCodeContext(
	ctx context.Context,
	query CompactCodeContextQuery,
) (CompactCodeContext, error) {
	limit := defaultQueryLimit(query.Limit)

	repoMap, err := store.RepoMap(ctx, query)
	if err != nil {
		return CompactCodeContext{}, err
	}

	symbols, err := store.SymbolSummaries(ctx, query)
	if err != nil {
		return CompactCodeContext{}, err
	}

	chunks, err := store.CodeChunks(ctx, CodeChunkQuery{
		Path:     strings.TrimSpace(query.Path),
		Language: strings.TrimSpace(query.Language),
		Limit:    limit,
	})
	if err != nil {
		return CompactCodeContext{}, err
	}

	err = store.validateASTContextPathsFresh(
		ctx,
		query.Root,
		compactContextPaths(repoMap, symbols, chunks),
	)
	if err != nil {
		return CompactCodeContext{}, err
	}

	return CompactCodeContext{
		IndexFresh: len(repoMap) > 0,
		RepoMap:    repoMap,
		Symbols:    symbols,
		Chunks:     chunks,
	}, nil
}

func (store *Store) RepoMap(
	ctx context.Context,
	query CompactCodeContextQuery,
) ([]RepoMapEntry, error) {
	rows, err := store.database.QueryContext(
		ctx,
		`SELECT file.path, file.language, file.line_count,
			COALESCE(file.stale_reason, ''),
			COUNT(chunk.chunk_id) AS chunks,
			SUM(CASE WHEN COALESCE(chunk.symbol_path, '') != '' THEN 1 ELSE 0 END) AS symbols
		FROM code_files file
		LEFT JOIN code_chunks chunk ON chunk.path = file.path
		WHERE (? = '' OR file.path = ?)
			AND (? = '' OR file.language = ?)
			AND COALESCE(file.deleted_at_utc, '') = ''
		GROUP BY file.path, file.language, file.line_count, file.stale_reason
		ORDER BY file.path
		LIMIT ?`,
		strings.TrimSpace(query.Path),
		strings.TrimSpace(query.Path),
		strings.TrimSpace(query.Language),
		strings.TrimSpace(query.Language),
		defaultQueryLimit(query.Limit),
	)
	if err != nil {
		return nil, fmt.Errorf("query repo map: %w", err)
	}
	defer rows.Close()

	results := []RepoMapEntry{}

	for rows.Next() {
		var result RepoMapEntry

		err = rows.Scan(
			&result.Path,
			&result.Language,
			&result.LineCount,
			&result.StaleReason,
			&result.Chunks,
			&result.Symbols,
		)
		if err != nil {
			return nil, fmt.Errorf("scan repo map entry: %w", err)
		}

		results = append(results, result)
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate repo map: %w", err)
	}

	err = store.validateASTContextPathsFresh(ctx, query.Root, repoMapPaths(results))
	if err != nil {
		return nil, err
	}

	return results, nil
}

func (store *Store) SymbolSummaries(
	ctx context.Context,
	query CompactCodeContextQuery,
) ([]SymbolSummary, error) {
	rows, err := store.database.QueryContext(
		ctx,
		`SELECT code_chunks.path, code_chunks.language, node_kind, COALESCE(symbol_kind, ''),
			COALESCE(symbol_name, ''), COALESCE(symbol_path, ''),
			start_line, end_line, code_chunks.content_hash, search_text
		FROM code_chunks
		JOIN code_files ON code_files.path = code_chunks.path
		WHERE COALESCE(symbol_path, '') != ''
			AND (? = '' OR code_chunks.path = ?)
			AND (? = '' OR code_chunks.language = ?)
			AND COALESCE(code_files.deleted_at_utc, '') = ''
		ORDER BY code_chunks.path, start_line, start_byte
		LIMIT ?`,
		strings.TrimSpace(query.Path),
		strings.TrimSpace(query.Path),
		strings.TrimSpace(query.Language),
		strings.TrimSpace(query.Language),
		defaultQueryLimit(query.Limit),
	)
	if err != nil {
		return nil, fmt.Errorf("query symbol summaries: %w", err)
	}
	defer rows.Close()

	results := []SymbolSummary{}

	for rows.Next() {
		var (
			result SymbolSummary
			text   string
		)

		err = rows.Scan(
			&result.Path,
			&result.Language,
			&result.NodeKind,
			&result.SymbolKind,
			&result.SymbolName,
			&result.SymbolPath,
			&result.StartLine,
			&result.EndLine,
			&result.ContentHash,
			&text,
		)
		if err != nil {
			return nil, fmt.Errorf("scan symbol summary: %w", err)
		}

		result.TokenSize = len(strings.Fields(text))
		results = append(results, result)
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate symbol summaries: %w", err)
	}

	return results, nil
}

func compactContextPaths(
	repoMap []RepoMapEntry,
	symbols []SymbolSummary,
	chunks []CodeChunk,
) []string {
	paths := repoMapPaths(repoMap)

	for _, symbol := range symbols {
		paths = append(paths, symbol.Path)
	}

	for _, chunk := range chunks {
		paths = append(paths, chunk.Path)
	}

	return paths
}

func repoMapPaths(entries []RepoMapEntry) []string {
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		paths = append(paths, entry.Path)
	}

	return paths
}
