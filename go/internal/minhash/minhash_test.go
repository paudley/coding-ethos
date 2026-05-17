// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package minhash_test

import (
	"math"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/minhash"
)

func TestShingleProducesCorrectNGrams(t *testing.T) {
	t.Parallel()

	tokens := []string{"a", "b", "c", "d", "e"}
	got := minhash.Shingle(tokens, 3)
	want := []string{"a b c", "b c d", "c d e"}

	if len(got) != len(want) {
		t.Fatalf("Shingle() len = %d, want %d", len(got), len(want))
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Shingle()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestShingleHandlesShortInput(t *testing.T) {
	t.Parallel()

	got := minhash.Shingle([]string{"a", "b"}, 5)
	if len(got) != 1 || got[0] != "a b" {
		t.Fatalf("Shingle(short) = %#v, want single combined shingle", got)
	}

	got = minhash.Shingle(nil, 3)
	if got != nil {
		t.Fatalf("Shingle(nil) = %#v, want nil", got)
	}
}

func TestIdenticalTokensProduceJaccardOne(t *testing.T) {
	t.Parallel()

	config := minhash.DefaultConfig()
	tokens := []string{
		"func",
		"$ID",
		"(",
		"$ID",
		"$ID",
		")",
		"{",
		"return",
		"$ID",
		"+",
		"$ID",
		"}",
	}
	sigA := minhash.ComputeSignature(tokens, config)
	sigB := minhash.ComputeSignature(tokens, config)

	j := minhash.EstimateJaccard(sigA, sigB)
	if j != 1.0 {
		t.Fatalf("Jaccard(identical) = %f, want 1.0", j)
	}
}

func TestDisjointTokensProduceLowJaccard(t *testing.T) {
	t.Parallel()

	config := minhash.DefaultConfig()
	tokensA := []string{
		"func",
		"main",
		"(",
		")",
		"{",
		"fmt",
		".",
		"Println",
		"(",
		")",
		"}",
	}
	tokensB := []string{
		"class",
		"Widget",
		":",
		"def",
		"__init__",
		"(",
		"self",
		")",
		":",
		"pass",
	}
	sigA := minhash.ComputeSignature(tokensA, config)
	sigB := minhash.ComputeSignature(tokensB, config)

	j := minhash.EstimateJaccard(sigA, sigB)
	if j > 0.3 {
		t.Fatalf("Jaccard(disjoint) = %f, want < 0.3", j)
	}
}

func TestSimilarTokensProduceHighJaccard(t *testing.T) {
	t.Parallel()

	config := minhash.DefaultConfig()
	base := []string{
		"func",
		"$ID",
		"(",
		"$ID",
		"$ID",
		")",
		"{",
		"if",
		"$ID",
		"==",
		"$NUM",
		"{",
		"return",
		"$ID",
		"}",
		"return",
		"$ID",
		"+",
		"$ID",
		"}",
	}
	variant := []string{
		"func",
		"$ID",
		"(",
		"$ID",
		"$ID",
		")",
		"{",
		"if",
		"$ID",
		"==",
		"$NUM",
		"{",
		"return",
		"$ID",
		"}",
		"return",
		"$ID",
		"-",
		"$ID",
		"}",
	}

	sigA := minhash.ComputeSignature(base, config)
	sigB := minhash.ComputeSignature(variant, config)

	j := minhash.EstimateJaccard(sigA, sigB)
	if j < 0.5 {
		t.Fatalf("Jaccard(similar) = %f, want >= 0.5", j)
	}
}

func TestEmptySignatureReturnsZeroJaccard(t *testing.T) {
	t.Parallel()

	a := minhash.Signature{Values: nil}
	b := minhash.Signature{Values: nil}

	if j := minhash.EstimateJaccard(a, b); j != 0 {
		t.Fatalf("Jaccard(empty) = %f, want 0", j)
	}
}

func TestMismatchedSignatureLengthReturnsZero(t *testing.T) {
	t.Parallel()

	a := minhash.Signature{Values: []uint64{1, 2, 3}}
	b := minhash.Signature{Values: []uint64{1, 2}}

	if j := minhash.EstimateJaccard(a, b); j != 0 {
		t.Fatalf("Jaccard(mismatched) = %f, want 0", j)
	}
}

func TestBandHashesDeterministic(t *testing.T) {
	t.Parallel()

	config := minhash.DefaultConfig()
	tokens := []string{"func", "$ID", "(", ")", "{", "return", "$NUM", "}"}
	sig := minhash.ComputeSignature(tokens, config)

	hashesA := minhash.BandHashes(sig, config)
	hashesB := minhash.BandHashes(sig, config)

	if len(hashesA) != config.Bands {
		t.Fatalf("BandHashes() len = %d, want %d", len(hashesA), config.Bands)
	}

	for i := range hashesA {
		if hashesA[i] != hashesB[i] {
			t.Fatalf("BandHashes() not deterministic at band %d", i)
		}
	}
}

func TestBandHashesShareBandsForIdenticalSignatures(t *testing.T) {
	t.Parallel()

	config := minhash.DefaultConfig()
	tokens := []string{"func", "$ID", "(", "$ID", "$ID", ")", "{", "return", "$ID", "}"}
	sig := minhash.ComputeSignature(tokens, config)

	hashesA := minhash.BandHashes(sig, config)
	hashesB := minhash.BandHashes(sig, config)

	shared := 0

	for i := range hashesA {
		if hashesA[i] == hashesB[i] {
			shared++
		}
	}

	if shared != len(hashesA) {
		t.Fatalf("identical sigs share %d/%d bands, want all", shared, len(hashesA))
	}
}

func TestBandHashesEmptySignature(t *testing.T) {
	t.Parallel()

	config := minhash.DefaultConfig()
	sig := minhash.Signature{Values: nil}
	hashes := minhash.BandHashes(sig, config)

	if hashes != nil {
		t.Fatalf("BandHashes(empty) = %#v, want nil", hashes)
	}
}

func TestSignatureValuesAreNotAllMaxUint64(t *testing.T) {
	t.Parallel()

	config := minhash.DefaultConfig()
	tokens := []string{"func", "main", "(", ")"}
	sig := minhash.ComputeSignature(tokens, config)

	allMax := true

	for _, v := range sig.Values {
		if v != math.MaxUint64 {
			allMax = false

			break
		}
	}

	if allMax {
		t.Fatal("signature values are all MaxUint64; hash permutation not working")
	}
}

func TestDefaultConfigConsistency(t *testing.T) {
	t.Parallel()

	config := minhash.DefaultConfig()
	if config.SignatureSize != config.Bands*config.RowsPerBand {
		t.Fatalf("SignatureSize (%d) != Bands*RowsPerBand (%d*%d = %d)",
			config.SignatureSize, config.Bands, config.RowsPerBand,
			config.Bands*config.RowsPerBand)
	}
}
