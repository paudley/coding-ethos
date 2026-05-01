// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hookoutput

import (
	"encoding/json"
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
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
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
	Tags        []string `json:"tags,omitempty"`
	PolicyID    string   `json:"policy_id,omitempty"`
	SkillID     string   `json:"skill_id,omitempty"`
	SourceTool  string   `json:"source_tool,omitempty"`
	EthosIDs    []string `json:"ethos_ids,omitempty"`
	CodingEthos bool     `json:"coding_ethos"`
}

type sarifHelp struct {
	Text     string `json:"text,omitempty"`
	Markdown string `json:"markdown,omitempty"`
}

type sarifResult struct {
	RuleID     string                `json:"ruleId"`
	Level      string                `json:"level"`
	Message    sarifMessage          `json:"message"`
	Locations  []sarifLocation       `json:"locations,omitempty"`
	Properties sarifResultProperties `json:"properties,omitempty"`
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

func FormatLintResultSARIF(result lint.Result) (string, error) {
	diagnostics := lint.OutputDiagnostics(result)
	log := sarifLog{
		Schema:  sarifSchema,
		Version: sarifVersion,
		Runs: []sarifRun{{
			Tool: sarifTool{Driver: sarifDriver{
				Name:           "coding-ethos",
				InformationURI: "https://github.com/paudley/coding-ethos",
				Rules:          sarifRules(diagnostics),
			}},
			Results: sarifResults(diagnostics),
		}},
	}

	payload, err := json.MarshalIndent(log, "", "  ")
	if err != nil {
		return "", err
	}

	return string(payload), nil
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
				Tags:        append([]string(nil), item.Tags...),
				PolicyID:    item.PolicyID,
				SkillID:     item.SkillID,
				SourceTool:  item.Tool,
				EthosIDs:    append([]string(nil), item.PrincipleIDs...),
				CodingEthos: true,
			},
		})
	}

	return rules
}

func sarifResults(items []diagnostics.Diagnostic) []sarifResult {
	results := make([]sarifResult, 0, len(items))
	for _, item := range items {
		results = append(results, sarifResult{
			RuleID:    sarifRuleID(item),
			Level:     sarifLevel(item.Severity),
			Message:   sarifMessage{Text: item.Message},
			Locations: sarifLocations(item),
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
	file := item.File
	if file == "" {
		file = sarifRepoURI
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
