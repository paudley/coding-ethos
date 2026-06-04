// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package adapter_test

import (
	"errors"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/agentproxy/adapter"
)

func TestOpenAIDetect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		reqCtx      agentproxy.RequestContext
		specificity int
		matched     bool
	}{
		{
			name: "host and path",
			reqCtx: agentproxy.RequestContext{
				Host: "api.openai.com",
				Path: "/v1/chat/completions",
			},
			specificity: 20,
			matched:     true,
		},
		{
			name:        "path only",
			reqCtx:      agentproxy.RequestContext{Path: "/v1/chat/completions"},
			specificity: 10,
			matched:     true,
		},
		{
			name:   "no match",
			reqCtx: agentproxy.RequestContext{Host: "api.anthropic.com"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := adapter.OpenAI{}.Detect(test.reqCtx)
			if got.Matched != test.matched {
				t.Fatalf("matched = %v, want %v", got.Matched, test.matched)
			}

			if got.Specificity != test.specificity {
				t.Fatalf("specificity = %d, want %d", got.Specificity, test.specificity)
			}
		})
	}
}

func TestOpenAINormalizeRequest(t *testing.T) {
	t.Parallel()

	body := loadFixture(t, "openai", "request.json")

	norm, err := adapter.OpenAI{}.NormalizeRequest(
		body,
		agentproxy.RequestContext{},
	)
	if err != nil {
		t.Fatalf("normalize request: %v", err)
	}

	if norm.Model != "gpt-4o" {
		t.Fatalf("model = %q", norm.Model)
	}

	if len(norm.Messages) != 2 {
		t.Fatalf("messages = %d", len(norm.Messages))
	}

	if norm.Messages[0].Role != agentproxy.RoleSystem {
		t.Fatalf("first role = %q", norm.Messages[0].Role)
	}

	if len(norm.ToolDefinitions) != 1 {
		t.Fatalf("tools = %d", len(norm.ToolDefinitions))
	}

	if norm.ToolDefinitions[0].Name != "list_dir" {
		t.Fatalf("tool name = %q", norm.ToolDefinitions[0].Name)
	}

	if norm.ToolDefinitions[0].SchemaHash == "" {
		t.Fatal("tool schema hash empty")
	}
}

func TestOpenAINormalizeResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		fixture   string
		respCtx   agentproxy.ResponseContext
		content   string
		callNames []string
		streamed  bool
		input     int
	}{
		{
			name:    "text response",
			fixture: "response.json",
			content: "The repo root contains README.md and go.mod.",
			input:   42,
		},
		{
			name:      "tool call response",
			fixture:   "response_tool_call.json",
			callNames: []string{"list_dir"},
			input:     42,
		},
		{
			name:     "streaming response",
			fixture:  "response.sse",
			respCtx:  agentproxy.ResponseContext{ContentType: "text/event-stream"},
			streamed: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			body := loadFixture(t, "openai", test.fixture)

			norm, err := adapter.OpenAI{}.NormalizeResponse(body, test.respCtx)
			if err != nil {
				t.Fatalf("normalize response: %v", err)
			}

			assertResponse(t, norm, responseExpectation{
				content:   test.content,
				callNames: test.callNames,
				streamed:  test.streamed,
				input:     test.input,
			})
		})
	}
}

func TestOpenAINormalizeRequestRejectsGarbage(t *testing.T) {
	t.Parallel()

	_, err := adapter.OpenAI{}.NormalizeRequest(
		[]byte("not json"),
		agentproxy.RequestContext{},
	)
	if !errors.Is(err, adapter.ErrUnsupportedSchema) {
		t.Fatalf("err = %v, want ErrUnsupportedSchema", err)
	}
}
