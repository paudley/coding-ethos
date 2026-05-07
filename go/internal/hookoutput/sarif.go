// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hookoutput

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/agentmsg"
	"blackcat.ca/coding-ethos/go/internal/evidence"
	"blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const (
	sarifLevelError   = "error"
	sarifLevelWarning = "warning"
	sarifSchema       = "https://json.schemastore.org/sarif-2.1.0.json"
	sarifVersion      = "2.1.0"
	sarifRepoURI      = "."
	sarifTagLimit     = 20
)

type sarifLog struct {
	Schema  string
	Version string
	Runs    []sarifRun
}

type sarifRun struct {
	AutomationDetails sarifRunAutomationDetails
	Tool              sarifTool
	Invocations       []sarifInvocation
	Results           []sarifResult
	Properties        sarifRunProperties
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string
	InformationURI string
	Rules          []sarifRule
}

type sarifRule struct {
	ID               string
	Name             string
	ShortDescription sarifMessage
	Help             sarifHelp
	Properties       sarifRuleProperty
}

type sarifRuleProperty struct {
	Precision          string
	SecuritySeverity   string
	PolicyID           string   `json:"policy_id,omitempty"`
	SkillID            string   `json:"skill_id,omitempty"`
	SourceTool         string   `json:"source_tool,omitempty"`
	Implementation     string   `json:"implementation,omitempty"`
	PolicySource       string   `json:"policy_source,omitempty"`
	CELExpression      string   `json:"cel_expression,omitempty"`
	Tags               []string `json:"tags,omitempty"`
	EthosIDs           []string `json:"ethos_ids,omitempty"`
	InputSchemaVersion int64    `json:"input_schema_version,omitempty"`
	CodingEthos        bool     `json:"coding_ethos"`
}

type sarifHelp struct {
	Text     string `json:"text,omitempty"`
	Markdown string `json:"markdown,omitempty"`
}

type sarifResult struct {
	RuleID              string
	RuleIndex           *int
	Level               string
	Message             sarifMessage
	Locations           []sarifLocation
	PartialFingerprints map[string]string
	Properties          sarifResultProperties
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation
	Region           sarifRegion
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine   int
	StartColumn int
	EndLine     int
}

type sarifResultProperties struct {
	sarifResultEvidenceProperties
	sarifResultASTProperties
	sarifResultPolicyProperties
	sarifResultRemediationProperties

	CodingEthos bool `json:"coding_ethos"`
}

type sarifResultEvidenceProperties struct {
	Finding    *evidence.Finding    `json:"finding,omitempty"`
	SourceSpan *evidence.SourceSpan `json:"source_span,omitempty"`
	SkillID    string               `json:"skill_id,omitempty"`
	SearchText string               `json:"search_text,omitempty"`
	Code       string               `json:"code,omitempty"`
	Detail     string               `json:"detail,omitempty"`
	Advice     string               `json:"advice,omitempty"`
}

type sarifResultASTProperties struct {
	ASTNodeKind         string `json:"ast_node_kind,omitempty"`
	ASTParentSymbolPath string `json:"ast_parent_symbol_path,omitempty"`
	ASTSymbolKind       string `json:"ast_symbol_kind,omitempty"`
	ASTSymbolName       string `json:"ast_symbol_name,omitempty"`
	ASTSymbolPath       string `json:"ast_symbol_path,omitempty"`
	ASTLanguage         string `json:"ast_language,omitempty"`
	ASTAction           string `json:"ast_action,omitempty"`
	ASTChangeSource     string `json:"ast_change_source,omitempty"`
}

type sarifResultPolicyProperties struct {
	CodingEthosGroupID        string `json:"coding_ethos_group_id,omitempty"`
	CodingEthosGroupKey       string `json:"coding_ethos_group_key,omitempty"`
	Implementation            string `json:"implementation,omitempty"`
	PolicyID                  string `json:"policy_id,omitempty"`
	SourceTool                string `json:"source_tool,omitempty"`
	PolicySource              string `json:"policy_source,omitempty"`
	CELExpression             string `json:"cel_expression,omitempty"`
	MatchedDiagnosticPolicyID string `json:"matched_diagnostic_policy_id,omitempty"`
	MatchedDiagnosticSeverity string `json:"matched_diagnostic_severity,omitempty"`
	InputSchemaVersion        int64  `json:"input_schema_version,omitempty"`
}

type sarifResultRemediationProperties struct {
	RemediationEvents []evidence.RemediationEvent `json:"remediation_events,omitempty"`
	AgentRemediation  []agentmsg.Remediation      `json:"agent_remediation,omitempty"`
	EthosIDs          []string                    `json:"ethos_ids,omitempty"`
}

type sarifInvocation struct {
	WorkingDirectory    sarifArtifactLocation
	ExecutionSuccessful bool
}

type sarifRunAutomationDetails struct {
	ID string `json:"id,omitempty"`
}

type sarifRunProperties struct {
	Sandbox        *lint.SandboxEvidence `json:"sandbox,omitempty"`
	Scope          string                `json:"scope,omitempty"`
	FindingGroups  []sarifFindingGroup   `json:"finding_groups,omitempty"`
	PolicyCoverage sarifPolicyCoverage   `json:"policy_coverage,omitzero"`
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

type sarifFindingGroup struct {
	ID                 string   `json:"id"`
	Key                string   `json:"key"`
	PolicyID           string   `json:"policy_id,omitempty"`
	SkillID            string   `json:"skill_id,omitempty"`
	File               string   `json:"file,omitempty"`
	SourceTools        []string `json:"source_tools,omitempty"`
	StableFingerprints []string `json:"stable_fingerprints,omitempty"`
	ResultCount        int      `json:"result_count"`
	Line               int      `json:"line,omitempty"`
}

func (log sarifLog) MarshalJSON() ([]byte, error) {
	return marshalSARIFFields("log", map[string]any{
		"$schema": log.Schema,
		"version": log.Version,
		"runs":    log.Runs,
	})
}

func (run sarifRun) MarshalJSON() ([]byte, error) {
	fields := map[string]any{
		"tool":       run.Tool,
		"results":    run.Results,
		"properties": run.Properties,
	}
	if run.AutomationDetails.ID != "" {
		fields["automationDetails"] = run.AutomationDetails
	}

	if len(run.Invocations) > 0 {
		fields["invocations"] = run.Invocations
	}

	return marshalSARIFFields("run", fields)
}

func (driver sarifDriver) MarshalJSON() ([]byte, error) {
	fields := map[string]any{"name": driver.Name}
	if driver.InformationURI != "" {
		fields["informationUri"] = driver.InformationURI
	}

	if len(driver.Rules) > 0 {
		fields["rules"] = driver.Rules
	}

	return marshalSARIFFields("driver", fields)
}

func (rule sarifRule) MarshalJSON() ([]byte, error) {
	fields := map[string]any{"id": rule.ID}
	if rule.Name != "" {
		fields["name"] = rule.Name
	}

	if rule.ShortDescription.Text != "" {
		fields["shortDescription"] = rule.ShortDescription
	}

	if rule.Help.Text != "" || rule.Help.Markdown != "" {
		fields["help"] = rule.Help
	}

	fields["properties"] = rule.Properties

	return marshalSARIFFields("rule", fields)
}

func (properties sarifRuleProperty) MarshalJSON() ([]byte, error) {
	fields := map[string]any{
		"coding_ethos": properties.CodingEthos,
	}
	putString(fields, "precision", properties.Precision)
	putString(fields, "security-severity", properties.SecuritySeverity)
	putString(fields, "policy_id", properties.PolicyID)
	putString(fields, "skill_id", properties.SkillID)
	putString(fields, "source_tool", properties.SourceTool)
	putString(fields, "implementation", properties.Implementation)
	putString(fields, "policy_source", properties.PolicySource)
	putString(fields, "cel_expression", properties.CELExpression)
	putStrings(fields, "tags", properties.Tags)
	putStrings(fields, "ethos_ids", properties.EthosIDs)

	if properties.InputSchemaVersion != 0 {
		fields["input_schema_version"] = properties.InputSchemaVersion
	}

	return marshalSARIFFields("rule properties", fields)
}

func (result sarifResult) MarshalJSON() ([]byte, error) {
	fields := map[string]any{
		"ruleId":     result.RuleID,
		"level":      result.Level,
		"message":    result.Message,
		"properties": result.Properties,
	}
	if result.RuleIndex != nil {
		fields["ruleIndex"] = result.RuleIndex
	}

	if len(result.Locations) > 0 {
		fields["locations"] = result.Locations
	}

	if len(result.PartialFingerprints) > 0 {
		fields["partialFingerprints"] = result.PartialFingerprints
	}

	return marshalSARIFFields("result", fields)
}

func (location sarifLocation) MarshalJSON() ([]byte, error) {
	return marshalSARIFFields("location", map[string]any{
		"physicalLocation": location.PhysicalLocation,
	})
}

func (location sarifPhysicalLocation) MarshalJSON() ([]byte, error) {
	fields := map[string]any{
		"artifactLocation": location.ArtifactLocation,
	}
	if location.Region.StartLine != 0 ||
		location.Region.StartColumn != 0 ||
		location.Region.EndLine != 0 {
		fields["region"] = location.Region
	}

	return marshalSARIFFields("physical location", fields)
}

func (region sarifRegion) MarshalJSON() ([]byte, error) {
	fields := map[string]any{}
	if region.StartLine != 0 {
		fields["startLine"] = region.StartLine
	}

	if region.StartColumn != 0 {
		fields["startColumn"] = region.StartColumn
	}

	if region.EndLine != 0 {
		fields["endLine"] = region.EndLine
	}

	return marshalSARIFFields("region", fields)
}

func (invocation sarifInvocation) MarshalJSON() ([]byte, error) {
	return marshalSARIFFields("invocation", map[string]any{
		"workingDirectory":    invocation.WorkingDirectory,
		"executionSuccessful": invocation.ExecutionSuccessful,
	})
}

func marshalSARIFFields(kind string, fields map[string]any) ([]byte, error) {
	payload, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("marshal SARIF %s: %w", kind, err)
	}

	return payload, nil
}

func putString(fields map[string]any, key, value string) {
	if value != "" {
		fields[key] = value
	}
}

func putStrings(fields map[string]any, key string, values []string) {
	if len(values) > 0 {
		fields[key] = values
	}
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
	findingGroups := sarifFindingGroups(diagnostics)
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
			Results: sarifResults(diagnostics, ruleIndexes, findingGroups),
			Properties: sarifRunProperties{
				Scope:          result.Scope,
				PolicyCoverage: sarifCoverage(result, diagnostics),
				FindingGroups:  findingGroups.Summaries(),
				Sandbox:        sarifSandboxEvidence(result),
			},
		}},
	}

	payload, err := json.MarshalIndent(log, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal SARIF log: %w", err)
	}

	return string(payload), nil
}

func sarifSandboxEvidence(result lint.Result) *lint.SandboxEvidence {
	if result.Capture == nil || result.Capture.Sandbox == nil {
		return nil
	}

	evidence := *result.Capture.Sandbox
	evidence.Command = append([]string(nil), result.Capture.Sandbox.Command...)
	evidence.Tags = append([]string(nil), result.Capture.Sandbox.Tags...)
	evidence.HiddenCredentialDirs = append(
		[]string(nil),
		result.Capture.Sandbox.HiddenCredentialDirs...,
	)
	evidence.ReadPaths = append([]string(nil), result.Capture.Sandbox.ReadPaths...)
	evidence.WritePaths = append([]string(nil), result.Capture.Sandbox.WritePaths...)

	return &evidence
}

func sarifDiagnostics(result lint.Result) []diagnostics.Diagnostic {
	var items []diagnostics.Diagnostic

	switch {
	case len(result.Diagnostics) > 0:
		items = lint.OutputDiagnostics(result)
	case len(result.Findings) > 0:
		items = lint.OutputDiagnostics(result)
	case result.Blocked():
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
	findingGroups sarifFindingGroupIndex,
) []sarifResult {
	results := make([]sarifResult, 0, len(items))
	for _, item := range items {
		ruleID := sarifRuleID(item)
		group := findingGroups.Group(item)
		finding := evidence.FromDiagnostic(item)
		remediation := agentmsg.FromDiagnostics([]diagnostics.Diagnostic{item})
		results = append(results, sarifResult{
			RuleID:              ruleID,
			RuleIndex:           sarifRuleIndex(ruleIndexes, ruleID),
			Level:               sarifLevel(item.Severity),
			Message:             sarifMessage{Text: item.Message},
			Locations:           sarifLocations(item),
			PartialFingerprints: sarifPartialFingerprints(item),
			Properties: sarifResultProperties{
				sarifResultEvidenceProperties: sarifResultEvidence(
					item,
					finding,
				),
				sarifResultASTProperties:    sarifResultAST(item),
				sarifResultPolicyProperties: sarifResultPolicy(item, group),
				sarifResultRemediationProperties: sarifResultRemediation(
					item,
					finding,
					remediation,
				),
				CodingEthos: true,
			},
		})
	}

	return results
}

func sarifResultEvidence(
	item diagnostics.Diagnostic,
	finding evidence.Finding,
) sarifResultEvidenceProperties {
	return sarifResultEvidenceProperties{
		Advice:     item.Advice,
		Code:       item.Code,
		Detail:     item.Detail,
		Finding:    &finding,
		SearchText: finding.SearchText,
		SkillID:    item.SkillID,
		SourceSpan: &finding.SourceSpan,
	}
}

func sarifResultAST(item diagnostics.Diagnostic) sarifResultASTProperties {
	return sarifResultASTProperties{
		ASTAction:           sarifStringMetadata(item, "ast_action"),
		ASTChangeSource:     sarifStringMetadata(item, "ast_change_source"),
		ASTLanguage:         sarifStringMetadata(item, "ast_language"),
		ASTNodeKind:         sarifStringMetadata(item, "ast_node_kind"),
		ASTParentSymbolPath: sarifStringMetadata(item, "ast_parent_symbol_path"),
		ASTSymbolKind:       sarifStringMetadata(item, "ast_symbol_kind"),
		ASTSymbolName:       sarifStringMetadata(item, "ast_symbol_name"),
		ASTSymbolPath:       sarifStringMetadata(item, "ast_symbol_path"),
	}
}

func sarifResultPolicy(
	item diagnostics.Diagnostic,
	group sarifFindingGroup,
) sarifResultPolicyProperties {
	return sarifResultPolicyProperties{
		CELExpression:       sarifStringMetadata(item, "when"),
		CodingEthosGroupID:  group.ID,
		CodingEthosGroupKey: group.Key,
		Implementation:      sarifStringMetadata(item, "implementation"),
		InputSchemaVersion:  sarifIntMetadata(item, "input_schema_version"),
		MatchedDiagnosticPolicyID: sarifStringMetadata(
			item,
			"matched_diagnostic_policy_id",
		),
		MatchedDiagnosticSeverity: sarifStringMetadata(
			item,
			"matched_diagnostic_severity",
		),
		PolicyID:     item.PolicyID,
		PolicySource: sarifStringMetadata(item, "policy_source"),
		SourceTool:   item.Tool,
	}
}

func sarifResultRemediation(
	item diagnostics.Diagnostic,
	finding evidence.Finding,
	remediation []agentmsg.Remediation,
) sarifResultRemediationProperties {
	return sarifResultRemediationProperties{
		AgentRemediation: remediation,
		EthosIDs:         append([]string(nil), item.PrincipleIDs...),
		RemediationEvents: evidence.RemediationEvents(
			remediation,
			[]evidence.Finding{finding},
			"",
			"suggested",
		),
	}
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

	return limitStrings(tags, sarifTagLimit)
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
	case sarifLevelError:
		return "8.0"
	case sarifLevelWarning:
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

	if endLine := int(sarifIntMetadata(item, "ast_end_line")); endLine > item.Line {
		location.PhysicalLocation.Region.EndLine = endLine
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
	astIdentity := sarifASTIdentity(item)
	locationSeed := []string{
		sarifRuleID(item),
		sarifArtifactURI(item.File),
		strconv.Itoa(item.Line),
		strconv.Itoa(item.Column),
		item.Message,
	}
	stableSeed := []string{
		sarifRuleID(item),
		sarifArtifactURI(item.File),
		item.Code,
		item.PolicyID,
		item.Message,
	}

	if astIdentity != "" {
		locationSeed = append(locationSeed, astIdentity)
		stableSeed = append(stableSeed, astIdentity)
	}

	fingerprints := map[string]string{
		"coding-ethos/v1":         sarifHashStrings(locationSeed...),
		"coding-ethos/stable/v1":  sarifHashStrings(stableSeed...),
		"coding-ethos/finding/v1": evidence.FromDiagnostic(item).ID,
	}
	if astIdentity != "" {
		fingerprints["coding-ethos/ast/v1"] = sarifHashStrings(
			sarifRuleID(item),
			sarifArtifactURI(item.File),
			astIdentity,
		)
	}

	return fingerprints
}

func sarifASTIdentity(item diagnostics.Diagnostic) string {
	if sarifStringMetadata(item, "ast_node_kind") == "" {
		return ""
	}

	parts := []string{
		sarifStringMetadata(item, "ast_change_source"),
		sarifStringMetadata(item, "ast_language"),
		sarifStringMetadata(item, "ast_node_kind"),
		sarifStringMetadata(item, "ast_symbol_kind"),
		sarifStringMetadata(item, "ast_symbol_path"),
		sarifStringMetadata(item, "ast_parent_symbol_path"),
	}

	return strings.Join(parts, "\x00")
}

type sarifFindingGroupIndex map[string]*sarifFindingGroupAccumulator

type sarifFindingGroupAccumulator struct {
	sourceTools        map[string]bool
	stableFingerprints map[string]bool
	summary            sarifFindingGroup
}

func sarifFindingGroups(items []diagnostics.Diagnostic) sarifFindingGroupIndex {
	groups := sarifFindingGroupIndex{}

	for _, item := range items {
		key := sarifFindingGroupKey(item)
		if key == "" {
			continue
		}

		group, ok := groups[key]
		if !ok {
			group = &sarifFindingGroupAccumulator{
				sourceTools:        map[string]bool{},
				stableFingerprints: map[string]bool{},
				summary: sarifFindingGroup{
					ID:       sarifHashStrings("finding-group", key),
					Key:      key,
					PolicyID: item.PolicyID,
					SkillID:  item.SkillID,
					File:     sarifArtifactURI(item.File),
					Line:     item.Line,
				},
			}
			groups[key] = group
		}

		group.summary.ResultCount++
		sarifAddCoverageValue(group.sourceTools, item.Tool)

		if stable := sarifPartialFingerprints(item)["coding-ethos/stable/v1"]; stable != "" {
			sarifAddCoverageValue(group.stableFingerprints, stable)
		}
	}

	return groups
}

func (groups sarifFindingGroupIndex) Group(
	item diagnostics.Diagnostic,
) sarifFindingGroup {
	key := sarifFindingGroupKey(item)
	if key == "" {
		return sarifFindingGroup{}
	}

	group, ok := groups[key]
	if !ok {
		return sarifFindingGroup{}
	}

	return group.withSortedValues()
}

func (groups sarifFindingGroupIndex) Summaries() []sarifFindingGroup {
	summaries := make([]sarifFindingGroup, 0, len(groups))
	for _, group := range groups {
		summaries = append(summaries, group.withSortedValues())
	}

	sort.Slice(summaries, func(left, right int) bool {
		return summaries[left].Key < summaries[right].Key
	})

	return summaries
}

func (group *sarifFindingGroupAccumulator) withSortedValues() sarifFindingGroup {
	summary := group.summary
	summary.SourceTools = sarifSortedKeys(group.sourceTools)
	summary.StableFingerprints = sarifSortedKeys(group.stableFingerprints)

	return summary
}

func sarifFindingGroupKey(item diagnostics.Diagnostic) string {
	file := sarifArtifactURI(item.File)
	if file == "" {
		return ""
	}

	return strings.Join([]string{
		firstSarifNonEmpty(item.PolicyID, sarifRuleID(item)),
		item.SkillID,
		file,
		strconv.Itoa(item.Line),
	}, "|")
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
	case sarifLevelWarning, "warn":
		return sarifLevelWarning
	case "note", "notice", "info", "information":
		return "note"
	default:
		return sarifLevelWarning
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

	return hex.EncodeToString(hash.Sum(nil))
}

func containsString(values []string, needle string) bool {
	return slices.Contains(values, needle)
}

func limitStrings(values []string, limit int) []string {
	if len(values) <= limit {
		return values
	}

	return values[:limit]
}

func joinSarifID(first, second string) string {
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
