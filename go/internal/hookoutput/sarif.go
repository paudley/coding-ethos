// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hookoutput

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/policy"
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
	Tags               []string `json:"tags,omitempty"`
	Precision          string   `json:"precision,omitempty"`
	SecuritySeverity   string   `json:"security-severity,omitempty"`
	PolicyID           string   `json:"policy_id,omitempty"`
	SkillID            string   `json:"skill_id,omitempty"`
	SourceTool         string   `json:"source_tool,omitempty"`
	Implementation     string   `json:"implementation,omitempty"`
	InputSchemaVersion int64    `json:"input_schema_version,omitempty"`
	PolicySource       string   `json:"policy_source,omitempty"`
	CELExpression      string   `json:"cel_expression,omitempty"`
	EthosIDs           []string `json:"ethos_ids,omitempty"`
	CodingEthos        bool     `json:"coding_ethos"`
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
	Advice                    string   `json:"advice,omitempty"`
	Code                      string   `json:"code,omitempty"`
	Detail                    string   `json:"detail,omitempty"`
	PolicyID                  string   `json:"policy_id,omitempty"`
	SkillID                   string   `json:"skill_id,omitempty"`
	SourceTool                string   `json:"source_tool,omitempty"`
	Implementation            string   `json:"implementation,omitempty"`
	InputSchemaVersion        int64    `json:"input_schema_version,omitempty"`
	PolicySource              string   `json:"policy_source,omitempty"`
	CELExpression             string   `json:"cel_expression,omitempty"`
	MatchedDiagnosticPolicyID string   `json:"matched_diagnostic_policy_id,omitempty"`
	MatchedDiagnosticSeverity string   `json:"matched_diagnostic_severity,omitempty"`
	EthosIDs                  []string `json:"ethos_ids,omitempty"`
	CodingEthos               bool     `json:"coding_ethos"`
}

type sarifInvocation struct {
	WorkingDirectory    sarifArtifactLocation `json:"workingDirectory,omitempty"`
	ExecutionSuccessful bool                  `json:"executionSuccessful"`
}

type sarifRunAutomationDetails struct {
	ID string `json:"id,omitempty"`
}

type sarifRunProperties struct {
	Scope          string              `json:"scope,omitempty"`
	PolicyCoverage sarifPolicyCoverage `json:"policy_coverage,omitempty"`
}

type sarifPolicyCoverage struct {
	Policies        []string `json:"policies,omitempty"`
	EthosIDs        []string `json:"ethos_ids,omitempty"`
	Skills          []string `json:"skills,omitempty"`
	Tools           []string `json:"tools,omitempty"`
	PolicyCount     int      `json:"policy_count,omitempty"`
	EthosCount      int      `json:"ethos_count,omitempty"`
	SkillCount      int      `json:"skill_count,omitempty"`
	ToolCount       int      `json:"tool_count,omitempty"`
	ResultCount     int      `json:"result_count,omitempty"`
	DecisionCount   int      `json:"decision_count,omitempty"`
	DiagnosticCount int      `json:"diagnostic_count,omitempty"`
}

type SARIFOptions struct {
	Category string
}

func FormatLintResultSARIF(result lint.Result) (string, error) {
	return FormatLintResultSARIFWithOptions(result, SARIFOptions{})
}

func FormatLintResultSARIFWithOptions(
	result lint.Result,
	options SARIFOptions,
) (string, error) {
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
				ID: sarifAutomationID(result.Scope, options),
			},
			Results: sarifResults(diagnostics, ruleIndexes),
			Properties: sarifRunProperties{
				Scope:          result.Scope,
				PolicyCoverage: sarifCoverage(result, diagnostics),
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
	var items []diagnostics.Diagnostic
	if len(result.Diagnostics) > 0 {
		items = lint.OutputDiagnostics(result)
	} else if len(result.Findings) > 0 && result.Blocked() {
		items = lint.OutputDiagnostics(result)
	} else if result.Blocked() {
		items = lint.OutputDiagnostics(result)
	}

	locatable := make([]diagnostics.Diagnostic, 0, len(items))
	for _, item := range items {
		if sarifArtifactURI(item.File) == "" {
			continue
		}
		locatable = append(locatable, item)
	}

	return locatable
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
				Tags:               sarifRuleTags(item),
				Precision:          sarifPrecision(item),
				SecuritySeverity:   sarifSecuritySeverity(item),
				PolicyID:           item.PolicyID,
				SkillID:            item.SkillID,
				SourceTool:         item.Tool,
				Implementation:     sarifStringMetadata(item, "implementation"),
				InputSchemaVersion: sarifIntMetadata(item, "input_schema_version"),
				PolicySource:       sarifStringMetadata(item, "policy_source"),
				CELExpression:      sarifStringMetadata(item, "when"),
				EthosIDs:           append([]string(nil), item.PrincipleIDs...),
				CodingEthos:        true,
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
				Advice:                    item.Advice,
				Code:                      item.Code,
				Detail:                    item.Detail,
				PolicyID:                  item.PolicyID,
				SkillID:                   item.SkillID,
				SourceTool:                item.Tool,
				Implementation:            sarifStringMetadata(item, "implementation"),
				InputSchemaVersion:        sarifIntMetadata(item, "input_schema_version"),
				PolicySource:              sarifStringMetadata(item, "policy_source"),
				CELExpression:             sarifStringMetadata(item, "when"),
				MatchedDiagnosticPolicyID: sarifStringMetadata(item, "matched_diagnostic_policy_id"),
				MatchedDiagnosticSeverity: sarifStringMetadata(item, "matched_diagnostic_severity"),
				EthosIDs:                  append([]string(nil), item.PrincipleIDs...),
				CodingEthos:               true,
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

func sarifCoverage(
	result lint.Result,
	items []diagnostics.Diagnostic,
) sarifPolicyCoverage {
	policies := map[string]bool{}
	ethosIDs := map[string]bool{}
	skills := map[string]bool{}
	tools := map[string]bool{}

	for _, decision := range result.Decisions {
		sarifAddPolicyCoverageDecision(policies, ethosIDs, skills, tools, decision)
	}
	for _, item := range items {
		sarifAddCoverageValue(policies, item.PolicyID)
		sarifAddCoverageValue(skills, item.SkillID)
		sarifAddCoverageValue(tools, item.Tool)
		for _, principleID := range item.PrincipleIDs {
			sarifAddCoverageValue(ethosIDs, principleID)
		}
	}

	return sarifPolicyCoverage{
		Policies:        sarifSortedKeys(policies),
		EthosIDs:        sarifSortedKeys(ethosIDs),
		Skills:          sarifSortedKeys(skills),
		Tools:           sarifSortedKeys(tools),
		PolicyCount:     len(policies),
		EthosCount:      len(ethosIDs),
		SkillCount:      len(skills),
		ToolCount:       len(tools),
		ResultCount:     len(items),
		DecisionCount:   len(result.Decisions),
		DiagnosticCount: len(result.Diagnostics),
	}
}

func sarifAddPolicyCoverageDecision(
	policies map[string]bool,
	ethosIDs map[string]bool,
	skills map[string]bool,
	tools map[string]bool,
	decision policy.Decision,
) {
	sarifAddCoverageValue(policies, decision.PolicyID)
	for _, principleID := range decision.PrincipleIDs {
		sarifAddCoverageValue(ethosIDs, principleID)
	}
	for _, diagnostic := range decision.Diagnostics {
		sarifAddCoverageValue(policies, diagnostic.PolicyID)
		sarifAddCoverageValue(skills, diagnostic.SkillID)
		sarifAddCoverageValue(tools, diagnostic.Tool)
		for _, principleID := range diagnostic.PrincipleIDs {
			sarifAddCoverageValue(ethosIDs, principleID)
		}
	}
}

func sarifAddCoverageValue(values map[string]bool, value string) {
	value = strings.TrimSpace(value)
	if value != "" {
		values[value] = true
	}
}

func sarifSortedKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for value := range values {
		keys = append(keys, value)
	}
	sort.Strings(keys)

	return keys
}

func sarifStringMetadata(item diagnostics.Diagnostic, key string) string {
	value, ok := item.Metadata[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	default:
		return ""
	}
}

func sarifIntMetadata(item diagnostics.Diagnostic, key string) int64 {
	value, ok := item.Metadata[key]
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case int32:
		return int64(typed)
	case float64:
		return int64(typed)
	default:
		return 0
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

func sarifAutomationID(scope string, options SARIFOptions) string {
	if category := sarifAutomationCategory(options.Category); category != "" {
		return category + "/"
	}

	scope = strings.TrimSpace(scope)
	if scope == "" {
		return "coding-ethos/default"
	}

	replacer := strings.NewReplacer(":", "/", " ", "-", "\\", "/", ",", "-")
	return "coding-ethos/" + strings.Trim(replacer.Replace(scope), "/")
}

func sarifAutomationCategory(category string) string {
	category = strings.TrimSpace(category)
	category = strings.Trim(category, "/")

	replacer := strings.NewReplacer("\\", "/", " ", "-", ",", "-")
	return strings.Trim(replacer.Replace(category), "/")
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
