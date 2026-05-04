// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type sarifLog struct {
	Runs []sarifInputRun `json:"runs"`
}

type sarifInputRun struct {
	Tool struct {
		Driver struct {
			Name  string           `json:"name,omitempty"`
			Rules []sarifInputRule `json:"rules,omitempty"`
		} `json:"driver"`
	} `json:"tool"`
	AutomationDetails struct {
		ID   string `json:"id,omitempty"`
		GUID string `json:"guid,omitempty"`
	} `json:"automationDetails,omitempty"`
	Properties struct {
		Scope string `json:"scope,omitempty"`
	} `json:"properties,omitempty"`
	Results      []sarifInputResult `json:"results"`
	BaselineGUID string             `json:"baselineGuid,omitempty"`
}

type sarifInputRule struct {
	ID         string               `json:"id"`
	Properties sarifInputProperties `json:"properties,omitempty"`
}

type sarifInputResult struct {
	Message struct {
		Text string `json:"text,omitempty"`
	} `json:"message"`
	Locations           []sarifInputLocation `json:"locations,omitempty"`
	PartialFingerprints map[string]string    `json:"partialFingerprints,omitempty"`
	Properties          sarifInputProperties `json:"properties,omitempty"`
	RuleID              string               `json:"ruleId"`
	Level               string               `json:"level,omitempty"`
	RuleIndex           *int                 `json:"ruleIndex,omitempty"`
	raw                 json.RawMessage
}

type sarifInputLocation struct {
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

type sarifInputProperties struct {
	Finding *struct {
		ID            string   `json:"id,omitempty"`
		PolicyID      string   `json:"policy_id,omitempty"`
		SkillID       string   `json:"skill_id,omitempty"`
		EvaluatorKind string   `json:"evaluator_kind,omitempty"`
		SearchText    string   `json:"search_text,omitempty"`
		PrincipleIDs  []string `json:"principle_ids,omitempty"`
	} `json:"finding,omitempty"`
	AgentRemediation []struct {
		ID       string `json:"id,omitempty"`
		PolicyID string `json:"policy_id,omitempty"`
		SkillID  string `json:"skill_id,omitempty"`
		Message  string `json:"message,omitempty"`
		Advice   string `json:"advice,omitempty"`
		File     string `json:"file,omitempty"`
		Path     string `json:"path,omitempty"`
	} `json:"agent_remediation,omitempty"`
	Advice         string   `json:"advice,omitempty"`
	ASTLanguage    string   `json:"ast_language,omitempty"`
	ASTNodeKind    string   `json:"ast_node_kind,omitempty"`
	ASTSymbolKind  string   `json:"ast_symbol_kind,omitempty"`
	ASTSymbolName  string   `json:"ast_symbol_name,omitempty"`
	ASTSymbolPath  string   `json:"ast_symbol_path,omitempty"`
	CELExpression  string   `json:"cel_expression,omitempty"`
	Code           string   `json:"code,omitempty"`
	EthosIDs       []string `json:"ethos_ids,omitempty"`
	Implementation string   `json:"implementation,omitempty"`
	PolicyID       string   `json:"policy_id,omitempty"`
	PolicySource   string   `json:"policy_source,omitempty"`
	SearchText     string   `json:"search_text,omitempty"`
	SkillID        string   `json:"skill_id,omitempty"`
	SourceTool     string   `json:"source_tool,omitempty"`
}

func (ingester TraceIngester) IngestSARIF(ctx context.Context, sourcePath string, payload []byte) error {
	runs, err := DecodeSARIFRuns(sourcePath, payload)
	if err != nil {
		return err
	}
	for _, run := range runs {
		if err := ingester.store.IngestSARIFRun(ctx, run); err != nil {
			return err
		}
	}

	return nil
}

func DecodeSARIFRun(sourcePath string, payload []byte) (SARIFRun, error) {
	runs, err := DecodeSARIFRuns(sourcePath, payload)
	if err != nil {
		return SARIFRun{}, err
	}

	return runs[0], nil
}

func DecodeSARIFRuns(sourcePath string, payload []byte) ([]SARIFRun, error) {
	var log sarifLog
	if err := json.Unmarshal(payload, &log); err != nil {
		return nil, fmt.Errorf("decode SARIF %q: %w", sourcePath, err)
	}
	if len(log.Runs) == 0 {
		return nil, fmt.Errorf("SARIF %q has no runs", sourcePath)
	}

	runs := make([]SARIFRun, 0, len(log.Runs))
	for index, input := range log.Runs {
		run, err := decodeSARIFRun(sourcePath, payload, index, input)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}

	return runs, nil
}

func decodeSARIFRun(
	sourcePath string,
	payload []byte,
	runIndex int,
	input sarifInputRun,
) (SARIFRun, error) {
	rawRun, err := json.Marshal(input)
	if err != nil {
		return SARIFRun{}, fmt.Errorf("marshal SARIF run %q: %w", sourcePath, err)
	}
	run := SARIFRun{
		ID: stableID(
			"sarif-run",
			sourcePath,
			fmt.Sprintf("%d", runIndex),
			input.Tool.Driver.Name,
			input.AutomationDetails.ID,
			string(rawRun),
		),
		SourcePath:   sourcePath,
		Category:     input.Properties.Scope,
		ToolName:     input.Tool.Driver.Name,
		AutomationID: input.AutomationDetails.ID,
		RunGUID:      input.AutomationDetails.GUID,
		BaselineGUID: input.BaselineGUID,
		Raw:          payload,
	}
	rules := map[string]sarifInputProperties{}
	for _, rule := range input.Tool.Driver.Rules {
		rules[rule.ID] = rule.Properties
	}
	for index, result := range input.Results {
		rawResult, err := json.Marshal(result)
		if err != nil {
			return SARIFRun{}, fmt.Errorf("marshal SARIF result %d: %w", index, err)
		}
		result.raw = rawResult
		run.Results = append(run.Results, sarifResultReference(run.ID, index, result, rules[result.RuleID]))
	}

	return run, nil
}

func sarifResultReference(
	runID string,
	index int,
	result sarifInputResult,
	ruleProperties sarifInputProperties,
) SARIFResultReference {
	properties := mergeSARIFInputProperties(ruleProperties, result.Properties)
	location := firstSARIFInputLocation(result.Locations)
	fingerprint := firstSARIFInputFingerprint(result.PartialFingerprints)
	remediationID := ""
	if len(properties.AgentRemediation) > 0 {
		remediationID = strings.TrimSpace(properties.AgentRemediation[0].ID)
	}
	findingID := ""
	if properties.Finding != nil {
		findingID = strings.TrimSpace(properties.Finding.ID)
	}
	searchText := firstNonEmpty(
		properties.SearchText,
		sarifReferenceSearchText(result, properties, location.URI),
	)
	reference := SARIFResultReference{
		ID:            stableID("sarif-result", runID, result.RuleID, fingerprint, location.URI, fmt.Sprintf("%d", index)),
		RuleID:        strings.TrimSpace(result.RuleID),
		Level:         strings.TrimSpace(result.Level),
		Message:       strings.TrimSpace(result.Message.Text),
		Fingerprint:   fingerprint,
		FindingID:     findingID,
		RemediationID: remediationID,
		PolicyID:      strings.TrimSpace(properties.PolicyID),
		SkillID:       strings.TrimSpace(properties.SkillID),
		PrincipleIDs:  compactStrings(properties.EthosIDs),
		Path:          strings.TrimSpace(location.URI),
		ASTLanguage:   strings.TrimSpace(properties.ASTLanguage),
		ASTNodeKind:   strings.TrimSpace(properties.ASTNodeKind),
		ASTSymbolKind: strings.TrimSpace(properties.ASTSymbolKind),
		ASTSymbolName: strings.TrimSpace(properties.ASTSymbolName),
		ASTSymbolPath: strings.TrimSpace(properties.ASTSymbolPath),
		EvaluatorKind: strings.TrimSpace(properties.Implementation),
		CELExpression: strings.TrimSpace(properties.CELExpression),
		PolicySource:  strings.TrimSpace(properties.PolicySource),
		SearchText:    searchText,
		StartLine:     location.StartLine,
		StartColumn:   location.StartColumn,
		Raw:           result.raw,
	}
	if properties.Finding != nil {
		reference.PolicyID = firstNonEmpty(reference.PolicyID, properties.Finding.PolicyID)
		reference.SkillID = firstNonEmpty(reference.SkillID, properties.Finding.SkillID)
		reference.EvaluatorKind = firstNonEmpty(reference.EvaluatorKind, properties.Finding.EvaluatorKind)
		reference.SearchText = firstNonEmpty(reference.SearchText, properties.Finding.SearchText)
		reference.PrincipleIDs = compactStrings(append(reference.PrincipleIDs, properties.Finding.PrincipleIDs...))
	}
	reference.CELPolicyID = firstNonEmpty(reference.CELPolicyID, reference.PolicyID)

	return reference
}

type sarifInputLocationValue struct {
	URI         string
	StartLine   int
	StartColumn int
}

func firstSARIFInputLocation(locations []sarifInputLocation) sarifInputLocationValue {
	if len(locations) == 0 {
		return sarifInputLocationValue{}
	}
	location := locations[0].PhysicalLocation

	return sarifInputLocationValue{
		URI:         location.ArtifactLocation.URI,
		StartLine:   location.Region.StartLine,
		StartColumn: location.Region.StartColumn,
	}
}

func firstSARIFInputFingerprint(fingerprints map[string]string) string {
	for _, key := range []string{
		"coding-ethos/finding/v1",
		"coding-ethos/group/v1",
		"primaryLocationLineHash",
	} {
		if value := strings.TrimSpace(fingerprints[key]); value != "" {
			return value
		}
	}
	for _, value := range fingerprints {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}

	return ""
}

func mergeSARIFInputProperties(
	rule sarifInputProperties,
	result sarifInputProperties,
) sarifInputProperties {
	merged := rule
	merged.Advice = firstNonEmpty(result.Advice, merged.Advice)
	merged.ASTLanguage = firstNonEmpty(result.ASTLanguage, merged.ASTLanguage)
	merged.ASTNodeKind = firstNonEmpty(result.ASTNodeKind, merged.ASTNodeKind)
	merged.ASTSymbolKind = firstNonEmpty(result.ASTSymbolKind, merged.ASTSymbolKind)
	merged.ASTSymbolName = firstNonEmpty(result.ASTSymbolName, merged.ASTSymbolName)
	merged.ASTSymbolPath = firstNonEmpty(result.ASTSymbolPath, merged.ASTSymbolPath)
	merged.CELExpression = firstNonEmpty(result.CELExpression, merged.CELExpression)
	merged.Code = firstNonEmpty(result.Code, merged.Code)
	merged.Implementation = firstNonEmpty(result.Implementation, merged.Implementation)
	merged.PolicyID = firstNonEmpty(result.PolicyID, merged.PolicyID)
	merged.PolicySource = firstNonEmpty(result.PolicySource, merged.PolicySource)
	merged.SearchText = firstNonEmpty(result.SearchText, merged.SearchText)
	merged.SkillID = firstNonEmpty(result.SkillID, merged.SkillID)
	merged.SourceTool = firstNonEmpty(result.SourceTool, merged.SourceTool)
	merged.EthosIDs = append(append([]string{}, merged.EthosIDs...), result.EthosIDs...)
	if result.Finding != nil {
		merged.Finding = result.Finding
	}
	if len(result.AgentRemediation) > 0 {
		merged.AgentRemediation = result.AgentRemediation
	}

	return merged
}

func sarifReferenceSearchText(
	result sarifInputResult,
	properties sarifInputProperties,
	path string,
) string {
	remediationText := ""
	if len(properties.AgentRemediation) > 0 {
		remediationText = strings.Join(compactStrings([]string{
			properties.AgentRemediation[0].Message,
			properties.AgentRemediation[0].Advice,
			properties.AgentRemediation[0].File,
			properties.AgentRemediation[0].Path,
		}), "\n")
	}

	return strings.Join(compactStrings([]string{
		result.RuleID,
		result.Level,
		result.Message.Text,
		properties.PolicyID,
		properties.SkillID,
		properties.SourceTool,
		properties.Code,
		properties.Advice,
		properties.CELExpression,
		properties.PolicySource,
		path,
		remediationText,
	}), "\n")
}
