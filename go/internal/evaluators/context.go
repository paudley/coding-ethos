// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import "blackcat.ca/coding-ethos/go/diagnostics"

type Context struct {
	EvaluatorOptions map[string]any
	Command          string
	Content          string
	CurrentBranch    string
	Cwd              string
	EventName        string
	Provider         string
	Scope            string
	Tool             string
	Files            []string
	ChangedFiles     []string
	StagedFiles      []string
	Argv             []string
	Stdin            []byte
	AdminApproved    bool
	Diagnostic       *diagnostics.Diagnostic
	Diagnostics      []diagnostics.Diagnostic
	Findings         []Finding
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
