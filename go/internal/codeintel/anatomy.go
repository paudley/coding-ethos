// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

const (
	defaultAnatomySymbolsPerFile = 6
	directoryAnatomyOverfetch    = 4
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

	symbols, err := store.directoryAnatomySymbols(ctx, query, dir)
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
			AND (? = '' OR file.path = ? OR file.path LIKE ?)
		GROUP BY file.path, file.language, file.line_count, file.size_bytes,
			file.stale_reason
		ORDER BY file.path
		LIMIT ?`,
		strings.TrimSpace(query.Language),
		strings.TrimSpace(query.Language),
		dir,
		dir,
		anatomyDirLike(dir),
		defaultQueryLimit(query.Limit)*directoryAnatomyOverfetch,
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

		if !directDirectoryAnatomyChild(file.Path, dir) {
			continue
		}

		file.EstimatedTokens = estimateSourceTokens(file.SizeBytes)
		files = append(files, file)

		if len(files) >= defaultQueryLimit(query.Limit) {
			break
		}
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate directory anatomy files: %w", err)
	}

	return files, nil
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
	dir string,
) ([]directoryAnatomySymbolRow, error) {
	rows, err := store.database.QueryContext(
		ctx,
		`SELECT code_chunks.path, COALESCE(symbol_kind, ''),
			COALESCE(symbol_name, ''), COALESCE(symbol_path, ''), start_line
		FROM code_chunks
		JOIN code_files ON code_files.path = code_chunks.path
		WHERE COALESCE(symbol_path, '') != ''
			AND COALESCE(code_files.deleted_at_utc, '') = ''
			AND (? = '' OR code_chunks.language = ?)
			AND (? = '' OR code_chunks.path = ? OR code_chunks.path LIKE ?)
		ORDER BY code_chunks.path, start_line, start_byte
		LIMIT ?`,
		strings.TrimSpace(query.Language),
		strings.TrimSpace(query.Language),
		dir,
		dir,
		anatomyDirLike(dir),
		defaultQueryLimit(query.Limit)*
			max(1, anatomySymbolsPerFile(query))*
			directoryAnatomyOverfetch,
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

		if directDirectoryAnatomyChild(symbol.file, dir) {
			symbols = append(symbols, symbol)
		}
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate directory anatomy symbols: %w", err)
	}

	return symbols, nil
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

func directDirectoryAnatomyChild(path, dir string) bool {
	if dir == "" {
		return !strings.Contains(path, "/")
	}

	rest, found := strings.CutPrefix(path, dir+"/")

	return found && rest != "" && !strings.Contains(rest, "/")
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
