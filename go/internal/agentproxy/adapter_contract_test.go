// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agentproxy_test

import (
	"testing"
	"time"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
)

func TestMeasure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		body  string
		bytes int
		lines int
	}{
		{name: "empty body", body: "", bytes: 0, lines: 0},
		{name: "single line no newline", body: "abc", bytes: 3, lines: 1},
		{name: "single line trailing newline", body: "abc\n", bytes: 4, lines: 2},
		{name: "two lines", body: "a\nb", bytes: 3, lines: 2},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := agentproxy.Measure([]byte(test.body))
			if got.Bytes != test.bytes {
				t.Fatalf("bytes = %d, want %d", got.Bytes, test.bytes)
			}

			if got.Lines != test.lines {
				t.Fatalf("lines = %d, want %d", got.Lines, test.lines)
			}
		})
	}
}

func TestOutboundEvent(t *testing.T) {
	t.Parallel()

	identity := agentproxy.EventIdentity{
		RecordedAtUTC: time.Unix(0, 0).UTC(),
		ID:            "evt-1",
		SessionID:     "sess-1",
		Provider:      "openai",
		TraceID:       "trace-1",
		TrackingID:    "track-1",
	}
	norm := agentproxy.RequestNormalization{
		Messages: []agentproxy.Message{
			{Role: agentproxy.RoleUser, Content: "hello"},
		},
		ToolDefinitions: []agentproxy.ToolDefinition{
			{Name: "list_dir", SchemaHash: "hash"},
		},
		Model:       "gpt-4o",
		BodyHash:    "body-hash",
		Measurement: agentproxy.PayloadMeasurement{Bytes: 5, Lines: 1},
		Metadata:    map[string]string{"keep": "yes"},
		Stream:      true,
	}

	event := agentproxy.OutboundEvent(identity, norm)

	if event.Kind != agentproxy.EventProviderCall {
		t.Fatalf("kind = %q", event.Kind)
	}

	if event.Direction != agentproxy.DirectionOutbound {
		t.Fatalf("direction = %q", event.Direction)
	}

	if event.InputHash != "body-hash" {
		t.Fatalf("input hash = %q", event.InputHash)
	}

	assertMeta(t, event.Metadata, "payload_body_retained", "false")
	assertMeta(t, event.Metadata, "message_count", "1")
	assertMeta(t, event.Metadata, "tool_definition_count", "1")
	assertMeta(t, event.Metadata, "stream_requested", "true")
	assertMeta(t, event.Metadata, "keep", "yes")
}

func TestInboundEvent(t *testing.T) {
	t.Parallel()

	norm := agentproxy.ResponseNormalization{
		Messages: []agentproxy.Message{
			{Role: agentproxy.RoleAssistant, Content: "hi"},
		},
		ToolCalls: []agentproxy.ToolCall{
			{Name: "list_dir", ArgsHash: "args"},
			{Name: "read_file", ArgsHash: "args2"},
		},
		Usage:    agentproxy.TokenUsage{InputTokens: 3, OutputTokens: 2},
		Model:    "gpt-4o",
		BodyHash: "resp-hash",
		Metadata: map[string]string{},
		Streamed: true,
	}

	event := agentproxy.InboundEvent(agentproxy.EventIdentity{}, norm)

	if event.Kind != agentproxy.EventProviderResponse {
		t.Fatalf("kind = %q", event.Kind)
	}

	if event.Direction != agentproxy.DirectionInbound {
		t.Fatalf("direction = %q", event.Direction)
	}

	if event.OutputHash != "resp-hash" {
		t.Fatalf("output hash = %q", event.OutputHash)
	}

	assertMeta(t, event.Metadata, "tool_call_count", "2")
	assertMeta(t, event.Metadata, "tool_call_names", "list_dir,read_file")
	assertMeta(t, event.Metadata, "streaming_not_normalized", "true")
}

func assertMeta(t *testing.T, meta map[string]string, key, want string) {
	t.Helper()

	got, present := meta[key]
	if !present {
		t.Fatalf("metadata missing key %q", key)
	}

	if got != want {
		t.Fatalf("metadata[%q] = %q, want %q", key, got, want)
	}
}
