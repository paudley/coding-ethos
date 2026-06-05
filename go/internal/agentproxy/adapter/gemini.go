// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package adapter

import (
	"encoding/json"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
)

const (
	// geminiName is the stable adapter identifier for generateContent.
	geminiName = "gemini"
	// geminiHostMarker identifies the Gemini API host.
	geminiHostMarker = "generativelanguage.googleapis.com"
	// geminiPathMarker identifies the generateContent endpoints.
	geminiPathMarker = ":generateContent"
	// geminiStreamPathMarker identifies the streaming endpoint variant.
	geminiStreamPathMarker = ":streamGenerateContent"
	// geminiModelSegment precedes the model name in the request path.
	geminiModelSegment = "/models/"
	// geminiSpecificityHostPath scores a host-plus-path match.
	geminiSpecificityHostPath = 20
	// geminiSpecificityPath scores a path-only match.
	geminiSpecificityPath = 10
)

// geminiRequest is the structural subset of a generateContent request. Its
// custom decoder reads camelCase wire keys without struct tags.
type geminiRequest struct {
	SystemInstruction geminiContent
	Contents          []geminiContent
	Tools             []geminiTool
}

// UnmarshalJSON decodes the camelCase request keys via raw field extraction.
func (request *geminiRequest) UnmarshalJSON(data []byte) error {
	fields, err := rawObject(data)
	if err != nil {
		return err
	}

	err = decodeField(fields, "systemInstruction", &request.SystemInstruction)
	if err != nil {
		return err
	}

	err = decodeField(fields, "contents", &request.Contents)
	if err != nil {
		return err
	}

	return decodeField(fields, "tools", &request.Tools)
}

// geminiContent is one conversational turn with typed parts.
type geminiContent struct {
	Role  string       `json:"role"`
	Parts []geminiPart `json:"parts"`
}

// geminiPart is one text or function-call fragment of a turn. Its custom
// decoder reads the camelCase functionCall key without struct tags.
type geminiPart struct {
	Text         string
	FunctionCall geminiFunctionCall
}

// UnmarshalJSON decodes the camelCase part keys via raw field extraction.
func (part *geminiPart) UnmarshalJSON(data []byte) error {
	fields, err := rawObject(data)
	if err != nil {
		return err
	}

	err = decodeField(fields, "text", &part.Text)
	if err != nil {
		return err
	}

	return decodeField(fields, "functionCall", &part.FunctionCall)
}

// geminiFunctionCall is an invoked tool call inside a response part.
type geminiFunctionCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

// geminiTool groups declared function tools. Its custom decoder reads the
// camelCase functionDeclarations key without struct tags.
type geminiTool struct {
	FunctionDeclarations []geminiFunctionDecl
}

// UnmarshalJSON decodes the camelCase tool key via raw field extraction.
func (tool *geminiTool) UnmarshalJSON(data []byte) error {
	fields, err := rawObject(data)
	if err != nil {
		return err
	}

	return decodeField(fields, "functionDeclarations", &tool.FunctionDeclarations)
}

// geminiFunctionDecl declares a callable function tool.
type geminiFunctionDecl struct {
	Name       string          `json:"name"`
	Parameters json.RawMessage `json:"parameters"`
}

// geminiResponse is the structural subset of a generateContent response. Its
// custom decoder reads the camelCase usageMetadata key without struct tags.
type geminiResponse struct {
	UsageMetadata geminiUsage
	Candidates    []geminiCandidate
}

// UnmarshalJSON decodes the camelCase response keys via raw field extraction.
func (response *geminiResponse) UnmarshalJSON(data []byte) error {
	fields, err := rawObject(data)
	if err != nil {
		return err
	}

	err = decodeField(fields, "candidates", &response.Candidates)
	if err != nil {
		return err
	}

	return decodeField(fields, "usageMetadata", &response.UsageMetadata)
}

// geminiCandidate wraps one response content turn.
type geminiCandidate struct {
	Content geminiContent `json:"content"`
}

// geminiUsage holds token accounting for a response. Its custom decoder reads
// the camelCase count keys without struct tags.
type geminiUsage struct {
	PromptTokenCount     json.Number
	CandidatesTokenCount json.Number
	TotalTokenCount      json.Number
}

// UnmarshalJSON decodes the camelCase usage keys via raw field extraction.
func (usage *geminiUsage) UnmarshalJSON(data []byte) error {
	fields, err := rawObject(data)
	if err != nil {
		return err
	}

	err = decodeField(fields, "promptTokenCount", &usage.PromptTokenCount)
	if err != nil {
		return err
	}

	err = decodeField(fields, "candidatesTokenCount", &usage.CandidatesTokenCount)
	if err != nil {
		return err
	}

	return decodeField(fields, "totalTokenCount", &usage.TotalTokenCount)
}

// Gemini adapts the Gemini generateContent JSON schema. It is IO-free.
type Gemini struct{}

// Name returns the stable adapter identifier.
func (Gemini) Name() string {
	return geminiName
}

// Detect reports whether the request targets a generateContent endpoint. The
// host must equal or be a subdomain of the API host and the request path must
// end with a generateContent method suffix, rejecting look-alike hosts.
func (Gemini) Detect(reqCtx agentproxy.RequestContext) agentproxy.MatchResult {
	trimmedPath := strings.TrimSpace(reqCtx.Path)
	hasPath := strings.HasSuffix(trimmedPath, geminiPathMarker) ||
		strings.HasSuffix(trimmedPath, geminiStreamPathMarker)
	hasHost := hostMatchesService(reqCtx.Host, geminiHostMarker)

	switch {
	case hasHost && hasPath:
		return agentproxy.MatchResult{
			Matched:     true,
			Specificity: geminiSpecificityHostPath,
		}
	case hasPath:
		return agentproxy.MatchResult{
			Matched:     true,
			Specificity: geminiSpecificityPath,
		}
	default:
		return agentproxy.MatchResult{}
	}
}

// NormalizeRequest parses a generateContent request into structural facts. The
// model name is recovered from the request path because the body omits it.
func (Gemini) NormalizeRequest(
	body []byte,
	reqCtx agentproxy.RequestContext,
) (agentproxy.RequestNormalization, error) {
	var wire geminiRequest

	err := json.Unmarshal(body, &wire)
	if err != nil {
		return agentproxy.RequestNormalization{}, ErrUnsupportedSchema
	}

	messages := geminiRequestMessages(wire)
	tools := geminiToolDefinitions(wire.Tools)

	return agentproxy.RequestNormalization{
		Messages:        messages,
		ToolDefinitions: tools,
		Model:           geminiModelFromPath(reqCtx.Path),
		BodyHash:        agentproxy.HashText(string(body)),
		Measurement:     agentproxy.Measure(body),
		Metadata:        map[string]string{},
		Stream:          strings.Contains(reqCtx.Path, geminiStreamPathMarker),
	}, nil
}

// NormalizeResponse parses a generateContent response into structural facts.
func (Gemini) NormalizeResponse(
	body []byte,
	respCtx agentproxy.ResponseContext,
) (agentproxy.ResponseNormalization, error) {
	if isStreamingResponse(respCtx.ContentType) {
		return geminiReconstructStream(body), nil
	}

	var wire geminiResponse

	err := json.Unmarshal(body, &wire)
	if err != nil {
		return agentproxy.ResponseNormalization{}, ErrUnsupportedSchema
	}

	messages, calls := geminiResponseTurns(wire.Candidates)

	return agentproxy.ResponseNormalization{
		Messages:    messages,
		ToolCalls:   calls,
		Usage:       geminiTokenUsage(wire.UsageMetadata),
		BodyHash:    agentproxy.HashText(string(body)),
		Measurement: agentproxy.Measure(body),
		Metadata:    map[string]string{},
	}, nil
}

// geminiRequestMessages flattens request turns into neutral messages,
// prepending the optional system instruction as a system message.
func geminiRequestMessages(wire geminiRequest) []agentproxy.Message {
	messages := make([]agentproxy.Message, 0, len(wire.Contents)+1)

	system := joinTextParts(geminiPartTexts(wire.SystemInstruction.Parts))
	if system != "" {
		messages = append(messages, agentproxy.Message{
			Role:    agentproxy.RoleSystem,
			Content: system,
		})
	}

	for _, content := range wire.Contents {
		messages = append(messages, agentproxy.Message{
			Role:    mapRole(content.Role),
			Content: joinTextParts(geminiPartTexts(content.Parts)),
		})
	}

	return messages
}

// geminiResponseTurns splits candidate turns into neutral messages and calls.
func geminiResponseTurns(
	candidates []geminiCandidate,
) ([]agentproxy.Message, []agentproxy.ToolCall) {
	messages := make([]agentproxy.Message, 0, len(candidates))
	calls := make([]agentproxy.ToolCall, 0, len(candidates))

	for _, candidate := range candidates {
		messages = append(messages, agentproxy.Message{
			Role:    mapRole(candidate.Content.Role),
			Content: joinTextParts(geminiPartTexts(candidate.Content.Parts)),
		})
		calls = append(calls, geminiPartCalls(candidate.Content.Parts)...)
	}

	return messages, calls
}

// geminiPartTexts extracts the text fragments from a slice of parts.
func geminiPartTexts(parts []geminiPart) []string {
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		if part.Text != "" {
			texts = append(texts, part.Text)
		}
	}

	return texts
}

// geminiPartCalls extracts structural tool calls from a slice of parts.
func geminiPartCalls(parts []geminiPart) []agentproxy.ToolCall {
	calls := make([]agentproxy.ToolCall, 0, len(parts))
	for _, part := range parts {
		if part.FunctionCall.Name != "" {
			calls = append(calls, agentproxy.ToolCall{
				Name:     part.FunctionCall.Name,
				ArgsHash: hashArgs(part.FunctionCall.Args),
			})
		}
	}

	return calls
}

// geminiToolDefinitions flattens declared function tools into definitions.
func geminiToolDefinitions(tools []geminiTool) []agentproxy.ToolDefinition {
	definitions := make([]agentproxy.ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		for _, decl := range tool.FunctionDeclarations {
			definitions = append(definitions, agentproxy.ToolDefinition{
				Name:       decl.Name,
				SchemaHash: hashArgs(decl.Parameters),
			})
		}
	}

	return definitions
}

// geminiModelFromPath recovers the model name from a generateContent path.
func geminiModelFromPath(path string) string {
	index := strings.LastIndex(path, geminiModelSegment)
	if index < 0 {
		return ""
	}

	rest := path[index+len(geminiModelSegment):]

	if colon := strings.IndexByte(rest, ':'); colon >= 0 {
		rest = rest[:colon]
	}

	return rest
}

// geminiTokenUsage converts wire usage metadata into neutral token usage.
func geminiTokenUsage(wire geminiUsage) agentproxy.TokenUsage {
	return agentproxy.TokenUsage{
		InputTokens:  jsonNumberToInt(wire.PromptTokenCount),
		OutputTokens: jsonNumberToInt(wire.CandidatesTokenCount),
		TotalTokens:  jsonNumberToInt(wire.TotalTokenCount),
	}
}

// geminiCandidateState accumulates the reconstructed message and tool calls of
// one candidate index across the streamed chunks that contribute to it.
type geminiCandidateState struct {
	role  string
	texts []string
	calls []agentproxy.ToolCall
}

// geminiStreamState accumulates the reconstructed per-candidate messages and
// last-seen usage across streamed generateContent chunks, preserving candidate
// index order.
type geminiStreamState struct {
	candidates map[int]*geminiCandidateState
	usage      geminiUsage
	order      []int
}

// newGeminiStreamState returns an empty state ready to accumulate chunks.
func newGeminiStreamState() *geminiStreamState {
	return &geminiStreamState{
		candidates: make(map[int]*geminiCandidateState),
	}
}

// geminiReconstructStream rebuilds a normalization from an accumulated Gemini
// SSE body, reconstructing one message per candidate index. A scan failure or a
// stream that contributes no recognizable candidate falls back to the streamed
// marker so a parse failure and an unrecognized stream both degrade gracefully
// rather than emitting a misleading reconstruction.
func geminiReconstructStream(body []byte) agentproxy.ResponseNormalization {
	events, err := parseSSEEvents(body)
	if err != nil {
		return streamedResponse(body)
	}

	state := newGeminiStreamState()

	parsed := false

	for _, event := range events {
		var chunk geminiResponse
		if json.Unmarshal(event.Data, &chunk) != nil {
			continue
		}

		if state.apply(chunk) {
			parsed = true
		}
	}

	if !parsed {
		return streamedResponse(body)
	}

	return state.normalization(body)
}

// apply folds one streamed generateContent chunk into the accumulating state,
// reporting whether the chunk carried at least one candidate so the caller can
// distinguish a real stream from an unrecognized payload. Each candidate's
// parts contribute to the message at its positional index within the chunk.
func (state *geminiStreamState) apply(chunk geminiResponse) bool {
	for index, candidate := range chunk.Candidates {
		target := state.candidateState(index)

		if candidate.Content.Role != "" {
			target.role = candidate.Content.Role
		}

		target.texts = append(target.texts, geminiPartTexts(candidate.Content.Parts)...)
		target.calls = append(target.calls, geminiPartCalls(candidate.Content.Parts)...)
	}

	if chunk.UsageMetadata.TotalTokenCount != "" ||
		chunk.UsageMetadata.PromptTokenCount != "" {
		state.usage = chunk.UsageMetadata
	}

	return len(chunk.Candidates) > 0
}

// candidateState returns the accumulator for one candidate index, registering
// it in first-seen order so reconstructed messages preserve their stream order.
func (state *geminiStreamState) candidateState(index int) *geminiCandidateState {
	candidate, present := state.candidates[index]
	if !present {
		candidate = &geminiCandidateState{}
		state.candidates[index] = candidate
		state.order = append(state.order, index)
	}

	return candidate
}

// normalization renders the accumulated state into a response normalization,
// emitting one message per candidate index in stream order.
func (state *geminiStreamState) normalization(
	body []byte,
) agentproxy.ResponseNormalization {
	messages := make([]agentproxy.Message, 0, len(state.order))
	calls := make([]agentproxy.ToolCall, 0, len(state.order))

	for _, index := range state.order {
		candidate := state.candidates[index]

		role := candidate.role
		if role == "" {
			role = "model"
		}

		messages = append(messages, agentproxy.Message{
			Role:    mapRole(role),
			Content: strings.Join(candidate.texts, ""),
		})
		calls = append(calls, candidate.calls...)
	}

	return agentproxy.ResponseNormalization{
		Messages:    messages,
		ToolCalls:   calls,
		Usage:       geminiTokenUsage(state.usage),
		BodyHash:    agentproxy.HashText(string(body)),
		Measurement: agentproxy.Measure(body),
		Metadata:    map[string]string{metaStreamingReconstructed: metaValueTrue},
	}
}
