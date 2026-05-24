// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"encoding/json"
	"fmt"
	"maps"
	"sort"
	"strconv"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
)

const (
	riskSummaryTopItemLimit = 10
	defaultNoisyRules       = 5
	severityRankError       = 4
	severityRankWarning     = 3
	severityRankNote        = 2
	severityRankDefault     = 1
)

type sarifLog struct {
	Runs []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool struct {
		Driver struct {
			Rules []sarifRule `json:"rules"`
		} `json:"driver"`
	} `json:"tool"`
	Properties struct {
		FindingGroups []sarifInputFindingGroup `json:"finding_groups,omitempty"`
	} `json:"properties,omitzero"`
	Results []sarifResult `json:"results"`
}

type sarifRule struct {
	Help             sarifHelp `json:"help,omitzero"`
	ShortDescription sarifMessage
	FullDescription  sarifMessage
	ID               string          `json:"id"`
	Name             string          `json:"name,omitempty"`
	Properties       sarifProperties `json:"properties,omitzero"`
}

type sarifResult struct {
	PartialFingerprints map[string]string
	RuleIndex           *int
	Message             sarifMessage
	RuleID              string
	Level               string
	Locations           []sarifLocation
	Properties          sarifProperties
}

type sarifMessage struct {
	Text string `json:"text,omitempty"`
}

type sarifHelp struct {
	Text     string `json:"text,omitempty"`
	Markdown string `json:"markdown,omitempty"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation
	Region           sarifRegion
}

type sarifArtifactLocation struct {
	URI string `json:"uri,omitempty"`
}

type sarifRegion struct {
	StartLine   int
	StartColumn int
}

type sarifProperties struct {
	Advice             string   `json:"advice,omitempty"`
	CELExpression      string   `json:"cel_expression,omitempty"`
	GroupID            string   `json:"coding_ethos_group_id,omitempty"`
	GroupKey           string   `json:"coding_ethos_group_key,omitempty"`
	Code               string   `json:"code,omitempty"`
	Detail             string   `json:"detail,omitempty"`
	Implementation     string   `json:"implementation,omitempty"`
	PolicyID           string   `json:"policy_id,omitempty"`
	PolicySource       string   `json:"policy_source,omitempty"`
	SkillID            string   `json:"skill_id,omitempty"`
	SourceTool         string   `json:"source_tool,omitempty"`
	EthosIDs           []string `json:"ethos_ids,omitempty"`
	InputSchemaVersion int      `json:"input_schema_version,omitempty"`
}

type sarifInputFindingGroup struct {
	ID          string `json:"id"`
	Key         string `json:"key"`
	PolicyID    string `json:"policy_id,omitempty"`
	SkillID     string `json:"skill_id,omitempty"`
	File        string `json:"file,omitempty"`
	ResultCount int    `json:"result_count,omitempty"`
	Line        int    `json:"line,omitempty"`
}

func (rule *sarifRule) UnmarshalJSON(payload []byte) error {
	fields, err := decodeMCPJSONFields(payload)
	if err != nil {
		return fmt.Errorf("decode SARIF rule fields: %w", err)
	}

	return decodeMCPJSONFieldsInto(fields, []mcpJSONFieldTarget{
		{key: "help", target: &rule.Help},
		{key: "shortDescription", target: &rule.ShortDescription},
		{key: "fullDescription", target: &rule.FullDescription},
		{key: "id", target: &rule.ID},
		{key: "name", target: &rule.Name},
		{key: "properties", target: &rule.Properties},
	})
}

func (result *sarifResult) UnmarshalJSON(payload []byte) error {
	fields, err := decodeMCPJSONFields(payload)
	if err != nil {
		return fmt.Errorf("decode SARIF result fields: %w", err)
	}

	return decodeMCPJSONFieldsInto(fields, []mcpJSONFieldTarget{
		{key: "partialFingerprints", target: &result.PartialFingerprints},
		{key: "ruleIndex", target: &result.RuleIndex},
		{key: "message", target: &result.Message},
		{key: "ruleId", target: &result.RuleID},
		{key: "level", target: &result.Level},
		{key: "locations", target: &result.Locations},
		{key: "properties", target: &result.Properties},
	})
}

func (location *sarifLocation) UnmarshalJSON(payload []byte) error {
	fields, err := decodeMCPJSONFields(payload)
	if err != nil {
		return fmt.Errorf("decode SARIF location fields: %w", err)
	}

	return decodeMCPJSONFieldsInto(fields, []mcpJSONFieldTarget{
		{key: "physicalLocation", target: &location.PhysicalLocation},
	})
}

func (location *sarifPhysicalLocation) UnmarshalJSON(payload []byte) error {
	fields, err := decodeMCPJSONFields(payload)
	if err != nil {
		return fmt.Errorf("decode SARIF physical location fields: %w", err)
	}

	return decodeMCPJSONFieldsInto(fields, []mcpJSONFieldTarget{
		{key: "artifactLocation", target: &location.ArtifactLocation},
		{key: "region", target: &location.Region},
	})
}

func (region *sarifRegion) UnmarshalJSON(payload []byte) error {
	fields, err := decodeMCPJSONFields(payload)
	if err != nil {
		return fmt.Errorf("decode SARIF region fields: %w", err)
	}

	return decodeMCPJSONFieldsInto(fields, []mcpJSONFieldTarget{
		{key: "startLine", target: &region.StartLine},
		{key: "startColumn", target: &region.StartColumn},
	})
}

func decodeMCPJSONFields(payload []byte) (map[string]json.RawMessage, error) {
	fields := map[string]json.RawMessage{}

	err := json.Unmarshal(payload, &fields)
	if err != nil {
		return nil, fmt.Errorf("decode JSON object: %w", err)
	}

	return fields, nil
}

type mcpJSONFieldTarget struct {
	target any
	key    string
}

func decodeMCPJSONFieldsInto(
	fields map[string]json.RawMessage,
	targets []mcpJSONFieldTarget,
) error {
	for _, target := range targets {
		raw, ok := fields[target.key]
		if !ok {
			continue
		}

		err := json.Unmarshal(raw, target.target)
		if err != nil {
			return fmt.Errorf("decode %q: %w", target.key, err)
		}
	}

	return nil
}

type sarifRemediationFinding struct {
	Fingerprints       map[string]string
	Level              string
	PolicyID           string
	CELExpression      string
	Code               string
	Detail             string
	File               string
	GroupID            string
	GroupKey           string
	Implementation     string
	PolicySource       string
	AdviceText         string
	Message            string
	SourceTool         string
	RuleHelp           string
	RuleID             string
	RuleName           string
	RuleSummary        string
	SkillID            string
	PrincipleIDs       []string
	InputSchemaVersion int
	Column             int
	Line               int
}

type sarifRiskSummary struct {
	Counts        map[string]int   `json:"counts"`
	Levels        map[string]int   `json:"levels"`
	Policies      []sarifRiskItem  `json:"policies,omitempty"`
	Skills        []sarifRiskItem  `json:"skills,omitempty"`
	SourceTools   []sarifRiskItem  `json:"source_tools,omitempty"`
	Files         []sarifRiskItem  `json:"files,omitempty"`
	FindingGroups []sarifRiskGroup `json:"finding_groups,omitempty"`
	Next          []map[string]any `json:"next"`
	RiskScore     int              `json:"risk_score"`
}

type sarifTrendAnalysis struct {
	Counts     map[string]int      `json:"counts"`
	Introduced []sarifTrendFinding `json:"introduced,omitempty"`
	Fixed      []sarifTrendFinding `json:"fixed,omitempty"`
	Persisting []sarifTrendFinding `json:"persisting,omitempty"`
	Reopened   []sarifTrendFinding `json:"reopened,omitempty"`
	Worsening  []sarifTrendFinding `json:"worsening,omitempty"`
	Next       []map[string]any    `json:"next"`
}

type sarifTrendFinding struct {
	Key      string `json:"key"`
	RuleID   string `json:"rule_id,omitempty"`
	PolicyID string `json:"policy_id,omitempty"`
	SkillID  string `json:"skill_id,omitempty"`
	File     string `json:"file,omitempty"`
	Message  string `json:"message,omitempty"`
	Level    string `json:"level,omitempty"`
	Line     int    `json:"line,omitempty"`
}

type sarifPolicyFeedback struct {
	Counts              map[string]int         `json:"counts"`
	UnmappedDiagnostics []sarifFeedbackFinding `json:"unmapped_diagnostics,omitempty"`
	MissingSkillIDs     []sarifFeedbackFinding `json:"missing_skill_ids,omitempty"`
	WeakSeverities      []sarifFeedbackFinding `json:"weak_severities,omitempty"`
	NoisyRules          []sarifFeedbackRule    `json:"noisy_rules,omitempty"`
	Next                []map[string]any       `json:"next"`
}

type sarifFeedbackFinding struct {
	RuleID   string `json:"rule_id"`
	File     string `json:"file,omitempty"`
	Message  string `json:"message,omitempty"`
	Level    string `json:"level,omitempty"`
	Code     string `json:"code,omitempty"`
	PolicyID string `json:"policy_id,omitempty"`
	SkillID  string `json:"skill_id,omitempty"`
	Line     int    `json:"line,omitempty"`
}

type sarifFeedbackRule struct {
	RuleID string `json:"rule_id"`
	Count  int    `json:"count"`
}

type sarifRiskItem struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

type sarifRiskGroup struct {
	ID          string `json:"id"`
	Key         string `json:"key"`
	PolicyID    string `json:"policy_id,omitempty"`
	SkillID     string `json:"skill_id,omitempty"`
	File        string `json:"file,omitempty"`
	ResultCount int    `json:"result_count"`
	Line        int    `json:"line,omitempty"`
}

func parseSARIFRemediationFinding(
	payload string,
	resultIndex int,
) (sarifRemediationFinding, error) {
	var log sarifLog

	err := json.Unmarshal([]byte(payload), &log)
	if err != nil {
		return sarifRemediationFinding{}, fmt.Errorf("parse SARIF: %w", err)
	}

	if len(log.Runs) == 0 {
		return sarifRemediationFinding{}, apperror.StaticError("SARIF run is required")
	}

	run := log.Runs[0]
	if len(run.Results) == 0 {
		return sarifRemediationFinding{}, apperror.StaticError(
			"SARIF result is required",
		)
	}

	if resultIndex >= len(run.Results) {
		return sarifRemediationFinding{}, apperror.Wrapf(
			apperror.StaticError("result_index %d out of range for %d SARIF results"),
			"result_index %d out of range for %d SARIF results",
			resultIndex,
			len(run.Results),
		)
	}

	result := run.Results[resultIndex]

	rule := sarifRule{}
	if result.RuleIndex != nil &&
		*result.RuleIndex >= 0 &&
		*result.RuleIndex < len(run.Tool.Driver.Rules) {
		rule = run.Tool.Driver.Rules[*result.RuleIndex]
	} else {
		rule = findSARIFRule(run.Tool.Driver.Rules, result.RuleID)
	}

	properties := mergeSARIFProperties(rule.Properties, result.Properties)
	location := firstSARIFLocation(result.Locations)

	return sarifRemediationFinding{
		Fingerprints:   result.PartialFingerprints,
		PrincipleIDs:   append([]string(nil), properties.EthosIDs...),
		AdviceText:     properties.Advice,
		CELExpression:  properties.CELExpression,
		Code:           properties.Code,
		Detail:         properties.Detail,
		File:           location.File,
		GroupID:        properties.GroupID,
		GroupKey:       properties.GroupKey,
		Implementation: properties.Implementation,
		Level:          result.Level,
		Message:        result.Message.Text,
		PolicyID:       properties.PolicyID,
		PolicySource:   properties.PolicySource,
		RuleHelp:       firstNonEmpty(rule.Help.Text, rule.Help.Markdown),
		RuleID:         result.RuleID,
		RuleName:       rule.Name,
		RuleSummary: firstNonEmpty(
			rule.ShortDescription.Text,
			rule.FullDescription.Text,
		),
		SkillID:            properties.SkillID,
		SourceTool:         properties.SourceTool,
		InputSchemaVersion: properties.InputSchemaVersion,
		Column:             location.Column,
		Line:               location.Line,
	}, nil
}

func summarizeSARIFRisk(payload string) (sarifRiskSummary, error) {
	var log sarifLog

	err := json.Unmarshal([]byte(payload), &log)
	if err != nil {
		return sarifRiskSummary{}, fmt.Errorf("parse SARIF: %w", err)
	}

	if len(log.Runs) == 0 {
		return sarifRiskSummary{}, apperror.StaticError("SARIF run is required")
	}

	accumulator := newSARIFRiskAccumulator()

	for _, run := range log.Runs {
		accumulator.AddRun(run)
	}

	return sarifRiskSummary{
		Counts: map[string]int{
			"results":          accumulator.ResultCount,
			"blocking_results": accumulator.BlockingCount,
			"security_results": accumulator.SecurityCount,
			"finding_groups":   len(accumulator.Groups),
		},
		Levels:        accumulator.Levels,
		Policies:      topRiskItems(accumulator.Policies),
		Skills:        topRiskItems(accumulator.Skills),
		SourceTools:   topRiskItems(accumulator.SourceTools),
		Files:         topRiskItems(accumulator.Files),
		FindingGroups: topRiskGroups(accumulator.Groups, riskSummaryTopItemLimit),
		Next: []map[string]any{{
			"tool": "sarif_remediation_advice",
			"arguments": map[string]any{
				"sarif":        "<same SARIF payload>",
				"result_index": 0,
			},
		}},
		RiskScore: accumulator.RiskScore(),
	}, nil
}

type sarifRiskAccumulator struct {
	Levels        map[string]int
	Policies      map[string]int
	Skills        map[string]int
	SourceTools   map[string]int
	Files         map[string]int
	Groups        map[string]sarifRiskGroup
	ResultCount   int
	BlockingCount int
	SecurityCount int
}

func newSARIFRiskAccumulator() sarifRiskAccumulator {
	return sarifRiskAccumulator{
		Levels:      map[string]int{},
		Policies:    map[string]int{},
		Skills:      map[string]int{},
		SourceTools: map[string]int{},
		Files:       map[string]int{},
		Groups:      map[string]sarifRiskGroup{},
	}
}

func (accumulator *sarifRiskAccumulator) AddRun(run sarifRun) {
	for _, inputGroup := range run.Properties.FindingGroups {
		if strings.TrimSpace(inputGroup.Key) != "" {
			accumulator.Groups[inputGroup.Key] = sarifRiskGroup(inputGroup)
		}
	}

	for _, result := range run.Results {
		accumulator.AddResult(run, result)
	}
}

func (accumulator *sarifRiskAccumulator) AddResult(run sarifRun, result sarifResult) {
	accumulator.ResultCount++
	level := firstNonEmpty(result.Level, "warning")

	accumulator.Levels[level]++
	if level == "error" {
		accumulator.BlockingCount++
	}

	rule := sarifRuleForResult(run.Tool.Driver.Rules, result)
	properties := mergeSARIFProperties(rule.Properties, result.Properties)
	countRiskValue(
		accumulator.Policies,
		firstNonEmpty(properties.PolicyID, result.RuleID),
	)
	countRiskValue(accumulator.Skills, properties.SkillID)
	countRiskValue(accumulator.SourceTools, properties.SourceTool)
	countRiskValue(accumulator.Files, firstSARIFLocation(result.Locations).File)

	if isSARIFSecurityFinding(result, rule, properties) {
		accumulator.SecurityCount++
	}
}

func sarifRuleForResult(rules []sarifRule, result sarifResult) sarifRule {
	if result.RuleIndex != nil &&
		*result.RuleIndex >= 0 &&
		*result.RuleIndex < len(rules) {
		return rules[*result.RuleIndex]
	}

	return findSARIFRule(rules, result.RuleID)
}

func (accumulator *sarifRiskAccumulator) RiskScore() int {
	return accumulator.BlockingCount*10 +
		accumulator.SecurityCount*5 +
		accumulator.ResultCount
}

func analyzeSARIFTrend(
	baselinePayload string,
	currentPayload string,
	historyPayloads []string,
) (sarifTrendAnalysis, error) {
	baseline, err := sarifTrendFindings(baselinePayload)
	if err != nil {
		return sarifTrendAnalysis{}, fmt.Errorf("parse baseline SARIF: %w", err)
	}

	current, err := sarifTrendFindings(currentPayload)
	if err != nil {
		return sarifTrendAnalysis{}, fmt.Errorf("parse current SARIF: %w", err)
	}

	history, err := sarifTrendHistoryFindings(historyPayloads)
	if err != nil {
		return sarifTrendAnalysis{}, err
	}

	introduced := trendDifference(current, baseline)
	fixed := trendDifference(baseline, current)
	persisting := trendIntersection(current, baseline)
	reopened := sortedTrendFindingsFromMap(
		trendIntersectionMap(trendDifferenceMap(current, baseline), history),
	)
	worsening := trendWorsening(current, baseline)

	return sarifTrendAnalysis{
		Counts: map[string]int{
			"baseline":   len(baseline),
			"current":    len(current),
			"history":    len(history),
			"introduced": len(introduced),
			"fixed":      len(fixed),
			"persisting": len(persisting),
			"reopened":   len(reopened),
			"worsening":  len(worsening),
		},
		Introduced: introduced,
		Fixed:      fixed,
		Persisting: persisting,
		Reopened:   reopened,
		Worsening:  worsening,
		Next: []map[string]any{{
			"tool": "sarif_remediation_advice",
			"arguments": map[string]any{
				"sarif":        "<current SARIF payload>",
				"result_index": 0,
			},
		}},
	}, nil
}

func analyzeSARIFPolicyFeedback(payload string) (sarifPolicyFeedback, error) {
	var log sarifLog

	err := json.Unmarshal([]byte(payload), &log)
	if err != nil {
		return sarifPolicyFeedback{}, fmt.Errorf("parse SARIF: %w", err)
	}

	ruleCounts := map[string]int{}
	feedback := sarifPolicyFeedback{
		Counts: map[string]int{},
		Next: []map[string]any{{
			"tool": "policy_explain",
			"arguments": map[string]any{
				"policy_id": "<policy_id>",
			},
		}, {
			"tool": "skill_recommend",
			"arguments": map[string]any{
				"diagnostic": "<unmapped diagnostic>",
			},
		}},
	}

	for _, run := range log.Runs {
		for _, result := range run.Results {
			rule := sarifRule{}
			if result.RuleIndex != nil &&
				*result.RuleIndex >= 0 &&
				*result.RuleIndex < len(run.Tool.Driver.Rules) {
				rule = run.Tool.Driver.Rules[*result.RuleIndex]
			} else {
				rule = findSARIFRule(run.Tool.Driver.Rules, result.RuleID)
			}

			properties := mergeSARIFProperties(rule.Properties, result.Properties)
			finding := sarifFeedbackFindingFromResult(result, properties)
			ruleCounts[result.RuleID]++
			feedback.Counts["results"]++

			if strings.TrimSpace(properties.PolicyID) == "" {
				feedback.UnmappedDiagnostics = append(
					feedback.UnmappedDiagnostics,
					finding,
				)
				feedback.Counts["unmapped_diagnostics"]++
			}

			if strings.TrimSpace(properties.SkillID) == "" {
				feedback.MissingSkillIDs = append(feedback.MissingSkillIDs, finding)
				feedback.Counts["missing_skill_ids"]++
			}

			if sarifWeakSeverity(result, rule, properties) {
				feedback.WeakSeverities = append(feedback.WeakSeverities, finding)
				feedback.Counts["weak_severities"]++
			}
		}
	}

	feedback.NoisyRules = noisySARIFRules(ruleCounts, defaultNoisyRules)
	feedback.Counts["noisy_rules"] = len(feedback.NoisyRules)

	return feedback, nil
}

func sarifTrendHistoryFindings(
	payloads []string,
) (map[string]sarifTrendFinding, error) {
	history := map[string]sarifTrendFinding{}

	for index, payload := range payloads {
		findings, err := sarifTrendFindings(payload)
		if err != nil {
			return nil, fmt.Errorf("parse history SARIF %d: %w", index, err)
		}

		maps.Copy(history, findings)
	}

	return history, nil
}

func sarifFeedbackFindingFromResult(
	result sarifResult,
	properties sarifProperties,
) sarifFeedbackFinding {
	location := firstSARIFLocation(result.Locations)

	return sarifFeedbackFinding{
		RuleID:   result.RuleID,
		File:     location.File,
		Message:  result.Message.Text,
		Level:    result.Level,
		Code:     properties.Code,
		PolicyID: properties.PolicyID,
		SkillID:  properties.SkillID,
		Line:     location.Line,
	}
}

func sarifWeakSeverity(
	result sarifResult,
	rule sarifRule,
	properties sarifProperties,
) bool {
	level := strings.ToLower(strings.TrimSpace(result.Level))

	return (level == "" || level == "note" || level == "warning") &&
		isSARIFSecurityFinding(result, rule, properties)
}

func noisySARIFRules(ruleCounts map[string]int, threshold int) []sarifFeedbackRule {
	rules := []sarifFeedbackRule{}

	for ruleID, count := range ruleCounts {
		if count >= threshold {
			rules = append(rules, sarifFeedbackRule{RuleID: ruleID, Count: count})
		}
	}

	sort.Slice(rules, func(left, right int) bool {
		if rules[left].Count != rules[right].Count {
			return rules[left].Count > rules[right].Count
		}

		return rules[left].RuleID < rules[right].RuleID
	})

	return rules
}

func sarifTrendFindings(payload string) (map[string]sarifTrendFinding, error) {
	var log sarifLog

	err := json.Unmarshal([]byte(payload), &log)
	if err != nil {
		return nil, fmt.Errorf("parse SARIF trend payload: %w", err)
	}

	findings := map[string]sarifTrendFinding{}

	for _, run := range log.Runs {
		for _, result := range run.Results {
			rule := sarifRule{}
			if result.RuleIndex != nil &&
				*result.RuleIndex >= 0 &&
				*result.RuleIndex < len(run.Tool.Driver.Rules) {
				rule = run.Tool.Driver.Rules[*result.RuleIndex]
			} else {
				rule = findSARIFRule(run.Tool.Driver.Rules, result.RuleID)
			}

			properties := mergeSARIFProperties(rule.Properties, result.Properties)
			location := firstSARIFLocation(result.Locations)

			key := firstNonEmpty(
				properties.GroupKey,
				result.PartialFingerprints["coding-ethos/stable/v1"],
				result.PartialFingerprints["coding-ethos/v1"],
				strings.Join([]string{
					firstNonEmpty(properties.PolicyID, result.RuleID),
					location.File,
					strconv.Itoa(location.Line),
					result.Message.Text,
				}, "|"),
			)
			if key == "" {
				continue
			}

			findings[key] = sarifTrendFinding{
				Key:      key,
				RuleID:   result.RuleID,
				PolicyID: properties.PolicyID,
				SkillID:  properties.SkillID,
				File:     location.File,
				Message:  result.Message.Text,
				Level:    result.Level,
				Line:     location.Line,
			}
		}
	}

	return findings, nil
}

func trendDifference(
	left map[string]sarifTrendFinding,
	right map[string]sarifTrendFinding,
) []sarifTrendFinding {
	return sortedTrendFindingsFromMap(trendDifferenceMap(left, right))
}

func trendDifferenceMap(
	left map[string]sarifTrendFinding,
	right map[string]sarifTrendFinding,
) map[string]sarifTrendFinding {
	items := map[string]sarifTrendFinding{}

	for key, finding := range left {
		if _, found := right[key]; !found {
			items[key] = finding
		}
	}

	return items
}

func trendIntersection(
	left map[string]sarifTrendFinding,
	right map[string]sarifTrendFinding,
) []sarifTrendFinding {
	return sortedTrendFindingsFromMap(trendIntersectionMap(left, right))
}

func trendIntersectionMap(
	left map[string]sarifTrendFinding,
	right map[string]sarifTrendFinding,
) map[string]sarifTrendFinding {
	items := map[string]sarifTrendFinding{}

	for key, finding := range left {
		if _, found := right[key]; found {
			items[key] = finding
		}
	}

	return items
}

func sortedTrendFindingsFromMap(
	values map[string]sarifTrendFinding,
) []sarifTrendFinding {
	items := make([]sarifTrendFinding, 0, len(values))
	for _, finding := range values {
		items = append(items, finding)
	}

	return sortedTrendFindings(items)
}

func trendWorsening(
	current map[string]sarifTrendFinding,
	baseline map[string]sarifTrendFinding,
) []sarifTrendFinding {
	items := []sarifTrendFinding{}

	for key, currentFinding := range current {
		baselineFinding, found := baseline[key]
		if !found {
			continue
		}

		if sarifTrendSeverityRank(currentFinding.Level) >
			sarifTrendSeverityRank(baselineFinding.Level) {
			items = append(items, currentFinding)
		}
	}

	return sortedTrendFindings(items)
}

func sarifTrendSeverityRank(level string) int {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "error":
		return severityRankError
	case "warning":
		return severityRankWarning
	case "note":
		return severityRankNote
	default:
		return severityRankDefault
	}
}

func sortedTrendFindings(items []sarifTrendFinding) []sarifTrendFinding {
	sort.Slice(items, func(left, right int) bool {
		return items[left].Key < items[right].Key
	})

	return items
}

func findSARIFRule(rules []sarifRule, ruleID string) sarifRule {
	for _, rule := range rules {
		if rule.ID == ruleID {
			return rule
		}
	}

	return sarifRule{ID: ruleID}
}

func mergeSARIFProperties(rule, result sarifProperties) sarifProperties {
	merged := rule
	mergeSARIFPropertyStrings(&merged, result)

	if len(result.EthosIDs) > 0 {
		merged.EthosIDs = result.EthosIDs
	}

	if result.InputSchemaVersion != 0 {
		merged.InputSchemaVersion = result.InputSchemaVersion
	}

	return merged
}

func mergeSARIFPropertyStrings(merged *sarifProperties, result sarifProperties) {
	for _, field := range []struct {
		target *string
		value  string
	}{
		{target: &merged.Advice, value: result.Advice},
		{target: &merged.CELExpression, value: result.CELExpression},
		{target: &merged.Code, value: result.Code},
		{target: &merged.GroupID, value: result.GroupID},
		{target: &merged.GroupKey, value: result.GroupKey},
		{target: &merged.Detail, value: result.Detail},
		{target: &merged.Implementation, value: result.Implementation},
		{target: &merged.PolicyID, value: result.PolicyID},
		{target: &merged.PolicySource, value: result.PolicySource},
		{target: &merged.SkillID, value: result.SkillID},
		{target: &merged.SourceTool, value: result.SourceTool},
	} {
		if field.value != "" {
			*field.target = field.value
		}
	}
}

func isSARIFSecurityFinding(
	result sarifResult,
	rule sarifRule,
	properties sarifProperties,
) bool {
	text := strings.ToLower(strings.Join([]string{
		result.RuleID,
		result.Message.Text,
		rule.ShortDescription.Text,
		rule.Help.Text,
		properties.PolicyID,
		properties.Code,
		properties.Detail,
	}, " "))

	return strings.Contains(text, "security") ||
		strings.Contains(text, "injection") ||
		strings.Contains(text, "secret") ||
		strings.Contains(text, "credential") ||
		strings.Contains(text, "unsafe")
}

func countRiskValue(counts map[string]int, value string) {
	value = strings.TrimSpace(value)
	if value != "" {
		counts[value]++
	}
}

func topRiskItems(counts map[string]int) []sarifRiskItem {
	items := make([]sarifRiskItem, 0, len(counts))
	for value, count := range counts {
		items = append(items, sarifRiskItem{Value: value, Count: count})
	}

	sort.Slice(items, func(left, right int) bool {
		if items[left].Count != items[right].Count {
			return items[left].Count > items[right].Count
		}

		return items[left].Value < items[right].Value
	})

	if len(items) > riskSummaryTopItemLimit {
		return items[:riskSummaryTopItemLimit]
	}

	return items
}

func topRiskGroups(
	groups map[string]sarifRiskGroup,
	limit int,
) []sarifRiskGroup {
	items := make([]sarifRiskGroup, 0, len(groups))
	for _, group := range groups {
		items = append(items, group)
	}

	sort.Slice(items, func(left, right int) bool {
		if items[left].ResultCount != items[right].ResultCount {
			return items[left].ResultCount > items[right].ResultCount
		}

		return items[left].Key < items[right].Key
	})

	if len(items) > limit {
		return items[:limit]
	}

	return items
}

type sarifFindingLocation struct {
	File   string
	Line   int
	Column int
}

func firstSARIFLocation(locations []sarifLocation) sarifFindingLocation {
	if len(locations) == 0 {
		return sarifFindingLocation{}
	}

	physical := locations[0].PhysicalLocation

	return sarifFindingLocation{
		File:   physical.ArtifactLocation.URI,
		Line:   physical.Region.StartLine,
		Column: physical.Region.StartColumn,
	}
}

func (finding sarifRemediationFinding) summary() map[string]any {
	return map[string]any{
		"rule_id":              finding.RuleID,
		"rule_name":            finding.RuleName,
		"policy_id":            finding.PolicyID,
		"skill_id":             finding.SkillID,
		"principle_ids":        finding.PrincipleIDs,
		"source_tool":          finding.SourceTool,
		"code":                 finding.Code,
		"level":                finding.Level,
		"message":              finding.Message,
		"file":                 finding.File,
		"group_id":             finding.GroupID,
		"group_key":            finding.GroupKey,
		"line":                 finding.Line,
		"column":               finding.Column,
		"implementation":       finding.Implementation,
		"input_schema_version": finding.InputSchemaVersion,
		"policy_source":        finding.PolicySource,
		"cel_expression":       finding.CELExpression,
		"fingerprints":         finding.Fingerprints,
	}
}

func (finding sarifRemediationFinding) advice() map[string]any {
	return map[string]any{
		"summary": firstNonEmpty(
			finding.AdviceText,
			finding.Detail,
			finding.RuleHelp,
			finding.RuleSummary,
			finding.Message,
		),
		"detail": finding.Detail,
		"steps": []string{
			"Inspect the cited file and line.",
			"Apply the policy-preserving structural fix described by the rule, " +
				"skill, and ETHOS principle.",
			"Rerun the managed lint check through MCP before committing.",
		},
	}
}

func (finding sarifRemediationFinding) rerun() map[string]any {
	arguments := map[string]any{}
	if strings.TrimSpace(finding.SourceTool) != "" {
		arguments["tool"] = finding.SourceTool
	} else {
		arguments["scope"] = "files"
	}

	if strings.TrimSpace(finding.File) != "" {
		arguments["files"] = []string{finding.File}
	}

	return map[string]any{
		"tool":      "managed_lint",
		"arguments": arguments,
	}
}

func (finding sarifRemediationFinding) diagnosticInput() lintAdviceInput {
	return lintAdviceInput{
		Tool:     finding.SourceTool,
		Code:     finding.Code,
		File:     finding.File,
		Line:     finding.Line,
		Column:   finding.Column,
		Severity: finding.Level,
		Message:  firstNonEmpty(finding.Message, finding.RuleSummary),
	}
}
