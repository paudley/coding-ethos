// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agenthooks_test

import (
	"strings"
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/agenthooks"
)

func TestDoctorProbesCoverProviderRewriteContracts(t *testing.T) {
	t.Parallel()

	rewriteProviders := map[string]bool{}
	blockProviders := map[string]bool{}

	for _, probe := range HookProbeSummaries() {
		if strings.Contains(probe.Payload, "git status --short") {
			rewriteProviders[probe.Provider] = true
		}

		if strings.Contains(probe.Payload, "coding-ethos-"+"hooks") {
			blockProviders[probe.Provider] = true
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

	stdout := `{"hookSpecificOutput":{"updatedInput":{"command":` +
		`"/repo/bin/coding-ethos-run policy-git 'status' '--short'"}}}`
	payload := map[string]any{
		"hookSpecificOutput": map[string]any{
			"updatedInput": map[string]any{
				"command": "/repo/bin/coding-ethos-run policy-git 'status' '--short'",
			},
		},
	}

	err := ValidateClaudeRewritePayload(stdout, payload)
	if err == nil {
		t.Fatal("Claude rewrite without redirection passed doctor validation")
	}

	err = ValidateCodexRewritePayload(stdout, payload)
	if err == nil {
		t.Fatal("Codex rewrite probe should reject unsupported updatedInput")
	}

	err = ValidateGeminiRewritePayload(stdout, payload)
	if err != nil {
		t.Fatalf("Gemini rewrite should not require Claude redirection: %v", err)
	}
}
