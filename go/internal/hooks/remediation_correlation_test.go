// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks

import (
	"testing"

	"blackcat.ca/coding-ethos/go/internal/agentmsg"
	"blackcat.ca/coding-ethos/go/internal/evidence"
)

func TestRemediationCorrelationRecordsRepeatedAttemptedAndFixedEvents(t *testing.T) {
	record := lifecycleSessionRecord{}
	remediations := []agentmsg.Remediation{{
		ID:       "remediation-1",
		PolicyID: "policy.test",
		SkillID:  "skill-test",
	}}
	findings := []evidence.Finding{{ID: "finding-1"}}
	blocked := Result{Status: statusBlocked}
	preTool := Event{HookEventName: eventPreToolUse, ToolName: toolBash}

	first := applyRemediationCorrelation(
		preTool,
		blocked,
		"trace-suggested",
		remediations,
		findings,
		&record,
	)
	if len(first) != 0 || len(record.PendingRemediations) != 1 {
		t.Fatalf("initial remediation correlation = %#v state=%#v", first, record)
	}

	repeated := applyRemediationCorrelation(
		preTool,
		blocked,
		"trace-repeated",
		remediations,
		findings,
		&record,
	)
	assertRemediationCorrelationEvent(
		t,
		repeated,
		"repeated",
		"trace-repeated",
		"trace-suggested",
	)

	attempted := applyRemediationCorrelation(
		preTool,
		Result{Status: statusAllowed},
		"trace-attempted",
		nil,
		nil,
		&record,
	)
	assertRemediationCorrelationEvent(
		t,
		attempted,
		"attempted",
		"trace-attempted",
		"trace-repeated",
	)

	fixed := applyRemediationCorrelation(
		Event{
			HookEventName: eventPostToolUse,
			ToolName:      toolBash,
			ToolResponse: map[string]any{
				"return_code": 0,
			},
		},
		Result{Status: statusAllowed},
		"trace-fixed",
		nil,
		nil,
		&record,
	)
	assertRemediationCorrelationEvent(
		t,
		fixed,
		"fixed",
		"trace-fixed",
		"trace-repeated",
	)

	if len(record.PendingRemediations) != 0 ||
		len(record.AttemptedRemediations) != 0 {
		t.Fatalf("completed remediation remained pending: %#v", record)
	}
}

func TestRemediationCorrelationDoesNotCrossSessions(t *testing.T) {
	t.Setenv(lifecycleStateRootEnv, t.TempDir())

	remediations := []agentmsg.Remediation{{
		ID:       "remediation-session",
		PolicyID: "policy.session",
	}}
	findings := []evidence.Finding{{ID: "finding-session"}}

	correlateRemediationEvents(
		Event{
			HookEventName: eventPreToolUse,
			SessionID:     "source-session",
			ToolName:      toolBash,
		},
		Result{Status: statusBlocked},
		"trace-source-session",
		remediations,
		findings,
	)

	differentSession := correlateRemediationEvents(
		Event{
			HookEventName: eventPreToolUse,
			SessionID:     "different-session",
			ToolName:      toolBash,
		},
		Result{Status: statusAllowed},
		"trace-different-session",
		nil,
		nil,
	)
	if len(differentSession) != 0 {
		t.Fatalf("different session consumed remediation: %#v", differentSession)
	}

	sourceSession := correlateRemediationEvents(
		Event{
			HookEventName: eventPreToolUse,
			SessionID:     "source-session",
			ToolName:      toolBash,
		},
		Result{Status: statusAllowed},
		"trace-source-attempt",
		nil,
		nil,
	)
	assertRemediationCorrelationEvent(
		t,
		sourceSession,
		"attempted",
		"trace-source-attempt",
		"trace-source-session",
	)
}

func TestRemediationCorrelationRecordsAbandonedEventAtSessionEnd(t *testing.T) {
	record := lifecycleSessionRecord{}
	remediations := []agentmsg.Remediation{{
		ID:       "remediation-abandoned",
		PolicyID: "policy.abandoned",
		SkillID:  "skill-abandoned",
		File:     "pkg/app.go",
	}}
	findings := []evidence.Finding{{ID: "finding-abandoned"}}

	applyRemediationCorrelation(
		Event{
			HookEventName: eventPreToolUse,
			ProviderHint:  providerCodex,
			ToolName:      toolBash,
		},
		Result{Status: statusBlocked},
		"trace-abandoned-source",
		remediations,
		findings,
		&record,
	)

	abandoned := applyRemediationCorrelation(
		Event{HookEventName: eventSessionEnd},
		Result{Status: statusAllowed},
		"trace-session-end",
		nil,
		nil,
		&record,
	)
	assertRemediationCorrelationEvent(
		t,
		abandoned,
		"abandoned",
		"trace-session-end",
		"trace-abandoned-source",
	)

	if abandoned[0].Provider != providerCodex ||
		abandoned[0].Tool != toolBash ||
		abandoned[0].File != "pkg/app.go" {
		t.Fatalf("abandoned remediation context = %#v", abandoned[0])
	}
}

func assertRemediationCorrelationEvent(
	t *testing.T,
	events []evidence.RemediationEvent,
	wantEvent string,
	wantTraceID string,
	wantSourceTraceID string,
) {
	t.Helper()

	if len(events) != 1 {
		t.Fatalf("remediation events = %#v, want one", events)
	}

	event := events[0]
	if event.Event != wantEvent || event.TraceID != wantTraceID ||
		event.SourceTraceID != wantSourceTraceID ||
		event.RemediationID == "" || event.FindingID == "" || event.ID == "" {
		t.Fatalf(
			"remediation event = %#v, want event=%q trace=%q source=%q with identities",
			event,
			wantEvent,
			wantTraceID,
			wantSourceTraceID,
		)
	}
}
