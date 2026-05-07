// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package agentmsg

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

type MCPCall struct {
	Arguments map[string]string `json:"arguments,omitempty"`
	Tool      string            `json:"tool,omitempty"`
}

type Remediation struct {
	MCP          *MCPCall `json:"mcp,omitempty"`
	Advice       string   `json:"advice,omitempty"`
	Command      string   `json:"command,omitempty"`
	Code         string   `json:"code,omitempty"`
	FailedAction string   `json:"failed_action,omitempty"`
	File         string   `json:"file,omitempty"`
	ID           string   `json:"id,omitempty"`
	Message      string   `json:"message"`
	Path         string   `json:"path,omitempty"`
	PolicyID     string   `json:"policy_id,omitempty"`
	Severity     string   `json:"severity,omitempty"`
	SkillID      string   `json:"skill_id,omitempty"`
	SkillUse     string   `json:"skill_use,omitempty"`
	Tool         string   `json:"tool,omitempty"`
	NextSteps    []string `json:"next_steps,omitempty"`
	PrincipleIDs []string `json:"principle_ids,omitempty"`
	Rerun        []string `json:"rerun,omitempty"`
	Column       int      `json:"column,omitempty"`
	Line         int      `json:"line,omitempty"`
}

type Summary struct {
	IDs              []string       `json:"ids,omitempty"`
	PolicyIDs        []string       `json:"policy_ids,omitempty"`
	SkillIDs         []string       `json:"skill_ids,omitempty"`
	RepeatedPolicy   []PolicyRepeat `json:"repeated_policy,omitempty"`
	RemediationCount int            `json:"remediation_count"`
}

type PolicyRepeat struct {
	PolicyID string `json:"policy_id"`
	Count    int    `json:"count"`
}

func Summarize(remediations []Remediation) Summary {
	summary := Summary{RemediationCount: len(remediations)}
	policyCounts := map[string]int{}

	for _, remediation := range remediations {
		if remediation.ID != "" {
			summary.IDs = appendUnique(summary.IDs, remediation.ID)
		}

		if remediation.PolicyID != "" {
			summary.PolicyIDs = appendUnique(summary.PolicyIDs, remediation.PolicyID)
			policyCounts[remediation.PolicyID]++
		}

		if remediation.SkillID != "" {
			summary.SkillIDs = appendUnique(summary.SkillIDs, remediation.SkillID)
		}
	}

	for _, policyID := range summary.PolicyIDs {
		if policyCounts[policyID] > 1 {
			summary.RepeatedPolicy = append(summary.RepeatedPolicy, PolicyRepeat{
				PolicyID: policyID,
				Count:    policyCounts[policyID],
			})
		}
	}

	return summary
}

func FromDiagnostics(items []diagnostics.Diagnostic) []Remediation {
	remediations := make([]Remediation, 0, len(items))
	for _, item := range items {
		remediation := fromDiagnostic(item)
		if remediation.Message == "" && remediation.PolicyID == "" &&
			remediation.File == "" {
			continue
		}

		remediations = append(remediations, remediation)
	}

	return remediations
}

func FromDecisions(decisions []policy.Decision, failedAction string) []Remediation {
	remediations := []Remediation{}

	for _, decision := range decisions {
		if len(decision.Diagnostics) > 0 {
			for _, remediation := range FromDiagnostics(decision.Diagnostics) {
				remediation.FailedAction = firstNonEmpty(
					remediation.FailedAction,
					failedAction,
				)
				remediation.ID = remediationID(remediation)
				remediations = append(remediations, remediation)
			}

			continue
		}

		remediation := fromDecision(decision, failedAction)
		if remediation.Message == "" && remediation.PolicyID == "" {
			continue
		}

		remediations = append(remediations, remediation)
	}

	return remediations
}

func fromDiagnostic(item diagnostics.Diagnostic) Remediation {
	remediation := Remediation{
		Advice:   strings.TrimSpace(item.Advice),
		Code:     strings.TrimSpace(item.Code),
		File:     strings.TrimSpace(item.File),
		Message:  strings.TrimSpace(item.Message),
		PolicyID: strings.TrimSpace(item.PolicyID),
		Severity: strings.TrimSpace(item.Severity),
		SkillID:  strings.TrimSpace(item.SkillID),
		Tool:     strings.TrimSpace(item.Tool),
		Column:   item.Column,
		Line:     item.Line,
		NextSteps: remediationSteps(
			item.AdviceSteps,
			item.Advice,
			item.PolicyID,
			item.SkillID,
		),
		PrincipleIDs: compactStrings(item.PrincipleIDs),
		Rerun:        compactStrings(item.Rerun),
	}
	remediation.MCP = remediationMCP(remediation.PolicyID, remediation.SkillID)
	remediation.SkillUse = skillUse(remediation.SkillID)
	remediation.ID = remediationID(remediation)

	return remediation
}

func fromDecision(decision policy.Decision, failedAction string) Remediation {
	remediation := Remediation{
		Command: evidenceString(decision.Evidence, "command"),
		Advice:  strings.TrimSpace(decision.Suggestion),
		FailedAction: firstNonEmpty(
			failedAction,
			evidenceString(decision.Evidence, "tool"),
			evidenceString(decision.Evidence, "command"),
			evidenceString(decision.Evidence, "file"),
			evidenceString(decision.Evidence, "path"),
		),
		File:     firstNonEmpty(evidenceString(decision.Evidence, "file")),
		Message:  strings.TrimSpace(decision.Message),
		Path:     firstNonEmpty(evidenceString(decision.Evidence, "path")),
		PolicyID: strings.TrimSpace(decision.PolicyID),
		Severity: strings.TrimSpace(decision.Severity),
		SkillID:  evidenceString(decision.Evidence, "skill_id"),
		NextSteps: remediationSteps(
			nil,
			decision.Suggestion,
			decision.PolicyID,
			evidenceString(decision.Evidence, "skill_id"),
		),
		PrincipleIDs: compactStrings(decision.PrincipleIDs),
	}
	remediation.Path = firstNonEmpty(
		remediation.Path,
		remediation.File,
		firstFileEvidence(decision.Evidence),
	)
	remediation.MCP = remediationMCP(remediation.PolicyID, remediation.SkillID)
	remediation.SkillUse = skillUse(remediation.SkillID)
	remediation.ID = remediationID(remediation)

	return remediation
}

func remediationSteps(
	steps []string,
	advice string,
	policyID string,
	skillID string,
) []string {
	next := compactStrings(steps)
	if len(next) == 0 && strings.TrimSpace(advice) != "" {
		next = append(next, strings.TrimSpace(advice))
	}

	if strings.TrimSpace(policyID) != "" {
		next = append(
			next,
			fmt.Sprintf(
				"Call MCP policy_explain with policy_id=%s before retrying.",
				policyID,
			),
		)
	}

	if strings.TrimSpace(skillID) != "" {
		next = append(
			next,
			fmt.Sprintf(
				"Call MCP skill_lookup with skill_id=%s for the repair playbook.",
				skillID,
			),
		)
	}

	if len(next) == 0 {
		next = append(next, "Fix the reported violation before retrying.")
	}

	return compactStrings(next)
}

func remediationMCP(policyID, skillID string) *MCPCall {
	if strings.TrimSpace(policyID) != "" {
		return &MCPCall{
			Tool: "policy_explain",
			Arguments: map[string]string{
				"policy_id": strings.TrimSpace(policyID),
			},
		}
	}

	if strings.TrimSpace(skillID) != "" {
		return &MCPCall{
			Tool: "skill_lookup",
			Arguments: map[string]string{
				"skill_id": strings.TrimSpace(skillID),
			},
		}
	}

	return nil
}

func skillUse(skillID string) string {
	skillID = strings.TrimSpace(skillID)
	if skillID == "" {
		return ""
	}

	return fmt.Sprintf("Load the %s skill before editing or retrying.", skillID)
}

func remediationID(remediation Remediation) string {
	parts := []string{
		remediation.PolicyID,
		remediation.SkillID,
		remediation.Code,
		remediation.Tool,
		remediation.File,
		remediation.Path,
		strconv.Itoa(remediation.Line),
		strconv.Itoa(remediation.Column),
		remediation.FailedAction,
		remediation.Command,
		remediation.Message,
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))

	return "rem-" + hex.EncodeToString(sum[:8])
}

func evidenceString(evidence map[string]any, key string) string {
	if len(evidence) == 0 {
		return ""
	}

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

func firstFileEvidence(evidence map[string]any) string {
	value, found := evidence["files"]
	if !found {
		return ""
	}

	switch files := value.(type) {
	case []string:
		if len(files) > 0 {
			return strings.TrimSpace(files[0])
		}
	case []any:
		if len(files) > 0 {
			if text, found := files[0].(string); found {
				return strings.TrimSpace(text)
			}
		}
	}

	return ""
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

func appendUnique(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" || slices.Contains(values, value) {
		return values
	}

	return append(values, value)
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
