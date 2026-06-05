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
// The host must equal or be a subdomain of the API host and the path must
// anchor on the Messages prefix, rejecting look-alike hosts and embedded paths.
func (Anthropic) Detect(reqCtx agentproxy.RequestContext) agentproxy.MatchResult {
	hasPath := pathHasServicePrefix(reqCtx.Path, anthropicPathMarker)
	hasHost := hostMatchesService(reqCtx.Host, anthropicHostMarker)

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
		return anthropicReconstructStream(body), nil
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

const (
	// anthropicEventMessageStart names the stream message-start event.
	anthropicEventMessageStart = "message_start"
	// anthropicEventBlockStart names the content-block-start event.
	anthropicEventBlockStart = "content_block_start"
	// anthropicEventBlockDelta names the content-block-delta event.
	anthropicEventBlockDelta = "content_block_delta"
	// anthropicEventMessageDelta names the message-delta event.
	anthropicEventMessageDelta = "message_delta"
	// anthropicTextDelta marks an incremental text delta.
	anthropicTextDelta = "text_delta"
	// anthropicInputJSONDelta marks an incremental tool-argument delta.
	anthropicInputJSONDelta = "input_json_delta"
)

// anthropicStreamEnvelope is the structural subset of one stream event payload.
type anthropicStreamEnvelope struct {
	Message      anthropicStreamMessage `json:"message"`
	Delta        anthropicStreamDelta   `json:"delta"`
	Usage        anthropicUsage         `json:"usage"`
	ContentBlock anthropicBlock         `json:"content_block"`
	Index        int                    `json:"index"`
}

// anthropicStreamMessage carries the role, model, and seed usage of a stream.
type anthropicStreamMessage struct {
	Role  string         `json:"role"`
	Model string         `json:"model"`
	Usage anthropicUsage `json:"usage"`
}

// anthropicStreamDelta carries one incremental text or tool-argument fragment.
type anthropicStreamDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	PartialJSON string `json:"partial_json"`
}

// anthropicStreamState accumulates the reconstructed message, tool calls, and
// usage across an Anthropic content-block stream keyed by block index.
type anthropicStreamState struct {
	blocks       map[int]*anthropicBlockAccumulator
	role         string
	model        string
	text         strings.Builder
	blockOrder   []int
	inputTokens  int
	outputTokens int
}

// anthropicBlockAccumulator gathers the name and argument fragments of one
// tool_use content block across the deltas that contribute to it.
type anthropicBlockAccumulator struct {
	name string
	args strings.Builder
}

// anthropicReconstructStream rebuilds a normalization from an accumulated
// Anthropic SSE body. An empty or unrecognized stream falls back to the
// streamed marker so the response degrades gracefully rather than erroring.
func anthropicReconstructStream(body []byte) agentproxy.ResponseNormalization {
	events := parseSSEEvents(body)

	state := newAnthropicStreamState()

	parsed := false

	for _, event := range events {
		var envelope anthropicStreamEnvelope
		if json.Unmarshal(event.Data, &envelope) != nil {
			continue
		}

		state.apply(event.Event, envelope)

		parsed = true
	}

	if !parsed {
		return streamedResponse(body)
	}

	return state.normalization(body)
}

// newAnthropicStreamState returns an empty state ready to accumulate events.
func newAnthropicStreamState() *anthropicStreamState {
	return &anthropicStreamState{
		blocks: make(map[int]*anthropicBlockAccumulator),
	}
}

// apply folds one typed stream event into the accumulating state.
func (state *anthropicStreamState) apply(
	eventType string,
	envelope anthropicStreamEnvelope,
) {
	switch eventType {
	case anthropicEventMessageStart:
		state.role = envelope.Message.Role
		state.model = envelope.Message.Model
		state.inputTokens = jsonNumberToInt(envelope.Message.Usage.InputTokens)
	case anthropicEventBlockStart:
		state.startBlock(envelope)
	case anthropicEventBlockDelta:
		state.applyDelta(envelope)
	case anthropicEventMessageDelta:
		state.outputTokens = jsonNumberToInt(envelope.Usage.OutputTokens)
	default:
	}
}

// startBlock registers a tool_use content block so later deltas can target it.
func (state *anthropicStreamState) startBlock(envelope anthropicStreamEnvelope) {
	if envelope.ContentBlock.Type != anthropicToolUseBlock {
		return
	}

	if _, present := state.blocks[envelope.Index]; !present {
		state.blockOrder = append(state.blockOrder, envelope.Index)
	}

	state.blocks[envelope.Index] = &anthropicBlockAccumulator{
		name: envelope.ContentBlock.Name,
	}
}

// applyDelta folds one content-block delta into text or a tool accumulator.
func (state *anthropicStreamState) applyDelta(envelope anthropicStreamEnvelope) {
	switch envelope.Delta.Type {
	case anthropicTextDelta:
		state.text.WriteString(envelope.Delta.Text)
	case anthropicInputJSONDelta:
		if accumulator, present := state.blocks[envelope.Index]; present {
			accumulator.args.WriteString(envelope.Delta.PartialJSON)
		}
	default:
	}
}

// normalization renders the accumulated state into a response normalization.
func (state *anthropicStreamState) normalization(
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

	calls := make([]agentproxy.ToolCall, 0, len(state.blockOrder))
	for _, index := range state.blockOrder {
		accumulator := state.blocks[index]
		calls = append(calls, agentproxy.ToolCall{
			Name:     accumulator.name,
			ArgsHash: agentproxy.HashText(accumulator.args.String()),
		})
	}

	return agentproxy.ResponseNormalization{
		Messages:    messages,
		ToolCalls:   calls,
		Usage:       state.tokenUsage(),
		Model:       state.model,
		BodyHash:    agentproxy.HashText(string(body)),
		Measurement: agentproxy.Measure(body),
		Metadata:    map[string]string{metaStreamingReconstructed: metaValueTrue},
	}
}

// tokenUsage assembles neutral token usage from the seed and delta counts.
func (state *anthropicStreamState) tokenUsage() agentproxy.TokenUsage {
	return agentproxy.TokenUsage{
		InputTokens:  state.inputTokens,
		OutputTokens: state.outputTokens,
		TotalTokens:  state.inputTokens + state.outputTokens,
	}
}
