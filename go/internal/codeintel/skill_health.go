// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	defaultSkillHealthLimit             = 50
	defaultSkillHealthStaleDays         = 30
	skillHealthStatusFrequentlyFailing  = "frequently_failing"
	skillHealthStatusHealthy            = "healthy"
	skillHealthStatusImproving          = "improving"
	skillHealthStatusStale              = "stale"
	skillHealthStatusUnknownSkill       = "unknown_skill"
	skillHealthStatusUnused             = "unused"
	skillHealthTOONBaseLines            = 12
	skillHealthTrendDegrading           = "degrading"
	skillHealthTrendFailing             = "failing"
	skillHealthTrendImproving           = "improving"
	skillHealthTrendNone                = "none"
	skillHealthTrendStableSuccess       = "stable_success"
	skillHealthTrendUnknown             = "unknown"
	skillHealthWeekDays                 = 7
	skillHealthMonthDays                = 30
	skillHealthWeightFrequentlyFailing  = 0
	skillHealthWeightUnknownSkill       = 1
	skillHealthWeightStale              = 2
	skillHealthWeightUnused             = 3
	skillHealthWeightImproving          = 4
	skillHealthWeightHealthy            = 5
	skillHealthWeightUnclassifiedStatus = 6
)

type SkillObservation struct {
	SkillID       string `json:"skill_id"`
	PolicyID      string `json:"policy_id,omitempty"`
	Path          string `json:"path,omitempty"`
	Provider      string `json:"provider,omitempty"`
	Tool          string `json:"tool,omitempty"`
	Surface       string `json:"surface,omitempty"`
	Outcome       string `json:"outcome,omitempty"`
	RecordedAtUTC string `json:"recorded_at_utc,omitempty"`
	Trigger       string `json:"trigger,omitempty"`
}

type SkillProvenance struct {
	ID         string `json:"id"`
	Title      string `json:"title,omitempty"`
	Source     string `json:"source,omitempty"`
	SourcePath string `json:"source_path,omitempty"`
	Generated  bool   `json:"generated"`
}

type SkillHealthQuery struct {
	SkillID     string
	NowUTC      string
	KnownSkills []SkillProvenance
	Limit       int
	StaleDays   int
}

type SkillHealthReport struct {
	Kind            string                `json:"kind"`
	GeneratedAtUTC  string                `json:"generated_at_utc"`
	PromotionPolicy string                `json:"promotion_policy"`
	Skills          []SkillHealthRecord   `json:"skills"`
	Windows         []SkillHealthWindowID `json:"windows"`
	Summary         SkillHealthSummary    `json:"summary"`
}

type SkillHealthSummary struct {
	Known             int `json:"known"`
	Observed          int `json:"observed"`
	Unknown           int `json:"unknown"`
	Unused            int `json:"unused"`
	FrequentlyFailing int `json:"frequently_failing"`
	Improving         int `json:"improving"`
	Stale             int `json:"stale"`
}

type SkillHealthWindowID struct {
	Name string `json:"name"`
	Days int    `json:"days"`
}

type SkillHealthRecord struct {
	Trend            string            `json:"trend"`
	LastUsedUTC      string            `json:"last_used_utc,omitempty"`
	Source           string            `json:"source,omitempty"`
	SourcePath       string            `json:"source_path,omitempty"`
	Provenance       string            `json:"provenance"`
	Status           string            `json:"status"`
	Title            string            `json:"title,omitempty"`
	SkillID          string            `json:"skill_id"`
	RecurrenceWindow string            `json:"recurrence_window,omitempty"`
	Paths            []string          `json:"paths,omitempty"`
	Policies         []string          `json:"policies,omitempty"`
	Providers        []string          `json:"providers,omitempty"`
	Surfaces         []string          `json:"surfaces,omitempty"`
	Window7          SkillHealthWindow `json:"window_7d"`
	Window30         SkillHealthWindow `json:"window_30d"`
	Fixed            int               `json:"fixed"`
	Superseded       int               `json:"superseded"`
	UnknownOutcome   int               `json:"unknown_outcome"`
	RetryCount       int               `json:"retry_count"`
	Abandoned        int               `json:"abandoned"`
	Attempted        int               `json:"attempted"`
	Repeated         int               `json:"repeated"`
	Total            int               `json:"total"`
	UnknownSkill     bool              `json:"unknown_skill"`
	Generated        bool              `json:"generated"`
	Known            bool              `json:"known"`
}

type SkillHealthWindow struct {
	Days           int `json:"days"`
	Total          int `json:"total"`
	Fixed          int `json:"fixed"`
	Repeated       int `json:"repeated"`
	Attempted      int `json:"attempted"`
	Abandoned      int `json:"abandoned"`
	Superseded     int `json:"superseded"`
	UnknownOutcome int `json:"unknown_outcome"`
	RetryCount     int `json:"retry_count"`
}

func (store *Store) RecordSkillObservation(
	ctx context.Context,
	observation SkillObservation,
) error {
	observation = normalizeSkillObservation(observation)
	if observation.SkillID == "" {
		return fmt.Errorf("%w: skill_id", errRequiredSkillObservationField)
	}

	recordedAt := firstNonEmpty(
		observation.RecordedAtUTC,
		time.Now().UTC().Format(time.RFC3339),
	)

	return store.RecordRemediationOutcome(ctx, RemediationOutcome{
		ID: stableID(
			"skill-observation",
			observation.SkillID,
			observation.PolicyID,
			observation.Path,
			observation.Provider,
			observation.Tool,
			recordedAt,
			observation.Trigger,
		),
		RemediationID: "skill-observation:" + observation.SkillID,
		PolicyID:      observation.PolicyID,
		SkillID:       observation.SkillID,
		Path:          observation.Path,
		Provider:      observation.Provider,
		Tool:          observation.Tool,
		Outcome:       observation.Outcome,
		RecordedAtUTC: recordedAt,
		SearchText: strings.Join(compactStrings([]string{
			observation.SkillID,
			observation.PolicyID,
			observation.Path,
			observation.Provider,
			observation.Tool,
			observation.Surface,
			observation.Trigger,
			observation.Outcome,
		}), " "),
	})
}

func (store *Store) SkillHealth(
	ctx context.Context,
	query SkillHealthQuery,
) (SkillHealthReport, error) {
	query = normalizeSkillHealthQuery(query)

	outcomes, err := store.skillOutcomeRows(ctx, query.SkillID)
	if err != nil {
		return SkillHealthReport{}, err
	}

	report := SkillHealthReport{
		Kind:            "code_intel.skill_health.v1",
		GeneratedAtUTC:  query.NowUTC,
		PromotionPolicy: "measurement_only_explicit_future_policy_required",
		Windows: []SkillHealthWindowID{
			{Name: "7d", Days: skillHealthWeekDays},
			{Name: "30d", Days: skillHealthMonthDays},
		},
	}

	now, err := parseSkillHealthTime(query.NowUTC)
	if err != nil {
		return SkillHealthReport{}, fmt.Errorf("parse skill health query time: %w", err)
	}

	records := buildSkillHealthRecords(query, outcomes, now)
	sortSkillHealthRecords(records)

	report.Summary = summarizeSkillHealth(query, records)

	if query.Limit > 0 && len(records) > query.Limit {
		records = records[:query.Limit]
	}

	report.Skills = records

	return report, nil
}

func normalizeSkillObservation(observation SkillObservation) SkillObservation {
	observation.SkillID = strings.TrimSpace(observation.SkillID)
	observation.PolicyID = strings.TrimSpace(observation.PolicyID)
	observation.Path = strings.TrimSpace(observation.Path)
	observation.Provider = firstNonEmpty(observation.Provider, "mcp")
	observation.Surface = strings.TrimSpace(observation.Surface)
	observation.Tool = firstNonEmpty(observation.Tool, observation.Surface)
	observation.Outcome = firstNonEmpty(observation.Outcome, "unknown")
	observation.RecordedAtUTC = strings.TrimSpace(observation.RecordedAtUTC)
	observation.Trigger = strings.TrimSpace(observation.Trigger)

	return observation
}

func normalizeSkillHealthQuery(query SkillHealthQuery) SkillHealthQuery {
	query.SkillID = strings.TrimSpace(query.SkillID)

	query.NowUTC = strings.TrimSpace(query.NowUTC)
	if query.NowUTC == "" {
		query.NowUTC = time.Now().UTC().Format(time.RFC3339)
	}

	if query.Limit <= 0 {
		query.Limit = defaultSkillHealthLimit
	}

	if query.StaleDays <= 0 {
		query.StaleDays = defaultSkillHealthStaleDays
	}

	for index := range query.KnownSkills {
		query.KnownSkills[index].ID = strings.TrimSpace(query.KnownSkills[index].ID)
		query.KnownSkills[index].Title = strings.TrimSpace(query.KnownSkills[index].Title)
		query.KnownSkills[index].Source = strings.TrimSpace(query.KnownSkills[index].Source)
		query.KnownSkills[index].SourcePath = strings.TrimSpace(
			query.KnownSkills[index].SourcePath,
		)
	}

	return query
}

func (store *Store) skillOutcomeRows(
	ctx context.Context,
	skillID string,
) ([]RemediationOutcome, error) {
	rows, err := store.database.QueryContext(
		ctx,
		`SELECT
			outcome_id, remediation_id, finding_id,
			COALESCE(source_trace_id, ''),
			COALESCE(followup_trace_id, ''),
			policy_id, skill_id, file, path, provider, tool, outcome,
			attempt_ordinal, recorded_at_utc, search_text
		FROM remediation_outcomes
		WHERE skill_id <> ''
			AND (? = '' OR skill_id = ?)
		ORDER BY skill_id, recorded_at_utc DESC, attempt_ordinal DESC`,
		skillID,
		skillID,
	)
	if err != nil {
		return nil, fmt.Errorf("query skill health outcomes: %w", err)
	}
	defer rows.Close()

	return scanRemediationOutcomes(rows)
}

func buildSkillHealthRecords(
	query SkillHealthQuery,
	outcomes []RemediationOutcome,
	now time.Time,
) []SkillHealthRecord {
	known := skillProvenanceMap(query.KnownSkills)
	observed := map[string]*SkillHealthRecord{}

	for _, outcome := range outcomes {
		record := skillHealthRecordFor(observed, known, outcome.SkillID)
		accumulateSkillOutcome(record, outcome, now)
	}

	for _, provenance := range query.KnownSkills {
		if provenance.ID == "" {
			continue
		}

		if query.SkillID != "" && query.SkillID != provenance.ID {
			continue
		}

		if _, found := observed[provenance.ID]; !found {
			observed[provenance.ID] = newKnownSkillHealthRecord(provenance)
		}
	}

	records := make([]SkillHealthRecord, 0, len(observed))
	for _, record := range observed {
		finalizeSkillHealthRecord(
			record,
			now,
			query.StaleDays,
			len(query.KnownSkills) > 0,
		)
		records = append(records, *record)
	}

	return records
}

func skillProvenanceMap(skills []SkillProvenance) map[string]SkillProvenance {
	known := make(map[string]SkillProvenance, len(skills))
	for _, skill := range skills {
		if strings.TrimSpace(skill.ID) == "" {
			continue
		}

		known[skill.ID] = skill
	}

	return known
}

func skillHealthRecordFor(
	records map[string]*SkillHealthRecord,
	known map[string]SkillProvenance,
	skillID string,
) *SkillHealthRecord {
	record, found := records[skillID]
	if found {
		return record
	}

	provenance, knownSkill := known[skillID]
	if knownSkill {
		record = newKnownSkillHealthRecord(provenance)
	} else {
		record = &SkillHealthRecord{
			SkillID:    skillID,
			Provenance: "observed",
			Status:     "observed",
			Trend:      skillHealthTrendUnknown,
			Window7:    SkillHealthWindow{Days: skillHealthWeekDays},
			Window30:   SkillHealthWindow{Days: skillHealthMonthDays},
		}
	}

	records[skillID] = record

	return record
}

func newKnownSkillHealthRecord(provenance SkillProvenance) *SkillHealthRecord {
	return &SkillHealthRecord{
		SkillID:    provenance.ID,
		Title:      provenance.Title,
		Source:     provenance.Source,
		SourcePath: provenance.SourcePath,
		Provenance: firstNonEmpty(provenance.Source, "generated"),
		Status:     skillHealthStatusUnused,
		Trend:      skillHealthTrendNone,
		Generated:  provenance.Generated,
		Known:      true,
		Window7:    SkillHealthWindow{Days: skillHealthWeekDays},
		Window30:   SkillHealthWindow{Days: skillHealthMonthDays},
	}
}

func accumulateSkillOutcome(
	record *SkillHealthRecord,
	outcome RemediationOutcome,
	now time.Time,
) {
	record.Total++
	record.RetryCount += retryCount(outcome.AttemptOrdinal)
	record.LastUsedUTC = maxUTC(record.LastUsedUTC, outcome.RecordedAtUTC)
	record.Surfaces = appendUniqueString(record.Surfaces, outcome.Tool)
	record.Policies = appendUniqueString(record.Policies, outcome.PolicyID)
	record.Providers = appendUniqueString(record.Providers, outcome.Provider)
	record.Paths = appendUniqueString(
		record.Paths,
		firstNonEmpty(outcome.File, outcome.Path),
	)
	accumulateOutcomeCounters(
		&record.Fixed,
		&record.Repeated,
		&record.Attempted,
		&record.Abandoned,
		&record.Superseded,
		&record.UnknownOutcome,
		outcome.Outcome,
	)

	if outcomeWithinWindow(outcome.RecordedAtUTC, now, skillHealthWeekDays) {
		accumulateSkillWindow(&record.Window7, outcome)
	}

	if outcomeWithinWindow(outcome.RecordedAtUTC, now, skillHealthMonthDays) {
		accumulateSkillWindow(&record.Window30, outcome)
	}
}

func accumulateSkillWindow(window *SkillHealthWindow, outcome RemediationOutcome) {
	window.Total++
	window.RetryCount += retryCount(outcome.AttemptOrdinal)
	accumulateOutcomeCounters(
		&window.Fixed,
		&window.Repeated,
		&window.Attempted,
		&window.Abandoned,
		&window.Superseded,
		&window.UnknownOutcome,
		outcome.Outcome,
	)
}

func accumulateOutcomeCounters(
	fixed *int,
	repeated *int,
	attempted *int,
	abandoned *int,
	superseded *int,
	unknown *int,
	outcome string,
) {
	switch strings.ToLower(strings.TrimSpace(outcome)) {
	case "fixed", "success", "succeeded":
		*fixed++
	case "repeated", SourceStatusFailed, "failure":
		*repeated++
	case "attempted":
		*attempted++
	case "abandoned":
		*abandoned++
	case "superseded":
		*superseded++
	default:
		*unknown++
	}
}

func finalizeSkillHealthRecord(
	record *SkillHealthRecord,
	now time.Time,
	staleDays int,
	knownCatalog bool,
) {
	record.Surfaces = sortedStrings(record.Surfaces)
	record.Policies = sortedStrings(record.Policies)
	record.Providers = sortedStrings(record.Providers)
	record.Paths = sortedStrings(record.Paths)
	record.RecurrenceWindow = recurrenceWindow(record)
	record.UnknownSkill = knownCatalog && !record.Known && record.Total > 0
	record.Trend = skillTrend(record)
	record.Status = skillStatus(record, now, staleDays)
}

func recurrenceWindow(record *SkillHealthRecord) string {
	if record.Total == 0 || record.Window30.Total == 0 {
		return ""
	}

	return "30d"
}

func skillTrend(record *SkillHealthRecord) string {
	switch {
	case record.Total == 0:
		return skillHealthTrendNone
	case record.Window7.Repeated > 0 &&
		record.Window7.Repeated >= record.Window7.Fixed:
		return skillHealthTrendFailing
	case record.Window7.Fixed > record.Window7.Repeated &&
		record.Window30.Repeated > record.Window7.Repeated:
		return skillHealthTrendImproving
	case record.Window30.Repeated > record.Window30.Fixed:
		return skillHealthTrendDegrading
	case record.Window30.Fixed > 0:
		return skillHealthTrendStableSuccess
	default:
		return skillHealthTrendUnknown
	}
}

func skillStatus(record *SkillHealthRecord, now time.Time, staleDays int) string {
	switch {
	case record.UnknownSkill:
		return skillHealthStatusUnknownSkill
	case record.Total == 0:
		return skillHealthStatusUnused
	case record.Window30.Repeated >= 2 &&
		record.Window30.Repeated >= record.Window30.Fixed:
		return skillHealthStatusFrequentlyFailing
	case record.Trend == skillHealthTrendImproving:
		return skillHealthStatusImproving
	case skillLastUsedBefore(record.LastUsedUTC, now, staleDays):
		return skillHealthStatusStale
	default:
		return skillHealthStatusHealthy
	}
}

func summarizeSkillHealth(
	query SkillHealthQuery,
	records []SkillHealthRecord,
) SkillHealthSummary {
	summary := SkillHealthSummary{Known: len(skillProvenanceMap(query.KnownSkills))}

	for _, record := range records {
		if record.Total > 0 {
			summary.Observed++
		}

		switch record.Status {
		case skillHealthStatusUnknownSkill:
			summary.Unknown++
		case skillHealthStatusUnused:
			summary.Unused++
		case skillHealthStatusFrequentlyFailing:
			summary.FrequentlyFailing++
		case skillHealthStatusImproving:
			summary.Improving++
		case skillHealthStatusStale:
			summary.Stale++
		}
	}

	return summary
}

func sortSkillHealthRecords(records []SkillHealthRecord) {
	slices.SortFunc(records, func(left, right SkillHealthRecord) int {
		leftWeight := skillHealthStatusWeight(left.Status)

		rightWeight := skillHealthStatusWeight(right.Status)
		if leftWeight != rightWeight {
			return leftWeight - rightWeight
		}

		if left.Total != right.Total {
			return right.Total - left.Total
		}

		return strings.Compare(left.SkillID, right.SkillID)
	})
}

func skillHealthStatusWeight(status string) int {
	switch status {
	case skillHealthStatusFrequentlyFailing:
		return skillHealthWeightFrequentlyFailing
	case skillHealthStatusUnknownSkill:
		return skillHealthWeightUnknownSkill
	case skillHealthStatusStale:
		return skillHealthWeightStale
	case skillHealthStatusUnused:
		return skillHealthWeightUnused
	case skillHealthStatusImproving:
		return skillHealthWeightImproving
	case skillHealthStatusHealthy:
		return skillHealthWeightHealthy
	default:
		return skillHealthWeightUnclassifiedStatus
	}
}

func retryCount(attemptOrdinal int) int {
	if attemptOrdinal <= 1 {
		return 0
	}

	return attemptOrdinal - 1
}

func outcomeWithinWindow(recordedAtUTC string, now time.Time, days int) bool {
	recordedAt, err := parseSkillHealthTime(recordedAtUTC)
	if err != nil {
		return false
	}

	cutoff := now.AddDate(0, 0, -days)

	return !recordedAt.Before(cutoff) && !recordedAt.After(now)
}

func skillLastUsedBefore(lastUsedUTC string, now time.Time, days int) bool {
	if strings.TrimSpace(lastUsedUTC) == "" {
		return false
	}

	lastUsed, err := parseSkillHealthTime(lastUsedUTC)
	if err != nil {
		return false
	}

	return lastUsed.Before(now.AddDate(0, 0, -days))
}

func parseSkillHealthTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, sql.ErrNoRows
	}

	parsed, err := time.Parse(time.RFC3339, value)
	if err == nil {
		return parsed, nil
	}

	parsed, fallbackErr := time.Parse("2006-01-02T15:04:05Z", value)
	if fallbackErr != nil {
		return time.Time{}, fmt.Errorf(
			"parse skill health time %q: %w",
			value,
			fallbackErr,
		)
	}

	return parsed, nil
}

func maxUTC(left, right string) string {
	left = strings.TrimSpace(left)

	right = strings.TrimSpace(right)
	if left == "" {
		return right
	}

	if right == "" {
		return left
	}

	if left >= right {
		return left
	}

	return right
}

func appendUniqueString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}

	if slices.Contains(values, value) {
		return values
	}

	return append(values, value)
}

func sortedStrings(values []string) []string {
	values = compactStrings(values)
	slices.Sort(values)

	return values
}

func FormatSkillHealthTOON(report SkillHealthReport) string {
	header := "  skills[skill_id,status,trend,total,7d_fixed,7d_repeated," +
		"30d_fixed,30d_repeated,last_used_utc,source_path]:"
	lines := make([]string, 0, skillHealthTOONBaseLines+len(report.Skills))
	lines = append(lines,
		"code_intel_skill_health:",
		"  kind: "+skillHealthTOONCell(report.Kind),
		"  generated_at_utc: "+skillHealthTOONCell(report.GeneratedAtUTC),
		"  promotion_policy: "+skillHealthTOONCell(report.PromotionPolicy),
		"  known: "+strconv.Itoa(report.Summary.Known),
		"  observed: "+strconv.Itoa(report.Summary.Observed),
		"  unknown: "+strconv.Itoa(report.Summary.Unknown),
		"  unused: "+strconv.Itoa(report.Summary.Unused),
		"  frequently_failing: "+strconv.Itoa(report.Summary.FrequentlyFailing),
		"  improving: "+strconv.Itoa(report.Summary.Improving),
		"  stale: "+strconv.Itoa(report.Summary.Stale),
		header,
	)

	for _, skill := range report.Skills {
		lines = append(lines, fmt.Sprintf(
			"    %s,%s,%s,%d,%d,%d,%d,%d,%s,%s",
			skillHealthTOONCell(skill.SkillID),
			skillHealthTOONCell(skill.Status),
			skillHealthTOONCell(skill.Trend),
			skill.Total,
			skill.Window7.Fixed,
			skill.Window7.Repeated,
			skill.Window30.Fixed,
			skill.Window30.Repeated,
			skillHealthTOONCell(skill.LastUsedUTC),
			skillHealthTOONCell(skill.SourcePath),
		))
	}

	return strings.Join(lines, "\n") + "\n"
}

func skillHealthTOONCell(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\n", " ")
	if value == "" {
		return `""`
	}

	if strings.ContainsAny(value, ",: []") {
		return strconv.Quote(value)
	}

	return value
}

var errRequiredSkillObservationField = errors.New("required skill observation field")
