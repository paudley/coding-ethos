// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import "blackcat.ca/coding-ethos/go/diagnostics"

type Context struct {
	Diagnostic         *diagnostics.Diagnostic
	EvaluatorOptions   map[string]any
	EventName          string
	OldContent         string
	CurrentBranch      string
	Cwd                string
	TranscriptPath     string
	EventMatcher       string
	EventSource        string
	Provider           string
	Scope              string
	SessionID          string
	Tool               string
	Content            string
	Command            string
	ToolResponseKeys   []string
	ToolInputKeys      []string
	Findings           []Finding
	Diagnostics        []diagnostics.Diagnostic
	Files              []string
	ChangedFiles       []string
	StagedFiles        []string
	Argv               []string
	Stdin              []byte
	ReturnCode         int
	HasToolResponse    bool
	AdminApproved      bool
	ReadOnlyInspection bool
	HasToolInput       bool
	HasReturnCode      bool
}

type Finding struct {
	Tool         string
	Code         string
	Message      string
	File         string
	Severity     string
	PolicyID     string
	SkillID      string
	PrincipleIDs []string
	Column       int
	Line         int
}
