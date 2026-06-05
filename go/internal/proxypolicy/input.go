// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package proxypolicy

import (
	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/celexpr"
)

const (
	// metaProxyEventID joins a denial to the originating proxy event.
	metaProxyEventID = "proxy_event_id"
	// metaProxySessionID carries the proxy session the request belonged to.
	metaProxySessionID = "proxy_session_id"
	// metaProxyEventKind carries the structural proxy event kind.
	metaProxyEventKind = "proxy_event_kind"
	// metaProxyProvider carries the upstream provider label.
	metaProxyProvider = "proxy_provider"
	// metaProxyDirection carries the traffic direction of the request.
	metaProxyDirection = "proxy_direction"
	// metaProxyPayloadKind carries the structural payload kind.
	metaProxyPayloadKind = "proxy_payload_kind"
	// metaProxyTraceID carries the cross-event trace correlation ID.
	metaProxyTraceID = "proxy_trace_id"
	// metaProxyTrackingID carries the cross-event tracking correlation ID.
	metaProxyTrackingID = "proxy_tracking_id"
)

// toCelProxyInput maps the body-free proxy decision facts onto the CEL proxy
// input the expression evaluates. DLP facts are converted to their CEL form and
// HasDLPFacts is set so a policy can branch on their presence without inspecting
// any payload content.
func toCelProxyInput(input agentproxy.ProxyDecisionInput) celexpr.ProxyInput {
	facts := celDLPFacts(input.DLPFacts)

	return celexpr.ProxyInput{
		EventID:      input.EventID,
		SessionID:    input.SessionID,
		Kind:         string(input.Kind),
		Provider:     input.Provider,
		Model:        input.Model,
		Tool:         input.Tool,
		Direction:    string(input.Direction),
		PayloadKind:  string(input.PayloadKind),
		TargetPath:   input.TargetPath,
		InputHash:    input.InputHash,
		OutputHash:   input.OutputHash,
		DLPFacts:     facts,
		HasDLPFacts:  len(facts) > 0,
		InputTokens:  int64(input.TokenUsage.InputTokens),
		OutputTokens: int64(input.TokenUsage.OutputTokens),
		TotalTokens:  int64(input.TokenUsage.TotalTokens),
		PayloadBytes: int64(input.Payload.Bytes),
	}
}

// celDLPFacts converts proxy DLP facts into their CEL projection. Only detector
// labels, locations, and confidence cross the boundary; no payload content is
// carried because the source facts never held any.
func celDLPFacts(facts []agentproxy.DLPFact) []celexpr.ProxyDLPFactInput {
	converted := make([]celexpr.ProxyDLPFactInput, 0, len(facts))
	for _, fact := range facts {
		converted = append(converted, celexpr.ProxyDLPFactInput{
			Type:       fact.Type,
			Path:       fact.Path,
			Reason:     fact.Reason,
			Confidence: fact.Confidence,
			Line:       int64(fact.Line),
			Column:     int64(fact.Column),
		})
	}

	return converted
}

// proxySARIFMetadata builds the stable proxy_* metadata keys a denial event
// carries so it can be joined to the request that triggered it. Numeric and
// enum facts are rendered as strings because event metadata is string-valued.
func proxySARIFMetadata(input agentproxy.ProxyDecisionInput) map[string]string {
	metadata := map[string]string{
		metaProxyEventID:     input.EventID,
		metaProxySessionID:   input.SessionID,
		metaProxyEventKind:   string(input.Kind),
		metaProxyProvider:    input.Provider,
		metaProxyDirection:   string(input.Direction),
		metaProxyPayloadKind: string(input.PayloadKind),
	}

	if input.Metadata != nil {
		if traceID := input.Metadata[metaProxyTraceID]; traceID != "" {
			metadata[metaProxyTraceID] = traceID
		}

		if trackingID := input.Metadata[metaProxyTrackingID]; trackingID != "" {
			metadata[metaProxyTrackingID] = trackingID
		}
	}

	return metadata
}
