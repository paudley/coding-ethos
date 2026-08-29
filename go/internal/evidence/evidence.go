// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strconv"
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/agentmsg"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const SchemaVersion = 1

type SourceSpan struct {
	Path        string `json:"path,omitempty"`
	Language    string `json:"language,omitempty"`
	SymbolName  string `json:"symbol_name,omitempty"`
	SymbolKind  string `json:"symbol_kind,omitempty"`
	ContentHash string `json:"content_hash,omitempty"`
	StartLine   int    `json:"start_line,omitempty"`
	StartColumn int    `json:"start_column,omitempty"`
	EndLine     int    `json:"end_line,omitempty"`
	EndColumn   int    `json:"end_column,omitempty"`
	StartByte   int    `json:"start_byte,omitempty"`
	EndByte     int    `json:"end_byte,omitempty"`
}

type Finding struct {
	SkillID            string     `json:"skill_id,omitempty"`
	EvaluatorKind      string     `json:"evaluator_kind,omitempty"`
	RuleID             string     `json:"rule_id,omitempty"`
	Tool               string     `json:"tool,omitempty"`
	Code               string     `json:"code,omitempty"`
	Message            string     `json:"message,omitempty"`
	Severity           string     `json:"severity,omitempty"`
	PolicyID           string     `json:"policy_id,omitempty"`
	ID                 string     `json:"id"`
	SearchText         string     `json:"search_text,omitempty"`
	RemediationContext string     `json:"remediation_context,omitempty"`
	CodeContext        string     `json:"code_context,omitempty"`
	PolicyContext      string     `json:"policy_context,omitempty"`
	EvidenceKeys       []string   `json:"evidence_keys,omitempty"`
	PrincipleIDs       []string   `json:"principle_ids,omitempty"`
	SourceSpan         SourceSpan `json:"source_span,omitzero"`
	SchemaVersion      int        `json:"schema_version"`
}

type Envelope struct {
	ID            string     `json:"id"`
	PolicyID      string     `json:"policy_id,omitempty"`
	SkillID       string     `json:"skill_id,omitempty"`
	EvaluatorKind string     `json:"evaluator_kind,omitempty"`
	EvidenceKeys  []string   `json:"evidence_keys,omitempty"`
	SourceSpan    SourceSpan `json:"source_span,omitzero"`
	Finding       Finding    `json:"finding,omitzero"`
	SchemaVersion int        `json:"schema_version"`
}

type RemediationEvent struct {
	ID            string `json:"id"`
	RemediationID string `json:"remediation_id,omitempty"`
	FindingID     string `json:"finding_id,omitempty"`
	SourceTraceID string `json:"source_trace_id,omitempty"`
	TraceID       string `json:"trace_id,omitempty"`
	Event         string `json:"event"`
	PolicyID      string `json:"policy_id,omitempty"`
	SkillID       string `json:"skill_id,omitempty"`
	File          string `json:"file,omitempty"`
	Path          string `json:"path,omitempty"`
	Provider      string `json:"provider,omitempty"`
	Tool          string `json:"tool,omitempty"`
	SearchText    string `json:"search_text,omitempty"`
	SchemaVersion int    `json:"schema_version"`
}

func FromDiagnostic(item diagnostics.Diagnostic) Finding {
	finding := Finding{
		SourceSpan: SourceSpanFromDiagnostic(item),
		RuleID: firstNonEmpty(
			item.PolicyID,
			joinID(item.Tool, item.Code),
			item.Code,
			item.Tool,
		),
		Tool:          strings.TrimSpace(item.Tool),
		Code:          strings.TrimSpace(item.Code),
		Message:       strings.TrimSpace(item.Message),
		Severity:      strings.TrimSpace(item.Severity),
		PolicyID:      strings.TrimSpace(item.PolicyID),
		SkillID:       strings.TrimSpace(item.SkillID),
		EvaluatorKind: stringMetadata(item.Metadata, "implementation"),
		CodeContext: firstNonEmpty(
			stringMetadata(item.Metadata, "code_context"),
			item.Detail,
		),
		PolicyContext:      strings.TrimSpace(item.Meaning),
		RemediationContext: strings.TrimSpace(item.Advice),
		PrincipleIDs:       compactStrings(item.PrincipleIDs),
		SchemaVersion:      SchemaVersion,
	}
	finding.SearchText = searchText(
		finding.Tool,
		finding.Code,
		finding.Message,
		finding.PolicyID,
		finding.SkillID,
		finding.SourceSpan.Path,
		finding.SourceSpan.SymbolName,
		finding.PolicyContext,
		finding.RemediationContext,
	)
	finding.ID = findingID(finding)

	return finding
}

func FromDiagnostics(items []diagnostics.Diagnostic) []Finding {
	findings := make([]Finding, 0, len(items))
	for _, item := range items {
		finding := FromDiagnostic(item)
		if finding.Message == "" && finding.PolicyID == "" &&
			finding.SourceSpan.Path == "" {
			continue
		}

		findings = append(findings, finding)
	}

	return findings
}

func FromDecision(decision policy.Decision) Finding {
	sourceSpan := sourceSpanFromEvidence(decision.Evidence)
	if sourceSpan.Path == "" {
		if files := decision.EvidenceFiles(); len(files) > 0 {
			sourceSpan.Path = files[0]
		}
	}

	finding := Finding{
		SourceSpan:    sourceSpan,
		RuleID:        strings.TrimSpace(decision.PolicyID),
		Tool:          decision.EvidenceTool(),
		Message:       strings.TrimSpace(decision.Message),
		Severity:      strings.TrimSpace(decision.Severity),
		PolicyID:      strings.TrimSpace(decision.PolicyID),
		SkillID:       decision.EvidenceSkillID(),
		EvaluatorKind: decision.EvidenceImplementation(),
		PolicyContext: strings.TrimSpace(decision.Suggestion),
		EvidenceKeys:  evidenceKeys(decision.Evidence),
		PrincipleIDs:  compactStrings(decision.PrincipleIDs),
		SchemaVersion: SchemaVersion,
	}
	finding.SearchText = searchText(
		finding.Tool,
		finding.Message,
		finding.PolicyID,
		finding.SkillID,
		finding.SourceSpan.Path,
		finding.SourceSpan.SymbolName,
		finding.PolicyContext,
		decision.EvidenceCommand(),
	)
	finding.ID = findingID(finding)

	return finding
}

func FromDecisions(decisions []policy.Decision) []Finding {
	findings := make([]Finding, 0, len(decisions))
	for _, decision := range decisions {
		if len(decision.Diagnostics) > 0 {
			findings = append(findings, FromDiagnostics(decision.EvidenceDiagnostics())...)

			continue
		}

		finding := FromDecision(decision)
		if finding.Message == "" && finding.PolicyID == "" {
			continue
		}

		findings = append(findings, finding)
	}

	return findings
}

func EnvelopeFromFinding(finding Finding) Envelope {
	return Envelope{
		SourceSpan:    finding.SourceSpan,
		Finding:       finding,
		ID:            finding.ID,
		PolicyID:      finding.PolicyID,
		SkillID:       finding.SkillID,
		EvaluatorKind: finding.EvaluatorKind,
		EvidenceKeys:  append([]string(nil), finding.EvidenceKeys...),
		SchemaVersion: SchemaVersion,
	}
}

func RemediationEvents(
	remediations []agentmsg.Remediation,
	findings []Finding,
	traceID string,
	event string,
) []RemediationEvent {
	events := make([]RemediationEvent, 0, len(remediations))
	for index, remediation := range remediations {
		findingID := ""
		if index < len(findings) {
			findingID = findings[index].ID
		}

		events = append(events, RemediationEventFromRemediation(
			remediation,
			findingID,
			traceID,
			event,
		))
	}

	return events
}

func RemediationEventFromRemediation(
	remediation agentmsg.Remediation,
	findingID string,
	traceID string,
	event string,
) RemediationEvent {
	if strings.TrimSpace(event) == "" {
		event = "suggested"
	}

	result := RemediationEvent{
		RemediationID: strings.TrimSpace(remediation.ID),
		FindingID:     strings.TrimSpace(findingID),
		TraceID:       strings.TrimSpace(traceID),
		Event:         strings.TrimSpace(event),
		PolicyID:      strings.TrimSpace(remediation.PolicyID),
		SkillID:       strings.TrimSpace(remediation.SkillID),
		SearchText: searchText(
			remediation.PolicyID,
			remediation.SkillID,
			remediation.Message,
			remediation.Advice,
			remediation.FailedAction,
			remediation.File,
			remediation.Path,
			remediation.Command,
			strings.Join(remediation.NextSteps, " "),
		),
		SchemaVersion: SchemaVersion,
	}
	result.ID = stableID(
		"remediation-event",
		result.RemediationID,
		result.FindingID,
		result.TraceID,
		result.Event,
		result.PolicyID,
		result.SkillID,
	)

	return result
}

func SourceSpanFromDiagnostic(item diagnostics.Diagnostic) SourceSpan {
	return SourceSpan{
		Path:     cleanPath(item.File),
		Language: stringMetadata(item.Metadata, "language"),
		SymbolName: firstNonEmpty(
			item.Function,
			stringMetadata(item.Metadata, "symbol_name"),
		),
		SymbolKind:  stringMetadata(item.Metadata, "symbol_kind"),
		ContentHash: stringMetadata(item.Metadata, "content_hash"),
		StartLine:   item.Line,
		StartColumn: item.Column,
		EndLine:     intMetadata(item.Metadata, "end_line"),
		EndColumn:   intMetadata(item.Metadata, "end_column"),
		StartByte:   intMetadata(item.Metadata, "start_byte"),
		EndByte:     intMetadata(item.Metadata, "end_byte"),
	}
}

func sourceSpanFromEvidence(evidence map[string]any) SourceSpan {
	return SourceSpan{
		Path: cleanPath(
			firstNonEmpty(
				stringEvidence(evidence, "file"),
				stringEvidence(evidence, "path"),
			),
		),
		Language:    stringEvidence(evidence, "language"),
		SymbolName:  stringEvidence(evidence, "symbol_name"),
		SymbolKind:  stringEvidence(evidence, "symbol_kind"),
		ContentHash: stringEvidence(evidence, "content_hash"),
		StartLine:   intEvidence(evidence, "line"),
		StartColumn: intEvidence(evidence, "column"),
		EndLine:     intEvidence(evidence, "end_line"),
		EndColumn:   intEvidence(evidence, "end_column"),
		StartByte:   intEvidence(evidence, "start_byte"),
		EndByte:     intEvidence(evidence, "end_byte"),
	}
}

func findingID(finding Finding) string {
	return stableID(
		"finding",
		finding.RuleID,
		finding.Tool,
		finding.Code,
		finding.PolicyID,
		finding.SourceSpan.Path,
		strconv.Itoa(finding.SourceSpan.StartLine),
		strconv.Itoa(finding.SourceSpan.StartColumn),
		finding.SourceSpan.SymbolName,
		finding.Message,
	)
}

func stableID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))

	return hex.EncodeToString(sum[:])
}

func searchText(values ...string) string {
	return strings.Join(compactStrings(values), "\n")
}

func joinID(values ...string) string {
	return strings.Join(compactStrings(values), ".")
}

func cleanPath(path string) string {
	path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	path = strings.TrimPrefix(path, "./")

	return path
}

func stringMetadata(metadata map[string]any, key string) string {
	value, found := metadata[key]
	if !found {
		return ""
	}

	text, found := value.(string)
	if !found {
		return ""
	}

	return strings.TrimSpace(text)
}

func intMetadata(metadata map[string]any, key string) int {
	value, found := metadata[key]
	if !found {
		return 0
	}

	return intValue(value)
}

func stringEvidence(evidence map[string]any, key string) string {
	value, found := evidence[key]
	if !found {
		return ""
	}

	text, found := value.(string)
	if !found {
		return ""
	}

	return strings.TrimSpace(text)
}

func intEvidence(evidence map[string]any, key string) int {
	value, found := evidence[key]
	if !found {
		return 0
	}

	return intValue(value)
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func evidenceKeys(evidence map[string]any) []string {
	if len(evidence) == 0 {
		return nil
	}

	keys := make([]string, 0, len(evidence))
	for key := range evidence {
		keys = append(keys, key)
	}

	slices.Sort(keys)

	return keys
}

func compactStrings(values []string) []string {
	compacted := []string{}

	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || slices.Contains(compacted, value) {
			continue
		}

		compacted = append(compacted, value)
	}

	return compacted
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}

	return ""
}
