// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hookoutput

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/lint"
)

const (
	sarifSchema  = "https://json.schemastore.org/sarif-2.1.0.json"
	sarifVersion = "2.1.0"
	sarifRepoURI = "."
)

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool              sarifTool                 `json:"tool"`
	Invocations       []sarifInvocation         `json:"invocations,omitempty"`
	AutomationDetails sarifRunAutomationDetails `json:"automationDetails,omitempty"`
	Results           []sarifResult             `json:"results"`
	Properties        sarifRunProperties        `json:"properties,omitempty"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	InformationURI string      `json:"informationUri,omitempty"`
	Rules          []sarifRule `json:"rules,omitempty"`
}

type sarifRule struct {
	ID               string            `json:"id"`
	Name             string            `json:"name,omitempty"`
	ShortDescription sarifMessage      `json:"shortDescription,omitempty"`
	Help             sarifHelp         `json:"help,omitempty"`
	Properties       sarifRuleProperty `json:"properties,omitempty"`
}

type sarifRuleProperty struct {
	Tags             []string `json:"tags,omitempty"`
	Precision        string   `json:"precision,omitempty"`
	SecuritySeverity string   `json:"security-severity,omitempty"`
	PolicyID         string   `json:"policy_id,omitempty"`
	SkillID          string   `json:"skill_id,omitempty"`
	SourceTool       string   `json:"source_tool,omitempty"`
	EthosIDs         []string `json:"ethos_ids,omitempty"`
	CodingEthos      bool     `json:"coding_ethos"`
}

type sarifHelp struct {
	Text     string `json:"text,omitempty"`
	Markdown string `json:"markdown,omitempty"`
}

type sarifResult struct {
	RuleID              string                `json:"ruleId"`
	RuleIndex           *int                  `json:"ruleIndex,omitempty"`
	Level               string                `json:"level"`
	Message             sarifMessage          `json:"message"`
	Locations           []sarifLocation       `json:"locations,omitempty"`
	PartialFingerprints map[string]string     `json:"partialFingerprints,omitempty"`
	Properties          sarifResultProperties `json:"properties,omitempty"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           sarifRegion           `json:"region,omitempty"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine   int `json:"startLine,omitempty"`
	StartColumn int `json:"startColumn,omitempty"`
}

type sarifResultProperties struct {
	Advice      string   `json:"advice,omitempty"`
	Code        string   `json:"code,omitempty"`
	Detail      string   `json:"detail,omitempty"`
	PolicyID    string   `json:"policy_id,omitempty"`
	SkillID     string   `json:"skill_id,omitempty"`
	SourceTool  string   `json:"source_tool,omitempty"`
	EthosIDs    []string `json:"ethos_ids,omitempty"`
	CodingEthos bool     `json:"coding_ethos"`
}

type sarifInvocation struct {
	WorkingDirectory    sarifArtifactLocation `json:"workingDirectory,omitempty"`
	ExecutionSuccessful bool                  `json:"executionSuccessful"`
}

type sarifRunAutomationDetails struct {
	ID string `json:"id,omitempty"`
}

type sarifRunProperties struct {
	Scope string `json:"scope,omitempty"`
}

func FormatLintResultSARIF(result lint.Result) (string, error) {
	diagnostics := sarifDiagnostics(result)
	rules := sarifRules(diagnostics)
	ruleIndexes := sarifRuleIndexes(rules)
	log := sarifLog{
		Schema:  sarifSchema,
		Version: sarifVersion,
		Runs: []sarifRun{{
			Tool: sarifTool{Driver: sarifDriver{
				Name:           "coding-ethos",
				InformationURI: "https://github.com/paudley/coding-ethos",
				Rules:          rules,
			}},
			Invocations: []sarifInvocation{{
				WorkingDirectory:    sarifArtifactLocation{URI: sarifRepoURI},
				ExecutionSuccessful: !result.Blocked(),
			}},
			AutomationDetails: sarifRunAutomationDetails{
				ID: sarifAutomationID(result.Scope),
			},
			Results: sarifResults(diagnostics, ruleIndexes),
			Properties: sarifRunProperties{
				Scope: result.Scope,
			},
		}},
	}

	payload, err := json.MarshalIndent(log, "", "  ")
	if err != nil {
		return "", err
	}

	return string(payload), nil
}

func sarifDiagnostics(result lint.Result) []diagnostics.Diagnostic {
	if len(result.Diagnostics) > 0 {
		return lint.OutputDiagnostics(result)
	}
	if len(result.Findings) > 0 && result.Blocked() {
		return lint.OutputDiagnostics(result)
	}
	if result.Blocked() {
		return lint.OutputDiagnostics(result)
	}

	return nil
}

func sarifRules(items []diagnostics.Diagnostic) []sarifRule {
	rules := []sarifRule{}
	seen := map[string]bool{}
	for _, item := range items {
		ruleID := sarifRuleID(item)
		if seen[ruleID] {
			continue
		}
		seen[ruleID] = true
		rules = append(rules, sarifRule{
			ID:   ruleID,
			Name: sarifRuleName(item),
			ShortDescription: sarifMessage{
				Text: firstSarifNonEmpty(item.Meaning, item.Message, ruleID),
			},
			Help: sarifHelp{
				Text:     item.Advice,
				Markdown: sarifHelpMarkdown(item),
			},
			Properties: sarifRuleProperty{
				Tags:             sarifRuleTags(item),
				Precision:        sarifPrecision(item),
				SecuritySeverity: sarifSecuritySeverity(item),
				PolicyID:         item.PolicyID,
				SkillID:          item.SkillID,
				SourceTool:       item.Tool,
				EthosIDs:         append([]string(nil), item.PrincipleIDs...),
				CodingEthos:      true,
			},
		})
	}

	return rules
}

func sarifResults(
	items []diagnostics.Diagnostic,
	ruleIndexes map[string]int,
) []sarifResult {
	results := make([]sarifResult, 0, len(items))
	for _, item := range items {
		ruleID := sarifRuleID(item)
		results = append(results, sarifResult{
			RuleID:              ruleID,
			RuleIndex:           sarifRuleIndex(ruleIndexes, ruleID),
			Level:               sarifLevel(item.Severity),
			Message:             sarifMessage{Text: item.Message},
			Locations:           sarifLocations(item),
			PartialFingerprints: sarifPartialFingerprints(item),
			Properties: sarifResultProperties{
				Advice:      item.Advice,
				Code:        item.Code,
				Detail:      item.Detail,
				PolicyID:    item.PolicyID,
				SkillID:     item.SkillID,
				SourceTool:  item.Tool,
				EthosIDs:    append([]string(nil), item.PrincipleIDs...),
				CodingEthos: true,
			},
		})
	}

	return results
}

func sarifRuleIndex(indexes map[string]int, ruleID string) *int {
	index, ok := indexes[ruleID]
	if !ok {
		return nil
	}

	return &index
}

func sarifRuleIndexes(rules []sarifRule) map[string]int {
	indexes := make(map[string]int, len(rules))
	for index, rule := range rules {
		indexes[rule.ID] = index
	}

	return indexes
}

func sarifRuleID(item diagnostics.Diagnostic) string {
	return firstSarifNonEmpty(
		item.PolicyID,
		joinSarifID(item.Tool, item.Code),
		item.Code,
		item.Tool,
		"coding-ethos",
	)
}

func sarifRuleName(item diagnostics.Diagnostic) string {
	return firstSarifNonEmpty(item.Code, item.PolicyID, item.Tool)
}

func sarifRuleTags(item diagnostics.Diagnostic) []string {
	tags := append([]string(nil), item.Tags...)
	if sarifSecuritySeverity(item) != "" && !containsString(tags, "security") {
		tags = append(tags, "security")
	}
	if item.PolicyID != "" && !containsString(tags, "policy") {
		tags = append(tags, "policy")
	}
	if item.SkillID != "" && !containsString(tags, "remediation") {
		tags = append(tags, "remediation")
	}

	return limitStrings(tags, 20)
}

func sarifPrecision(item diagnostics.Diagnostic) string {
	if item.PolicyID != "" || len(item.PrincipleIDs) > 0 {
		return "high"
	}
	if item.Code != "" {
		return "medium"
	}

	return ""
}

func sarifSecuritySeverity(item diagnostics.Diagnostic) string {
	text := strings.ToLower(strings.Join([]string{
		item.PolicyID,
		item.Code,
		item.Message,
		item.Detail,
		strings.Join(item.Tags, " "),
	}, " "))
	if !strings.Contains(text, "security") &&
		!strings.Contains(text, "injection") &&
		!strings.Contains(text, "secret") &&
		!strings.Contains(text, "credential") &&
		!strings.Contains(text, "unsafe") &&
		!sarifBanditSecurityCode(item.Code) {
		return ""
	}

	switch sarifLevel(item.Severity) {
	case "error":
		return "8.0"
	case "warning":
		return "5.0"
	default:
		return "3.0"
	}
}

func sarifBanditSecurityCode(code string) bool {
	code = strings.ToUpper(strings.TrimSpace(code))
	if len(code) < 2 || code[0] != 'S' {
		return false
	}

	return code[1] >= '0' && code[1] <= '9'
}

func sarifHelpMarkdown(item diagnostics.Diagnostic) string {
	parts := []string{}
	if item.Advice != "" {
		parts = append(parts, item.Advice)
	}
	if item.SkillID != "" {
		parts = append(parts, "Skill: `"+item.SkillID+"`")
	}
	if len(item.PrincipleIDs) > 0 {
		parts = append(parts, "ETHOS: `"+strings.Join(item.PrincipleIDs, "`, `")+"`")
	}

	return strings.Join(parts, "\n\n")
}

func sarifLocations(item diagnostics.Diagnostic) []sarifLocation {
	file := sarifArtifactURI(item.File)
	if file == "" {
		return nil
	}

	location := sarifLocation{
		PhysicalLocation: sarifPhysicalLocation{
			ArtifactLocation: sarifArtifactLocation{
				URI: file,
			},
		},
	}
	if item.Line > 0 {
		location.PhysicalLocation.Region.StartLine = item.Line
	}
	if item.Column > 0 {
		location.PhysicalLocation.Region.StartColumn = item.Column
	}

	return []sarifLocation{location}
}

func sarifArtifactURI(file string) string {
	file = strings.TrimSpace(file)
	if file == "" {
		return ""
	}

	return strings.TrimPrefix(strings.ReplaceAll(file, "\\", "/"), "./")
}

func sarifPartialFingerprints(item diagnostics.Diagnostic) map[string]string {
	return map[string]string{
		"coding-ethos/v1": sarifHashStrings(
			sarifRuleID(item),
			sarifArtifactURI(item.File),
			fmt.Sprint(item.Line),
			fmt.Sprint(item.Column),
			item.Message,
		),
		"coding-ethos/stable/v1": sarifHashStrings(
			sarifRuleID(item),
			sarifArtifactURI(item.File),
			item.Code,
			item.PolicyID,
			item.Message,
		),
	}
}

func sarifLevel(severity string) string {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "block", "error", "fatal":
		return "error"
	case "warning", "warn":
		return "warning"
	case "note", "notice", "info", "information":
		return "note"
	default:
		return "warning"
	}
}

func sarifAutomationID(scope string) string {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return "coding-ethos/default"
	}

	replacer := strings.NewReplacer(":", "/", " ", "-", "\\", "/", ",", "-")
	return "coding-ethos/" + strings.Trim(replacer.Replace(scope), "/")
}

func sarifHashStrings(values ...string) string {
	hash := sha256.New()
	for _, value := range values {
		hash.Write([]byte(strings.TrimSpace(value)))
		hash.Write([]byte{0})
	}

	return fmt.Sprintf("%x", hash.Sum(nil))
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}

	return false
}

func limitStrings(values []string, limit int) []string {
	if len(values) <= limit {
		return values
	}

	return values[:limit]
}

func joinSarifID(first string, second string) string {
	if first == "" || second == "" {
		return ""
	}

	return first + ":" + second
}

func firstSarifNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}

	return ""
}
