// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package adapter

import (
	"encoding/json"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
)

const (
	// anthropicName is the stable adapter identifier for the Messages API.
	anthropicName = "anthropic"
	// anthropicHostMarker identifies the Anthropic API host.
	anthropicHostMarker = "api.anthropic.com"
	// anthropicPathMarker identifies the Messages endpoint.
	anthropicPathMarker = "/v1/messages"
	// anthropicSpecificityHostPath scores a host-plus-path match.
	anthropicSpecificityHostPath = 20
	// anthropicSpecificityPath scores a path-only match.
	anthropicSpecificityPath = 10
	// anthropicTextBlock marks a text content block.
	anthropicTextBlock = "text"
	// anthropicToolUseBlock marks a tool invocation content block.
	anthropicToolUseBlock = "tool_use"
)

// anthropicRequest is the structural subset of a Messages API request.
type anthropicRequest struct {
	Model    string             `json:"model"`
	System   string             `json:"system"`
	Messages []anthropicMessage `json:"messages"`
	Tools    []anthropicTool    `json:"tools"`
	Stream   bool               `json:"stream"`
}

// anthropicMessage is one request message with structured content blocks.
type anthropicMessage struct {
	Role    string           `json:"role"`
	Content anthropicContent `json:"content"`
}

// anthropicContent holds the text fragments of a request message. The Messages
// API permits content as either a bare string or an array of typed blocks, so a
// custom decoder normalizes both shapes into a single slice of text fragments.
type anthropicContent struct {
	Texts []string
}

// UnmarshalJSON decodes a string or a block array into text fragments. Non-text
// blocks are ignored because only structural text is retained.
func (content *anthropicContent) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "[") {
		return content.unmarshalBlocks(data)
	}

	var text string

	err := json.Unmarshal(data, &text)
	if err != nil {
		return ErrUnsupportedSchema
	}

	content.Texts = []string{text}

	return nil
}

// unmarshalBlocks decodes an array of content blocks into text fragments.
func (content *anthropicContent) unmarshalBlocks(data []byte) error {
	var blocks []anthropicBlock

	err := json.Unmarshal(data, &blocks)
	if err != nil {
		return ErrUnsupportedSchema
	}

	texts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Type == anthropicTextBlock {
			texts = append(texts, block.Text)
		}
	}

	content.Texts = texts

	return nil
}

// anthropicTool declares a callable tool with an input schema.
type anthropicTool struct {
	Name        string          `json:"name"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// anthropicResponse is the structural subset of a Messages API response.
type anthropicResponse struct {
	Model   string           `json:"model"`
	Role    string           `json:"role"`
	Usage   anthropicUsage   `json:"usage"`
	Content []anthropicBlock `json:"content"`
}

// anthropicBlock is one response content block.
type anthropicBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// anthropicUsage holds token accounting for a response.
type anthropicUsage struct {
	InputTokens  json.Number `json:"input_tokens"`
	OutputTokens json.Number `json:"output_tokens"`
}

// Anthropic adapts the Anthropic Messages API JSON schema. It is IO-free.
type Anthropic struct{}

// Name returns the stable adapter identifier.
func (Anthropic) Name() string {
	return anthropicName
}

// Detect reports whether the request targets the Anthropic Messages endpoint.
func (Anthropic) Detect(reqCtx agentproxy.RequestContext) agentproxy.MatchResult {
	hasPath := strings.Contains(reqCtx.Path, anthropicPathMarker)
	hasHost := strings.Contains(reqCtx.Host, anthropicHostMarker)

	switch {
	case hasHost && hasPath:
		return agentproxy.MatchResult{
			Matched:     true,
			Specificity: anthropicSpecificityHostPath,
		}
	case hasPath:
		return agentproxy.MatchResult{
			Matched:     true,
			Specificity: anthropicSpecificityPath,
		}
	default:
		return agentproxy.MatchResult{}
	}
}

// NormalizeRequest parses an Anthropic Messages request into structural facts.
func (Anthropic) NormalizeRequest(
	body []byte,
	_ agentproxy.RequestContext,
) (agentproxy.RequestNormalization, error) {
	var wire anthropicRequest

	err := json.Unmarshal(body, &wire)
	if err != nil {
		return agentproxy.RequestNormalization{}, ErrUnsupportedSchema
	}

	messages := anthropicRequestMessages(wire)

	tools := make([]agentproxy.ToolDefinition, 0, len(wire.Tools))
	for _, tool := range wire.Tools {
		tools = append(tools, agentproxy.ToolDefinition{
			Name:       tool.Name,
			SchemaHash: hashArgs(tool.InputSchema),
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

// NormalizeResponse parses an Anthropic Messages response into structural facts.
func (Anthropic) NormalizeResponse(
	body []byte,
	respCtx agentproxy.ResponseContext,
) (agentproxy.ResponseNormalization, error) {
	if isStreamingResponse(respCtx.ContentType) {
		return streamedResponse(body), nil
	}

	var wire anthropicResponse

	err := json.Unmarshal(body, &wire)
	if err != nil {
		return agentproxy.ResponseNormalization{}, ErrUnsupportedSchema
	}

	text, calls := anthropicResponseBlocks(wire.Content)

	messages := []agentproxy.Message{{
		Role:    mapRole(wire.Role),
		Content: text,
	}}

	return agentproxy.ResponseNormalization{
		Messages:    messages,
		ToolCalls:   calls,
		Usage:       anthropicTokenUsage(wire.Usage),
		Model:       wire.Model,
		BodyHash:    agentproxy.HashText(string(body)),
		Measurement: agentproxy.Measure(body),
		Metadata:    map[string]string{},
	}, nil
}

// anthropicRequestMessages flattens request content blocks into neutral
// messages, prepending the optional system prompt as a system message.
func anthropicRequestMessages(wire anthropicRequest) []agentproxy.Message {
	messages := make([]agentproxy.Message, 0, len(wire.Messages)+1)

	if strings.TrimSpace(wire.System) != "" {
		messages = append(messages, agentproxy.Message{
			Role:    agentproxy.RoleSystem,
			Content: wire.System,
		})
	}

	for _, msg := range wire.Messages {
		messages = append(messages, agentproxy.Message{
			Role:    mapRole(msg.Role),
			Content: joinTextParts(msg.Content.Texts),
		})
	}

	return messages
}

// anthropicResponseBlocks splits response content blocks into joined text and
// structural tool calls.
func anthropicResponseBlocks(
	blocks []anthropicBlock,
) (string, []agentproxy.ToolCall) {
	texts := make([]string, 0, len(blocks))
	calls := make([]agentproxy.ToolCall, 0, len(blocks))

	for _, block := range blocks {
		switch block.Type {
		case anthropicTextBlock:
			texts = append(texts, block.Text)
		case anthropicToolUseBlock:
			calls = append(calls, agentproxy.ToolCall{
				Name:     block.Name,
				ArgsHash: hashArgs(block.Input),
			})
		default:
		}
	}

	return joinTextParts(texts), calls
}

// anthropicTokenUsage converts wire usage into neutral token usage. Anthropic
// omits a total, so it is derived from the input and output counts.
func anthropicTokenUsage(wire anthropicUsage) agentproxy.TokenUsage {
	input := jsonNumberToInt(wire.InputTokens)
	output := jsonNumberToInt(wire.OutputTokens)

	return agentproxy.TokenUsage{
		InputTokens:  input,
		OutputTokens: output,
		TotalTokens:  input + output,
	}
}
