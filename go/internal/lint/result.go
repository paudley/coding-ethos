// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package lint

import (
	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/sandbox"
)

type Result struct {
	TraceID     string                   `json:"trace_id,omitempty"`
	Scope       string                   `json:"scope"`
	Status      string                   `json:"status"`
	Decisions   []policy.Decision        `json:"decisions"`
	Capture     *ToolCapture             `json:"capture,omitempty"`
	Diagnostics []diagnostics.Diagnostic `json:"diagnostics,omitempty"`
	Findings    []Finding                `json:"findings,omitempty"`
	SkillHints  []SkillHint              `json:"skill_hints,omitempty"`
	Files       []string                 `json:"files,omitempty"`
}

func (result Result) Blocked() bool {
	return result.Status == "blocked"
}

type Finding struct {
	RawOutcome   map[string]any `json:"raw_outcome,omitempty"`
	Advice       string         `json:"advice,omitempty"`
	CheckID      string         `json:"check_id"`
	Code         string         `json:"code,omitempty"`
	File         string         `json:"file,omitempty"`
	Message      string         `json:"message"`
	PolicyID     string         `json:"policy_id,omitempty"`
	PolicySource string         `json:"policy_source,omitempty"`
	Severity     string         `json:"severity"`
	SkillID      string         `json:"skill_id,omitempty"`
	SourceTool   string         `json:"source_tool,omitempty"`
	Status       string         `json:"status"`
	EthosIDs     []string       `json:"ethos_ids,omitempty"`
	Files        []string       `json:"files,omitempty"`
	Blocking     bool           `json:"blocking"`
	Column       int            `json:"column,omitempty"`
	Line         int            `json:"line,omitempty"`
}

func (finding Finding) FindingTool() string {
	return finding.SourceTool
}

func (finding Finding) FindingCode() string {
	return finding.Code
}

func (finding Finding) FindingMessage() string {
	return finding.Message
}

func (finding Finding) FindingFile() string {
	return finding.File
}

func (finding Finding) FindingSeverity() string {
	return finding.Severity
}

func (finding Finding) FindingPolicyID() string {
	return finding.PolicyID
}

func (finding Finding) FindingSkillID() string {
	return finding.SkillID
}

func (finding Finding) FindingPrincipleIDs() []string {
	return append([]string(nil), finding.EthosIDs...)
}

func (finding Finding) FindingColumn() int {
	return finding.Column
}

func (finding Finding) FindingLine() int {
	return finding.Line
}

type RunResult = Result

type SkillHint struct {
	PrincipleID string `json:"principle_id,omitempty"`
	SkillID     string `json:"skill_id"`
	Message     string `json:"message"`
	Next        string `json:"next"`
}

type ToolCapture struct {
	Sandbox       *SandboxEvidence `json:"sandbox,omitempty"`
	Tool          string           `json:"tool"`
	Parser        string           `json:"parser"`
	Category      string           `json:"category,omitempty"`
	ParseStatus   string           `json:"parse_status"`
	OutputExcerpt string           `json:"output_excerpt,omitempty"`
	Stdout        string           `json:"stdout,omitempty"`
	Stderr        string           `json:"stderr,omitempty"`
	Args          []string         `json:"args,omitempty"`
	RunArgs       []string         `json:"run_args,omitempty"`
	ExitCode      int              `json:"exit_code"`
}

type SandboxEvidence = sandbox.Evidence
