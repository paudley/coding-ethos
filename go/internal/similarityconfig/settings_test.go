// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package similarityconfig_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/minhash"
	"blackcat.ca/coding-ethos/go/internal/similarityconfig"
)

func TestLoadFromRootUsesDefaultsWhenConfigMissing(t *testing.T) {
	t.Parallel()

	settings, err := similarityconfig.LoadFromRoot(t.TempDir())
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}

	defaultConfig := minhash.DefaultConfig()
	if !settings.Enabled ||
		!settings.ExactNormalized ||
		settings.SignatureSize != defaultConfig.SignatureSize ||
		settings.StructuralThreshold != 0.8 {
		t.Fatalf("unexpected default settings: %#v", settings)
	}
}

func TestLoadFromRootAppliesSimilarityOverrides(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte(`similarity:
  enabled: false
  minhash_size: 64
  shingle_size: 4
  lsh_bands: 8
  lsh_rows: 8
  min_symbol_lines: 9
  exact_normalized: false
  candidate_threshold: 0.75
  structural_threshold: 0.9
  max_matches: 3
`), 0o600)
	if err != nil {
		t.Fatalf("write config: %v", err)
	}

	settings, err := similarityconfig.LoadFromRoot(root)
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}

	expected := similarityconfig.Settings{
		Enabled:             false,
		ExactNormalized:     false,
		SignatureSize:       64,
		ShingleSize:         4,
		LSHBands:            8,
		LSHRows:             8,
		MinSymbolLines:      9,
		MaxMatches:          3,
		CandidateThreshold:  0.75,
		StructuralThreshold: 0.9,
	}

	if !reflect.DeepEqual(settings, expected) {
		t.Fatalf("settings did not apply overrides: %#v", settings)
	}
}

func TestLoadFromRootRejectsInvalidSimilarityShape(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte(`similarity:
  minhash_size: 8
  lsh_bands: 16
  lsh_rows: 8
`), 0o600)
	if err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err = similarityconfig.LoadFromRoot(root)
	if err == nil {
		t.Fatal("expected invalid shape error")
	}
}

func TestLoadFromRootRejectsNegativeSimilarityShape(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte(`similarity:
  minhash_size: -1
`), 0o600)
	if err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err = similarityconfig.LoadFromRoot(root)
	if err == nil {
		t.Fatal("expected negative shape error")
	}
}

func TestSettingsExposeMinHashConfigAndThresholdOverride(t *testing.T) {
	t.Parallel()

	settings := similarityconfig.Settings{
		Enabled:             true,
		ExactNormalized:     true,
		SignatureSize:       64,
		ShingleSize:         4,
		LSHBands:            8,
		LSHRows:             8,
		MinSymbolLines:      3,
		MaxMatches:          7,
		CandidateThreshold:  0.6,
		StructuralThreshold: 0.75,
	}

	config := settings.MinHashConfig()
	if config.SignatureSize != 64 ||
		config.ShingleSize != 4 ||
		config.Bands != 8 ||
		config.RowsPerBand != 8 {
		t.Fatalf("minhash config = %#v", config)
	}

	overridden, err := settings.WithStructuralThreshold(0.9)
	if err != nil {
		t.Fatalf("override structural threshold: %v", err)
	}

	if overridden.StructuralThreshold != 0.9 {
		t.Fatalf("structural threshold = %f", overridden.StructuralThreshold)
	}

	preserved, err := settings.WithStructuralThreshold(0)
	if err != nil {
		t.Fatalf("preserve structural threshold: %v", err)
	}

	if preserved.StructuralThreshold != settings.StructuralThreshold {
		t.Fatalf("zero threshold should preserve settings: %#v", preserved)
	}
}

func TestWithStructuralThresholdRejectsInvalidOverride(t *testing.T) {
	t.Parallel()

	_, err := similarityconfig.DefaultSettings().WithStructuralThreshold(1.1)
	if err == nil {
		t.Fatal("expected invalid threshold error")
	}
}
