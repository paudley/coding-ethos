// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"context"
	"database/sql"
	"encoding/binary"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/celexpr"
	"blackcat.ca/coding-ethos/go/internal/minhash"
	"blackcat.ca/coding-ethos/go/internal/similarityconfig"
)

func TestUnpackSigBlob_Empty(t *testing.T) {
	t.Parallel()

	result := unpackSigBlob(nil)
	if result != nil {
		t.Fatalf("expected nil for empty input, got %v", result)
	}
}

func TestUnpackSigBlob_InvalidLength(t *testing.T) {
	t.Parallel()

	result := unpackSigBlob([]byte{0x01, 0x02, 0x03})
	if result != nil {
		t.Fatalf("expected nil for non-aligned input, got %v", result)
	}
}

func TestUnpackSigBlob_Valid(t *testing.T) {
	t.Parallel()

	data := make([]byte, 16)
	binary.LittleEndian.PutUint64(data[0:], 42)
	binary.LittleEndian.PutUint64(data[8:], 99)

	result := unpackSigBlob(data)
	if len(result) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(result))
	}

	if result[0] != 42 {
		t.Errorf("expected result[0]=42, got %d", result[0])
	}

	if result[1] != 99 {
		t.Errorf("expected result[1]=99, got %d", result[1])
	}
}

func TestCelSimilarityFacts_NoKeyword(t *testing.T) {
	t.Parallel()

	ctx := Context{Cwd: "/tmp"}
	result := celSimilarityFacts(ctx, "some_other_expression")

	if result != nil {
		t.Fatalf("expected nil when expression lacks similarity_facts, got %v", result)
	}
}

func TestCelSimilarityFacts_CachedFacts(t *testing.T) {
	t.Parallel()

	cached := []celexpr.SimilarityFactInput{{
		File:            "test.go",
		SymbolName:      "Foo",
		SymbolKind:      "function",
		ExactNormalized: true,
		Similarity:      1.0,
	}}

	ctx := Context{
		Cwd:             "/tmp",
		SimilarityFacts: cached,
	}
	result := celSimilarityFacts(ctx, "similarity_facts.any()")

	if len(result) != 1 {
		t.Fatalf("expected cached facts returned, got %d", len(result))
	}

	if result[0].SymbolName != "Foo" {
		t.Errorf("expected Foo, got %s", result[0].SymbolName)
	}
}

func TestCelSimilarityFacts_NoCwd(t *testing.T) {
	t.Parallel()

	ctx := Context{Cwd: ""}
	result := celSimilarityFacts(ctx, "similarity_facts.any()")

	if result != nil {
		t.Fatalf("expected nil when Cwd is empty, got %v", result)
	}
}

func createTestSimilarityDB(t *testing.T) *sql.DB {
	t.Helper()

	database, err := sql.Open("sqlite", ":memory:?cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	database.SetMaxOpenConns(1)

	schema := `
		CREATE TABLE code_chunks (
			chunk_id TEXT PRIMARY KEY,
			path TEXT NOT NULL,
			symbol_name TEXT NOT NULL,
			symbol_kind TEXT NOT NULL,
			symbol_path TEXT NOT NULL,
			language TEXT NOT NULL,
			start_line INTEGER NOT NULL DEFAULT 0,
			normalized_hash TEXT,
			minhash_sig BLOB
		);
		CREATE TABLE lsh_bands (
			chunk_id TEXT NOT NULL,
			band_hash TEXT NOT NULL
		);
		CREATE INDEX idx_chunks_path ON code_chunks(path);
		CREATE INDEX idx_chunks_nhash ON code_chunks(normalized_hash);
		CREATE INDEX idx_lsh_bands_hash ON lsh_bands(band_hash);
	`

	_, execErr := database.ExecContext(
		context.Background(), schema,
	)
	if execErr != nil {
		t.Fatalf("create schema: %v", execErr)
	}

	return database
}

func TestQuerySimilarityForFile_NoMatches(t *testing.T) {
	t.Parallel()

	database := createTestSimilarityDB(t)
	defer database.Close()

	ctx := context.Background()
	config := minhash.DefaultConfig()
	settings := similarityconfig.DefaultSettings()

	facts := querySimilarityForFile(ctx, database, "nonexistent.go", config, settings)
	if len(facts) != 0 {
		t.Fatalf("expected no facts for nonexistent path, got %d", len(facts))
	}
}

func TestQueryExactMatches_FindsDuplicates(t *testing.T) {
	t.Parallel()

	database := createTestSimilarityDB(t)
	defer database.Close()

	ctx := context.Background()

	_, err := database.ExecContext(ctx, `
		INSERT INTO code_chunks
			(chunk_id, path, symbol_name, symbol_kind,
			 symbol_path, language, start_line,
			 normalized_hash)
		VALUES
			('c1', 'a.go', 'Foo', 'function',
			 'pkg.Foo', 'go', 10, 'abc123'),
			('c2', 'b.go', 'Bar', 'function',
			 'pkg.Bar', 'go', 20, 'abc123')
	`)
	if err != nil {
		t.Fatalf("insert test data: %v", err)
	}

	facts := queryExactMatches(
		ctx, database, "a.go", "Foo",
		"function", "pkg.Foo", "go", "abc123",
	)
	if len(facts) != 1 {
		t.Fatalf("expected 1 exact match, got %d", len(facts))
	}

	if facts[0].MatchPath != "b.go" {
		t.Errorf("expected match in b.go, got %s", facts[0].MatchPath)
	}

	if !facts[0].ExactNormalized {
		t.Error("expected ExactNormalized=true")
	}

	if facts[0].Similarity != 1.0 {
		t.Errorf("expected similarity 1.0, got %f", facts[0].Similarity)
	}
}

func TestQuerySimilarityForFile_NoChunks(t *testing.T) {
	t.Parallel()

	database := createTestSimilarityDB(t)
	defer database.Close()

	ctx := context.Background()

	_, err := database.ExecContext(ctx, `
		INSERT INTO code_chunks
			(chunk_id, path, symbol_name, symbol_kind,
			 symbol_path, language, start_line,
			 normalized_hash)
		VALUES
			('c1', 'other.go', 'Hello', 'function',
			 'main.Hello', 'go', 5, 'deadbeef')
	`)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	config := minhash.DefaultConfig()
	settings := similarityconfig.DefaultSettings()

	// Query for a path that has no chunks — exercises the row iteration with zero results.
	facts := querySimilarityForFile(ctx, database, "src/a.go", config, settings)
	if len(facts) != 0 {
		t.Fatalf("expected 0 facts for unmatched path, got %d", len(facts))
	}
}

func TestAppendChunkFacts_NoHashNoSig(t *testing.T) {
	t.Parallel()

	database := createTestSimilarityDB(t)
	defer database.Close()

	ctx := context.Background()
	config := minhash.DefaultConfig()
	settings := similarityconfig.DefaultSettings()

	row := chunkRow{
		symbolName: "Empty",
		symbolKind: "function",
		symbolPath: "pkg.Empty",
		language:   "go",
	}

	facts := appendChunkFacts(ctx, nil, database, "test.go", row, config, settings)
	if len(facts) != 0 {
		t.Fatalf("expected no facts when no hash and no sig, got %d", len(facts))
	}
}

func TestQueryLSHMatches_EmptySig(t *testing.T) {
	t.Parallel()

	database := createTestSimilarityDB(t)
	defer database.Close()

	ctx := context.Background()
	config := minhash.DefaultConfig()
	settings := similarityconfig.DefaultSettings()

	facts := queryLSHMatches(
		ctx, database, "a.go", "Fn", "function", "pkg.Fn", "go",
		[]byte{}, config, settings,
	)

	if facts != nil {
		t.Fatalf("expected nil for empty sig, got %v", facts)
	}
}

func TestQueryLSHMatches_InvalidSig(t *testing.T) {
	t.Parallel()

	database := createTestSimilarityDB(t)
	defer database.Close()

	ctx := context.Background()
	config := minhash.DefaultConfig()
	settings := similarityconfig.DefaultSettings()

	facts := queryLSHMatches(
		ctx, database, "a.go", "Fn", "function", "pkg.Fn", "go",
		[]byte{0x01, 0x02, 0x03}, config, settings,
	)

	if facts != nil {
		t.Fatalf("expected nil for invalid sig, got %v", facts)
	}
}

func TestQueryLSHMatchesFindsCandidateFacts(t *testing.T) {
	t.Parallel()

	database := createTestSimilarityDB(t)
	defer database.Close()

	ctx := context.Background()
	config := minhash.DefaultConfig()
	settings := similarityconfig.DefaultSettings()
	tokens := strings.Fields("alpha beta gamma delta epsilon zeta eta theta")
	signature := minhash.ComputeSignature(tokens, config)
	sigBlob := packTestSigBlob(signature.Values)

	_, err := database.ExecContext(ctx, `
		INSERT INTO code_chunks
			(chunk_id, path, symbol_name, symbol_kind,
			 symbol_path, language, start_line, minhash_sig)
		VALUES
			('candidate', 'b.go', 'Bar', 'function',
			 'pkg.Bar', 'go', 20, ?)
	`, sigBlob)
	if err != nil {
		t.Fatalf("insert candidate: %v", err)
	}

	for _, bandHash := range minhash.BandHashes(signature, config) {
		_, err = database.ExecContext(
			ctx,
			`INSERT INTO lsh_bands (chunk_id, band_hash) VALUES (?, ?)`,
			"candidate",
			bandHash,
		)
		if err != nil {
			t.Fatalf("insert band: %v", err)
		}
	}

	facts := queryLSHMatches(
		ctx, database, "a.go", "Foo", "function", "pkg.Foo", "go",
		sigBlob, config, settings,
	)
	if len(facts) != 1 {
		t.Fatalf("expected 1 LSH fact, got %d: %#v", len(facts), facts)
	}

	if facts[0].MatchPath != "b.go" ||
		facts[0].ExactNormalized ||
		facts[0].Similarity < 0.99 {
		t.Fatalf("unexpected LSH fact: %#v", facts[0])
	}
}

func TestThresholdSimilarityFactsPreservesExactAndHighSimilarity(t *testing.T) {
	t.Parallel()

	facts := []celexpr.SimilarityFactInput{
		{MatchPath: "exact.go", ExactNormalized: true, Similarity: 0.1},
		{MatchPath: "high.go", Similarity: 0.9},
		{MatchPath: "low.go", Similarity: 0.4},
	}

	filtered := thresholdSimilarityFacts(facts, 0.8)
	if len(filtered) != 2 {
		t.Fatalf("filtered facts = %#v", filtered)
	}

	if filtered[0].MatchPath != "exact.go" || filtered[1].MatchPath != "high.go" {
		t.Fatalf("unexpected filtered order: %#v", filtered)
	}
}

func TestApplySimilarityDiagnosticAnnotatesMatchingFacts(t *testing.T) {
	t.Parallel()

	diagnostic := diagnostics.Diagnostic{Metadata: map[string]any{}}
	activation := map[string]any{
		"similarity_facts": []celexpr.SimilarityFactInput{{
			File:            "a.go",
			SymbolName:      "Foo",
			MatchPath:       "b.go",
			MatchSymbolName: "Bar",
			MatchSymbolKind: "function",
			MatchStartLine:  20,
			Similarity:      1.0,
			ExactNormalized: true,
		}},
	}

	applySimilarityDiagnostic(&diagnostic, activation)

	if diagnostic.File != "a.go" ||
		len(diagnostic.RelatedLocations) != 1 ||
		diagnostic.Metadata["similarity_match_count"] != 1 {
		t.Fatalf("diagnostic was not annotated: %#v", diagnostic)
	}
}

func TestApplySimilarityDiagnosticUsesPreFilteredFacts(t *testing.T) {
	t.Parallel()

	diagnostic := diagnostics.Diagnostic{Metadata: map[string]any{}}
	activation := map[string]any{
		"similarity_facts": []celexpr.SimilarityFactInput{{
			File:            "a.go",
			SymbolName:      "Foo",
			MatchPath:       "b.go",
			MatchSymbolName: "Bar",
			MatchSymbolKind: "function",
			MatchStartLine:  20,
			Similarity:      0.1,
			ExactNormalized: true,
		}},
	}

	applySimilarityDiagnostic(&diagnostic, activation)

	if diagnostic.Metadata["similarity_match_count"] != 1 ||
		len(diagnostic.RelatedLocations) != 1 {
		t.Fatalf("diagnostic dropped a pre-filtered fact: %#v", diagnostic)
	}
}

func packTestSigBlob(values []uint64) []byte {
	data := make([]byte, len(values)*uint64ByteSize)

	for index, value := range values {
		binary.LittleEndian.PutUint64(data[index*uint64ByteSize:], value)
	}

	return data
}
