// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

const (
	defaultAnatomySymbolsPerFile = 6
	directoryAnatomySymbolArgs   = 4
	tokenEstimateBytes           = 4
)

// DirectoryAnatomy returns a compact directory-local file map inspired by
// Aider's repomap, using coding-ethos' repo-local AST index instead of a
// prompt-time parser/cache.
func (store *Store) DirectoryAnatomy(
	ctx context.Context,
	query DirectoryAnatomyQuery,
) (DirectoryAnatomy, error) {
	dir := cleanDirectoryAnatomyPath(query.Path)

	files, err := store.directoryAnatomyFiles(ctx, query, dir)
	if err != nil {
		return DirectoryAnatomy{}, err
	}

	if len(files) == 0 {
		return DirectoryAnatomy{Path: dir}, nil
	}

	err = store.validateASTContextPathsFresh(
		ctx,
		query.Root,
		directoryAnatomyFilePaths(files),
	)
	if err != nil {
		return DirectoryAnatomy{}, err
	}

	symbols, err := store.directoryAnatomySymbols(ctx, query, files)
	if err != nil {
		return DirectoryAnatomy{}, err
	}

	symbolsByFile := map[string][]DirectoryAnatomySymbol{}
	for _, symbol := range symbols {
		if len(symbolsByFile[symbol.file]) >= anatomySymbolsPerFile(query) {
			continue
		}

		symbolsByFile[symbol.file] = append(
			symbolsByFile[symbol.file],
			DirectoryAnatomySymbol{
				Kind:       symbol.kind,
				Name:       symbol.name,
				SymbolPath: symbol.symbolPath,
				StartLine:  symbol.startLine,
			},
		)
	}

	for index := range files {
		files[index].Symbols = symbolsByFile[files[index].Path]
	}

	return DirectoryAnatomy{Path: dir, Files: files}, nil
}

func (store *Store) directoryAnatomyFiles(
	ctx context.Context,
	query DirectoryAnatomyQuery,
	dir string,
) ([]DirectoryAnatomyFile, error) {
	rows, err := store.database.QueryContext(
		ctx,
		`SELECT file.path, file.language, file.line_count, file.size_bytes,
			COALESCE(file.stale_reason, ''),
			COUNT(chunk.chunk_id) AS chunks,
			SUM(CASE WHEN COALESCE(chunk.symbol_path, '') != '' THEN 1 ELSE 0 END) AS symbols
		FROM code_files file
		LEFT JOIN code_chunks chunk ON chunk.path = file.path
		WHERE COALESCE(file.deleted_at_utc, '') = ''
			AND (? = '' OR file.language = ?)
			AND (
				(? = 0 AND ((? = '' AND file.path NOT LIKE ?)
					OR (? != '' AND file.path LIKE ? AND file.path NOT LIKE ?)))
				OR (? != 0 AND ((? = '' AND (? = '' OR file.path NOT LIKE ?))
					OR (? != '' AND file.path LIKE ? AND (? = '' OR file.path NOT LIKE ?))))
			)
		GROUP BY file.path, file.language, file.line_count, file.size_bytes,
			file.stale_reason
		ORDER BY file.path
		LIMIT ?`,
		strings.TrimSpace(query.Language),
		strings.TrimSpace(query.Language),
		anatomyBoolInt(query.IncludeNested),
		dir,
		anatomyNestedLike(""),
		dir,
		anatomyDirLike(dir),
		anatomyNestedLike(dir),
		anatomyBoolInt(query.IncludeNested),
		dir,
		anatomyBeyondDepthLike("", query.MaxDepth),
		anatomyBeyondDepthLike("", query.MaxDepth),
		dir,
		anatomyDirLike(dir),
		anatomyBeyondDepthLike(dir, query.MaxDepth),
		anatomyBeyondDepthLike(dir, query.MaxDepth),
		defaultQueryLimit(query.Limit),
	)
	if err != nil {
		return nil, fmt.Errorf("query directory anatomy files: %w", err)
	}
	defer rows.Close()

	files := []DirectoryAnatomyFile{}

	for rows.Next() {
		var file DirectoryAnatomyFile

		err = rows.Scan(
			&file.Path,
			&file.Language,
			&file.LineCount,
			&file.SizeBytes,
			&file.StaleReason,
			&file.ChunkCount,
			&file.SymbolCount,
		)
		if err != nil {
			return nil, fmt.Errorf("scan directory anatomy file: %w", err)
		}

		file.EstimatedTokens = estimateSourceTokens(file.SizeBytes)
		files = append(files, file)
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate directory anatomy files: %w", err)
	}

	return files, nil
}

func directoryAnatomyFilePaths(files []DirectoryAnatomyFile) []string {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Path)
	}

	return paths
}

type directoryAnatomySymbolRow struct {
	file       string
	kind       string
	name       string
	symbolPath string
	startLine  int
}

func (store *Store) directoryAnatomySymbols(
	ctx context.Context,
	query DirectoryAnatomyQuery,
	files []DirectoryAnatomyFile,
) ([]directoryAnatomySymbolRow, error) {
	if len(files) == 0 {
		return nil, nil
	}

	placeholders, args := directoryAnatomySymbolQueryArgs(query, files)

	// #nosec G202 -- IN-list placeholders are generated from selected file rows.
	rows, err := store.database.QueryContext(
		ctx,
		`SELECT path, kind, name, symbol_path, start_line
		FROM (
			SELECT code_chunks.path,
				COALESCE(symbol_kind, '') AS kind,
				COALESCE(symbol_name, '') AS name,
				COALESCE(symbol_path, '') AS symbol_path,
				start_line,
				start_byte,
				ROW_NUMBER() OVER (
					PARTITION BY code_chunks.path
					ORDER BY start_line, start_byte
				) AS symbol_rank
			FROM code_chunks
			JOIN code_files ON code_files.path = code_chunks.path
			WHERE COALESCE(symbol_path, '') != ''
				AND COALESCE(code_files.deleted_at_utc, '') = ''
				AND code_chunks.path IN (`+strings.Join(placeholders, ",")+`)
				AND (? = '' OR code_chunks.language = ?)
		)
		WHERE symbol_rank <= ?
		ORDER BY path, start_line, start_byte
		LIMIT ?`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("query directory anatomy symbols: %w", err)
	}
	defer rows.Close()

	symbols := []directoryAnatomySymbolRow{}

	for rows.Next() {
		var symbol directoryAnatomySymbolRow

		err = rows.Scan(
			&symbol.file,
			&symbol.kind,
			&symbol.name,
			&symbol.symbolPath,
			&symbol.startLine,
		)
		if err != nil {
			return nil, fmt.Errorf("scan directory anatomy symbol: %w", err)
		}

		symbols = append(symbols, symbol)
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate directory anatomy symbols: %w", err)
	}

	return symbols, nil
}

func directoryAnatomySymbolQueryArgs(
	query DirectoryAnatomyQuery,
	files []DirectoryAnatomyFile,
) ([]string, []any) {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Path)
	}

	placeholders := make([]string, len(paths))

	args := make([]any, 0, len(paths)+directoryAnatomySymbolArgs)
	for index, path := range paths {
		placeholders[index] = "?"

		args = append(args, path)
	}

	args = append(
		args,
		strings.TrimSpace(query.Language),
		strings.TrimSpace(query.Language),
		anatomySymbolsPerFile(query),
		defaultQueryLimit(query.Limit)*max(1, anatomySymbolsPerFile(query)),
	)

	return placeholders, args
}

func cleanDirectoryAnatomyPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}

	cleaned := filepath.ToSlash(filepath.Clean(path))
	if cleaned == "." {
		return ""
	}

	return strings.TrimSuffix(cleaned, "/")
}

func anatomyDirLike(dir string) string {
	if dir == "" {
		return "%"
	}

	return dir + "/%"
}

func anatomyNestedLike(dir string) string {
	if dir == "" {
		return "%/%"
	}

	return dir + "/%/%"
}

func anatomyBeyondDepthLike(dir string, maxDepth int) string {
	if maxDepth <= 0 {
		return ""
	}

	segments := strings.Repeat("/%", maxDepth)
	if dir == "" {
		return strings.TrimPrefix(segments+"/%", "/")
	}

	return dir + segments + "/%"
}

func anatomyBoolInt(value bool) int {
	if value {
		return 1
	}

	return 0
}

func estimateSourceTokens(sizeBytes int) int {
	if sizeBytes <= 0 {
		return 0
	}

	return max(1, (sizeBytes+tokenEstimateBytes-1)/tokenEstimateBytes)
}

func anatomySymbolsPerFile(query DirectoryAnatomyQuery) int {
	if query.SymbolsPerFile <= 0 {
		return defaultAnatomySymbolsPerFile
	}

	return query.SymbolsPerFile
}
