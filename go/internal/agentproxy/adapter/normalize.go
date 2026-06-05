// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

// Package adapter implements pure, IO-free provider adapters that translate
// OpenAI, Anthropic, and Gemini HTTP JSON into the provider-neutral agentproxy
// normalization structures. Adapters retain only structural facts, hashes, and
// measurements; raw bodies and authentication material are never persisted.
package adapter

import (
	"bufio"
	"bytes"
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

// ErrSSEParse marks an SSE body whose line scan failed, for example because a
// single line exceeded the bounded scanner buffer. A reconstructor treats it as
// a parse failure and falls back to the streamed marker rather than returning
// the partially scanned events as if the stream had been read in full.
var ErrSSEParse = apperror.StaticError(
	"provider adapter: server-sent-events scan failed",
)

const (
	// sseContentTypeMarker identifies a server-sent-events response.
	sseContentTypeMarker = "text/event-stream"
	// sseDataPrefix prefixes a server-sent-events data line.
	sseDataPrefix = "data:"
	// sseEventPrefix prefixes a server-sent-events event-type line.
	sseEventPrefix = "event:"
	// sseDoneSentinel marks the OpenAI end-of-stream data payload.
	sseDoneSentinel = "[DONE]"
	// metaStreamingReconstructed marks a stream reconstructed into facts.
	metaStreamingReconstructed = "streaming_reconstructed"
	// metaValueTrue is the canonical truthy metadata value.
	metaValueTrue = "true"
	// sseScannerBufferBytes bounds the per-line scanner buffer.
	sseScannerBufferBytes = 1 << 20
)

// sseEvent is one parsed server-sent-events record carrying its optional event
// type and its raw JSON data payload. Adapters interpret the payload per their
// own provider schema while sharing this line-level parsing.
type sseEvent struct {
	Event string
	Data  json.RawMessage
}

// sseAccumulator gathers the event type and the data: payload lines of one
// in-progress SSE event until a blank line dispatches it. Per the SSE spec an
// event may carry several data: lines that must be concatenated with newlines
// into a single data payload, which this accumulator reproduces. A non-nil data
// slice marks that at least one data: line was seen, even an empty one.
type sseAccumulator struct {
	event string
	data  []string
}

// parseSSEEvents splits an accumulated SSE body into events. It accumulates the
// event type from event: lines and the JSON payload from data: lines, joining
// multiple data: lines of one event with newlines, dispatching a completed
// event on each blank line, and skipping comments and the OpenAI [DONE]
// sentinel. A non-nil error reports a scan failure (for example a line longer
// than the bounded buffer) so a reconstructor fails closed instead of treating
// the partial scan as a complete stream.
func parseSSEEvents(body []byte) ([]sseEvent, error) {
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), sseScannerBufferBytes)

	events := make([]sseEvent, 0)

	var current sseAccumulator

	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			dispatchSSEEvent(&current, &events)

			continue
		}

		accumulateSSELine(line, &current)
	}

	err := scanner.Err()
	if err != nil {
		return nil, ErrSSEParse
	}

	dispatchSSEEvent(&current, &events)

	return events, nil
}

// accumulateSSELine folds one non-blank SSE line into the in-progress event,
// retaining the event type and appending each data: payload line. Comment lines
// beginning with a colon are ignored per the SSE specification.
func accumulateSSELine(line string, current *sseAccumulator) {
	switch {
	case strings.HasPrefix(line, sseEventPrefix):
		current.event = strings.TrimSpace(strings.TrimPrefix(line, sseEventPrefix))
	case strings.HasPrefix(line, sseDataPrefix):
		payload := strings.TrimSpace(strings.TrimPrefix(line, sseDataPrefix))
		// append makes a nil slice non-nil even for an empty payload, so a
		// non-nil data slice reliably marks that a data: line was seen.
		current.data = append(current.data, payload)
	default:
	}
}

// dispatchSSEEvent emits the accumulated event when it carries a usable data
// payload, joining its data: lines with newlines, then resets the accumulator.
// A [DONE] sentinel or an empty payload is dropped so adapters see only JSON.
func dispatchSSEEvent(current *sseAccumulator, events *[]sseEvent) {
	if current.data == nil {
		*current = sseAccumulator{}

		return
	}

	payload := strings.Join(current.data, "\n")
	if payload != sseDoneSentinel && payload != "" {
		*events = append(*events, sseEvent{
			Event: current.event,
			Data:  json.RawMessage(payload),
		})
	}

	*current = sseAccumulator{}
}

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
