// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel_test

import (
	"context"
	"strings"
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/codeintel"
)

func TestStoreReportsSkillHealthTrendsAndUnknownSkillIDs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)

	recordSkillHealthOutcome(
		t,
		ctx,
		store,
		"lint-remediation",
		"repeated",
		2,
		"2026-01-20T00:00:00Z",
	)
	recordSkillHealthOutcome(
		t,
		ctx,
		store,
		"lint-remediation",
		"repeated",
		3,
		"2026-01-25T00:00:00Z",
	)
	recordSkillHealthOutcome(
		t,
		ctx,
		store,
		"improving-skill",
		"repeated",
		1,
		"2026-01-10T00:00:00Z",
	)
	recordSkillHealthOutcome(
		t,
		ctx,
		store,
		"improving-skill",
		"fixed",
		1,
		"2026-01-31T00:00:00Z",
	)
	recordSkillHealthOutcome(
		t,
		ctx,
		store,
		"stale-skill",
		"fixed",
		1,
		"2025-12-01T00:00:00Z",
	)
	recordSkillHealthOutcome(
		t,
		ctx,
		store,
		"unknown-skill",
		"fixed",
		1,
		"2026-01-29T00:00:00Z",
	)

	report, err := store.SkillHealth(ctx, SkillHealthQuery{
		NowUTC: "2026-02-01T00:00:00Z",
		KnownSkills: []SkillProvenance{
			{
				ID:         "lint-remediation",
				Title:      "Lint remediation",
				Source:     "config.yaml",
				SourcePath: "skills.lint-remediation",
				Generated:  true,
			},
			{ID: "improving-skill", Source: "config.yaml", Generated: true},
			{ID: "stale-skill", Source: "config.yaml", Generated: true},
			{ID: "unused-skill", Source: "config.yaml", Generated: true},
		},
	})
	if err != nil {
		t.Fatalf("skill health: %v", err)
	}

	records := skillHealthByID(report)
	assertSkillHealthStatus(t, records, "lint-remediation", "frequently_failing")
	assertSkillHealthStatus(t, records, "unknown-skill", "unknown_skill")
	assertSkillHealthStatus(t, records, "improving-skill", "improving")
	assertSkillHealthStatus(t, records, "stale-skill", "stale")
	assertSkillHealthStatus(t, records, "unused-skill", "unused")

	failing := records["lint-remediation"]
	if failing.Window30.Repeated != 2 || failing.RetryCount != 3 ||
		failing.SourcePath != "skills.lint-remediation" || !failing.Known {
		t.Fatalf("failing skill health = %#v", failing)
	}

	if report.Summary.FrequentlyFailing != 1 || report.Summary.Unknown != 1 ||
		report.Summary.Improving != 1 || report.Summary.Stale != 1 ||
		report.Summary.Unused != 1 {
		t.Fatalf("summary = %#v", report.Summary)
	}
}

func TestStoreSkillHealthSummaryUsesFullRecordSetBeforeLimit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)

	report, err := store.SkillHealth(ctx, SkillHealthQuery{
		NowUTC: "2026-02-01T00:00:00Z",
		Limit:  1,
		KnownSkills: []SkillProvenance{
			{ID: "unused-a", Source: "config.yaml", Generated: true},
			{ID: "unused-b", Source: "config.yaml", Generated: true},
			{ID: "unused-c", Source: "config.yaml", Generated: true},
		},
	})
	if err != nil {
		t.Fatalf("skill health: %v", err)
	}

	if len(report.Skills) != 1 {
		t.Fatalf("skills = %#v, want limited page", report.Skills)
	}

	if report.Summary.Known != 3 || report.Summary.Unused != 3 {
		t.Fatalf("summary = %#v, want full unpaginated counts", report.Summary)
	}
}

func TestStoreRecordsSkillObservationAsUnknownOutcome(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)

	err := store.RecordSkillObservation(ctx, SkillObservation{
		SkillID:       "safe-git-workflow",
		PolicyID:      "git.hook_bypass",
		Path:          "Makefile",
		Provider:      "mcp",
		Tool:          "skill_lookup",
		RecordedAtUTC: "2026-01-02T03:04:05Z",
		Trigger:       "lookup",
	})
	if err != nil {
		t.Fatalf("record skill observation: %v", err)
	}

	outcomes, err := store.RemediationOutcomes(ctx, RemediationOutcomeQuery{
		SkillID: "safe-git-workflow",
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("query remediation outcomes: %v", err)
	}

	if len(outcomes) != 1 || outcomes[0].Outcome != "unknown" ||
		outcomes[0].Tool != "skill_lookup" ||
		!strings.Contains(outcomes[0].SearchText, "safe-git-workflow") {
		t.Fatalf("outcomes = %#v", outcomes)
	}
}

func recordSkillHealthOutcome(
	t *testing.T,
	ctx context.Context,
	store *Store,
	skillID string,
	outcome string,
	attempt int,
	recordedAtUTC string,
) {
	t.Helper()

	err := store.RecordRemediationOutcome(ctx, RemediationOutcome{
		ID:             skillID + ":" + outcome + ":" + recordedAtUTC,
		RemediationID:  "rem-" + skillID,
		FindingID:      "finding-" + skillID,
		PolicyID:       "policy." + skillID,
		SkillID:        skillID,
		Path:           "pkg/app.py",
		Provider:       "codex",
		Tool:           "Edit",
		Outcome:        outcome,
		AttemptOrdinal: attempt,
		RecordedAtUTC:  recordedAtUTC,
	})
	if err != nil {
		t.Fatalf("record outcome %s/%s: %v", skillID, outcome, err)
	}
}

func skillHealthByID(report SkillHealthReport) map[string]SkillHealthRecord {
	result := map[string]SkillHealthRecord{}
	for _, record := range report.Skills {
		result[record.SkillID] = record
	}

	return result
}

func assertSkillHealthStatus(
	t *testing.T,
	records map[string]SkillHealthRecord,
	skillID string,
	status string,
) {
	t.Helper()

	record, found := records[skillID]
	if !found {
		t.Fatalf("missing skill %q in %#v", skillID, records)
	}

	if record.Status != status {
		t.Fatalf("skill %q status = %q, want %q: %#v", skillID, record.Status, status, record)
	}
}
