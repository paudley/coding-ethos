// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks

import (
	"strings"

	"go.uber.org/zap"

	"blackcat.ca/coding-ethos/go/internal/agentmsg"
	"blackcat.ca/coding-ethos/go/internal/debuglog"
	"blackcat.ca/coding-ethos/go/internal/evidence"
)

const maxPendingRemediationReferences = 32

type remediationReference struct {
	RemediationID string `json:"remediation_id"`
	FindingID     string `json:"finding_id,omitempty"`
	SourceTraceID string `json:"source_trace_id"`
	PolicyID      string `json:"policy_id,omitempty"`
	SkillID       string `json:"skill_id,omitempty"`
	File          string `json:"file,omitempty"`
	Path          string `json:"path,omitempty"`
	Provider      string `json:"provider,omitempty"`
	Tool          string `json:"tool,omitempty"`
}

func correlateRemediationEvents(
	event Event,
	result Result,
	traceID string,
	remediations []agentmsg.Remediation,
	findings []evidence.Finding,
) []evidence.RemediationEvent {
	if strings.TrimSpace(event.SessionID) == "" {
		return nil
	}

	if len(remediations) == 0 &&
		event.HookEventName != eventPreToolUse &&
		event.HookEventName != eventPostToolUse &&
		event.HookEventName != eventSessionEnd {
		return nil
	}

	correlated := []evidence.RemediationEvent{}

	err := withLifecycleSessionRecord(event, func(record *lifecycleSessionRecord) {
		correlated = applyRemediationCorrelation(
			event,
			result,
			traceID,
			remediations,
			findings,
			record,
		)
	})
	if err != nil {
		debuglog.Debug(
			"hooks.remediation_correlation.warn",
			zap.String("event", event.HookEventName),
			zap.String("session_id", event.SessionID),
			zap.Error(err),
		)

		return nil
	}

	return correlated
}

func applyRemediationCorrelation(
	event Event,
	result Result,
	traceID string,
	remediations []agentmsg.Remediation,
	findings []evidence.Finding,
	record *lifecycleSessionRecord,
) []evidence.RemediationEvent {
	if len(remediations) > 0 {
		return correlateRepeatedRemediations(
			event,
			traceID,
			remediations,
			findings,
			record,
		)
	}

	if result.Blocked() {
		return nil
	}

	switch event.HookEventName {
	case eventPreToolUse:
		return correlateRemediationAttempt(event, traceID, record)
	case eventPostToolUse:
		return correlateRemediationResult(event, traceID, record)
	case eventSessionEnd:
		return abandonPendingRemediations(traceID, record)
	default:
		return nil
	}
}

func correlateRepeatedRemediations(
	event Event,
	traceID string,
	remediations []agentmsg.Remediation,
	findings []evidence.Finding,
	record *lifecycleSessionRecord,
) []evidence.RemediationEvent {
	current := remediationReferences(event, traceID, remediations, findings)
	correlated := []evidence.RemediationEvent{}
	remaining := make([]remediationReference, 0, len(record.PendingRemediations))

	for _, pending := range record.PendingRemediations {
		if remediationRepeated(pending, current, event.ToolName) {
			correlated = append(
				correlated,
				remediationOutcomeEvent(pending, traceID, "repeated"),
			)

			continue
		}

		remaining = append(remaining, pending)
	}

	record.PendingRemediations = cappedRemediationReferences(
		append(remaining, current...),
	)
	record.AttemptedRemediations = withoutRemediationTool(
		record.AttemptedRemediations,
		event.ToolName,
	)

	return correlated
}

func correlateRemediationAttempt(
	event Event,
	traceID string,
	record *lifecycleSessionRecord,
) []evidence.RemediationEvent {
	matched, remaining := partitionRemediationsByTool(
		record.PendingRemediations,
		event.ToolName,
	)
	if len(matched) == 0 {
		return nil
	}

	record.PendingRemediations = remaining
	record.AttemptedRemediations = cappedRemediationReferences(
		append(record.AttemptedRemediations, matched...),
	)

	return remediationOutcomeEvents(matched, traceID, "attempted")
}

func correlateRemediationResult(
	event Event,
	traceID string,
	record *lifecycleSessionRecord,
) []evidence.RemediationEvent {
	matched, remaining := partitionRemediationsByTool(
		record.AttemptedRemediations,
		event.ToolName,
	)
	if len(matched) == 0 {
		return nil
	}

	record.AttemptedRemediations = remaining
	outcome := "fixed"

	if event.ReturnCode() != 0 || responseStatusFailed(event.ToolResponse) {
		outcome = "repeated"
	}

	return remediationOutcomeEvents(matched, traceID, outcome)
}

func abandonPendingRemediations(
	traceID string,
	record *lifecycleSessionRecord,
) []evidence.RemediationEvent {
	pending := append(
		append([]remediationReference(nil), record.PendingRemediations...),
		record.AttemptedRemediations...,
	)
	record.PendingRemediations = nil
	record.AttemptedRemediations = nil

	return remediationOutcomeEvents(pending, traceID, "abandoned")
}

func remediationReferences(
	event Event,
	traceID string,
	remediations []agentmsg.Remediation,
	findings []evidence.Finding,
) []remediationReference {
	references := make([]remediationReference, 0, len(remediations))
	for index, remediation := range remediations {
		findingID := ""
		if index < len(findings) {
			findingID = findings[index].ID
		}

		references = append(references, remediationReference{
			RemediationID: remediation.ID,
			FindingID:     findingID,
			SourceTraceID: traceID,
			PolicyID:      remediation.PolicyID,
			SkillID:       remediation.SkillID,
			File:          remediation.File,
			Path:          remediation.Path,
			Provider:      event.Provider(),
			Tool:          event.ToolName,
		})
	}

	return references
}

func remediationRepeated(
	pending remediationReference,
	current []remediationReference,
	tool string,
) bool {
	if !sameRemediationTool(pending.Tool, tool) {
		return false
	}

	for _, candidate := range current {
		if pending.RemediationID == candidate.RemediationID ||
			(pending.PolicyID != "" && pending.PolicyID == candidate.PolicyID) {
			return true
		}
	}

	return false
}

func partitionRemediationsByTool(
	references []remediationReference,
	tool string,
) ([]remediationReference, []remediationReference) {
	matched := []remediationReference{}
	remaining := []remediationReference{}

	for _, reference := range references {
		if sameRemediationTool(reference.Tool, tool) {
			matched = append(matched, reference)

			continue
		}

		remaining = append(remaining, reference)
	}

	return matched, remaining
}

func withoutRemediationTool(
	references []remediationReference,
	tool string,
) []remediationReference {
	_, remaining := partitionRemediationsByTool(references, tool)

	return remaining
}

func sameRemediationTool(left, right string) bool {
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}

func remediationOutcomeEvents(
	references []remediationReference,
	traceID string,
	outcome string,
) []evidence.RemediationEvent {
	events := make([]evidence.RemediationEvent, 0, len(references))
	for _, reference := range references {
		events = append(events, remediationOutcomeEvent(reference, traceID, outcome))
	}

	return events
}

func remediationOutcomeEvent(
	reference remediationReference,
	traceID string,
	outcome string,
) evidence.RemediationEvent {
	result := evidence.RemediationEvent{
		RemediationID: reference.RemediationID,
		FindingID:     reference.FindingID,
		SourceTraceID: reference.SourceTraceID,
		TraceID:       traceID,
		Event:         outcome,
		PolicyID:      reference.PolicyID,
		SkillID:       reference.SkillID,
		File:          reference.File,
		Path:          reference.Path,
		Provider:      reference.Provider,
		Tool:          reference.Tool,
		SearchText: strings.TrimSpace(strings.Join([]string{
			reference.PolicyID,
			reference.SkillID,
			outcome,
		}, " ")),
		SchemaVersion: evidence.SchemaVersion,
	}
	result.ID = "remediation-event-" + sha256Hex(strings.Join([]string{
		reference.RemediationID,
		reference.FindingID,
		reference.SourceTraceID,
		traceID,
		outcome,
	}, "\x00"))[:16]

	return result
}

func cappedRemediationReferences(
	references []remediationReference,
) []remediationReference {
	if len(references) <= maxPendingRemediationReferences {
		return references
	}

	return references[len(references)-maxPendingRemediationReferences:]
}
