// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package adapter

import (
	"encoding/json"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
)

const (
	// openAIName is the stable adapter identifier for OpenAI chat completions.
	openAIName = "openai"
	// openAIHostMarker identifies the OpenAI API host.
	openAIHostMarker = "api.openai.com"
	// openAIPathMarker identifies the chat completions endpoint.
	openAIPathMarker = "/chat/completions"
	// openAISpecificityHostPath scores a host-plus-path match.
	openAISpecificityHostPath = 20
	// openAISpecificityPath scores a path-only match.
	openAISpecificityPath = 10
)

// openAIChatRequest is the structural subset of an OpenAI chat request.
type openAIChatRequest struct {
	Model    string               `json:"model"`
	Messages []openAIChatMessage  `json:"messages"`
	Tools    []openAIToolEnvelope `json:"tools"`
	Stream   bool                 `json:"stream"`
}

// openAIChatMessage is one request or response message.
type openAIChatMessage struct {
	Role      string               `json:"role"`
	Content   string               `json:"content"`
	Name      string               `json:"name"`
	ToolCalls []openAIToolCallWire `json:"tool_calls"`
}

// openAIToolEnvelope wraps a declared function tool.
type openAIToolEnvelope struct {
	Type     string             `json:"type"`
	Function openAIFunctionDecl `json:"function"`
}

// openAIFunctionDecl declares a callable function tool.
type openAIFunctionDecl struct {
	Name       string          `json:"name"`
	Parameters json.RawMessage `json:"parameters"`
}

// openAIToolCallWire is one invoked tool call inside a response message.
type openAIToolCallWire struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAIFunctionCall `json:"function"`
}

// openAIFunctionCall holds an invoked function name and raw arguments.
type openAIFunctionCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// openAIChatResponse is the structural subset of an OpenAI chat response.
type openAIChatResponse struct {
	Model   string             `json:"model"`
	Usage   openAIUsageWire    `json:"usage"`
	Choices []openAIChoiceWire `json:"choices"`
}

// openAIChoiceWire wraps one response choice message.
type openAIChoiceWire struct {
	Message openAIChatMessage `json:"message"`
}

// openAIUsageWire holds token accounting for a response.
type openAIUsageWire struct {
	PromptTokens     json.Number `json:"prompt_tokens"`
	CompletionTokens json.Number `json:"completion_tokens"`
	TotalTokens      json.Number `json:"total_tokens"`
}

// OpenAI adapts the OpenAI chat completions JSON schema. It is pure and IO-free.
type OpenAI struct{}

// Name returns the stable adapter identifier.
func (OpenAI) Name() string {
	return openAIName
}

// Detect reports whether the request targets the OpenAI chat endpoint.
func (OpenAI) Detect(reqCtx agentproxy.RequestContext) agentproxy.MatchResult {
	hasPath := strings.Contains(reqCtx.Path, openAIPathMarker)
	hasHost := strings.Contains(reqCtx.Host, openAIHostMarker)

	switch {
	case hasHost && hasPath:
		return agentproxy.MatchResult{Matched: true, Specificity: openAISpecificityHostPath}
	case hasPath:
		return agentproxy.MatchResult{Matched: true, Specificity: openAISpecificityPath}
	default:
		return agentproxy.MatchResult{}
	}
}

// NormalizeRequest parses an OpenAI chat request into structural facts.
func (OpenAI) NormalizeRequest(
	body []byte,
	_ agentproxy.RequestContext,
) (agentproxy.RequestNormalization, error) {
	var wire openAIChatRequest

	err := json.Unmarshal(body, &wire)
	if err != nil {
		return agentproxy.RequestNormalization{}, ErrUnsupportedSchema
	}

	messages := make([]agentproxy.Message, 0, len(wire.Messages))
	for _, msg := range wire.Messages {
		messages = append(messages, agentproxy.Message{
			Role:    mapRole(msg.Role),
			Content: msg.Content,
			Name:    msg.Name,
		})
	}

	tools := make([]agentproxy.ToolDefinition, 0, len(wire.Tools))
	for _, tool := range wire.Tools {
		tools = append(tools, agentproxy.ToolDefinition{
			Name:       tool.Function.Name,
			SchemaHash: hashArgs(tool.Function.Parameters),
		})
	}

	return agentproxy.RequestNormalization{
		Messages:        messages,
		ToolDefinitions: tools,
		Model:           wire.Model,
		BodyHash:        agentproxy.HashText(string(body)),
		Measurement:     agentproxy.Measure(body),
		Metadata:        map[string]string{},
		Stream:          wire.Stream,
	}, nil
}

// NormalizeResponse parses an OpenAI chat response into structural facts.
func (OpenAI) NormalizeResponse(
	body []byte,
	respCtx agentproxy.ResponseContext,
) (agentproxy.ResponseNormalization, error) {
	if isStreamingResponse(respCtx.ContentType) {
		return streamedResponse(body), nil
	}

	var wire openAIChatResponse

	err := json.Unmarshal(body, &wire)
	if err != nil {
		return agentproxy.ResponseNormalization{}, ErrUnsupportedSchema
	}

	messages := make([]agentproxy.Message, 0, len(wire.Choices))
	calls := make([]agentproxy.ToolCall, 0, len(wire.Choices))

	for _, choice := range wire.Choices {
		messages = append(messages, agentproxy.Message{
			Role:    mapRole(choice.Message.Role),
			Content: choice.Message.Content,
			Name:    choice.Message.Name,
		})
		calls = append(calls, openAIToolCalls(choice.Message.ToolCalls)...)
	}

	return agentproxy.ResponseNormalization{
		Messages:    messages,
		ToolCalls:   calls,
		Usage:       openAIUsage(wire.Usage),
		Model:       wire.Model,
		BodyHash:    agentproxy.HashText(string(body)),
		Measurement: agentproxy.Measure(body),
		Metadata:    map[string]string{},
	}, nil
}

// openAIToolCalls converts wire tool calls into structural tool calls.
func openAIToolCalls(wire []openAIToolCallWire) []agentproxy.ToolCall {
	calls := make([]agentproxy.ToolCall, 0, len(wire))
	for _, call := range wire {
		calls = append(calls, agentproxy.ToolCall{
			Name:     call.Function.Name,
			ArgsHash: hashArgs(call.Function.Arguments),
		})
	}

	return calls
}

// openAIUsage converts wire usage into neutral token usage.
func openAIUsage(wire openAIUsageWire) agentproxy.TokenUsage {
	return agentproxy.TokenUsage{
		InputTokens:  jsonNumberToInt(wire.PromptTokens),
		OutputTokens: jsonNumberToInt(wire.CompletionTokens),
		TotalTokens:  jsonNumberToInt(wire.TotalTokens),
	}
}
