// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks

import (
	"strconv"
	"strings"
)

const (
	tokenBudgetSourceEnv          = "env"
	tokenBudgetSourceFallback     = "fallback"
	tokenBudgetSourceModelContext = "model_context"
	tokenBudgetSourceRepoConfig   = "repo_config"
	tokenBudgetSafetyMaxTokens    = 50000

	contextWindowSmallTokens      = 32000
	contextWindowMediumTokens     = 128000
	contextWindowLargeTokens      = 256000
	contextWindowExtraLargeTokens = 1000000
	tokenBudgetSmallContext       = 4000
	tokenBudgetMediumContext      = 8000
	tokenBudgetLargeContext       = 12000
	tokenBudgetExtraLargeContext  = 24000
	tokenBudgetHugeContext        = 32000
)

type tokenBudgetResolution struct {
	Source              string
	Model               string
	ContextWindowTokens int
	MaxTokens           int
	SafetyMaxTokens     int
}

func resolveHookTokenBudget(
	event Event,
	options hookOutputCompressionOptions,
) tokenBudgetResolution {
	resolution := tokenBudgetResolution{
		Source:              strings.TrimSpace(options.MaxTokensSource),
		Model:               strings.TrimSpace(event.Model),
		ContextWindowTokens: event.ContextWindowTokens,
		MaxTokens:           options.MaxTokens,
		SafetyMaxTokens:     tokenBudgetSafetyMaxTokens,
	}
	if resolution.Source == "" {
		resolution.Source = tokenBudgetSourceFallback
	}

	if resolution.Source == tokenBudgetSourceRepoConfig ||
		resolution.Source == tokenBudgetSourceEnv {
		resolution.MaxTokens = min(resolution.MaxTokens, tokenBudgetSafetyMaxTokens)

		return resolution
	}

	modelBudget := modelContextTokenBudget(event.ContextWindowTokens)
	if modelBudget > 0 {
		resolution.Source = tokenBudgetSourceModelContext
		resolution.MaxTokens = min(modelBudget, tokenBudgetSafetyMaxTokens)

		return resolution
	}

	resolution.Source = tokenBudgetSourceFallback
	resolution.MaxTokens = defaultHookOutputMaxTokens

	return resolution
}

func modelContextTokenBudget(contextWindowTokens int) int {
	switch {
	case contextWindowTokens <= 0:
		return 0
	case contextWindowTokens <= contextWindowSmallTokens:
		return tokenBudgetSmallContext
	case contextWindowTokens <= contextWindowMediumTokens:
		return tokenBudgetMediumContext
	case contextWindowTokens <= contextWindowLargeTokens:
		return tokenBudgetLargeContext
	case contextWindowTokens <= contextWindowExtraLargeTokens:
		return tokenBudgetExtraLargeContext
	default:
		return tokenBudgetHugeContext
	}
}

func (resolution tokenBudgetResolution) metadata() map[string]string {
	metadata := map[string]string{
		"coding_ethos.token_budget.source":     resolution.Source,
		"coding_ethos.token_budget.max_tokens": strconv.Itoa(resolution.MaxTokens),
		"coding_ethos.token_budget.safety_max_tokens": strconv.Itoa(
			resolution.SafetyMaxTokens,
		),
	}

	if resolution.Model != "" {
		metadata["coding_ethos.model"] = resolution.Model
	}

	if resolution.ContextWindowTokens > 0 {
		metadata["coding_ethos.context_window_tokens"] = strconv.Itoa(
			resolution.ContextWindowTokens,
		)
	}

	return metadata
}
