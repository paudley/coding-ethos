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
	FileScope     string
	Selector      GeminiFileSelector `json:"selector"`
	BatchSize     int
	MaxFileSizeKB int
}

type GeminiFileSelector struct {
	IncludeExtensions           []string
	ExcludeSubstrings           []string
	ExcludePrefixes             []string
	ShebangMarkers              []string
	AllowExtensionlessInScripts bool
}

type GeminiPromptPack struct {
	Checks  map[string]GeminiPromptCheckSpec `json:"checks"`
	Prompts map[string]string                `json:"prompts"`
	Version int                              `json:"version"`
}

func (spec *GeminiPromptCheckSpec) UnmarshalJSON(payload []byte) error {
	fields, err := decodeGeminiJSONFields(payload)
	if err != nil {
		return fmt.Errorf("decode Gemini prompt check: %w", err)
	}

	return decodeGeminiJSONFieldsInto(fields, []geminiJSONFieldTarget{
		{key: "fileScope", target: &spec.FileScope},
		{key: "selector", target: &spec.Selector},
		{key: "batchSize", target: &spec.BatchSize},
		{key: "maxFileSizeKb", target: &spec.MaxFileSizeKB},
	})
}

func (selector *GeminiFileSelector) UnmarshalJSON(payload []byte) error {
	fields, err := decodeGeminiJSONFields(payload)
	if err != nil {
		return fmt.Errorf("decode Gemini file selector: %w", err)
	}

	return decodeGeminiJSONFieldsInto(fields, []geminiJSONFieldTarget{
		{key: "includeExtensions", target: &selector.IncludeExtensions},
		{key: "excludeSubstrings", target: &selector.ExcludeSubstrings},
		{key: "excludePrefixes", target: &selector.ExcludePrefixes},
		{key: "shebangMarkers", target: &selector.ShebangMarkers},
		{
			key:    "allowExtensionlessInScripts",
			target: &selector.AllowExtensionlessInScripts,
		},
	})
}

func decodeGeminiJSONFields(payload []byte) (map[string]json.RawMessage, error) {
	fields := map[string]json.RawMessage{}

	err := json.Unmarshal(payload, &fields)
	if err != nil {
		return nil, fmt.Errorf("decode Gemini JSON object: %w", err)
	}

	return fields, nil
}

type geminiJSONFieldTarget struct {
	target any
	key    string
}

func decodeGeminiJSONFieldsInto(
	fields map[string]json.RawMessage,
	targets []geminiJSONFieldTarget,
) error {
	for _, target := range targets {
		raw, ok := fields[target.key]
		if !ok {
			continue
		}

		err := json.Unmarshal(raw, target.target)
		if err != nil {
			return fmt.Errorf("decode %q: %w", target.key, err)
		}
	}

	return nil
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
