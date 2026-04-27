// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

type Context struct {
	Command string
	Content string
	Cwd     string
	Scope   string
	Tool    string
	Files   []string
	Argv    []string
}
