// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agentproxy

import (
	"strconv"
	"strings"
	"time"
)

const (
	// adapterPolicyID labels every event emitted from a provider adapter.
	adapterPolicyID = "proxy.provider_adapter"
	// metaPayloadBodyRetained records that no raw provider body was kept.
	metaPayloadBodyRetained = "payload_body_retained"
	// metaMessageCount records the structural message count.
	metaMessageCount = "message_count"
	// metaToolDefinitionCount records the number of declared tools.
	metaToolDefinitionCount = "tool_definition_count"
	// metaToolCallCount records the number of tool calls in a response.
	metaToolCallCount = "tool_call_count"
	// metaToolCallNames carries comma-joined structural tool-call names.
	metaToolCallNames = "tool_call_names"
	// metaStreamRequested marks a request that asked for streaming.
	metaStreamRequested = "stream_requested"
	// metaStreamingNotNormalized marks a streamed response left unparsed.
	metaStreamingNotNormalized = "streaming_not_normalized"
	// metaValueTrue is the canonical truthy metadata value.
	metaValueTrue = "true"
	// metaValueFalse is the canonical falsy metadata value.
	metaValueFalse = "false"
	// toolCallNameSeparator joins structural tool-call names in metadata.
	toolCallNameSeparator = ","
)

// RequestContext carries transport-level facts used to detect and normalize a
// provider request. It deliberately excludes auth headers and raw bodies so
// adapters cannot retain sensitive material.
type RequestContext struct {
	Method      string
	Host        string
	Path        string
	ContentType string
}

// ResponseContext carries transport-level facts used to normalize a provider
// response. The status code lets callers gate parsing while the content type
// signals streaming payloads.
type ResponseContext struct {
	ContentType string
	StatusCode  int
}

// MatchResult reports whether an adapter claims a request and how specific the
// match is. Higher specificity wins when several adapters match.
type MatchResult struct {
	Specificity int
	Matched     bool
}

// ToolDefinition captures a declared tool by name and a hash of its schema.
// The raw schema is never retained.
type ToolDefinition struct {
	Name       string
	SchemaHash string
}

// ToolCall captures an invoked tool by name and a hash of its arguments. The
// raw argument JSON is never retained.
type ToolCall struct {
	Name     string
	ArgsHash string
}

// RequestNormalization is the provider-neutral view of a request body. It holds
// only structural facts, hashes, and measurements rather than raw content.
type RequestNormalization struct {
	Metadata        map[string]string
	Model           string
	BodyHash        string
	Messages        []Message
	ToolDefinitions []ToolDefinition
	Measurement     PayloadMeasurement
	Stream          bool
}

// ResponseNormalization is the provider-neutral view of a response body. It
// holds only structural facts, hashes, usage, and measurements.
type ResponseNormalization struct {
	Metadata    map[string]string
	Model       string
	BodyHash    string
	Messages    []Message
	ToolCalls   []ToolCall
	Usage       TokenUsage
	Measurement PayloadMeasurement
	Streamed    bool
}

// EventIdentity supplies caller-owned correlation fields applied to every
// emitted event. Adapters never invent these values.
type EventIdentity struct {
	RecordedAtUTC time.Time
	ID            string
	SessionID     string
	Provider      string
	RepoRoot      string
	Cwd           string
	TraceID       string
	TrackingID    string
}

// Adapter converts a single provider's HTTP JSON schema into the neutral
// normalization structures. Implementations are pure and IO-free.
type Adapter interface {
	// Name returns the stable adapter identifier.
	Name() string
	// Detect reports whether the adapter claims the request transport facts.
	Detect(reqCtx RequestContext) MatchResult
	// NormalizeRequest parses a request body into structural facts.
	NormalizeRequest(body []byte, reqCtx RequestContext) (RequestNormalization, error)
	// NormalizeResponse parses a response body into structural facts.
	NormalizeResponse(body []byte, respCtx ResponseContext) (ResponseNormalization, error)
}

// AdapterRegistry selects the most specific adapter for a request.
type AdapterRegistry interface {
	// Match returns the winning adapter for the request, if any.
	Match(reqCtx RequestContext) (Adapter, bool)
}

// OutboundEvent builds a body-free outbound provider-call event from a request
// normalization. No message content is copied into the event.
func OutboundEvent(
	identity EventIdentity,
	norm RequestNormalization,
) ProviderEvent {
	metadata := cloneMetadata(norm.Metadata)
	metadata[metaPayloadBodyRetained] = metaValueFalse
	metadata[metaMessageCount] = strconv.Itoa(len(norm.Messages))
	metadata[metaToolDefinitionCount] = strconv.Itoa(len(norm.ToolDefinitions))

	if norm.Stream {
		metadata[metaStreamRequested] = metaValueTrue
	}

	return ProviderEvent{
		RecordedAtUTC: identity.RecordedAtUTC,
		Metadata:      metadata,
		Cwd:           identity.Cwd,
		InputHash:     norm.BodyHash,
		Model:         norm.Model,
		Kind:          EventProviderCall,
		RepoRoot:      identity.RepoRoot,
		ID:            identity.ID,
		Provider:      identity.Provider,
		PolicyID:      adapterPolicyID,
		SessionID:     identity.SessionID,
		TraceID:       identity.TraceID,
		TrackingID:    identity.TrackingID,
		Direction:     DirectionOutbound,
		PayloadKind:   PayloadPrompt,
		Payload:       norm.Measurement,
	}
}

// InboundEvent builds a body-free inbound provider-response event from a
// response normalization. Tool-call argument JSON appears only as a hash.
func InboundEvent(
	identity EventIdentity,
	norm ResponseNormalization,
) ProviderEvent {
	metadata := cloneMetadata(norm.Metadata)
	metadata[metaPayloadBodyRetained] = metaValueFalse
	metadata[metaMessageCount] = strconv.Itoa(len(norm.Messages))
	metadata[metaToolCallCount] = strconv.Itoa(len(norm.ToolCalls))

	if norm.Streamed {
		metadata[metaStreamingNotNormalized] = metaValueTrue
	}

	if names := toolCallNames(norm.ToolCalls); names != "" {
		metadata[metaToolCallNames] = names
	}

	return ProviderEvent{
		RecordedAtUTC: identity.RecordedAtUTC,
		Metadata:      metadata,
		Cwd:           identity.Cwd,
		OutputHash:    norm.BodyHash,
		Model:         norm.Model,
		Kind:          EventProviderResponse,
		RepoRoot:      identity.RepoRoot,
		ID:            identity.ID,
		Provider:      identity.Provider,
		PolicyID:      adapterPolicyID,
		SessionID:     identity.SessionID,
		TraceID:       identity.TraceID,
		TrackingID:    identity.TrackingID,
		Direction:     DirectionInbound,
		PayloadKind:   PayloadResponse,
		TokenUsage:    norm.Usage,
		Payload:       norm.Measurement,
	}
}

// toolCallNames joins structural tool-call names for metadata. Argument JSON is
// never included here; only the names are structural facts.
func toolCallNames(calls []ToolCall) string {
	if len(calls) == 0 {
		return ""
	}

	names := make([]string, 0, len(calls))
	for _, call := range calls {
		if call.Name != "" {
			names = append(names, call.Name)
		}
	}

	return strings.Join(names, toolCallNameSeparator)
}

// Measure reports the byte and line size of a body without retaining it.
// Adapters call it to record payload size while discarding raw content.
func Measure(body []byte) PayloadMeasurement {
	return PayloadMeasurement{
		Bytes: len(body),
		Lines: lineCount(body),
	}
}

// lineCount returns the number of lines in a body, counting a final
// unterminated line. An empty body has zero lines.
func lineCount(body []byte) int {
	if len(body) == 0 {
		return 0
	}

	const finalLineAdjustment = 1

	count := finalLineAdjustment

	for _, character := range body {
		if character == '\n' {
			count++
		}
	}

	return count
}
