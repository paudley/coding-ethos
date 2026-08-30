// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/evidence"
)

func TestHookTraceMaterializesTerminalAutomaticRemediationOutcomes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)
	ingester := codeintel.NewTraceIngester(store)

	for _, traceID := range []string{"trace-source-fixed", "trace-source-abandoned"} {
		err := ingester.IngestHookTrace(
			ctx,
			hookTracePayloadWithIDs(
				t,
				traceID,
				"tracking-"+traceID,
				"2026-01-01T00:01:00Z",
			),
		)
		if err != nil {
			t.Fatalf("ingest source trace %q: %v", traceID, err)
		}
	}

	followup := hookTraceWithRemediationEvents(t, []evidence.RemediationEvent{
		{
			ID:            "event-fixed",
			RemediationID: "remediation-fixed",
			FindingID:     "finding-fixed",
			SourceTraceID: "trace-source-fixed",
			TraceID:       "trace-followup",
			Event:         "fixed",
			PolicyID:      "policy.fixed",
			SkillID:       "skill-fixed",
			Path:          "pkg/fixed.go",
			Provider:      "claude",
			Tool:          "Edit",
			SchemaVersion: evidence.SchemaVersion,
		},
		{
			ID:            "event-abandoned",
			RemediationID: "remediation-abandoned",
			FindingID:     "finding-abandoned",
			SourceTraceID: "trace-source-abandoned",
			TraceID:       "trace-followup",
			Event:         "abandoned",
			PolicyID:      "policy.abandoned",
			SkillID:       "skill-abandoned",
			File:          "pkg/abandoned.go",
			SchemaVersion: evidence.SchemaVersion,
		},
		{
			ID:            "event-attempted",
			RemediationID: "remediation-attempted",
			SourceTraceID: "trace-source-fixed",
			TraceID:       "trace-followup",
			Event:         "attempted",
			SchemaVersion: evidence.SchemaVersion,
		},
		{
			ID:            "event-repeated",
			RemediationID: "remediation-repeated",
			SourceTraceID: "trace-source-fixed",
			TraceID:       "trace-followup",
			Event:         "repeated",
			SchemaVersion: evidence.SchemaVersion,
		},
	})

	err := ingester.IngestHookTrace(ctx, followup)
	if err != nil {
		t.Fatalf("ingest follow-up trace: %v", err)
	}
	assertAutomaticRemediationOutcomes(t, ctx, store)

	err = store.RecordRemediationOutcome(ctx, codeintel.RemediationOutcome{
		ID:              "explicit-outcome",
		SourceTraceID:   "trace-source-fixed",
		FollowupTraceID: "trace-followup",
		PolicyID:        "policy.explicit",
		Outcome:         "repeated",
	})
	if err != nil {
		t.Fatalf("record explicit outcome: %v", err)
	}

	err = ingester.IngestHookTrace(ctx, followup)
	if err != nil {
		t.Fatalf("reingest follow-up trace: %v", err)
	}
	assertAutomaticRemediationOutcomes(t, ctx, store)

	err = ingester.IngestHookTrace(ctx, hookTraceWithRemediationEvents(t, nil))
	if err != nil {
		t.Fatalf("reingest follow-up without outcomes: %v", err)
	}

	outcomes, err := store.RemediationOutcomes(
		ctx,
		codeintel.RemediationOutcomeQuery{Limit: 10},
	)
	if err != nil {
		t.Fatalf("query outcomes after cleanup: %v", err)
	}
	if len(outcomes) != 1 || outcomes[0].ID != "explicit-outcome" {
		t.Fatalf("outcomes after cleanup = %#v, want explicit outcome only", outcomes)
	}
}

func hookTraceWithRemediationEvents(
	t *testing.T,
	events []evidence.RemediationEvent,
) []byte {
	t.Helper()

	payload := map[string]any{}
	err := json.Unmarshal(
		hookTracePayloadWithIDs(
			t,
			"trace-followup",
			"tracking-trace-followup",
			"2026-01-01T00:02:00Z",
		),
		&payload,
	)
	if err != nil {
		t.Fatalf("decode follow-up trace fixture: %v", err)
	}

	payload["status"] = "allowed"
	payload["remediation_events"] = events

	return mustJSON(t, payload)
}

func assertAutomaticRemediationOutcomes(
	t *testing.T,
	ctx context.Context,
	store *codeintel.Store,
) {
	t.Helper()

	outcomes, err := store.RemediationOutcomes(
		ctx,
		codeintel.RemediationOutcomeQuery{Limit: 10},
	)
	if err != nil {
		t.Fatalf("query automatic outcomes: %v", err)
	}

	automatic := map[string]codeintel.RemediationOutcome{}
	for _, outcome := range outcomes {
		if strings.HasPrefix(outcome.ID, "automatic-remediation-outcome:") {
			automatic[outcome.Outcome] = outcome
		}
	}
	if len(automatic) != 2 {
		t.Fatalf("automatic outcomes = %#v, want fixed and abandoned", automatic)
	}

	fixed := automatic["fixed"]
	if fixed.SourceTraceID != "trace-source-fixed" ||
		fixed.FollowupTraceID != "trace-followup" ||
		fixed.Provider != "claude" ||
		fixed.Tool != "Edit" ||
		fixed.Path != "pkg/fixed.go" {
		t.Fatalf("fixed outcome = %#v", fixed)
	}

	abandoned := automatic["abandoned"]
	if abandoned.SourceTraceID != "trace-source-abandoned" ||
		abandoned.FollowupTraceID != "trace-followup" ||
		abandoned.Provider != "codex" ||
		abandoned.Tool != "Bash" ||
		abandoned.File != "pkg/abandoned.go" {
		t.Fatalf("abandoned outcome = %#v", abandoned)
	}
}
