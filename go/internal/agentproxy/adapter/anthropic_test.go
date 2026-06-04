// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package adapter_test

import (
	"errors"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/agentproxy/adapter"
)

func TestAnthropicDetect(t *testing.T) {
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
				Host: "api.anthropic.com",
				Path: "/v1/messages",
			},
			specificity: 20,
			matched:     true,
		},
		{
			name: "host with port and path",
			reqCtx: agentproxy.RequestContext{
				Host: "api.anthropic.com:443",
				Path: "/v1/messages",
			},
			specificity: 20,
			matched:     true,
		},
		{
			name:        "path only",
			reqCtx:      agentproxy.RequestContext{Path: "/v1/messages"},
			specificity: 10,
			matched:     true,
		},
		{
			name: "look-alike host rejected via path only",
			reqCtx: agentproxy.RequestContext{
				Host: "api.anthropic.com.evil.tld",
				Path: "/v1/messages",
			},
			specificity: 10,
			matched:     true,
		},
		{
			name: "non-prefix path rejected",
			reqCtx: agentproxy.RequestContext{
				Host: "api.anthropic.com",
				Path: "/proxy/v1/messages",
			},
		},
		{
			name:   "no match",
			reqCtx: agentproxy.RequestContext{Host: "api.openai.com"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := adapter.Anthropic{}.Detect(test.reqCtx)
			if got.Matched != test.matched {
				t.Fatalf("matched = %v, want %v", got.Matched, test.matched)
			}

			if got.Specificity != test.specificity {
				t.Fatalf("specificity = %d, want %d", got.Specificity, test.specificity)
			}
		})
	}
}

func TestAnthropicNormalizeRequest(t *testing.T) {
	t.Parallel()

	body := loadFixture(t, "anthropic", "request.json")

	norm, err := adapter.Anthropic{}.NormalizeRequest(
		body,
		agentproxy.RequestContext{},
	)
	if err != nil {
		t.Fatalf("normalize request: %v", err)
	}

	if norm.Model != "claude-sonnet-4" {
		t.Fatalf("model = %q", norm.Model)
	}

	if len(norm.Messages) != 3 {
		t.Fatalf("messages = %d", len(norm.Messages))
	}

	if norm.Messages[0].Role != agentproxy.RoleSystem {
		t.Fatalf("first role = %q", norm.Messages[0].Role)
	}

	if norm.Messages[1].Content != "List the files in the repo root." {
		t.Fatalf("block content = %q", norm.Messages[1].Content)
	}

	if len(norm.ToolDefinitions) != 1 {
		t.Fatalf("tools = %d", len(norm.ToolDefinitions))
	}
}

func TestAnthropicNormalizeResponse(t *testing.T) {
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
			content:   "Let me list the directory.",
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

			body := loadFixture(t, "anthropic", test.fixture)

			norm, err := adapter.Anthropic{}.NormalizeResponse(body, test.respCtx)
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

func TestAnthropicTokenUsageDerivesTotal(t *testing.T) {
	t.Parallel()

	body := loadFixture(t, "anthropic", "response.json")

	norm, err := adapter.Anthropic{}.NormalizeResponse(
		body,
		agentproxy.ResponseContext{},
	)
	if err != nil {
		t.Fatalf("normalize response: %v", err)
	}

	if norm.Usage.TotalTokens != 53 {
		t.Fatalf("total tokens = %d, want 53", norm.Usage.TotalTokens)
	}
}

func TestAnthropicNormalizeRequestRejectsGarbage(t *testing.T) {
	t.Parallel()

	_, err := adapter.Anthropic{}.NormalizeRequest(
		[]byte("not json"),
		agentproxy.RequestContext{},
	)
	if !errors.Is(err, adapter.ErrUnsupportedSchema) {
		t.Fatalf("err = %v, want ErrUnsupportedSchema", err)
	}
}
