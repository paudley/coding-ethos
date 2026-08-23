// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const (
	// HookContractV1 identifies the stable provider-neutral hook JSON contract.
	HookContractV1 = "coding-ethos.hook/v1"
	// HookContractV1Selector is the CLI value that selects provider-neutral v1 output.
	HookContractV1Selector = "neutral-v1"
	// HookContractV1MaxInputBytes bounds one hook request before JSON decoding.
	HookContractV1MaxInputBytes = 1 << 20

	hookContractV1MaxCorrelationBytes = 128
	hookContractV1MaxIdentifierBytes  = 256
	hookContractV1MaxPathBytes        = 4096
)

var (
	errHookContractField = apperror.StaticError(
		"unsupported field",
	)
	errHookContractEvent = apperror.StaticError(
		"unsupported hook_event_name",
	)
	errHookContractProvider = apperror.StaticError(
		"unsupported provider",
	)
	errHookContractVersion = apperror.StaticError(
		"unsupported contract_version",
	)
	errHookContractIdentifierRequired = apperror.StaticError(
		"required hook contract identifier is empty",
	)
	errHookContractIdentifierTooLong = apperror.StaticError(
		"hook contract identifier exceeds its byte limit",
	)
	errHookContractIdentifierControl = apperror.StaticError(
		"hook contract identifier contains control characters",
	)
	errHookContractNegativeContext = apperror.StaticError(
		"context_window_tokens must be non-negative",
	)
)

// HookContractCapability describes one stable hook request/response contract.
type HookContractCapability struct {
	ContractVersion string   `json:"contract_version"`
	Selector        string   `json:"selector"`
	InputEncoding   string   `json:"input_encoding"`
	OutputEncoding  string   `json:"output_encoding"`
	Events          []string `json:"events"`
	Outcomes        []string `json:"outcomes"`
	Effects         []string `json:"effects"`
	MaxInputBytes   int64    `json:"max_input_bytes"`
}

// HookContractV1Capability returns the machine-readable neutral v1 contract.
func HookContractV1Capability() HookContractCapability {
	return HookContractCapability{
		ContractVersion: HookContractV1,
		Selector:        HookContractV1Selector,
		MaxInputBytes:   HookContractV1MaxInputBytes,
		InputEncoding:   "application/json",
		OutputEncoding:  "application/json",
		Events:          hookContractV1Events(),
		Outcomes:        []string{"allow", "deny"},
		Effects: []string{
			"allow",
			"block",
			"continue",
			"rewrite",
		},
	}
}

// ValidateHookContractV1 validates canonical v1 fields when a request declares
// the neutral contract. Provider-native requests without contract_version keep
// the existing alias-compatible decoder.
func ValidateHookContractV1(
	payload map[string]json.RawMessage,
	event Event,
) error {
	err := validateHookContractVersionAndFields(payload, event)
	if err != nil {
		return err
	}

	err = validateHookContractEvent(event)
	if err != nil {
		return err
	}

	err = validateHookContractProvider(event)
	if err != nil {
		return err
	}

	err = validateHookContractOptionalFields(event)
	if err != nil {
		return err
	}

	if event.ContextWindowTokens < 0 {
		return fmt.Errorf(
			"%w: %s",
			errHookContractNegativeContext,
			HookContractV1,
		)
	}

	return nil
}

func validateHookContractVersionAndFields(
	payload map[string]json.RawMessage,
	event Event,
) error {
	if event.ContractVersion != HookContractV1 {
		return fmt.Errorf(
			"%w: %q",
			errHookContractVersion,
			event.ContractVersion,
		)
	}

	for field := range payload {
		if !hookContractV1FieldSupported(field) {
			return fmt.Errorf(
				"%w: %s %q",
				errHookContractField,
				HookContractV1,
				field,
			)
		}
	}

	return nil
}

func validateHookContractEvent(event Event) error {
	err := validateHookContractIdentifier(
		"correlation_id",
		event.CorrelationID,
		hookContractV1MaxCorrelationBytes,
		false,
	)
	if err != nil {
		return err
	}

	err = validateHookContractIdentifier(
		"hook_event_name",
		event.HookEventName,
		hookContractV1MaxIdentifierBytes,
		true,
	)
	if err != nil {
		return err
	}

	if !slices.Contains(hookContractV1Events(), event.HookEventName) {
		return fmt.Errorf(
			"%w: %s %q",
			errHookContractEvent,
			HookContractV1,
			event.HookEventName,
		)
	}

	return nil
}

func validateHookContractProvider(event Event) error {
	err := validateHookContractIdentifier(
		"provider",
		event.ProviderHint,
		hookContractV1MaxIdentifierBytes,
		true,
	)
	if err != nil {
		return err
	}

	resolvedProvider := hookContractV1ProviderFromHint(event.ProviderHint)
	if resolvedProvider == "" ||
		!slices.Contains(hookContractV1Providers(), resolvedProvider) {
		return fmt.Errorf(
			"%w: %s %q",
			errHookContractProvider,
			HookContractV1,
			event.ProviderHint,
		)
	}

	return nil
}

func hookContractV1ProviderFromHint(providerHint string) string {
	provider := strings.ToLower(strings.TrimSpace(providerHint))
	switch {
	case strings.Contains(provider, providerCodingEthos):
		return providerCodingEthos
	case strings.Contains(provider, providerKimi):
		return providerKimi
	case strings.Contains(provider, providerGemini):
		return providerGemini
	case strings.Contains(provider, providerCodex):
		return providerCodex
	case strings.Contains(provider, providerClaude):
		return providerClaude
	case provider == "generic":
		return provider
	default:
		return ""
	}
}

func validateHookContractOptionalFields(event Event) error {
	for _, value := range []struct {
		name     string
		value    string
		maxBytes int
	}{
		{name: "cwd", value: event.Cwd, maxBytes: hookContractV1MaxPathBytes},
		{
			name:     "transcript_path",
			value:    event.TranscriptPath,
			maxBytes: hookContractV1MaxPathBytes,
		},
		{name: "matcher", value: event.Matcher, maxBytes: hookContractV1MaxIdentifierBytes},
		{name: "model", value: event.Model, maxBytes: hookContractV1MaxIdentifierBytes},
		{
			name:     "session_id",
			value:    event.SessionID,
			maxBytes: hookContractV1MaxIdentifierBytes,
		},
		{name: "source", value: event.Source, maxBytes: hookContractV1MaxIdentifierBytes},
		{
			name:     "tool_name",
			value:    event.ToolName,
			maxBytes: hookContractV1MaxIdentifierBytes,
		},
	} {
		err := validateHookContractIdentifier(
			value.name,
			value.value,
			value.maxBytes,
			false,
		)
		if err != nil {
			return err
		}
	}

	return nil
}

func validateHookContractIdentifier(
	name string,
	value string,
	maxBytes int,
	required bool,
) error {
	if required && strings.TrimSpace(value) == "" {
		return fmt.Errorf(
			"%w: %s %s is required",
			errHookContractIdentifierRequired,
			HookContractV1,
			name,
		)
	}

	if len(value) > maxBytes {
		return fmt.Errorf(
			"%w: %s %s exceeds %d bytes",
			errHookContractIdentifierTooLong,
			HookContractV1,
			name,
			maxBytes,
		)
	}

	if strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return fmt.Errorf(
			"%w: %s %s contains control characters",
			errHookContractIdentifierControl,
			HookContractV1,
			name,
		)
	}

	return nil
}

// NeutralHookResultV1 is the stable provider-neutral hook response.
type NeutralHookResultV1 struct {
	ContractVersion string              `json:"contract_version"`
	CorrelationID   string              `json:"correlation_id"`
	Event           NeutralHookEventV1  `json:"event"`
	Decision        string              `json:"decision"`
	Effect          NeutralHookEffectV1 `json:"effect"`
	Status          string              `json:"status"`
	TrackingID      string              `json:"tracking_id,omitempty"`
	Decisions       []policy.Decision   `json:"decisions,omitempty"`
	Advice          policy.Advice       `json:"advice,omitzero"`
	RuntimeMS       int64               `json:"runtime_ms,omitempty"`
}

// NeutralHookEventV1 identifies the evaluated event without provider-specific names.
type NeutralHookEventV1 struct {
	Name     string `json:"name"`
	Provider string `json:"provider,omitempty"`
	Tool     string `json:"tool,omitempty"`
}

// NeutralHookEffectV1 describes what a supervisor should do with the decision.
type NeutralHookEffectV1 struct {
	UpdatedInput      map[string]any `json:"updated_input,omitempty"`
	Action            string         `json:"action"`
	Reason            string         `json:"reason,omitempty"`
	AdditionalContext string         `json:"additional_context,omitempty"`
}

// EncodeNeutralHookResultV1 writes a stable provider-neutral hook response.
func EncodeNeutralHookResultV1(writer io.Writer, result Result) error {
	effect := neutralHookEffectV1(result)

	decision := "allow"
	if result.Blocked() {
		decision = "deny"
	}

	output := NeutralHookResultV1{
		ContractVersion: HookContractV1,
		CorrelationID:   result.CorrelationID,
		Event: NeutralHookEventV1{
			Name:     result.Event,
			Provider: result.Provider,
			Tool:     result.Tool,
		},
		Decision:   decision,
		Effect:     effect,
		Status:     result.Status,
		TrackingID: result.TrackingID,
		Decisions:  result.Decisions,
		Advice:     result.Advice,
		RuntimeMS:  result.RuntimeMS,
	}

	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")

	err := encoder.Encode(output)
	if err != nil {
		return fmt.Errorf("encode neutral hook result %s: %w", HookContractV1, err)
	}

	return nil
}

func neutralHookEffectV1(result Result) NeutralHookEffectV1 {
	effect := NeutralHookEffectV1{Action: "allow"}

	if result.Blocked() {
		effect.Action = "block"

		effect.Reason = ProviderBlockMessage(result)
		if result.Event == eventStop {
			effect.Action = "continue"
		}
	}

	if result.HookSpecificOutput == nil {
		return effect
	}

	effect.AdditionalContext = result.HookSpecificOutput.AdditionalContext
	effect.UpdatedInput = result.HookSpecificOutput.UpdatedInput

	if len(effect.UpdatedInput) > 0 && !result.Blocked() {
		effect.Action = "rewrite"
	}

	return effect
}

func hookContractV1FieldSupported(field string) bool {
	switch field {
	case "contract_version",
		"correlation_id",
		"context_window_tokens",
		"cwd",
		"hook_event_name",
		"matcher",
		"model",
		"provider",
		"session_id",
		"source",
		"tool_input",
		"tool_name",
		"tool_response",
		"transcript_path":
		return true
	default:
		return false
	}
}

func hookContractV1Events() []string {
	return []string{
		"Interrupt",
		"Notification",
		"PermissionRequest",
		"PermissionResult",
		"PostCompact",
		eventPostToolBatch,
		"PostToolUse",
		"PostToolUseFailure",
		"PreCompact",
		"PreToolUse",
		eventSessionEnd,
		"SessionStart",
		"Stop",
		"StopFailure",
		eventSubagentStart,
		eventSubagentStop,
		"UserPromptSubmit",
	}
}

func hookContractV1Providers() []string {
	return []string{
		providerClaude,
		providerCodex,
		providerCodingEthos,
		providerGemini,
		providerKimi,
		"generic",
	}
}
