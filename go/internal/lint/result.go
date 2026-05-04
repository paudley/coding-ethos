// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package lint

import (
	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/policy"
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
	Tool          string           `json:"tool"`
	Parser        string           `json:"parser"`
	ParseStatus   string           `json:"parse_status"`
	OutputExcerpt string           `json:"output_excerpt,omitempty"`
	Args          []string         `json:"args,omitempty"`
	RunArgs       []string         `json:"run_args,omitempty"`
	Sandbox       *SandboxEvidence `json:"sandbox,omitempty"`
	ExitCode      int              `json:"exit_code"`
}

type SandboxEvidence struct {
	Mode                 string   `json:"mode,omitempty"`
	Backend              string   `json:"backend,omitempty"`
	BackendPath          string   `json:"backend_path,omitempty"`
	Profile              string   `json:"profile,omitempty"`
	Tool                 string   `json:"tool,omitempty"`
	Command              []string `json:"command,omitempty"`
	Tags                 []string `json:"tags,omitempty"`
	HiddenCredentialDirs []string `json:"hidden_credential_dirs,omitempty"`
	ReadPaths            []string `json:"read_paths,omitempty"`
	WritePaths           []string `json:"write_paths,omitempty"`
	TimeoutSeconds       int      `json:"timeout_seconds,omitempty"`
	MemoryMB             int      `json:"memory_mb,omitempty"`
	CPUQuotaPercent      int      `json:"cpu_quota_percent,omitempty"`
	RequiresNetwork      bool     `json:"requires_network,omitempty"`
	RequiresGit          bool     `json:"requires_git,omitempty"`
	RequiresEnv          bool     `json:"requires_env,omitempty"`
	RequiresProcesses    bool     `json:"requires_processes,omitempty"`
	GitReadOnly          bool     `json:"git_read_only,omitempty"`
	ReadOnlyRoot         bool     `json:"read_only_root,omitempty"`
	NetworkIsolated      bool     `json:"network_isolated,omitempty"`
	ProcessIsolated      bool     `json:"process_isolated,omitempty"`
	TimeoutEnforced      bool     `json:"timeout_enforced,omitempty"`
	CgroupRequested      bool     `json:"cgroup_requested,omitempty"`
	CgroupEnabled        bool     `json:"cgroup_enabled,omitempty"`
	CgroupPath           string   `json:"cgroup_path,omitempty"`
	SeccompProfile       string   `json:"seccomp_profile,omitempty"`
	SeccompEnabled       bool     `json:"seccomp_enabled,omitempty"`
	Enabled              bool     `json:"enabled"`
	Denied               bool     `json:"denied,omitempty"`
	Reason               string   `json:"reason,omitempty"`
}
