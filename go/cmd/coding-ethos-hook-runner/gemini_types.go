// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type GeminiSettings struct {
	ServiceTierOverrides    map[string]string
	ThinkingBudgetOverrides map[string]int
	ModelOverrides          map[string]string
	ThinkingBudget          *int
	ServiceTier             string
	Model                   string
	ModalAllowlistFiles     []string
	Cache                   GeminiCacheSettings
	MaxRetries              int
	TimeoutSeconds          int
	InitialBackoffSeconds   float64
	MaxConcurrentAPICalls   int
	Enabled                 bool
	DisableSafetyFilters    bool
}

type GeminiCacheSettings struct {
	Dirname       string
	TTLSeconds    int
	APITTLSeconds int
	Enabled       bool
	APIEnabled    bool
}

type QuietFilterConfig struct {
	ANSIRegex        string
	FailedRegex      string
	PassedRegex      string
	PreexistingRegex string
	SeparatorRegex   string
	SkippedRegex     string
	StatusRegex      string
	MetadataPrefixes []string
	SuppressExact    []string
	SuppressPrefixes []string
	SuppressRegexes  []string
	BannerWidth      int
}

type GeminiPromptCheckSpec struct {
	FileScope     string             `json:"fileScope"`
	Selector      GeminiFileSelector `json:"selector"`
	BatchSize     int                `json:"batchSize"`
	MaxFileSizeKB int                `json:"maxFileSizeKb"`
}

type GeminiFileSelector struct {
	IncludeExtensions           []string `json:"includeExtensions"`
	ExcludeSubstrings           []string `json:"excludeSubstrings"`
	ExcludePrefixes             []string `json:"excludePrefixes"`
	ShebangMarkers              []string `json:"shebangMarkers"`
	AllowExtensionlessInScripts bool     `json:"allowExtensionlessInScripts"`
}

type GeminiPromptPack struct {
	Checks  map[string]GeminiPromptCheckSpec `json:"checks"`
	Prompts map[string]string                `json:"prompts"`
	Version int                              `json:"version"`
}

func loadGeminiPromptPack(bundleRoot string) (GeminiPromptPack, error) {
	var pack GeminiPromptPack

	ethosRoot := filepath.Dir(bundleRoot)
	consumer := consumerRoot(ethosRoot)

	candidates := []string{
		filepath.Join(consumer, ".code-ethos", "gemini", "prompt-pack.json"),
		filepath.Join(ethosRoot, ".code-ethos", "gemini", "prompt-pack.json"),
	}
	for _, candidate := range candidates {
		data, err := os.ReadFile(candidate)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}

			return pack, fmt.Errorf("read %s: %w", candidate, err)
		}

		err = json.Unmarshal(data, &pack)
		if err != nil {
			return pack, fmt.Errorf("parse %s: %w", candidate, err)
		}

		if len(pack.Prompts) == 0 {
			return pack, fmt.Errorf(
				"%w: %s",
				errGeminiPackMissingPrompts,
				candidate,
			)
		}

		if len(pack.Checks) == 0 {
			return pack, fmt.Errorf(
				"%w: %s",
				errGeminiPackMissingChecks,
				candidate,
			)
		}

		return pack, nil
	}

	return pack, fmt.Errorf("%w: %s", errGeminiPackNotFound, bundleRoot)
}
