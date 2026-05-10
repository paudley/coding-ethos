// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package minhash

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math"
	"strings"
)

const (
	DefaultSignatureSize = 128
	DefaultShingleSize   = 5
	DefaultBands         = 16
	DefaultRowsPerBand   = 8

	// uint64ByteSize is the size in bytes of a uint64 value.
	uint64ByteSize = 8
)

type Signature struct {
	Values []uint64
}

type Config struct {
	SignatureSize int
	ShingleSize   int
	Bands         int
	RowsPerBand   int
}

func DefaultConfig() Config {
	return Config{
		SignatureSize: DefaultSignatureSize,
		ShingleSize:   DefaultShingleSize,
		Bands:         DefaultBands,
		RowsPerBand:   DefaultRowsPerBand,
	}
}

func ComputeSignature(tokens []string, config Config) Signature {
	shingles := Shingle(tokens, config.ShingleSize)

	size := config.SignatureSize
	sig := make([]uint64, size)

	for idx := range sig {
		sig[idx] = math.MaxUint64
	}

	for _, shingle := range shingles {
		hash := hashShingle(shingle)

		for sigIdx := range size {
			permuted := permuteHash(hash, uint64(sigIdx))
			if permuted < sig[sigIdx] {
				sig[sigIdx] = permuted
			}
		}
	}

	return Signature{Values: sig}
}

func EstimateJaccard(sigA, sigB Signature) float64 {
	if len(sigA.Values) != len(sigB.Values) || len(sigA.Values) == 0 {
		return 0
	}

	agreeing := 0

	for idx := range sigA.Values {
		if sigA.Values[idx] == sigB.Values[idx] {
			agreeing++
		}
	}

	return float64(agreeing) / float64(len(sigA.Values))
}

func BandHashes(sig Signature, config Config) []string {
	bands := config.Bands
	rows := config.RowsPerBand

	if len(sig.Values) < bands*rows {
		bands = len(sig.Values) / rows
		if bands == 0 {
			return nil
		}
	}

	hashes := make([]string, bands)

	for bandIdx := range bands {
		start := bandIdx * rows
		end := start + rows
		band := sig.Values[start:end]

		buf := make([]byte, rows*uint64ByteSize)
		for idx, val := range band {
			binary.LittleEndian.PutUint64(buf[idx*uint64ByteSize:], val)
		}

		sum := sha256.Sum256(buf)
		hashes[bandIdx] = hex.EncodeToString(sum[:])
	}

	return hashes
}

func Shingle(tokens []string, size int) []string {
	if len(tokens) < size {
		if len(tokens) == 0 {
			return nil
		}

		return []string{joinTokens(tokens)}
	}

	shingles := make([]string, 0, len(tokens)-size+1)

	for offset := 0; offset <= len(tokens)-size; offset++ {
		shingles = append(shingles, joinTokens(tokens[offset:offset+size]))
	}

	return shingles
}

func joinTokens(tokens []string) string {
	return strings.Join(tokens, " ")
}

func hashShingle(shingle string) uint64 {
	sum := sha256.Sum256([]byte(shingle))

	return binary.LittleEndian.Uint64(sum[:uint64ByteSize])
}

// permuteHash applies the Stafford variant 13 of the 64-bit finalizer from
// MurmurHash3. Constants are fixed by the algorithm specification.
func permuteHash(hash, seed uint64) uint64 {
	const (
		murmurGoldenRatio = 0x9e3779b97f4a7c15
		murmurMix1        = 0xbf58476d1ce4e5b9
		murmurMix2        = 0x94d049bb133111eb
		murmurShift1      = 30
		murmurShift2      = 27
		murmurShift3      = 31
	)

	mixed := hash ^ (seed * murmurGoldenRatio)
	mixed ^= mixed >> murmurShift1
	mixed *= murmurMix1
	mixed ^= mixed >> murmurShift2
	mixed *= murmurMix2
	mixed ^= mixed >> murmurShift3

	return mixed
}
