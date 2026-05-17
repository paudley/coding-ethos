// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	defaultRepoMapLimit          = 20
	defaultRepoMapSymbolsPerFile = 4
	repoMapSymbolQueryArgs       = 4
	repoMapSymbolWeight          = 10
	repoMapChunkWeight           = 2
	repoMapLineDivisor           = 50
	repoMapLineWeightLimit       = 20
	repoMapSignatureMaxRunes     = 96
)

// GlobalRepoMap returns a compact repository-level AST map for startup and MCP
// context. It ranks files by indexed symbol density and then includes the first
// high-signal symbols from each selected file.
func (store *Store) GlobalRepoMap(
	ctx context.Context,
	query RepoMapQuery,
) (RepoMap, error) {
	files, err := store.repoMapFiles(ctx, query)
	if err != nil {
		return RepoMap{}, err
	}

	if len(files) == 0 {
		return RepoMap{Root: strings.TrimSpace(query.Root)}, nil
	}

	err = store.validateASTContextPathsFresh(ctx, query.Root, repoMapFilePaths(files))
	if err != nil {
		return RepoMap{}, err
	}

	symbols, err := store.repoMapSymbols(ctx, query, files)
	if err != nil {
		return RepoMap{}, err
	}

	symbolsByFile := map[string][]RepoMapSymbol{}
	for _, symbol := range symbols {
		if len(symbolsByFile[symbol.Path]) >= repoMapSymbolsPerFile(query) {
			continue
		}

		symbolsByFile[symbol.Path] = append(symbolsByFile[symbol.Path], symbol)
	}

	for index := range files {
		files[index].Symbols = symbolsByFile[files[index].Path]
	}

	return RepoMap{
		Root:  strings.TrimSpace(query.Root),
		Files: files,
	}, nil
}

func (store *Store) repoMapFiles(
	ctx context.Context,
	query RepoMapQuery,
) ([]RepoMapFile, error) {
	rows, err := store.database.QueryContext(
		ctx,
		`SELECT file.path, file.language, file.line_count,
			COUNT(chunk.chunk_id) AS chunks,
			SUM(CASE WHEN COALESCE(chunk.symbol_path, '') != '' THEN 1 ELSE 0 END) AS symbols
		FROM code_files file
		LEFT JOIN code_chunks chunk ON chunk.path = file.path
		WHERE (? = '' OR file.path = ?)
			AND (? = '' OR file.language = ?)
			AND COALESCE(file.deleted_at_utc, '') = ''
			AND COALESCE(file.stale_reason, '') = ''
		GROUP BY file.path, file.language, file.line_count
		ORDER BY symbols DESC, chunks DESC, file.line_count DESC, file.path
		LIMIT ?`,
		strings.TrimSpace(query.Path),
		strings.TrimSpace(query.Path),
		strings.TrimSpace(query.Language),
		strings.TrimSpace(query.Language),
		repoMapLimit(query),
	)
	if err != nil {
		return nil, fmt.Errorf("query global repo map files: %w", err)
	}
	defer rows.Close()

	files := []RepoMapFile{}

	for rows.Next() {
		var file RepoMapFile

		err = rows.Scan(
			&file.Path,
			&file.Language,
			&file.LineCount,
			&file.ChunkCount,
			&file.SymbolCount,
		)
		if err != nil {
			return nil, fmt.Errorf("scan global repo map file: %w", err)
		}

		file.Score = repoMapFileScore(file)
		files = append(files, file)
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate global repo map files: %w", err)
	}

	return files, nil
}

func (store *Store) repoMapSymbols(
	ctx context.Context,
	query RepoMapQuery,
	files []RepoMapFile,
) ([]RepoMapSymbol, error) {
	if len(files) == 0 {
		return nil, nil
	}

	placeholders, args := repoMapSymbolQueryArgsForFiles(query, files)

	// #nosec G202 -- IN-list placeholders are generated from selected file rows.
	rows, err := store.database.QueryContext(
		ctx,
		`SELECT path, language, kind, name, symbol_path, start_line, end_line, raw_text
		FROM (
			SELECT code_chunks.path,
				code_chunks.language,
				COALESCE(symbol_kind, '') AS kind,
				COALESCE(symbol_name, '') AS name,
				COALESCE(symbol_path, '') AS symbol_path,
				start_line,
				end_line,
				raw_text,
				ROW_NUMBER() OVER (
					PARTITION BY code_chunks.path
					ORDER BY start_line, start_byte
				) AS symbol_rank
			FROM code_chunks
			JOIN code_files ON code_files.path = code_chunks.path
			WHERE COALESCE(symbol_path, '') != ''
				AND COALESCE(code_files.deleted_at_utc, '') = ''
				AND COALESCE(code_files.stale_reason, '') = ''
				AND code_chunks.path IN (`+strings.Join(placeholders, ",")+`)
				AND (? = '' OR code_chunks.language = ?)
		)
		WHERE symbol_rank <= ?
		ORDER BY path, start_line, symbol_path
		LIMIT ?`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("query global repo map symbols: %w", err)
	}
	defer rows.Close()

	symbols := []RepoMapSymbol{}

	for rows.Next() {
		var (
			symbol  RepoMapSymbol
			rawText string
		)

		err = rows.Scan(
			&symbol.Path,
			&symbol.Language,
			&symbol.Kind,
			&symbol.Name,
			&symbol.SymbolPath,
			&symbol.StartLine,
			&symbol.EndLine,
			&rawText,
		)
		if err != nil {
			return nil, fmt.Errorf("scan global repo map symbol: %w", err)
		}

		symbol.Signature = repoMapSignature(rawText)
		symbols = append(symbols, symbol)
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate global repo map symbols: %w", err)
	}

	return symbols, nil
}

func repoMapSymbolQueryArgsForFiles(
	query RepoMapQuery,
	files []RepoMapFile,
) ([]string, []any) {
	placeholders := make([]string, len(files))
	args := make([]any, 0, len(files)+repoMapSymbolQueryArgs)

	for index, file := range files {
		placeholders[index] = "?"

		args = append(args, file.Path)
	}

	symbolLimit := repoMapSymbolsPerFile(query)

	args = append(
		args,
		strings.TrimSpace(query.Language),
		strings.TrimSpace(query.Language),
		symbolLimit,
		len(files)*symbolLimit,
	)

	return placeholders, args
}

func repoMapFilePaths(files []RepoMapFile) []string {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Path)
	}

	return paths
}

func repoMapLimit(query RepoMapQuery) int {
	if query.Limit > 0 {
		return query.Limit
	}

	return defaultRepoMapLimit
}

func repoMapSymbolsPerFile(query RepoMapQuery) int {
	if query.SymbolsPerFile > 0 {
		return query.SymbolsPerFile
	}

	return defaultRepoMapSymbolsPerFile
}

func repoMapFileScore(file RepoMapFile) int {
	lineWeight := int(math.Min(
		float64(file.LineCount/repoMapLineDivisor),
		repoMapLineWeightLimit,
	))

	return file.SymbolCount*repoMapSymbolWeight +
		file.ChunkCount*repoMapChunkWeight +
		lineWeight
}

func repoMapSignature(rawText string) string {
	for line := range strings.Lines(rawText) {
		signature := strings.TrimSpace(line)
		if signature == "" {
			continue
		}

		signature = strings.ReplaceAll(signature, ";", " ")

		return truncateRepoMapSignature(signature)
	}

	return ""
}

func truncateRepoMapSignature(value string) string {
	runes := []rune(value)
	if len(runes) <= repoMapSignatureMaxRunes {
		return value
	}

	return string(runes[:repoMapSignatureMaxRunes]) + "..."
}

// RenderRepoMapTOON renders the repository map as compact startup/MCP context.
func RenderRepoMapTOON(repoMap RepoMap) string {
	if len(repoMap.Files) == 0 {
		return ""
	}

	lines := []string{
		"coding_ethos_repo_map:",
		"root: " + quoteAnatomyValue(repoMap.Root),
		"files[" + strconv.Itoa(len(repoMap.Files)) +
			"]{path,language,lines,score,symbols}:",
	}

	for _, file := range repoMap.Files {
		lines = append(lines, strings.Join([]string{
			"  " + quoteAnatomyValue(file.Path),
			quoteAnatomyValue(file.Language),
			strconv.Itoa(file.LineCount),
			strconv.Itoa(file.Score),
			quoteAnatomyValue(renderRepoMapSymbols(file.Symbols)),
		}, ","))
	}

	return strings.Join(lines, "\n")
}

func renderRepoMapSymbols(symbols []RepoMapSymbol) string {
	parts := make([]string, 0, len(symbols))

	for _, symbol := range symbols {
		name := firstNonEmpty(symbol.SymbolPath, symbol.Name)
		if name == "" {
			continue
		}

		part := name
		if symbol.StartLine > 0 {
			part += "@" + strconv.Itoa(symbol.StartLine)
		}

		if symbol.Signature != "" {
			part += "=" + symbol.Signature
		}

		parts = append(parts, part)
	}

	return strings.Join(parts, ";")
}
