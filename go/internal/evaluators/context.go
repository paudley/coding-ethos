// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

type Context struct {
	Files   []string
	Argv    []string
	Command string
	Cwd     string
	Scope   string
}
