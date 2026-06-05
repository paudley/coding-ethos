// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package adapter_test

import (
	"errors"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/agentproxy/adapter"
)

const geminiGenerateContentPath = "/v1beta/models/gemini-pro:generateContent"

func TestGeminiDetect(t *testing.T) {
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
				Host: "generativelanguage.googleapis.com",
				Path: geminiGenerateContentPath,
			},
			specificity: 20,
			matched:     true,
		},
		{
			name: "host with port and path",
			reqCtx: agentproxy.RequestContext{
				Host: "generativelanguage.googleapis.com:443",
				Path: geminiGenerateContentPath,
			},
			specificity: 20,
			matched:     true,
		},
		{
			name:        "path only",
			reqCtx:      agentproxy.RequestContext{Path: geminiGenerateContentPath},
			specificity: 10,
			matched:     true,
		},
		{
			name: "look-alike host rejected via path only",
			reqCtx: agentproxy.RequestContext{
				Host: "generativelanguage.googleapis.com.evil.tld",
				Path: geminiGenerateContentPath,
			},
			specificity: 10,
			matched:     true,
		},
		{
			name: "non-method path rejected",
			reqCtx: agentproxy.RequestContext{
				Host: "generativelanguage.googleapis.com",
				Path: "/v1beta/models/gemini-pro",
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

			got := adapter.Gemini{}.Detect(test.reqCtx)
			if got.Matched != test.matched {
				t.Fatalf("matched = %v, want %v", got.Matched, test.matched)
			}

			if got.Specificity != test.specificity {
				t.Fatalf("specificity = %d, want %d", got.Specificity, test.specificity)
			}
		})
	}
}

func TestGeminiNormalizeRequest(t *testing.T) {
	t.Parallel()

	body := loadFixture(t, "gemini", "request.json")

	norm, err := adapter.Gemini{}.NormalizeRequest(
		body,
		agentproxy.RequestContext{Path: geminiGenerateContentPath},
	)
	if err != nil {
		t.Fatalf("normalize request: %v", err)
	}

	if norm.Model != "gemini-pro" {
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
}

func TestGeminiNormalizeRequestDetectsStream(t *testing.T) {
	t.Parallel()

	body := loadFixture(t, "gemini", "request.json")

	norm, err := adapter.Gemini{}.NormalizeRequest(body, agentproxy.RequestContext{
		Path: "/v1beta/models/gemini-pro:streamGenerateContent",
	})
	if err != nil {
		t.Fatalf("normalize request: %v", err)
	}

	if !norm.Stream {
		t.Fatal("stream = false, want true")
	}
}

func TestGeminiNormalizeResponse(t *testing.T) {
	t.Parallel()

	streamCtx := agentproxy.ResponseContext{ContentType: "text/event-stream"}

	tests := []struct {
		name          string
		fixture       string
		respCtx       agentproxy.ResponseContext
		content       string
		callNames     []string
		streamed      bool
		reconstructed bool
		input         int
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
			name:          "streaming text reconstructed",
			fixture:       "response.sse",
			respCtx:       streamCtx,
			content:       "The repo root contains README.md.",
			reconstructed: true,
			input:         42,
		},
		{
			name:          "streaming tool call reconstructed",
			fixture:       "response_tool_call.sse",
			respCtx:       streamCtx,
			callNames:     []string{"list_dir"},
			reconstructed: true,
			input:         42,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			body := loadFixture(t, "gemini", test.fixture)

			norm, err := adapter.Gemini{}.NormalizeResponse(body, test.respCtx)
			if err != nil {
				t.Fatalf("normalize response: %v", err)
			}

			assertResponse(t, norm, responseExpectation{
				content:       test.content,
				callNames:     test.callNames,
				streamed:      test.streamed,
				reconstructed: test.reconstructed,
				input:         test.input,
			})
		})
	}
}

func TestGeminiNormalizeResponseFallsBackOnMalformedStream(t *testing.T) {
	t.Parallel()

	body := []byte("data: not-json\n")

	norm, err := adapter.Gemini{}.NormalizeResponse(
		body,
		agentproxy.ResponseContext{ContentType: "text/event-stream"},
	)
	if err != nil {
		t.Fatalf("normalize response: %v", err)
	}

	assertResponse(t, norm, responseExpectation{streamed: true})
}

func TestGeminiNormalizeResponseFallsBackOnUnrecognizedStream(t *testing.T) {
	t.Parallel()

	body := []byte("data: {\"unexpected\":true}\n\n")

	norm, err := adapter.Gemini{}.NormalizeResponse(
		body,
		agentproxy.ResponseContext{ContentType: "text/event-stream"},
	)
	if err != nil {
		t.Fatalf("normalize response: %v", err)
	}

	assertResponse(t, norm, responseExpectation{streamed: true})
}

func TestGeminiNormalizeResponseReconstructsMultipleCandidates(t *testing.T) {
	t.Parallel()

	body := []byte("data: {\"candidates\":[" +
		"{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"first \"}]}}," +
		"{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"second \"}]}}]}\n\n" +
		"data: {\"candidates\":[" +
		"{\"content\":{\"parts\":[{\"text\":\"candidate\"}]}}," +
		"{\"content\":{\"parts\":[{\"text\":\"candidate\"}]}}]}\n\n")

	norm, err := adapter.Gemini{}.NormalizeResponse(
		body,
		agentproxy.ResponseContext{ContentType: "text/event-stream"},
	)
	if err != nil {
		t.Fatalf("normalize response: %v", err)
	}

	if len(norm.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(norm.Messages))
	}

	if norm.Messages[0].Content != "first candidate" {
		t.Fatalf("candidate 0 content = %q", norm.Messages[0].Content)
	}

	if norm.Messages[1].Content != "second candidate" {
		t.Fatalf("candidate 1 content = %q", norm.Messages[1].Content)
	}
}

func TestGeminiNormalizeRequestRejectsGarbage(t *testing.T) {
	t.Parallel()

	_, err := adapter.Gemini{}.NormalizeRequest(
		[]byte("not json"),
		agentproxy.RequestContext{},
	)
	if !errors.Is(err, adapter.ErrUnsupportedSchema) {
		t.Fatalf("err = %v, want ErrUnsupportedSchema", err)
	}
}
