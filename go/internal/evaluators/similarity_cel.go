// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package evaluators

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	_ "github.com/duckdb/duckdb-go/v2"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/celexpr"
	"blackcat.ca/coding-ethos/go/internal/minhash"
	"blackcat.ca/coding-ethos/go/internal/similarityconfig"
)

const (
	// similarityPercentScale converts a float64 similarity to a percentage.
	similarityPercentScale = 100
	// uint64ByteSize is the byte width of a uint64 value.
	uint64ByteSize = 8
)

func celSimilarityFacts(
	evalContext Context,
	expression string,
) []celexpr.SimilarityFactInput {
	if !strings.Contains(expression, "similarity_facts") {
		return nil
	}

	if len(evalContext.SimilarityFacts) > 0 {
		return evalContext.SimilarityFacts
	}

	if evalContext.Cwd == "" {
		return nil
	}

	settings, err := similarityconfig.LoadFromRoot(evalContext.Cwd)
	if err != nil || !settings.Enabled {
		return nil
	}

	return loadSimilarityFactsFromDB(evalContext.Cwd, evalContext.Files, settings)
}

func loadSimilarityFactsFromDB(
	cwd string,
	files []string,
	settings similarityconfig.Settings,
) []celexpr.SimilarityFactInput {
	if len(files) == 0 {
		return nil
	}

	dbPath := filepath.Join(cwd, ".coding-ethos", "code-intel.duckdb")

	_, statErr := os.Stat(dbPath)
	if statErr != nil {
		return nil
	}

	database, err := sql.Open("duckdb", dbPath+"?access_mode=READ_ONLY")
	if err != nil {
		return nil
	}
	defer database.Close()

	ctx := context.Background()
	config := settings.MinHashConfig()

	var facts []celexpr.SimilarityFactInput

	for _, file := range files {
		fileFacts := querySimilarityForFile(ctx, database, file, config, settings)
		facts = append(facts, fileFacts...)
	}

	return thresholdSimilarityFacts(facts, settings.StructuralThreshold)
}

type chunkRow struct {
	symbolName     string
	symbolKind     string
	symbolPath     string
	language       string
	normalizedHash sql.NullString
	sigBlob        []byte
}

func querySimilarityForFile(
	ctx context.Context,
	database *sql.DB,
	path string,
	config minhash.Config,
	settings similarityconfig.Settings,
) []celexpr.SimilarityFactInput {
	rows, err := database.QueryContext(
		ctx,
		`SELECT chunk_id, symbol_name, symbol_kind, symbol_path, language,
			normalized_hash, minhash_sig
		FROM code_chunks WHERE path = ?`,
		path,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var facts []celexpr.SimilarityFactInput

	for rows.Next() {
		var (
			chunkID string
			row     chunkRow
		)

		scanErr := rows.Scan(
			&chunkID, &row.symbolName, &row.symbolKind, &row.symbolPath,
			&row.language, &row.normalizedHash, &row.sigBlob,
		)
		if scanErr != nil {
			continue
		}

		facts = appendChunkFacts(
			ctx, facts, database, path, row, config, settings,
		)
	}

	rowsErr := rows.Err()
	if rowsErr != nil {
		return nil
	}

	return facts
}

func appendChunkFacts(
	ctx context.Context,
	facts []celexpr.SimilarityFactInput,
	database *sql.DB,
	path string,
	row chunkRow,
	config minhash.Config,
	settings similarityconfig.Settings,
) []celexpr.SimilarityFactInput {
	if settings.ExactNormalized && row.normalizedHash.Valid &&
		row.normalizedHash.String != "" {
		exactFacts := queryExactMatches(
			ctx, database, path, row.symbolName, row.symbolKind,
			row.symbolPath, row.language, row.normalizedHash.String,
		)
		facts = append(facts, exactFacts...)
	}

	if len(row.sigBlob) > 0 {
		lshFacts := queryLSHMatches(
			ctx, database, path, row.symbolName, row.symbolKind,
			row.symbolPath, row.language, row.sigBlob, config, settings,
		)
		facts = append(facts, lshFacts...)
	}

	return facts
}

func queryExactMatches(
	ctx context.Context,
	database *sql.DB,
	sourcePath string,
	symbolName string,
	symbolKind string,
	symbolPath string,
	language string,
	normalizedHash string,
) []celexpr.SimilarityFactInput {
	rows, err := database.QueryContext(
		ctx,
		`SELECT path, symbol_name, symbol_kind, start_line
		FROM code_chunks
		WHERE normalized_hash = ? AND path != ?
		LIMIT 10`,
		normalizedHash, sourcePath,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var facts []celexpr.SimilarityFactInput

	for rows.Next() {
		var (
			matchPath       string
			matchSymbolName string
			matchSymbolKind string
			matchStartLine  int64
		)

		scanErr := rows.Scan(
			&matchPath, &matchSymbolName,
			&matchSymbolKind, &matchStartLine,
		)
		if scanErr != nil {
			continue
		}

		facts = append(facts, celexpr.SimilarityFactInput{
			File:            sourcePath,
			SymbolName:      symbolName,
			SymbolKind:      symbolKind,
			SymbolPath:      symbolPath,
			Language:        language,
			MatchPath:       matchPath,
			MatchSymbolName: matchSymbolName,
			MatchSymbolKind: matchSymbolKind,
			MatchStartLine:  matchStartLine,
			Similarity:      1.0,
			ExactNormalized: true,
		})
	}

	exactRowsErr := rows.Err()
	if exactRowsErr != nil {
		return nil
	}

	return facts
}

func queryLSHMatches(
	ctx context.Context,
	database *sql.DB,
	sourcePath string,
	symbolName string,
	symbolKind string,
	symbolPath string,
	language string,
	sigBlob []byte,
	config minhash.Config,
	settings similarityconfig.Settings,
) []celexpr.SimilarityFactInput {
	querySig := minhash.Signature{Values: unpackSigBlob(sigBlob)}
	if len(querySig.Values) == 0 {
		return nil
	}

	bandHashes := minhash.BandHashes(querySig, config)
	if len(bandHashes) == 0 {
		return nil
	}

	rows := queryLSHCandidateRows(ctx, database, bandHashes, sourcePath)
	if rows == nil {
		return nil
	}
	defer rows.Close()

	facts := collectLSHFacts(
		rows, querySig, sourcePath, symbolName,
		symbolKind, symbolPath, language, settings.CandidateThreshold,
	)

	if rows.Err() != nil {
		return nil
	}

	return facts
}

func queryLSHCandidateRows(
	ctx context.Context,
	database *sql.DB,
	bandHashes []string,
	sourcePath string,
) *sql.Rows {
	placeholders := make([]string, len(bandHashes))
	args := make([]any, 0, len(bandHashes)+1)

	for idx, hash := range bandHashes {
		placeholders[idx] = "?"

		args = append(args, hash)
	}

	args = append(args, sourcePath)

	//nolint:gosec // Integer placeholders from internal computed band hashes
	query := `SELECT DISTINCT lb.chunk_id, cc.path, cc.symbol_name,
		cc.symbol_kind, cc.start_line, cc.minhash_sig
		FROM lsh_bands lb
		JOIN code_chunks cc ON cc.chunk_id = lb.chunk_id
		WHERE lb.band_hash IN (` + strings.Join(placeholders, ",") +
		`) AND cc.path != ?
		LIMIT 50`

	rows, err := database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil
	}

	return rows
}

func collectLSHFacts(
	rows *sql.Rows,
	querySig minhash.Signature,
	sourcePath string,
	symbolName string,
	symbolKind string,
	symbolPath string,
	language string,
	threshold float64,
) []celexpr.SimilarityFactInput {
	var facts []celexpr.SimilarityFactInput

	for rows.Next() {
		var (
			candidateID  string
			matchPath    string
			matchName    string
			matchKind    string
			matchLine    int64
			candidateSig []byte
		)

		scanErr := rows.Scan(
			&candidateID, &matchPath, &matchName,
			&matchKind, &matchLine, &candidateSig,
		)
		if scanErr != nil {
			continue
		}

		candidateValues := unpackSigBlob(candidateSig)
		if len(candidateValues) == 0 {
			continue
		}

		similarity := minhash.EstimateJaccard(
			querySig,
			minhash.Signature{Values: candidateValues},
		)

		if similarity < threshold {
			continue
		}

		facts = append(facts, celexpr.SimilarityFactInput{
			File:            sourcePath,
			SymbolName:      symbolName,
			SymbolKind:      symbolKind,
			SymbolPath:      symbolPath,
			Language:        language,
			MatchPath:       matchPath,
			MatchSymbolName: matchName,
			MatchSymbolKind: matchKind,
			MatchStartLine:  matchLine,
			Similarity:      similarity,
			ExactNormalized: false,
		})
	}

	return facts
}

func thresholdSimilarityFacts(
	facts []celexpr.SimilarityFactInput,
	threshold float64,
) []celexpr.SimilarityFactInput {
	if len(facts) == 0 {
		return nil
	}

	filtered := make([]celexpr.SimilarityFactInput, 0, len(facts))
	for _, fact := range facts {
		if fact.ExactNormalized || fact.Similarity >= threshold {
			filtered = append(filtered, fact)
		}
	}

	return filtered
}

func applySimilarityDiagnostic(
	diagnostic *diagnostics.Diagnostic,
	activation map[string]any,
) {
	facts, ok := activation["similarity_facts"].([]celexpr.SimilarityFactInput)
	if !ok || len(facts) == 0 {
		return
	}

	first := facts[0]
	diagnostic.File = first.File

	var details []string

	for _, fact := range facts {
		pct := int(fact.Similarity * similarityPercentScale)
		kind := "similar"

		if fact.ExactNormalized {
			kind = "structurally identical"
		}

		details = append(details, fmt.Sprintf(
			"`%s` in `%s:%d` is %s (%d%% match)",
			fact.MatchSymbolName, fact.MatchPath,
			fact.MatchStartLine, kind, pct,
		))

		diagnostic.RelatedLocations = append(
			diagnostic.RelatedLocations,
			diagnostics.RelatedLocation{
				File:       fact.MatchPath,
				SymbolName: fact.MatchSymbolName,
				SymbolKind: fact.MatchSymbolKind,
				Line:       int(fact.MatchStartLine),
				Message: kind + " to " + fact.SymbolName +
					" (" + strconv.Itoa(pct) + "%)",
			},
		)
	}

	diagnostic.Message = "Similar code detected: " +
		strings.Join(details, "; ")
	diagnostic.Metadata["similarity_match_count"] = len(facts)
	diagnostic.Metadata["similarity_source_symbol"] = first.SymbolName
	diagnostic.Metadata["similarity_source_file"] = first.File
}

func unpackSigBlob(data []byte) []uint64 {
	if len(data) == 0 || len(data)%uint64ByteSize != 0 {
		return nil
	}

	sig := make([]uint64, len(data)/uint64ByteSize)

	for i := range sig {
		sig[i] = binary.LittleEndian.Uint64(data[i*uint64ByteSize:])
	}

	return sig
}
