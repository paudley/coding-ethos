// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agentproxy

import (
	"encoding/json"
	"maps"
	"net/http"
)

const (
	// interceptDecisionDeny marks an outbound request the policy evaluator blocked.
	interceptDecisionDeny = "deny"
	// reasonProxyEvalError labels a fail-closed denial caused by an evaluator error.
	reasonProxyEvalError = "proxy_eval_error"
	// denyResponseError is the stable error label in a denial response body.
	denyResponseError = "coding-ethos policy denial"
	// metaDecision records the proxy decision verdict in event metadata.
	metaDecision = "decision"
	// metaProxyTraceID carries the cross-event trace correlation ID.
	metaProxyTraceID = "proxy_trace_id"
	// metaProxyTrackingID carries the cross-event tracking correlation ID.
	metaProxyTrackingID = "proxy_tracking_id"
	// metaProxyEventID joins a deny event back to its originating proxy event.
	metaProxyEventID = "proxy_event_id"
	// metaProxySessionID carries the proxy session the denied request belonged to.
	metaProxySessionID = "proxy_session_id"
	// metaProxyEventKind carries the structural proxy event kind of the denial.
	metaProxyEventKind = "proxy_event_kind"
	// metaProxyProvider carries the upstream provider label of the denial.
	metaProxyProvider = "proxy_provider"
	// metaProxyDirection carries the traffic direction of the denied request.
	metaProxyDirection = "proxy_direction"
	// metaProxyPayloadKind carries the structural payload kind of the denial.
	metaProxyPayloadKind = "proxy_payload_kind"
)

// enforceOutbound runs the proxy policy evaluator against the decoded request
// before it reaches the upstream. It returns true when the request is allowed
// to proceed. On an evaluator error it fails closed, and on an explicit denial
// it returns false; in both denial cases it writes a 403 and records exactly one
// outbound deny event, so the caller must not record an allow event. The decoded
// bytes and DLP facts stay transient: only detector-labeled facts, hashes, and
// metadata are ever recorded.
func (proxy *InterceptProxy) enforceOutbound(
	writer http.ResponseWriter,
	request *http.Request,
	outbound outboundInput,
) bool {
	if proxy.evaluator == nil {
		return true
	}

	now := proxy.now().UTC()
	identity := proxy.identity(outbound.sessionID, outbound.host, now)

	// Outbound provider requests carry no file target today, so targetPath is
	// empty. The field is still populated and passed through so the path-based
	// DLP checks and proxy.target_path work uniformly if a future event kind
	// supplies a file target; content-based detection covers the body in the
	// meantime.
	targetPath := outboundTargetPath(outbound)
	dlpFacts := ScanRequest(outbound.decoded, targetPath)

	decision, err := proxy.evaluator.EvaluateOutbound(
		request.Context(),
		outboundDecisionInput(identity, outbound, targetPath, dlpFacts),
	)
	if err != nil {
		proxy.denyOutbound(writer, request, denyOutboundInput{
			identity: identity,
			host:     outbound.host,
			dlpFacts: dlpFacts,
			policyID: interceptPolicyID,
			reason:   reasonProxyEvalError,
		})

		return false
	}

	if !decision.Allowed {
		proxy.denyOutbound(writer, request, denyOutboundInput{
			identity: identity,
			host:     outbound.host,
			dlpFacts: dlpFacts,
			policyID: decision.PolicyID,
			reason:   decision.Reason,
			decision: decision,
		})

		return false
	}

	return true
}

// outboundTargetPath returns the file target an outbound request writes to. A
// provider-call request has no file target, so this is empty today; the helper
// exists so the target is derived in one place and the field stays populated for
// any future event kind that does carry one.
func outboundTargetPath(_ outboundInput) string {
	return ""
}

// outboundDecisionInput builds the body-free decision input from the outbound
// request facts. Outbound requests carry tool definitions, never tool calls, so
// ToolCalls is left nil. No raw body is included; payload size is a measurement.
func outboundDecisionInput(
	identity EventIdentity,
	outbound outboundInput,
	targetPath string,
	dlpFacts []DLPFact,
) ProxyDecisionInput {
	return ProxyDecisionInput{
		Direction:   DirectionOutbound,
		Kind:        EventProviderCall,
		Provider:    identity.Provider,
		PayloadKind: PayloadPrompt,
		EventID:     identity.ID,
		SessionID:   identity.SessionID,
		TargetPath:  targetPath,
		InputHash:   HashText(string(outbound.decoded)),
		TokenUsage:  TokenUsage{},
		Payload:     Measure(outbound.reqBytes),
		DLPFacts:    dlpFacts,
		Metadata:    outboundDecisionMetadata(identity),
	}
}

// outboundDecisionMetadata carries the proxy trace and tracking correlation
// fields the evaluator copies onto a denial event. Empty fields are omitted so
// the metadata stays sparse and joinable.
func outboundDecisionMetadata(identity EventIdentity) map[string]string {
	metadata := map[string]string{}
	if identity.TraceID != "" {
		metadata[metaProxyTraceID] = identity.TraceID
	}

	if identity.TrackingID != "" {
		metadata[metaProxyTrackingID] = identity.TrackingID
	}

	if len(metadata) == 0 {
		return nil
	}

	return metadata
}

// denyOutboundInput bundles the facts needed to write a 403 and record a single
// outbound deny event for a blocked or fail-closed request.
type denyOutboundInput struct {
	identity EventIdentity
	host     string
	policyID string
	reason   string
	dlpFacts []DLPFact
	decision ProxyDecision
}

// denyOutbound writes a 403 JSON denial to the client and records exactly one
// outbound deny event. The decoded body and any matched secret value are never
// stored: only detector-labeled DLP facts, the policy identity, and structural
// metadata are recorded so the denial is auditable without retaining content.
func (proxy *InterceptProxy) denyOutbound(
	writer http.ResponseWriter,
	request *http.Request,
	input denyOutboundInput,
) {
	writeDenyResponse(writer, input.policyID, input.reason)
	proxy.record(request.Context(), denyOutboundEvent(input))
}

// writeDenyResponse emits the 403 denial body identifying the policy and reason
// without echoing any request content.
func writeDenyResponse(writer http.ResponseWriter, policyID, reason string) {
	body := map[string]string{
		"error":     denyResponseError,
		"policy_id": policyID,
		"reason":    reason,
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		// Marshal the body before committing the status line so a (rare) marshal
		// failure cannot leave a sent 403 header with a partial or absent body.
		writer.WriteHeader(http.StatusInternalServerError)

		return
	}

	writer.Header().Set(contentTypeHeader, "application/json")
	writer.WriteHeader(http.StatusForbidden)

	_, writeErr := writer.Write(encoded)
	discardClientWriteError(writeErr)
}

// denyOutboundEvent builds the single outbound deny event for a blocked request.
// It records the policy evidence, detector-labeled DLP facts, and SARIF proxy
// metadata while explicitly flagging that no payload body was retained.
func denyOutboundEvent(input denyOutboundInput) ProviderEvent {
	metadata := denyEventMetadata(input)

	return ProviderEvent{
		RecordedAtUTC: input.identity.RecordedAtUTC,
		Metadata:      metadata,
		Kind:          EventProviderCall,
		RepoRoot:      input.identity.RepoRoot,
		ID:            input.identity.ID,
		Provider:      input.identity.Provider,
		PolicyID:      input.policyID,
		Decision:      interceptDecisionDeny,
		SessionID:     input.identity.SessionID,
		TraceID:       input.identity.TraceID,
		TrackingID:    input.identity.TrackingID,
		Direction:     DirectionOutbound,
		PayloadKind:   PayloadPrompt,
		Policy: PolicyEvidence{
			PolicyID:     input.policyID,
			SkillID:      input.decision.SkillID,
			Decision:     interceptDecisionDeny,
			Reason:       input.reason,
			EvidenceID:   denyEvidenceID(input),
			PrincipleIDs: append([]string(nil), input.decision.PrincipleIDs...),
		},
		DLPFacts: input.dlpFacts,
	}
}

// denyEvidenceID returns the evidence ID correlating a deny event to its
// request. A policy denial supplies decision.EvidenceID; a fail-closed
// eval-error denial has a zero decision, so the originating event identity is
// used instead, keeping every denial joinable in SARIF and code-intel.
func denyEvidenceID(input denyOutboundInput) string {
	if input.decision.EvidenceID != "" {
		return input.decision.EvidenceID
	}

	return input.identity.ID
}

// denyEventMetadata assembles the deny event metadata. It copies any evaluator
// SARIF keys, then unconditionally rebuilds the stable proxy_* correlation keys
// from the request identity so a fail-closed eval-error denial (whose decision
// is the zero value and carries no metadata) is just as joinable as a policy
// denial. The host, decision, and no-body-retained flags are always recorded.
func denyEventMetadata(input denyOutboundInput) map[string]string {
	metadata := map[string]string{}
	maps.Copy(metadata, input.decision.Metadata)

	maps.Copy(metadata, proxyCorrelationMetadata(input.identity))

	metadata[metaIntercepted] = metaValueTrue
	metadata[metaHost] = input.host
	metadata[metaDecision] = interceptDecisionDeny
	metadata[metaPayloadBodyRetained] = metaValueFalse

	return metadata
}

// proxyCorrelationMetadata builds the stable proxy_* SARIF correlation keys from
// the request identity. These mirror the keys the policy evaluator attaches on a
// match, but are derived here so they are present on every denial — including the
// fail-closed eval-error path where no decision metadata exists. Empty
// correlation fields are omitted so the metadata stays sparse.
func proxyCorrelationMetadata(identity EventIdentity) map[string]string {
	metadata := map[string]string{
		metaProxyEventID:     identity.ID,
		metaProxySessionID:   identity.SessionID,
		metaProxyEventKind:   string(EventProviderCall),
		metaProxyProvider:    identity.Provider,
		metaProxyDirection:   string(DirectionOutbound),
		metaProxyPayloadKind: string(PayloadPrompt),
	}

	if identity.TraceID != "" {
		metadata[metaProxyTraceID] = identity.TraceID
	}

	if identity.TrackingID != "" {
		metadata[metaProxyTrackingID] = identity.TrackingID
	}

	return metadata
}
