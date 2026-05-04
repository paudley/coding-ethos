// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package agenthooks

import (
	"strings"
	"testing"
)

func TestDoctorProbesCoverProviderRewriteContracts(t *testing.T) {
	t.Parallel()

	rewriteProviders := map[string]bool{}
	blockProviders := map[string]bool{}
	for _, probe := range hookProbes() {
		if strings.Contains(probe.payload, "git status --short") {
			rewriteProviders[probe.provider] = true
		}
		if strings.Contains(probe.payload, "coding-ethos-"+"hooks") {
			blockProviders[probe.provider] = true
		}
	}

	for _, provider := range []string{
		string(ProviderClaude),
		string(ProviderCodex),
		string(ProviderGemini),
	} {
		if !rewriteProviders[provider] {
			t.Fatalf("missing rewrite doctor probe for %s", provider)
		}
		if !blockProviders[provider] {
			t.Fatalf("missing block doctor probe for %s", provider)
		}
	}
}

func TestClaudeDoctorRewriteRequiresRedirection(t *testing.T) {
	t.Parallel()

	result := hookProbeResult{
		exitCode: 0,
		stdout:   `{"hookSpecificOutput":{"updatedInput":{"command":"/repo/bin/coding-ethos-run policy-git 'status' '--short'"}}}`,
		payload: map[string]any{
			"hookSpecificOutput": map[string]any{
				"updatedInput": map[string]any{
					"command": "/repo/bin/coding-ethos-run policy-git 'status' '--short'",
				},
			},
		},
	}

	if err := validateClaudeRewriteProbe(result); err == nil {
		t.Fatal("Claude rewrite without redirection passed doctor validation")
	}
	if err := validateCodexRewriteProbe(result); err != nil {
		t.Fatalf("Codex rewrite should not require Claude redirection: %v", err)
	}
	if err := validateGeminiRewriteProbe(result); err != nil {
		t.Fatalf("Gemini rewrite should not require Claude redirection: %v", err)
	}
}
