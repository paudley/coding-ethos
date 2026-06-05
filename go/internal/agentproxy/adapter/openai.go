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
	openAIPathMarker = "/v1/chat/completions"
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

// Detect reports whether the request targets the OpenAI chat endpoint. The host
// must equal or be a subdomain of the API host and the path must anchor on the
// chat completions prefix, so look-alike hosts and embedded paths are rejected.
func (OpenAI) Detect(reqCtx agentproxy.RequestContext) agentproxy.MatchResult {
	hasPath := pathHasServicePrefix(reqCtx.Path, openAIPathMarker)
	hasHost := hostMatchesService(reqCtx.Host, openAIHostMarker)

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
		return openAIReconstructStream(body), nil
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

// openAIStreamChunk is the structural subset of one chat.completion.chunk.
type openAIStreamChunk struct {
	Model   string               `json:"model"`
	Usage   openAIUsageWire      `json:"usage"`
	Choices []openAIStreamChoice `json:"choices"`
}

// openAIStreamChoice carries the incremental delta of one streamed choice.
type openAIStreamChoice struct {
	Delta openAIStreamDelta `json:"delta"`
}

// openAIStreamDelta is one incremental chunk of an assistant message.
type openAIStreamDelta struct {
	Role      string                 `json:"role"`
	Content   string                 `json:"content"`
	ToolCalls []openAIStreamToolCall `json:"tool_calls"`
}

// openAIStreamToolCall is one indexed tool-call fragment within a delta.
type openAIStreamToolCall struct {
	Function openAIStreamToolFunction `json:"function"`
	Index    int                      `json:"index"`
}

// openAIStreamToolFunction carries the streamed name and argument fragments.
type openAIStreamToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// openAIToolAccumulator gathers the name and argument fragments of one indexed
// tool call across the chunks that contribute to it.
type openAIToolAccumulator struct {
	name string
	args strings.Builder
}

// openAIReconstructStream rebuilds a normalization from an accumulated OpenAI
// SSE body. A parse that yields no chunks falls back to the streamed marker so
// an unrecognized stream degrades gracefully rather than erroring the response.
func openAIReconstructStream(body []byte) agentproxy.ResponseNormalization {
	events := parseSSEEvents(body)

	state := newOpenAIStreamState()

	parsed := false

	for _, event := range events {
		var chunk openAIStreamChunk
		if json.Unmarshal(event.Data, &chunk) != nil {
			continue
		}

		state.apply(chunk)

		parsed = true
	}

	if !parsed {
		return streamedResponse(body)
	}

	return state.normalization(body)
}

// openAIStreamState accumulates the reconstructed assistant message, tool
// calls, usage, and model across streamed chunks.
type openAIStreamState struct {
	tools map[int]*openAIToolAccumulator
	usage openAIUsageWire
	role  string
	model string
	text  strings.Builder
	order []int
}

// newOpenAIStreamState returns an empty state ready to accumulate chunks.
func newOpenAIStreamState() *openAIStreamState {
	return &openAIStreamState{
		tools: make(map[int]*openAIToolAccumulator),
	}
}

// apply folds one chunk into the accumulating reconstruction state.
func (state *openAIStreamState) apply(chunk openAIStreamChunk) {
	if chunk.Model != "" {
		state.model = chunk.Model
	}

	if chunk.Usage.PromptTokens != "" || chunk.Usage.CompletionTokens != "" {
		state.usage = chunk.Usage
	}

	for _, choice := range chunk.Choices {
		state.applyDelta(choice.Delta)
	}
}

// applyDelta folds one streamed delta into the state.
func (state *openAIStreamState) applyDelta(delta openAIStreamDelta) {
	if state.role == "" && delta.Role != "" {
		state.role = delta.Role
	}

	state.text.WriteString(delta.Content)

	for _, call := range delta.ToolCalls {
		state.applyToolCall(call)
	}
}

// applyToolCall merges one indexed tool-call fragment into its accumulator.
func (state *openAIStreamState) applyToolCall(call openAIStreamToolCall) {
	accumulator, present := state.tools[call.Index]
	if !present {
		accumulator = &openAIToolAccumulator{}
		state.tools[call.Index] = accumulator
		state.order = append(state.order, call.Index)
	}

	if accumulator.name == "" && call.Function.Name != "" {
		accumulator.name = call.Function.Name
	}

	accumulator.args.WriteString(call.Function.Arguments)
}

// normalization renders the accumulated state into a response normalization.
func (state *openAIStreamState) normalization(
	body []byte,
) agentproxy.ResponseNormalization {
	role := state.role
	if role == "" {
		role = string(agentproxy.RoleAssistant)
	}

	messages := []agentproxy.Message{{
		Role:    mapRole(role),
		Content: state.text.String(),
	}}

	calls := make([]agentproxy.ToolCall, 0, len(state.order))
	for _, index := range state.order {
		accumulator := state.tools[index]
		calls = append(calls, agentproxy.ToolCall{
			Name:     accumulator.name,
			ArgsHash: agentproxy.HashText(accumulator.args.String()),
		})
	}

	return agentproxy.ResponseNormalization{
		Messages:    messages,
		ToolCalls:   calls,
		Usage:       openAIUsage(state.usage),
		Model:       state.model,
		BodyHash:    agentproxy.HashText(string(body)),
		Measurement: agentproxy.Measure(body),
		Metadata:    map[string]string{metaStreamingReconstructed: metaValueTrue},
	}
}
