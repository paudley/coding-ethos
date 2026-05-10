// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/minhash"
)

const uint64ByteSize = 8

type SimilarChunk struct {
	ChunkID         string `json:"chunk_id"`
	Path            string `json:"path"`
	SymbolName      string `json:"symbol_name"`
	SymbolKind      string `json:"symbol_kind"`
	sigBlob         []byte
	Similarity      float64 `json:"similarity"`
	StartLine       int     `json:"start_line"`
	ExactNormalized bool    `json:"exact_normalized"`
}

func FindExactNormalizedMatches(
	ctx context.Context,
	database *sql.DB,
	normalizedHash string,
	excludePath string,
) ([]SimilarChunk, error) {
	rows, err := database.QueryContext(
		ctx,
		`SELECT chunk_id, path, symbol_name, symbol_kind, start_line
		FROM code_chunks
		WHERE normalized_hash = ? AND path != ?
		LIMIT 10`,
		normalizedHash,
		excludePath,
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
