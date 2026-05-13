// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/astfacts"
	"blackcat.ca/coding-ethos/go/internal/minhash"
	"blackcat.ca/coding-ethos/go/internal/similarityconfig"
)

const (
	fallbackSimilarityLimit = 10
	// candidateMapSizeFactor bounds intermediate matches before final ranking.
	candidateMapSizeFactor = 4
	uint64ByteSize         = 8
)

type SimilarChunk struct {
	ChunkID         string `json:"chunk_id"`
	Path            string `json:"path"`
	SourcePath      string `json:"source_path,omitempty"`
	SourceSymbol    string `json:"source_symbol,omitempty"`
	SourceKind      string `json:"source_kind,omitempty"`
	SymbolName      string `json:"symbol_name"`
	SymbolKind      string `json:"symbol_kind"`
	sigBlob         []byte
	Similarity      float64 `json:"similarity"`
	StartLine       int     `json:"start_line"`
	ExactNormalized bool    `json:"exact_normalized"`
}

// SimilarCodeQuery describes an ad hoc code snippet similarity lookup.
type SimilarCodeQuery struct {
	Code     string
	Path     string
	Language string
	Settings similarityconfig.Settings
	Limit    int
}

// SimilarCode compares proposed code against indexed repository chunks.
func (store *Store) SimilarCode(
	ctx context.Context,
	query SimilarCodeQuery,
) ([]SimilarChunk, error) {
	if strings.TrimSpace(query.Code) == "" {
		return nil, nil
	}

	settings := query.Settings
	if settings.SignatureSize == 0 {
		settings = similarityconfig.DefaultSettings()
	}

	if !settings.Enabled {
		return nil, nil
	}

	chunks, err := querySymbols(query, settings)
	if err != nil {
		return nil, err
	}

	limit := defaultSimilarityLimit(query.Limit, settings.MaxMatches)
	matchesByKey := map[string]SimilarChunk{}

	for _, chunk := range chunks {
		err = store.recordChunkSimilarity(ctx, matchesByKey, chunk, settings, limit)
		if err != nil {
			return nil, err
		}
	}

	matches := make([]SimilarChunk, 0, len(matchesByKey))
	for _, match := range matchesByKey {
		if match.ExactNormalized || match.Similarity >= settings.StructuralThreshold {
			matches = append(matches, match)
		}
	}

	slices.SortFunc(matches, compareSimilarChunks)

	if len(matches) > limit {
		matches = matches[:limit]
	}

	return matches, nil
}

func (store *Store) recordChunkSimilarity(
	ctx context.Context,
	matchesByKey map[string]SimilarChunk,
	chunk astfacts.Symbol,
	settings similarityconfig.Settings,
	limit int,
) error {
	if chunk.LineCount < settings.MinSymbolLines {
		return nil
	}

	err := store.recordExactSimilarity(ctx, matchesByKey, chunk, settings, limit)
	if err != nil {
		return err
	}

	return store.recordLSHSimilarity(ctx, matchesByKey, chunk, settings, limit)
}

func (store *Store) recordExactSimilarity(
	ctx context.Context,
	matchesByKey map[string]SimilarChunk,
	chunk astfacts.Symbol,
	settings similarityconfig.Settings,
	limit int,
) error {
	if !settings.ExactNormalized || chunk.NormalizedHash == "" {
		return nil
	}

	exact, err := FindExactNormalizedMatches(
		ctx,
		store.database,
		chunk.NormalizedHash,
		chunk.Path,
		limit,
	)
	if err != nil {
		return err
	}

	recordSimilarMatches(matchesByKey, chunk, exact, limit)

	return nil
}

func (store *Store) recordLSHSimilarity(
	ctx context.Context,
	matchesByKey map[string]SimilarChunk,
	chunk astfacts.Symbol,
	settings similarityconfig.Settings,
	limit int,
) error {
	if len(chunk.MinHashSig) == 0 {
		return nil
	}

	sig := minhash.Signature{Values: chunk.MinHashSig}

	candidates, err := FindLSHCandidates(
		ctx,
		store.database,
		minhash.BandHashes(sig, settings.MinHashConfig()),
		chunk.Path,
	)
	if err != nil {
		return err
	}

	refined := RefineLSHCandidates(
		sig,
		candidates,
		settings.CandidateThreshold,
	)
	recordSimilarMatches(matchesByKey, chunk, refined, limit)

	return nil
}

func FindExactNormalizedMatches(
	ctx context.Context,
	database *sql.DB,
	normalizedHash string,
	excludePath string,
	limit int,
) ([]SimilarChunk, error) {
	limit = defaultSimilarityLimit(limit, fallbackSimilarityLimit)

	rows, err := database.QueryContext(
		ctx,
		`SELECT chunk_id, path, symbol_name, symbol_kind, start_line
		FROM code_chunks
		WHERE normalized_hash = ? AND path != ?
		LIMIT ?`,
		normalizedHash,
		excludePath,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query normalized matches: %w", err)
	}
	defer rows.Close()

	var matches []SimilarChunk

	for rows.Next() {
		var match SimilarChunk

		scanErr := rows.Scan(
			&match.ChunkID,
			&match.Path,
			&match.SymbolName,
			&match.SymbolKind,
			&match.StartLine,
		)
		if scanErr != nil {
			return nil, fmt.Errorf("scan normalized match: %w", scanErr)
		}

		match.Similarity = 1.0
		match.ExactNormalized = true

		matches = append(matches, match)
	}

	rowsErr := rows.Err()
	if rowsErr != nil {
		return nil, fmt.Errorf("iterate normalized matches: %w", rowsErr)
	}

	return matches, nil
}

func querySymbols(
	query SimilarCodeQuery,
	settings similarityconfig.Settings,
) ([]astfacts.Symbol, error) {
	path := similarityPath(query)

	file, analyzed, err := astfacts.Analyze(path, []byte(query.Code))
	if err != nil {
		return nil, fmt.Errorf("analyze similarity query code: %w", err)
	}

	if analyzed && len(file.Symbols) > 0 {
		return file.Symbols, nil
	}

	language := strings.TrimSpace(query.Language)
	if language == "" && analyzed {
		language = file.Language
	}

	tokens := minhash.NormalizeTokens(query.Code, language)
	sig := minhash.ComputeSignature(tokens, settings.MinHashConfig())

	return []astfacts.Symbol{{
		Path:           path,
		Language:       language,
		SymbolName:     "snippet",
		SymbolKind:     "snippet",
		SymbolPath:     "snippet",
		RawText:        query.Code,
		ContentHash:    astfacts.ContentHash([]byte(query.Code)),
		NormalizedHash: minhash.NormalizedHash(tokens),
		MinHashSig:     sig.Values,
		StartLine:      1,
		EndLine:        astfacts.LineCount([]byte(query.Code)),
		LineCount:      astfacts.LineCount([]byte(query.Code)),
	}}, nil
}

func similarityPath(query SimilarCodeQuery) string {
	if strings.TrimSpace(query.Path) != "" {
		return filepath.Clean(strings.TrimSpace(query.Path))
	}

	switch strings.ToLower(strings.TrimSpace(query.Language)) {
	case astfacts.LanguageGo:
		return "__query__.go"
	case astfacts.LanguagePython:
		return "__query__.py"
	case astfacts.LanguageJavaScript:
		return "__query__.js"
	case astfacts.LanguageJSON:
		return "__query__.json"
	case astfacts.LanguageYAML:
		return "__query__.yaml"
	case astfacts.LanguageTOML:
		return "__query__.toml"
	case astfacts.LanguageMarkdown:
		return "__query__.md"
	default:
		return "__query__.txt"
	}
}

func recordSimilarMatches(
	matchesByKey map[string]SimilarChunk,
	source astfacts.Symbol,
	matches []SimilarChunk,
	limit int,
) {
	for _, match := range matches {
		match.SourcePath = source.Path
		match.SourceSymbol = source.SymbolName
		match.SourceKind = source.SymbolKind

		key := strings.Join(
			[]string{
				source.SymbolPath,
				match.ChunkID,
				strconv.FormatBool(match.ExactNormalized),
			},
			"\x00",
		)

		existing, found := matchesByKey[key]
		if found && existing.Similarity >= match.Similarity {
			continue
		}

		matchesByKey[key] = match

		if len(matchesByKey) >= limit*candidateMapSizeFactor {
			return
		}
	}
}

func compareSimilarChunks(left, right SimilarChunk) int {
	if left.ExactNormalized != right.ExactNormalized {
		if left.ExactNormalized {
			return -1
		}

		return 1
	}

	if left.Similarity > right.Similarity {
		return -1
	}

	if left.Similarity < right.Similarity {
		return 1
	}

	return strings.Compare(left.Path+left.SymbolName, right.Path+right.SymbolName)
}

func defaultSimilarityLimit(input, configured int) int {
	if input > 0 {
		return input
	}

	if configured > 0 {
		return configured
	}

	return fallbackSimilarityLimit
}

func FindLSHCandidates(
	ctx context.Context,
	database *sql.DB,
	bandHashes []string,
	excludePath string,
) ([]SimilarChunk, error) {
	if len(bandHashes) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(bandHashes))
	args := make([]any, 0, len(bandHashes)+1)

	for idx, hash := range bandHashes {
		placeholders[idx] = "?"

		args = append(args, hash)
	}

	args = append(args, excludePath)

	//nolint:gosec // Integer placeholders from internal computed band hashes
	query := fmt.Sprintf(
		`SELECT DISTINCT lb.chunk_id, cc.path, cc.symbol_name, cc.symbol_kind,
			cc.start_line, cc.minhash_sig
		FROM lsh_bands lb
		JOIN code_chunks cc ON cc.chunk_id = lb.chunk_id
		WHERE lb.band_hash IN (%s) AND cc.path != ?
		LIMIT 50`,
		strings.Join(placeholders, ","),
	)

	rows, err := database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query LSH candidates: %w", err)
	}
	defer rows.Close()

	var candidates []SimilarChunk

	for rows.Next() {
		var (
			candidate SimilarChunk
			sigBlob   []byte
		)

		scanErr := rows.Scan(
			&candidate.ChunkID,
			&candidate.Path,
			&candidate.SymbolName,
			&candidate.SymbolKind,
			&candidate.StartLine,
			&sigBlob,
		)
		if scanErr != nil {
			return nil, fmt.Errorf("scan LSH candidate: %w", scanErr)
		}

		candidate.Similarity = -1
		candidate.ExactNormalized = false
		candidate.sigBlob = sigBlob

		candidates = append(candidates, candidate)
	}

	rowsErr := rows.Err()
	if rowsErr != nil {
		return nil, fmt.Errorf("iterate LSH candidates: %w", rowsErr)
	}

	return candidates, nil
}

func RefineLSHCandidates(
	querySig minhash.Signature,
	candidates []SimilarChunk,
	threshold float64,
) []SimilarChunk {
	var refined []SimilarChunk

	for _, candidate := range candidates {
		candidateSig := minhash.Signature{Values: unpackMinHashSig(candidate.sigBlob)}
		if len(candidateSig.Values) == 0 {
			continue
		}

		similarity := minhash.EstimateJaccard(querySig, candidateSig)

		if similarity >= threshold {
			candidate.Similarity = similarity
			refined = append(refined, candidate)
		}
	}

	return refined
}

func StoreLSHBands(
	ctx context.Context,
	transaction *sql.Tx,
	chunkID string,
	path string,
	symbolName string,
	bandHashes []string,
) error {
	for bandIndex, bandHash := range bandHashes {
		_, err := transaction.ExecContext(
			ctx,
			`INSERT OR IGNORE INTO lsh_bands(band_hash, band_index, chunk_id, path, symbol_name)
			VALUES (?, ?, ?, ?, ?)`,
			bandHash,
			bandIndex,
			chunkID,
			path,
			symbolName,
		)
		if err != nil {
			return fmt.Errorf("store LSH band %d for chunk %q: %w", bandIndex, chunkID, err)
		}
	}

	return nil
}

func DeleteLSHBandsForPath(
	ctx context.Context,
	transaction *sql.Tx,
	path string,
) error {
	_, err := transaction.ExecContext(
		ctx,
		`DELETE FROM lsh_bands WHERE path = ?`,
		path,
	)
	if err != nil {
		return fmt.Errorf("delete LSH bands for path %q: %w", path, err)
	}

	return nil
}

func packMinHashSig(sig []uint64) []byte {
	if len(sig) == 0 {
		return nil
	}

	buf := make([]byte, len(sig)*uint64ByteSize)

	for i, v := range sig {
		binary.LittleEndian.PutUint64(buf[i*uint64ByteSize:], v)
	}

	return buf
}

func unpackMinHashSig(data []byte) []uint64 {
	if len(data) == 0 || len(data)%uint64ByteSize != 0 {
		return nil
	}

	sig := make([]uint64, len(data)/uint64ByteSize)

	for i := range sig {
		sig[i] = binary.LittleEndian.Uint64(data[i*uint64ByteSize:])
	}

	return sig
}
