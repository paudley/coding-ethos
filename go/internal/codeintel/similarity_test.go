// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel_test

import (
	"context"
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/minhash"
)

func TestFindExactNormalizedMatches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)

	indexExactNormalizedTestData(t, ctx, store)

	matches, err := FindExactNormalizedMatches(
		ctx,
		store.Database(),
		"norm-shared",
		"foo.go",
	)
	if err != nil {
		t.Fatalf("find exact normalized matches: %v", err)
	}

	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d: %+v", len(matches), matches)
	}

	if matches[0].Path != "bar.go" {
		t.Errorf("expected match from bar.go, got %s", matches[0].Path)
	}

	if matches[0].SymbolName != "DoThings" {
		t.Errorf("expected symbol DoThings, got %s", matches[0].SymbolName)
	}

	if !matches[0].ExactNormalized {
		t.Error("expected ExactNormalized = true")
	}
}

func indexExactNormalizedTestData(
	t *testing.T,
	ctx context.Context,
	store *Store,
) {
	t.Helper()

	chunks := []CodeChunk{
		{
			ID: "chunk-a", Path: "foo.go", Language: "go",
			NodeKind: "function_declaration", SymbolKind: "function",
			SymbolName: "DoStuff", SymbolPath: "DoStuff",
			ContentHash: "hash-a", NormalizedHash: "norm-shared",
			StartLine: 1, EndLine: 10, SearchText: "DoStuff",
		},
		{
			ID: "chunk-b", Path: "bar.go", Language: "go",
			NodeKind: "function_declaration", SymbolKind: "function",
			SymbolName: "DoThings", SymbolPath: "DoThings",
			ContentHash: "hash-b", NormalizedHash: "norm-shared",
			StartLine: 5, EndLine: 15, SearchText: "DoThings",
		},
		{
			ID: "chunk-c", Path: "baz.go", Language: "go",
			NodeKind: "function_declaration", SymbolKind: "function",
			SymbolName: "Unique", SymbolPath: "Unique",
			ContentHash: "hash-c", NormalizedHash: "norm-unique",
			StartLine: 1, EndLine: 5, SearchText: "Unique",
		},
	}

	files := []CodeFile{
		{
			Path: "foo.go", Language: "go", ContentHash: "fh-a",
			IndexedAtUTC: "2026-01-01T00:00:00Z", SizeBytes: 100, LineCount: 10,
		},
		{
			Path: "bar.go", Language: "go", ContentHash: "fh-b",
			IndexedAtUTC: "2026-01-01T00:00:00Z", SizeBytes: 100, LineCount: 15,
		},
		{
			Path: "baz.go", Language: "go", ContentHash: "fh-c",
			IndexedAtUTC: "2026-01-01T00:00:00Z", SizeBytes: 50, LineCount: 5,
		},
	}

	for i, file := range files {
		err := store.ReplaceCodeFileIndex(ctx, file, []CodeChunk{chunks[i]}, nil)
		if err != nil {
			t.Fatalf("index file %s: %v", file.Path, err)
		}
	}
}

func TestFindLSHCandidatesAndRefine(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)
	config := minhash.DefaultConfig()

	sigA, bandsA, bandsB := indexLSHTestData(t, ctx, store, config)

	verifyLSHCandidatesFromA(t, ctx, store, sigA, bandsA)
	verifyLSHCandidatesFromB(t, ctx, store, bandsB)
}

func indexLSHTestData(
	t *testing.T,
	ctx context.Context,
	store *Store,
	config minhash.Config,
) (minhash.Signature, []string, []string) {
	t.Helper()

	funcTokens := []string{
		"func", "$ID", "(", "$ID", "$ID", ")", "{",
		"return", "$ID", ".", "$ID", "(", ")", "}",
	}
	importTokens := []string{
		"import", "(", "fmt", "os", "net/http", "encoding/json", ")",
	}

	sigA := minhash.ComputeSignature(funcTokens, config)
	sigB := minhash.ComputeSignature(funcTokens, config)
	sigC := minhash.ComputeSignature(importTokens, config)

	bandsA := minhash.BandHashes(sigA, config)
	bandsB := minhash.BandHashes(sigB, config)

	entries := lshTestEntries(sigA, sigB, sigC)

	for _, entry := range entries {
		err := store.ReplaceCodeFileIndex(
			ctx, entry.file, []CodeChunk{entry.chunk}, nil,
		)
		if err != nil {
			t.Fatalf("index %s: %v", entry.file.Path, err)
		}
	}

	return sigA, bandsA, bandsB
}

type lshTestEntry struct {
	file  CodeFile
	chunk CodeChunk
}

func lshTestEntries(sigA, sigB, sigC minhash.Signature) []lshTestEntry {
	return []lshTestEntry{
		{
			file: CodeFile{
				Path: "alpha.go", Language: "go", ContentHash: "fa",
				IndexedAtUTC: "2026-01-01T00:00:00Z", SizeBytes: 50, LineCount: 5,
			},
			chunk: CodeChunk{
				ID: "lsh-chunk-a", Path: "alpha.go", Language: "go",
				NodeKind: "function_declaration", SymbolKind: "function",
				SymbolName: "Alpha", SymbolPath: "Alpha",
				ContentHash: "lsh-ha", NormalizedHash: "lsh-norm-a",
				MinHashSig: sigA.Values, StartLine: 1, EndLine: 5,
				SearchText: "Alpha",
			},
		},
		{
			file: CodeFile{
				Path: "beta.go", Language: "go", ContentHash: "fb",
				IndexedAtUTC: "2026-01-01T00:00:00Z", SizeBytes: 50, LineCount: 5,
			},
			chunk: CodeChunk{
				ID: "lsh-chunk-b", Path: "beta.go", Language: "go",
				NodeKind: "function_declaration", SymbolKind: "function",
				SymbolName: "Beta", SymbolPath: "Beta",
				ContentHash: "lsh-hb", NormalizedHash: "lsh-norm-b",
				MinHashSig: sigB.Values, StartLine: 1, EndLine: 5,
				SearchText: "Beta",
			},
		},
		{
			file: CodeFile{
				Path: "gamma.go", Language: "go", ContentHash: "fc",
				IndexedAtUTC: "2026-01-01T00:00:00Z", SizeBytes: 30, LineCount: 3,
			},
			chunk: CodeChunk{
				ID: "lsh-chunk-c", Path: "gamma.go", Language: "go",
				NodeKind: "import_declaration", SymbolKind: "import",
				SymbolName: "imports", SymbolPath: "imports",
				ContentHash: "lsh-hc", NormalizedHash: "lsh-norm-c",
				MinHashSig: sigC.Values, StartLine: 1, EndLine: 3,
				SearchText: "imports",
			},
		},
	}
}

func verifyLSHCandidatesFromA(
	t *testing.T,
	ctx context.Context,
	store *Store,
	sigA minhash.Signature,
	bandsA []string,
) {
	t.Helper()

	candidates, err := FindLSHCandidates(ctx, store.Database(), bandsA, "alpha.go")
	if err != nil {
		t.Fatalf("find LSH candidates: %v", err)
	}

	if !candidatesContainPath(candidates, "beta.go") {
		t.Errorf("expected beta.go in LSH candidates, got: %+v", candidates)
	}

	refined := RefineLSHCandidates(sigA, candidates, store.Database(), ctx, 0.5)

	foundBetaRefined := false

	for _, result := range refined {
		if result.Path == "beta.go" {
			foundBetaRefined = true

			if result.Similarity < 0.99 {
				t.Errorf(
					"expected near-1.0 similarity for identical tokens, got %f",
					result.Similarity,
				)
			}
		}
	}

	if !foundBetaRefined {
		t.Errorf("expected beta.go in refined results, got: %+v", refined)
	}
}

func verifyLSHCandidatesFromB(
	t *testing.T,
	ctx context.Context,
	store *Store,
	bandsB []string,
) {
	t.Helper()

	candidatesFromB, err := FindLSHCandidates(ctx, store.Database(), bandsB, "beta.go")
	if err != nil {
		t.Fatalf("find LSH candidates from B: %v", err)
	}

	if !candidatesContainPath(candidatesFromB, "alpha.go") {
		t.Errorf("expected alpha.go in candidates from B, got: %+v", candidatesFromB)
	}
}

func candidatesContainPath(candidates []SimilarChunk, path string) bool {
	for _, c := range candidates {
		if c.Path == path {
			return true
		}
	}

	return false
}
