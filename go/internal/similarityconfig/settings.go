// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package similarityconfig

import (
	"fmt"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/minhash"
)

const (
	defaultCandidateThreshold  = 0.7
	defaultStructuralThreshold = 0.8
	defaultMinSymbolLines      = 5
	defaultMaxMatches          = 10
)

var (
	errInvalidNumericSettings = apperror.StaticError(
		"invalid similarity config: numeric settings must be positive",
	)
	errInvalidLSHShape = apperror.StaticError(
		"invalid similarity config: lsh_bands*lsh_rows exceeds minhash_size",
	)
	errInvalidThreshold = apperror.StaticError(
		"invalid similarity config: thresholds must be > 0 and <= 1",
	)
)

// Settings controls structural similarity indexing and lookup behavior.
type Settings struct {
	Enabled             bool
	ExactNormalized     bool
	SignatureSize       int
	ShingleSize         int
	LSHBands            int
	LSHRows             int
	MinSymbolLines      int
	MaxMatches          int
	CandidateThreshold  float64
	StructuralThreshold float64
}

type rootConfig struct {
	Similarity rawSettings `yaml:"similarity"`
}

type rawSettings struct {
	Enabled             *bool    `yaml:"enabled"`
	ExactNormalized     *bool    `yaml:"exact_normalized"`
	CandidateThreshold  *float64 `yaml:"candidate_threshold"`
	StructuralThreshold *float64 `yaml:"structural_threshold"`
	MinHashSize         *int     `yaml:"minhash_size"`
	ShingleSize         *int     `yaml:"shingle_size"`
	LSHBands            *int     `yaml:"lsh_bands"`
	LSHRows             *int     `yaml:"lsh_rows"`
	MinSymbolLines      *int     `yaml:"min_symbol_lines"`
	MaxMatches          *int     `yaml:"max_matches"`
}

// DefaultSettings returns the repo-wide defaults for structural similarity.
func DefaultSettings() Settings {
	config := minhash.DefaultConfig()

	return Settings{
		Enabled:             true,
		ExactNormalized:     true,
		SignatureSize:       config.SignatureSize,
		ShingleSize:         config.ShingleSize,
		LSHBands:            config.Bands,
		LSHRows:             config.RowsPerBand,
		MinSymbolLines:      defaultMinSymbolLines,
		MaxMatches:          defaultMaxMatches,
		CandidateThreshold:  defaultCandidateThreshold,
		StructuralThreshold: defaultStructuralThreshold,
	}
}

// LoadFromRoot reads similarity settings from root/config.yaml.
func LoadFromRoot(root string) (Settings, error) {
	settings := DefaultSettings()

	if root == "" {
		return settings, nil
	}

	path := filepath.Join(root, "config.yaml")

	payload, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return settings, nil
		}

		return Settings{}, fmt.Errorf("read similarity config: %w", err)
	}

	var decoded rootConfig

	err = yaml.Unmarshal(payload, &decoded)
	if err != nil {
		return Settings{}, fmt.Errorf("parse similarity config: %w", err)
	}

	settings = settings.apply(decoded.Similarity)

	err = settings.Validate()
	if err != nil {
		return Settings{}, err
	}

	return settings, nil
}

// Validate rejects settings that would make MinHash or LSH behavior ambiguous.
func (settings Settings) Validate() error {
	if settings.SignatureSize <= 0 ||
		settings.ShingleSize <= 0 ||
		settings.LSHBands <= 0 ||
		settings.LSHRows <= 0 ||
		settings.MinSymbolLines <= 0 ||
		settings.MaxMatches <= 0 {
		return errInvalidNumericSettings
	}

	if settings.LSHBands*settings.LSHRows > settings.SignatureSize {
		return errInvalidLSHShape
	}

	if !validThreshold(settings.CandidateThreshold) ||
		!validThreshold(settings.StructuralThreshold) {
		return errInvalidThreshold
	}

	return nil
}

// MinHashConfig converts repo similarity settings into MinHash parameters.
func (settings Settings) MinHashConfig() minhash.Config {
	return minhash.Config{
		SignatureSize: settings.SignatureSize,
		ShingleSize:   settings.ShingleSize,
		Bands:         settings.LSHBands,
		RowsPerBand:   settings.LSHRows,
	}
}

func validThreshold(value float64) bool {
	return value > 0 && value <= 1
}

// WithStructuralThreshold returns a copy with a caller-supplied threshold.
func (settings Settings) WithStructuralThreshold(threshold float64) (Settings, error) {
	if threshold == 0 {
		return settings, nil
	}

	if !validThreshold(threshold) {
		return Settings{}, errInvalidThreshold
	}

	settings.StructuralThreshold = threshold

	return settings, nil
}

func (settings Settings) apply(raw rawSettings) Settings {
	settings = settings.applyFlags(raw)
	settings = settings.applyShape(raw)

	return settings.applyThresholds(raw)
}

func (settings Settings) applyFlags(raw rawSettings) Settings {
	if raw.Enabled != nil {
		settings.Enabled = *raw.Enabled
	}

	if raw.ExactNormalized != nil {
		settings.ExactNormalized = *raw.ExactNormalized
	}

	return settings
}

func (settings Settings) applyShape(raw rawSettings) Settings {
	if raw.MinHashSize != nil {
		settings.SignatureSize = *raw.MinHashSize
	}

	if raw.ShingleSize != nil {
		settings.ShingleSize = *raw.ShingleSize
	}

	if raw.LSHBands != nil {
		settings.LSHBands = *raw.LSHBands
	}

	if raw.LSHRows != nil {
		settings.LSHRows = *raw.LSHRows
	}

	if raw.MinSymbolLines != nil {
		settings.MinSymbolLines = *raw.MinSymbolLines
	}

	if raw.MaxMatches != nil {
		settings.MaxMatches = *raw.MaxMatches
	}

	return settings
}

func (settings Settings) applyThresholds(raw rawSettings) Settings {
	if raw.CandidateThreshold != nil && *raw.CandidateThreshold > 0 {
		settings.CandidateThreshold = *raw.CandidateThreshold
	}

	if raw.StructuralThreshold != nil && *raw.StructuralThreshold > 0 {
		settings.StructuralThreshold = *raw.StructuralThreshold
	}

	return settings
}
