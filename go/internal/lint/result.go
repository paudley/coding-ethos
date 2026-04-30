// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package lint

import (
	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

type Result struct {
	Scope       string                   `json:"scope"`
	Status      string                   `json:"status"`
	Decisions   []policy.Decision        `json:"decisions"`
	Capture     *ToolCapture             `json:"capture,omitempty"`
	Diagnostics []diagnostics.Diagnostic `json:"diagnostics,omitempty"`
	Findings    []Finding                `json:"findings,omitempty"`
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
	SourceTool   string         `json:"source_tool,omitempty"`
	Status       string         `json:"status"`
	EthosIDs     []string       `json:"ethos_ids,omitempty"`
	Files        []string       `json:"files,omitempty"`
	Blocking     bool           `json:"blocking"`
	Column       int            `json:"column,omitempty"`
	Line         int            `json:"line,omitempty"`
}

type RunResult = Result

type ToolCapture struct {
	Tool          string   `json:"tool"`
	Parser        string   `json:"parser"`
	ParseStatus   string   `json:"parse_status"`
	OutputExcerpt string   `json:"output_excerpt,omitempty"`
	Args          []string `json:"args,omitempty"`
	RunArgs       []string `json:"run_args,omitempty"`
	ExitCode      int      `json:"exit_code"`
}
