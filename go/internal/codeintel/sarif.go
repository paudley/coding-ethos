// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
)

type sarifLog struct {
	Runs []sarifInputRun `json:"runs"`
}

type sarifInputRun struct {
	AutomationDetails sarifInputAutomationDetails
	Properties        sarifInputRunProperties
	BaselineGUID      string
	Tool              sarifInputTool
	Results           []sarifInputResult
	raw               json.RawMessage
}

type sarifInputAutomationDetails struct {
	ID   string `json:"id,omitempty"`
	GUID string `json:"guid,omitempty"`
}

type sarifInputRunProperties struct {
	Scope string `json:"scope,omitempty"`
}

type sarifInputTool struct {
	Driver sarifInputDriver `json:"driver,omitzero"`
}

type sarifInputDriver struct {
	Name  string           `json:"name,omitempty"`
	Rules []sarifInputRule `json:"rules,omitempty"`
}

type sarifInputRule struct {
	ID         string               `json:"id"`
	Properties sarifInputProperties `json:"properties,omitzero"`
}

type sarifInputResult struct {
	Message             sarifInputMessage
	Locations           []sarifInputLocation
	PartialFingerprints map[string]string
	Properties          sarifInputProperties
	RuleID              string
	Level               string
	RuleIndex           *int
	raw                 json.RawMessage
}

type sarifInputMessage struct {
	Text string `json:"text,omitempty"`
}

type sarifInputLocation struct {
	PhysicalLocation sarifInputPhysicalLocation
}

type sarifInputPhysicalLocation struct {
	ArtifactLocation sarifInputArtifactLocation
	Region           sarifInputRegion
}

type sarifInputArtifactLocation struct {
	URI string `json:"uri,omitempty"`
}

type sarifInputRegion struct {
	StartLine   int
	StartColumn int
}

type sarifInputProperties struct {
	Finding          *sarifInputFinding `json:"finding,omitempty"`
	CELExpression    string             `json:"cel_expression,omitempty"`
	Implementation   string             `json:"implementation,omitempty"`
	ProxyEventID     string             `json:"proxy_event_id,omitempty"`
	ProxySessionID   string             `json:"proxy_session_id,omitempty"`
	ProxyEventKind   string             `json:"proxy_event_kind,omitempty"`
	ProxyDirection   string             `json:"proxy_direction,omitempty"`
	ProxyPayloadKind string             `json:"proxy_payload_kind,omitempty"`
	ProxyTraceID     string             `json:"proxy_trace_id,omitempty"`
	ProxyTrackingID  string             `json:"proxy_tracking_id,omitempty"`
	ProxyTransform   string             `json:"proxy_transform,omitempty"`
	ASTLanguage      string             `json:"ast_language,omitempty"`
	ASTNodeKind      string             `json:"ast_node_kind,omitempty"`
	ASTSymbolKind    string             `json:"ast_symbol_kind,omitempty"`
	ASTSymbolName    string             `json:"ast_symbol_name,omitempty"`
	Advice           string             `json:"advice,omitempty"`
	SourceTool       string             `json:"source_tool,omitempty"`
	Code             string             `json:"code,omitempty"`
	SkillID          string             `json:"skill_id,omitempty"`
	ASTSymbolPath    string             `json:"ast_symbol_path,omitempty"`
	PolicyID         string             `json:"policy_id,omitempty"`
	PolicySource     string             `json:"policy_source,omitempty"`
	SearchText       string             `json:"search_text,omitempty"`
	AgentRemediation []struct {
		ID       string `json:"id,omitempty"`
		PolicyID string `json:"policy_id,omitempty"`
		SkillID  string `json:"skill_id,omitempty"`
		Message  string `json:"message,omitempty"`
		Advice   string `json:"advice,omitempty"`
		File     string `json:"file,omitempty"`
		Path     string `json:"path,omitempty"`
	} `json:"agent_remediation,omitempty"`
	EthosIDs []string `json:"ethos_ids,omitempty"`
}

type sarifInputFinding struct {
	ID            string   `json:"id,omitempty"`
	PolicyID      string   `json:"policy_id,omitempty"`
	SkillID       string   `json:"skill_id,omitempty"`
	EvaluatorKind string   `json:"evaluator_kind,omitempty"`
	SearchText    string   `json:"search_text,omitempty"`
	PrincipleIDs  []string `json:"principle_ids,omitempty"`
}

func (run *sarifInputRun) UnmarshalJSON(payload []byte) error {
	run.raw = append(run.raw[:0], payload...)

	fields, err := decodeJSONFields(payload)
	if err != nil {
		return fmt.Errorf("decode SARIF run fields: %w", err)
	}

	return decodeOptionalJSONFields(fields, []jsonFieldTarget{
		{key: "automationDetails", target: &run.AutomationDetails},
		{key: "properties", target: &run.Properties},
		{key: "baselineGuid", target: &run.BaselineGUID},
		{key: "tool", target: &run.Tool},
		{key: "results", target: &run.Results},
	})
}

func (result *sarifInputResult) UnmarshalJSON(payload []byte) error {
	result.raw = append(result.raw[:0], payload...)

	fields, err := decodeJSONFields(payload)
	if err != nil {
		return fmt.Errorf("decode SARIF result fields: %w", err)
	}

	return decodeOptionalJSONFields(fields, []jsonFieldTarget{
		{key: "message", target: &result.Message},
		{key: "locations", target: &result.Locations},
		{key: "partialFingerprints", target: &result.PartialFingerprints},
		{key: "properties", target: &result.Properties},
		{key: "ruleId", target: &result.RuleID},
		{key: "level", target: &result.Level},
		{key: "ruleIndex", target: &result.RuleIndex},
	})
}

func (location *sarifInputLocation) UnmarshalJSON(payload []byte) error {
	fields, err := decodeJSONFields(payload)
	if err != nil {
		return fmt.Errorf("decode SARIF location fields: %w", err)
	}

	return decodeOptionalJSONFields(fields, []jsonFieldTarget{
		{key: "physicalLocation", target: &location.PhysicalLocation},
	})
}

func (location *sarifInputPhysicalLocation) UnmarshalJSON(payload []byte) error {
	fields, err := decodeJSONFields(payload)
	if err != nil {
		return fmt.Errorf("decode SARIF physical location fields: %w", err)
	}

	return decodeOptionalJSONFields(fields, []jsonFieldTarget{
		{key: "artifactLocation", target: &location.ArtifactLocation},
		{key: "region", target: &location.Region},
	})
}

func (region *sarifInputRegion) UnmarshalJSON(payload []byte) error {
	fields, err := decodeJSONFields(payload)
	if err != nil {
		return fmt.Errorf("decode SARIF region fields: %w", err)
	}

	return decodeOptionalJSONFields(fields, []jsonFieldTarget{
		{key: "startLine", target: &region.StartLine},
		{key: "startColumn", target: &region.StartColumn},
	})
}

func decodeJSONFields(payload []byte) (map[string]json.RawMessage, error) {
	fields := map[string]json.RawMessage{}

	err := json.Unmarshal(payload, &fields)
	if err != nil {
		return nil, fmt.Errorf("decode JSON object: %w", err)
	}

	return fields, nil
}

type jsonFieldTarget struct {
	target any
	key    string
}

func decodeOptionalJSONFields(
	fields map[string]json.RawMessage,
	targets []jsonFieldTarget,
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

func (ingester TraceIngester) IngestSARIF(
	ctx context.Context,
	sourcePath string,
	payload []byte,
) error {
	runs, err := DecodeSARIFRuns(sourcePath, payload)
	if err != nil {
		return err
	}

	for _, run := range runs {
		err := ingester.store.IngestSARIFRun(ctx, run)
		if err != nil {
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

	err := json.Unmarshal(payload, &log)
	if err != nil {
		return nil, fmt.Errorf("decode SARIF %q: %w", sourcePath, err)
	}

	if len(log.Runs) == 0 {
		return nil, apperror.Wrapf(
			apperror.StaticError("SARIF %q has no runs"),
			"SARIF %q has no runs",
			sourcePath,
		)
	}

	runs := make([]SARIFRun, 0, len(log.Runs))
	for index, input := range log.Runs {
		run := decodeSARIFRun(sourcePath, payload, index, input)
		runs = append(runs, run)
	}

	return runs, nil
}

func decodeSARIFRun(
	sourcePath string,
	payload []byte,
	runIndex int,
	input sarifInputRun,
) SARIFRun {
	run := SARIFRun{
		ID: stableID(
			"sarif-run",
			sourcePath,
			strconv.Itoa(runIndex),
			input.Tool.Driver.Name,
			input.AutomationDetails.ID,
			string(input.raw),
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
		run.Results = append(
			run.Results,
			sarifResultReference(run.ID, index, result, rules[result.RuleID]),
		)
	}

	return run
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

	reference := baseSARIFResultReference(
		runID,
		index,
		result,
		properties,
		location,
		fingerprint,
		searchText,
		findingID,
		remediationID,
	)
	if properties.Finding != nil {
		mergeSARIFResultFinding(&reference, properties.Finding)
	}

	reference.CELPolicyID = firstNonEmpty(reference.CELPolicyID, reference.PolicyID)

	return reference
}

func baseSARIFResultReference(
	runID string,
	index int,
	result sarifInputResult,
	properties sarifInputProperties,
	location sarifInputLocationValue,
	fingerprint string,
	searchText string,
	findingID string,
	remediationID string,
) SARIFResultReference {
	return SARIFResultReference{
		ID: stableID(
			"sarif-result",
			runID,
			result.RuleID,
			fingerprint,
			location.URI,
			strconv.Itoa(index),
		),
		RuleID:           strings.TrimSpace(result.RuleID),
		Level:            strings.TrimSpace(result.Level),
		Message:          strings.TrimSpace(result.Message.Text),
		Fingerprint:      fingerprint,
		ProxyEventID:     strings.TrimSpace(properties.ProxyEventID),
		ProxySessionID:   strings.TrimSpace(properties.ProxySessionID),
		ProxyEventKind:   strings.TrimSpace(properties.ProxyEventKind),
		ProxyDirection:   strings.TrimSpace(properties.ProxyDirection),
		ProxyPayloadKind: strings.TrimSpace(properties.ProxyPayloadKind),
		ProxyTraceID:     strings.TrimSpace(properties.ProxyTraceID),
		ProxyTrackingID:  strings.TrimSpace(properties.ProxyTrackingID),
		ProxyTransform:   strings.TrimSpace(properties.ProxyTransform),
		FindingID:        findingID,
		RemediationID:    remediationID,
		PolicyID:         strings.TrimSpace(properties.PolicyID),
		SkillID:          strings.TrimSpace(properties.SkillID),
		PrincipleIDs:     compactStrings(properties.EthosIDs),
		Path:             strings.TrimSpace(location.URI),
		ASTLanguage:      strings.TrimSpace(properties.ASTLanguage),
		ASTNodeKind:      strings.TrimSpace(properties.ASTNodeKind),
		ASTSymbolKind:    strings.TrimSpace(properties.ASTSymbolKind),
		ASTSymbolName:    strings.TrimSpace(properties.ASTSymbolName),
		ASTSymbolPath:    strings.TrimSpace(properties.ASTSymbolPath),
		EvaluatorKind:    strings.TrimSpace(properties.Implementation),
		CELExpression:    strings.TrimSpace(properties.CELExpression),
		PolicySource:     strings.TrimSpace(properties.PolicySource),
		SearchText:       searchText,
		StartLine:        location.StartLine,
		StartColumn:      location.StartColumn,
		Raw:              result.raw,
	}
}

func mergeSARIFResultFinding(
	reference *SARIFResultReference,
	finding *sarifInputFinding,
) {
	reference.PolicyID = firstNonEmpty(reference.PolicyID, finding.PolicyID)
	reference.SkillID = firstNonEmpty(reference.SkillID, finding.SkillID)
	reference.EvaluatorKind = firstNonEmpty(
		reference.EvaluatorKind,
		finding.EvaluatorKind,
	)
	reference.SearchText = firstNonEmpty(reference.SearchText, finding.SearchText)
	reference.PrincipleIDs = compactStrings(
		append(reference.PrincipleIDs, finding.PrincipleIDs...),
	)
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
	merged.ProxyEventID = firstNonEmpty(result.ProxyEventID, merged.ProxyEventID)
	merged.ProxySessionID = firstNonEmpty(result.ProxySessionID, merged.ProxySessionID)
	merged.ProxyEventKind = firstNonEmpty(result.ProxyEventKind, merged.ProxyEventKind)
	merged.ProxyDirection = firstNonEmpty(result.ProxyDirection, merged.ProxyDirection)
	merged.ProxyPayloadKind = firstNonEmpty(
		result.ProxyPayloadKind,
		merged.ProxyPayloadKind,
	)
	merged.ProxyTraceID = firstNonEmpty(result.ProxyTraceID, merged.ProxyTraceID)
	merged.ProxyTrackingID = firstNonEmpty(
		result.ProxyTrackingID,
		merged.ProxyTrackingID,
	)
	merged.ProxyTransform = firstNonEmpty(result.ProxyTransform, merged.ProxyTransform)
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
