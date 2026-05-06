// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks_test

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/hooks"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestEncodeProviderResultMatchesCodexRemediationFixture(t *testing.T) {
	t.Parallel()

	output := encodedProviderOutput(t, `{
		"provider": "codex",
		"event": "PreToolUse",
		"tool": "Bash",
		"input": {"command": "git commit --no-verify -m test"}
	}`)

	assertJSONMatchesFixture(t, output, "testdata/provider_remediation_codex.json")
}

func TestEncodeProviderResultMatchesGeminiRemediationFixture(t *testing.T) {
	t.Parallel()

	output := encodedProviderOutput(t, `{
		"provider": "gemini-cli",
		"hookEventName": "BeforeTool",
		"toolName": "run_shell_command",
		"toolInput": {"command": "git commit --no-verify -m test"}
	}`)

	assertJSONMatchesFixture(t, output, "testdata/provider_remediation_gemini.json")
}

func TestEncodeProviderResultIncludesUpdatedInputForSupportedProviders(t *testing.T) {
	t.Parallel()

	for _, provider := range []string{"claude", "gemini-cli"} {
		t.Run(provider, func(t *testing.T) {
			t.Parallel()

			output := encodedProviderOutput(
				t,
				providerGitPayload(provider, t.TempDir(), "git status"),
			)

			for _, expected := range []string{
				`"hookSpecificOutput"`,
				`"updatedInput"`,
				`policy-git`,
			} {
				if !strings.Contains(output, expected) {
					t.Fatalf("missing %q in provider output: %s", expected, output)
				}
			}
		})
	}
}

func TestEncodeProviderResultDoesNotEmitCodexUpdatedInput(t *testing.T) {
	t.Parallel()

	output := encodedProviderOutput(
		t,
		providerGitPayload("codex", t.TempDir(), "git status"),
	)

	var decoded map[string]any

	err := json.Unmarshal([]byte(output), &decoded)
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}

	hookOutput, _ := decoded["hookSpecificOutput"].(map[string]any)
	if _, ok := hookOutput["updatedInput"]; ok {
		t.Fatalf("Codex output must not include unsupported updatedInput field: %s", output)
	}

	if strings.TrimSpace(output) != "{}" {
		t.Fatalf("Codex allowed rewrite fallback should emit empty output, got: %s", output)
	}
}

func TestProviderDenialIncludesTrackingID(t *testing.T) {
	t.Parallel()

	output := encodedProviderOutput(t, `{
		"provider": "codex",
		"event": "PreToolUse",
		"tool": "Bash",
		"input": {"command": "git commit --no-verify -m test"}
	}`)

	for _, expected := range []string{
		`"trackingID": "hook-`,
		`"traceId": "hook-`,
		`trackingID: hook-`,
		`"permissionDecisionReason": "trackingID: hook-`,
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("missing %q in provider output: %s", expected, output)
		}
	}
}

func TestBlockedAdviceTOONIncludesAgentRemediation(t *testing.T) {
	t.Setenv("CODE_ETHOS_HOOK_OUTPUT_FORMAT", "toon")

	advice := BlockedAdvice(Result{
		Event:      "PreToolUse",
		Tool:       "Bash",
		Status:     statusBlocked,
		TrackingID: "hook-test123",
		Decisions: []policy.Decision{{
			PolicyID:   "shell.github_admin",
			Decision:   "block",
			Severity:   "block",
			Message:    "gh --admin bypasses normal review gates.",
			Suggestion: "Use the normal review path.",
		}},
	})

	for _, expected := range []string{
		"trackingID: hook-test123",
		"agent_remediation[1]{policy_id,skill_id,failed_action,next,mcp_tool}:",
		"shell.github_admin,,Bash,Use the normal review path.,policy_explain",
	} {
		if !strings.Contains(advice, expected) {
			t.Fatalf("missing %q in advice: %s", expected, advice)
		}
	}
}

func TestBlockedAdviceJSONIncludesAgentRemediation(t *testing.T) {
	t.Setenv("CODE_ETHOS_HOOK_OUTPUT_FORMAT", "json")

	advice := BlockedAdvice(severeViolationResult())

	for _, expected := range []string{
		`"agent_remediation":`,
		`"tool": "policy_explain"`,
	} {
		if !strings.Contains(advice, expected) {
			t.Fatalf("missing %q in advice: %s", expected, advice)
		}
	}
}

func encodedProviderOutput(t *testing.T, payload string) string {
	t.Helper()

	event, err := DecodeEvent(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("decode event: %v", err)
	}

	result, err := Run(policy.ExampleBundle(), Options{Event: event})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}

	buffer := strings.Builder{}
	if err := EncodeResult(&buffer, result); err != nil {
		t.Fatalf("encode result: %v", err)
	}

	return buffer.String()
}

func assertJSONMatchesFixture(t *testing.T, output, fixturePath string) {
	t.Helper()

	var got any
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatalf("decode provider output: %v\n%s", err, output)
	}

	fixture, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture %s: %v", fixturePath, err)
	}

	var want any
	if err := json.Unmarshal(fixture, &want); err != nil {
		t.Fatalf("decode fixture %s: %v", fixturePath, err)
	}

	assertJSONContains(t, got, want, fixturePath)
}

func assertJSONContains(t *testing.T, got, want any, path string) {
	t.Helper()

	switch expected := want.(type) {
	case map[string]any:
		actual, ok := got.(map[string]any)
		if !ok {
			t.Fatalf("%s: got %T, want object", path, got)
		}

		for key, value := range expected {
			actualValue, ok := actual[key]
			if !ok {
				t.Fatalf("%s.%s missing in %#v", path, key, actual)
			}

			assertJSONContains(t, actualValue, value, path+"."+key)
		}
	case []any:
		actual, ok := got.([]any)
		if !ok {
			t.Fatalf("%s: got %T, want array", path, got)
		}

		if len(actual) < len(expected) {
			t.Fatalf("%s: got %d items, want at least %d", path, len(actual), len(expected))
		}

		for index, value := range expected {
			assertJSONContains(t, actual[index], value, fmt.Sprintf("%s[%d]", path, index))
		}
	default:
		if got != expected {
			t.Fatalf("%s: got %#v, want %#v", path, got, expected)
		}
	}
}
