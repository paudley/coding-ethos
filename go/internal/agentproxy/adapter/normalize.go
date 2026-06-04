// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

// Package adapter implements pure, IO-free provider adapters that translate
// OpenAI, Anthropic, and Gemini HTTP JSON into the provider-neutral agentproxy
// normalization structures. Adapters retain only structural facts, hashes, and
// measurements; raw bodies and authentication material are never persisted.
package adapter

import (
	"encoding/json"
	"net"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/apperror"
)

// ErrUnsupportedSchema marks a body that a matched adapter could not parse.
var ErrUnsupportedSchema = apperror.StaticError(
	"provider adapter: unsupported or unparseable schema",
)

// sseContentTypeMarker identifies a server-sent-events response.
const sseContentTypeMarker = "text/event-stream"

// mapRole converts a raw provider role string into the neutral role. Unknown
// roles default to the user role to remain structurally meaningful.
func mapRole(raw string) agentproxy.Role {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(agentproxy.RoleSystem):
		return agentproxy.RoleSystem
	case string(agentproxy.RoleAssistant), "model":
		return agentproxy.RoleAssistant
	case string(agentproxy.RoleTool):
		return agentproxy.RoleTool
	default:
		return agentproxy.RoleUser
	}
}

// hashArgs hashes a raw JSON argument fragment so argument content never
// survives as plain text. Empty input yields an empty hash.
func hashArgs(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	return agentproxy.HashText(string(raw))
}

// jsonNumberToInt converts a JSON number into an int, ignoring fractional
// noise. A nil or non-numeric value yields zero.
func jsonNumberToInt(value json.Number) int {
	if value == "" {
		return 0
	}

	parsed, err := value.Int64()
	if err != nil {
		return 0
	}

	return int(parsed)
}

// isStreamingResponse reports whether a response content type signals SSE.
func isStreamingResponse(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), sseContentTypeMarker)
}

// rawObject decodes a JSON object into a map of raw fields so adapters can read
// externally cased keys without struct tags. A non-object yields the schema
// error so callers fail fast rather than silently producing empty data.
func rawObject(data []byte) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage

	err := json.Unmarshal(data, &fields)
	if err != nil {
		return nil, ErrUnsupportedSchema
	}

	return fields, nil
}

// decodeField decodes a single named raw field into the target. A missing field
// is a no-op so optional keys leave the target at its zero value.
func decodeField(
	fields map[string]json.RawMessage,
	key string,
	target any,
) error {
	raw, present := fields[key]
	if !present {
		return nil
	}

	err := json.Unmarshal(raw, target)
	if err != nil {
		return ErrUnsupportedSchema
	}

	return nil
}

// streamedResponse builds a normalization for a server-sent-events body that
// the adapter intentionally does not parse. Only structural measurements and
// the body hash are retained so callers still observe payload size.
func streamedResponse(body []byte) agentproxy.ResponseNormalization {
	return agentproxy.ResponseNormalization{
		Messages:    []agentproxy.Message{},
		ToolCalls:   []agentproxy.ToolCall{},
		BodyHash:    agentproxy.HashText(string(body)),
		Measurement: agentproxy.Measure(body),
		Metadata:    map[string]string{},
		Streamed:    true,
	}
}

// hostMatchesService reports whether host targets serviceHost exactly or as a
// subdomain of it. The host is lowercased, trimmed, and stripped of any port so
// look-alike hosts such as api.openai.com.evil.tld never satisfy the suffix
// rule. serviceHost must already be lowercase (for example api.openai.com).
func hostMatchesService(host, serviceHost string) bool {
	normalized := strings.ToLower(strings.TrimSpace(host))

	hostOnly, _, err := net.SplitHostPort(normalized)
	if err == nil {
		normalized = hostOnly
	}

	return normalized == serviceHost ||
		strings.HasSuffix(normalized, "."+serviceHost)
}

// pathHasServicePrefix reports whether path equals prefix or sits beneath it as
// a path segment. A trailing path such as /proxy/v1/chat/completions is
// rejected because the prefix must anchor at the start of the trimmed path.
func pathHasServicePrefix(path, prefix string) bool {
	trimmed := strings.TrimSpace(path)

	return trimmed == prefix || strings.HasPrefix(trimmed, prefix+"/")
}

// joinTextParts concatenates non-empty text fragments with newlines.
func joinTextParts(parts []string) string {
	filtered := make([]string, 0, len(parts))

	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			filtered = append(filtered, part)
		}
	}

	return strings.Join(filtered, "\n")
}
