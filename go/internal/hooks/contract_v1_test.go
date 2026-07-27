// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks_test

import (
	"strings"
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/hooks"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestNeutralHookContractV1MatchesGoldenFixtures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload string
		fixture string
	}{
		{
			name: "allowed",
			payload: `{
				"contract_version": "coding-ethos.hook/v1",
				"correlation_id": "request-allowed-001",
				"provider": "codex",
				"hook_event_name": "PreToolUse",
				"tool_name": "functions.update_plan",
				"tool_input": {}
			}`,
			fixture: "testdata/neutral_v1_allowed.json",
		},
		{
			name: "blocked",
			payload: `{
				"contract_version": "coding-ethos.hook/v1",
				"correlation_id": "request-blocked-001",
				"provider": "codex",
				"hook_event_name": "PreToolUse",
				"tool_name": "Bash",
				"tool_input": {"command": "git commit --no-verify -m test"}
			}`,
			fixture: "testdata/neutral_v1_blocked.json",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			event, err := DecodeEvent(strings.NewReader(test.payload))
			if err != nil {
				t.Fatalf("decode event: %v", err)
			}

			result, err := Run(policy.ExampleBundle(), Options{Event: event})
			if err != nil {
				t.Fatalf("run hook: %v", err)
			}

			var output strings.Builder
			if err := EncodeNeutralHookResultV1(&output, result); err != nil {
				t.Fatalf("encode neutral result: %v", err)
			}

			assertJSONMatchesFixture(t, output.String(), test.fixture)
		})
	}
}

func TestNeutralHookContractV1GeneratesCorrelationID(t *testing.T) {
	t.Parallel()

	event, err := DecodeEvent(strings.NewReader(`{
		"provider": "codex",
		"event": "SessionStart"
	}`))
	if err != nil {
		t.Fatalf("decode event: %v", err)
	}

	result, err := Run(policy.ExampleBundle(), Options{Event: event})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}
	if !strings.HasPrefix(result.CorrelationID, "hook-") {
		t.Fatalf("generated correlation_id = %q", result.CorrelationID)
	}
}

func TestDeclaredNeutralHookContractV1RejectsInvalidShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload string
		want    string
	}{
		{
			name: "unsupported contract",
			payload: `{
				"contract_version": "coding-ethos.hook/v2",
				"provider": "codex",
				"hook_event_name": "Stop"
			}`,
			want: "unsupported contract_version",
		},
		{
			name: "provider required",
			payload: `{
				"contract_version": "coding-ethos.hook/v1",
				"hook_event_name": "Stop"
			}`,
			want: "provider is required",
		},
		{
			name: "unknown provider",
			payload: `{
				"contract_version": "coding-ethos.hook/v1",
				"provider": "unknown-supervisor",
				"source": "codex",
				"hook_event_name": "Stop"
			}`,
			want: "unsupported provider",
		},
		{
			name: "unknown field",
			payload: `{
				"contract_version": "coding-ethos.hook/v1",
				"provider": "codex",
				"hook_event_name": "Stop",
				"unexpected": true
			}`,
			want: "unsupported field",
		},
		{
			name: "multiple values",
			payload: `{
				"provider": "codex",
				"event": "Stop"
			} {}`,
			want: "multiple JSON values",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := DecodeEvent(strings.NewReader(test.payload))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("DecodeEvent error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestHookEventInputIsBounded(t *testing.T) {
	t.Parallel()

	payload := `{"provider":"codex","event":"Stop","padding":"` +
		strings.Repeat("x", HookContractV1MaxInputBytes) +
		`"}`

	_, err := DecodeEvent(strings.NewReader(payload))
	if err == nil || !strings.Contains(err.Error(), "payload exceeds") {
		t.Fatalf("DecodeEvent error = %v, want payload limit", err)
	}
}

func TestHookContractV1CapabilityIsMachineReadable(t *testing.T) {
	t.Parallel()

	capability := HookContractV1Capability()
	if capability.ContractVersion != HookContractV1 ||
		capability.Selector != HookContractV1Selector ||
		capability.MaxInputBytes != HookContractV1MaxInputBytes {
		t.Fatalf("capability = %#v", capability)
	}
}
