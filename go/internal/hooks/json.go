// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import (
	"encoding/json"
	"fmt"
	"io"

	"blackcat.ca/coding-ethos/go/internal/toolaliases"
)

const (
	canonicalToolBash  = "Bash"
	canonicalToolWrite = "Write"
)

func DecodeEvent(reader io.Reader) (Event, error) {
	payload := map[string]json.RawMessage{}

	decoder := json.NewDecoder(reader)

	err := decoder.Decode(&payload)
	if err != nil {
		return Event{}, fmt.Errorf("decode hook event: %w", err)
	}

	return normalizeEvent(payload), nil
}

func EncodeResult(writer io.Writer, result Result) error {
	if result.Provider != "" {
		return EncodeProviderResult(writer, result)
	}

	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")

	err := encoder.Encode(result)
	if err != nil {
		return fmt.Errorf("encode hook result: %w", err)
	}

	return nil
}

func normalizeEvent(payload map[string]json.RawMessage) Event {
	event := Event{
		Cwd:            firstString(payload, "cwd", "working_directory", "workingDirectory"),
		HookEventName:  primaryHookEventName(payload),
		Matcher:        firstString(payload, "matcher"),
		ProviderHint:   firstString(payload, "provider", "agent", "runtime"),
		SessionID:      firstString(payload, "session_id", "sessionID", "sessionId"),
		Source:         firstString(payload, "source"),
		ToolInput:      primaryToolInput(payload),
		ToolName:       firstString(payload, "tool_name", "toolName", "tool"),
		ToolResponse:   primaryToolResponse(payload),
		TranscriptPath: firstString(payload, "transcript_path", "transcriptPath"),
	}
	if event.Source == "" {
		event.Source = event.ProviderHint
	}

	event = normalizeNestedTool(payload, event)
	event = normalizeParallelTool(event)
	event.HookEventName = normalizedHookEventName(event.HookEventName)
	event.ToolInput = normalizeToolInputForAlias(event.ToolName, event.ToolInput)
	event.ToolName = normalizedToolName(event.ToolName)
	event.ToolResponse = mergeTopLevelResponseStatus(payload, event.ToolResponse)

	return event
}

func normalizedHookEventName(name string) string {
	switch name {
	case "BeforeTool":
		return "PreToolUse"
	default:
		return name
	}
}

func normalizedToolName(name string) string {
	if canonical, ok := toolaliases.ActiveCanonical(name); ok {
		return canonical
	}
	if toolaliases.NoopCanonical(name) {
		return toolaliases.CanonicalNoop
	}

	return name
}

func primaryHookEventName(payload map[string]json.RawMessage) string {
	return firstString(
		payload,
		"hook_event_name",
		"hookEventName",
		"event",
		"event_name",
	)
}

func primaryToolInput(payload map[string]json.RawMessage) map[string]any {
	return firstMap(
		payload,
		"tool_input",
		"toolInput",
		"input",
		"arguments",
		"args",
		"parameters",
	)
}

func primaryToolResponse(payload map[string]json.RawMessage) map[string]any {
	return firstResponseMap(
		payload,
		"tool_response",
		"toolResponse",
		"response",
		"output",
		"result",
	)
}

func normalizeNestedTool(payload map[string]json.RawMessage, event Event) Event {
	for _, key := range []string{"tool_call", "toolCall", "tool"} {
		nested := decodeObject(payload[key])
		if len(nested) == 0 {
			continue
		}

		if event.ToolName == "" {
			event.ToolName = firstString(nested, "name", "tool_name", "toolName", "tool")
		}

		if event.ToolInput == nil {
			event.ToolInput = firstMap(nested, "input", "arguments", "args", "parameters")
		}

		if event.ToolResponse == nil {
			event.ToolResponse = firstResponseMap(nested, "response", "output", "result")
		}
	}

	return event
}

func normalizeParallelTool(event Event) Event {
	if event.ToolName != "multi_tool_use.parallel" || event.ToolInput == nil {
		return event
	}

	toolUses := anySlice(event.ToolInput["tool_uses"])
	if len(toolUses) > 1 {
		event.ToolInput[parallelToolBatchMarker] = true
		return event
	}

	for _, toolUse := range toolUses {
		nested := mapFromAny(toolUse)
		toolName := firstStringAny(nested, "recipient_name", "name", "tool_name", "toolName", "tool")
		canonical, ok := toolaliases.ActiveCanonical(toolName)
		if !ok {
			continue
		}

		event.ToolName = canonical
		event.ToolInput = normalizeToolInputForAlias(toolName, mapFromAny(nested["parameters"]))

		return event
	}

	return event
}

func normalizeToolInputForAlias(toolName string, input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	if command, ok := input["cmd"].(string); ok && command != "" {
		input["command"] = command
	}
	if command, ok := input["chars"].(string); ok && command != "" {
		input["command"] = command
	}
	if _, ok := toolaliases.ActiveCanonical(toolName); ok {
		return input
	}

	return input
}

func anySlice(value any) []any {
	if typed, ok := value.([]any); ok {
		return typed
	}

	return nil
}

func mapFromAny(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}

	return nil
}

func firstStringAny(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && value != "" {
			return value
		}
	}

	return ""
}

func mergeTopLevelResponseStatus(
	payload map[string]json.RawMessage,
	response map[string]any,
) map[string]any {
	for _, key := range []string{
		"return_code",
		"returnCode",
		"exitCode",
		"exit_code",
		"code",
	} {
		value, ok := decodeNumberOrString(payload[key])
		if !ok {
			continue
		}

		if response == nil {
			response = map[string]any{}
		}

		response[key] = value
	}
	for _, key := range []string{"status", "state", "outcome"} {
		value, ok := decodeString(payload[key])
		if !ok {
			continue
		}

		if response == nil {
			response = map[string]any{}
		}

		response[key] = value
	}

	return response
}

func firstString(payload map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		value, ok := decodeString(payload[key])
		if ok {
			return value
		}
	}

	return ""
}

func firstMap(payload map[string]json.RawMessage, keys ...string) map[string]any {
	for _, key := range keys {
		value := decodeMap(payload[key])
		if value != nil {
			return value
		}
	}

	return nil
}

func firstResponseMap(
	payload map[string]json.RawMessage,
	keys ...string,
) map[string]any {
	for _, key := range keys {
		if response := decodeMap(payload[key]); response != nil {
			return response
		}

		if response, ok := decodeString(payload[key]); ok {
			return map[string]any{"output": response}
		}
	}

	return nil
}

func decodeString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}

	var value string

	err := json.Unmarshal(raw, &value)
	if err != nil || value == "" {
		return "", false
	}

	return value, true
}

func decodeMap(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}

	value := map[string]any{}

	err := json.Unmarshal(raw, &value)
	if err != nil {
		return nil
	}

	return value
}

func decodeNumberOrString(raw json.RawMessage) (any, bool) {
	if len(raw) == 0 {
		return nil, false
	}

	var value any

	err := json.Unmarshal(raw, &value)
	if err != nil {
		return nil, false
	}

	switch value.(type) {
	case float64, string:
		return value, true
	default:
		return nil, false
	}
}

func decodeObject(raw json.RawMessage) map[string]json.RawMessage {
	if len(raw) == 0 {
		return nil
	}

	value := map[string]json.RawMessage{}

	err := json.Unmarshal(raw, &value)
	if err != nil {
		return nil
	}

	return value
}
