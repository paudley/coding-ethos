// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package mcp

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
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
	} `json:"properties,omitempty"`
	Results []sarifResult `json:"results"`
}

type sarifRule struct {
	ShortDescription sarifMessage    `json:"shortDescription,omitempty"`
	FullDescription  sarifMessage    `json:"fullDescription,omitempty"`
	Help             sarifHelp       `json:"help,omitempty"`
	Properties       sarifProperties `json:"properties,omitempty"`
	ID               string          `json:"id"`
	Name             string          `json:"name,omitempty"`
}

type sarifResult struct {
	Message             sarifMessage      `json:"message"`
	Properties          sarifProperties   `json:"properties,omitempty"`
	PartialFingerprints map[string]string `json:"partialFingerprints,omitempty"`
	Locations           []sarifLocation   `json:"locations,omitempty"`
	RuleID              string            `json:"ruleId"`
	Level               string            `json:"level,omitempty"`
	RuleIndex           *int              `json:"ruleIndex,omitempty"`
}

type sarifMessage struct {
	Text string `json:"text,omitempty"`
}

type sarifHelp struct {
	Text     string `json:"text,omitempty"`
	Markdown string `json:"markdown,omitempty"`
}

type sarifLocation struct {
	PhysicalLocation struct {
		ArtifactLocation struct {
			URI string `json:"uri,omitempty"`
		} `json:"artifactLocation,omitempty"`
		Region struct {
			StartLine   int `json:"startLine,omitempty"`
			StartColumn int `json:"startColumn,omitempty"`
		} `json:"region,omitempty"`
	} `json:"physicalLocation,omitempty"`
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

type sarifRemediationFinding struct {
	Fingerprints       map[string]string
	PrincipleIDs       []string
	AdviceText         string
	CELExpression      string
	Code               string
	Detail             string
	File               string
	GroupID            string
	GroupKey           string
	Implementation     string
	Level              string
	Message            string
	PolicyID           string
	PolicySource       string
	RuleHelp           string
	RuleID             string
	RuleName           string
	RuleSummary        string
	SkillID            string
	SourceTool         string
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
	if err := json.Unmarshal([]byte(payload), &log); err != nil {
		return sarifRemediationFinding{}, fmt.Errorf("parse SARIF: %w", err)
	}
	if len(log.Runs) == 0 {
		return sarifRemediationFinding{}, fmt.Errorf("SARIF run is required")
	}

	run := log.Runs[0]
	if len(run.Results) == 0 {
		return sarifRemediationFinding{}, fmt.Errorf("SARIF result is required")
	}
	if resultIndex >= len(run.Results) {
		return sarifRemediationFinding{}, fmt.Errorf(
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
		Fingerprints:       result.PartialFingerprints,
		PrincipleIDs:       append([]string(nil), properties.EthosIDs...),
		AdviceText:         properties.Advice,
		CELExpression:      properties.CELExpression,
		Code:               properties.Code,
		Detail:             properties.Detail,
		File:               location.File,
		GroupID:            properties.GroupID,
		GroupKey:           properties.GroupKey,
		Implementation:     properties.Implementation,
		Level:              result.Level,
		Message:            result.Message.Text,
		PolicyID:           properties.PolicyID,
		PolicySource:       properties.PolicySource,
		RuleHelp:           firstNonEmpty(rule.Help.Text, rule.Help.Markdown),
		RuleID:             result.RuleID,
		RuleName:           rule.Name,
		RuleSummary:        firstNonEmpty(rule.ShortDescription.Text, rule.FullDescription.Text),
		SkillID:            properties.SkillID,
		SourceTool:         properties.SourceTool,
		InputSchemaVersion: properties.InputSchemaVersion,
		Column:             location.Column,
		Line:               location.Line,
	}, nil
}

func summarizeSARIFRisk(payload string) (sarifRiskSummary, error) {
	var log sarifLog
	if err := json.Unmarshal([]byte(payload), &log); err != nil {
		return sarifRiskSummary{}, fmt.Errorf("parse SARIF: %w", err)
	}
	if len(log.Runs) == 0 {
		return sarifRiskSummary{}, fmt.Errorf("SARIF run is required")
	}

	levels := map[string]int{}
	policies := map[string]int{}
	skills := map[string]int{}
	sourceTools := map[string]int{}
	files := map[string]int{}
	groups := map[string]sarifRiskGroup{}
	resultCount := 0
	blockingCount := 0
	securityCount := 0

	for _, run := range log.Runs {
		for _, inputGroup := range run.Properties.FindingGroups {
			if strings.TrimSpace(inputGroup.Key) == "" {
				continue
			}
			groups[inputGroup.Key] = sarifRiskGroup{
				ID:          inputGroup.ID,
				Key:         inputGroup.Key,
				PolicyID:    inputGroup.PolicyID,
				SkillID:     inputGroup.SkillID,
				File:        inputGroup.File,
				ResultCount: inputGroup.ResultCount,
				Line:        inputGroup.Line,
			}
		}
		for _, result := range run.Results {
			resultCount++
			level := firstNonEmpty(result.Level, "warning")
			levels[level]++
			if level == "error" {
				blockingCount++
			}
			rule := sarifRule{}
			if result.RuleIndex != nil &&
				*result.RuleIndex >= 0 &&
				*result.RuleIndex < len(run.Tool.Driver.Rules) {
				rule = run.Tool.Driver.Rules[*result.RuleIndex]
			} else {
				rule = findSARIFRule(run.Tool.Driver.Rules, result.RuleID)
			}
			properties := mergeSARIFProperties(rule.Properties, result.Properties)
			countRiskValue(policies, firstNonEmpty(properties.PolicyID, result.RuleID))
			countRiskValue(skills, properties.SkillID)
			countRiskValue(sourceTools, properties.SourceTool)
			location := firstSARIFLocation(result.Locations)
			countRiskValue(files, location.File)
			if isSARIFSecurityFinding(result, rule, properties) {
				securityCount++
			}
		}
	}

	return sarifRiskSummary{
		Counts: map[string]int{
			"results":          resultCount,
			"blocking_results": blockingCount,
			"security_results": securityCount,
			"finding_groups":   len(groups),
		},
		Levels:        levels,
		Policies:      topRiskItems(policies, 10),
		Skills:        topRiskItems(skills, 10),
		SourceTools:   topRiskItems(sourceTools, 10),
		Files:         topRiskItems(files, 10),
		FindingGroups: topRiskGroups(groups, 10),
		Next: []map[string]any{{
			"tool": "sarif_remediation_advice",
			"arguments": map[string]any{
				"sarif":        "<same SARIF payload>",
				"result_index": 0,
			},
		}},
		RiskScore: blockingCount*10 + securityCount*5 + resultCount,
	}, nil
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
	if err := json.Unmarshal([]byte(payload), &log); err != nil {
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
				feedback.UnmappedDiagnostics = append(feedback.UnmappedDiagnostics, finding)
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

	feedback.NoisyRules = noisySARIFRules(ruleCounts, 5)
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
		for key, finding := range findings {
			history[key] = finding
		}
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
	sort.Slice(rules, func(left int, right int) bool {
		if rules[left].Count != rules[right].Count {
			return rules[left].Count > rules[right].Count
		}

		return rules[left].RuleID < rules[right].RuleID
	})

	return rules
}

func sarifTrendFindings(payload string) (map[string]sarifTrendFinding, error) {
	var log sarifLog
	if err := json.Unmarshal([]byte(payload), &log); err != nil {
		return nil, err
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
					fmt.Sprint(location.Line),
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
	items := []sarifTrendFinding{}
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
		return 4
	case "warning":
		return 3
	case "note":
		return 2
	default:
		return 1
	}
}

func sortedTrendFindings(items []sarifTrendFinding) []sarifTrendFinding {
	sort.Slice(items, func(left int, right int) bool {
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

func mergeSARIFProperties(rule sarifProperties, result sarifProperties) sarifProperties {
	merged := rule
	if result.Advice != "" {
		merged.Advice = result.Advice
	}
	if result.CELExpression != "" {
		merged.CELExpression = result.CELExpression
	}
	if result.Code != "" {
		merged.Code = result.Code
	}
	if result.GroupID != "" {
		merged.GroupID = result.GroupID
	}
	if result.GroupKey != "" {
		merged.GroupKey = result.GroupKey
	}
	if result.Detail != "" {
		merged.Detail = result.Detail
	}
	if result.Implementation != "" {
		merged.Implementation = result.Implementation
	}
	if result.PolicyID != "" {
		merged.PolicyID = result.PolicyID
	}
	if result.PolicySource != "" {
		merged.PolicySource = result.PolicySource
	}
	if result.SkillID != "" {
		merged.SkillID = result.SkillID
	}
	if result.SourceTool != "" {
		merged.SourceTool = result.SourceTool
	}
	if len(result.EthosIDs) > 0 {
		merged.EthosIDs = result.EthosIDs
	}
	if result.InputSchemaVersion != 0 {
		merged.InputSchemaVersion = result.InputSchemaVersion
	}

	return merged
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

func topRiskItems(counts map[string]int, limit int) []sarifRiskItem {
	items := make([]sarifRiskItem, 0, len(counts))
	for value, count := range counts {
		items = append(items, sarifRiskItem{Value: value, Count: count})
	}
	sort.Slice(items, func(left int, right int) bool {
		if items[left].Count != items[right].Count {
			return items[left].Count > items[right].Count
		}

		return items[left].Value < items[right].Value
	})
	if len(items) > limit {
		return items[:limit]
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
	sort.Slice(items, func(left int, right int) bool {
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
			"Apply the policy-preserving structural fix described by the rule, skill, and ETHOS principle.",
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
		"tool":      "lint_check",
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
