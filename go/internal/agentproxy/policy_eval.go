// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agentproxy

import "context"

// ProxyPolicyEvaluator decides whether a proxy event is allowed from body-free
// structural facts. It mirrors the injected-interface pattern used by
// EventRecorder and LeafIssuer: the contract lives in agentproxy so the package
// stays a leaf, while the concrete CEL-backed implementation lives elsewhere.
type ProxyPolicyEvaluator interface {
	// EvaluateOutbound returns a decision for the structural facts in input.
	EvaluateOutbound(ctx context.Context, input ProxyDecisionInput) (ProxyDecision, error)
	// EvaluateInbound returns a decision for the structural facts in input.
	EvaluateInbound(ctx context.Context, input ProxyDecisionInput) (ProxyDecision, error)
}

// ProxyDecisionInput carries only the structural, body-free facts a policy may
// inspect for a proxy event. It never includes the raw request or response
// body; DLP findings and tool calls are summarized as detector-labeled facts,
// tool names, and argument hashes, and payload size is given as a measurement
// rather than content.
type ProxyDecisionInput struct {
	Metadata    map[string]string
	TargetPath  string
	Direction   EventDirection
	EventID     string
	SessionID   string
	Provider    string
	Model       string
	PayloadKind PayloadKind
	InputHash   string
	Tool        string
	OutputHash  string
	Kind        EventKind
	DLPFacts    []DLPFact
	ToolCalls   []ToolCall
	TokenUsage  TokenUsage
	Payload     PayloadMeasurement
}

// ProxyDecision is the body-free verdict a policy returns for an outbound event.
// Metadata holds the proxy_* SARIF keys the caller copies onto the recorded
// event, and the remaining fields describe the matched policy and remediation.
type ProxyDecision struct {
	Metadata     map[string]string
	PolicyID     string
	Reason       string
	Severity     string
	Message      string
	Suggestion   string
	EvidenceID   string
	SkillID      string
	PrincipleIDs []string
	Allowed      bool
}
