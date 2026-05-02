// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import "blackcat.ca/coding-ethos/go/diagnostics"

type Context struct {
	EvaluatorOptions map[string]any
	Command          string
	Content          string
	Cwd              string
	Scope            string
	Tool             string
	Files            []string
	Argv             []string
	AdminApproved    bool
	Diagnostic       *diagnostics.Diagnostic
}
