// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agentskills

type Options struct {
	EthosRoot string
	RepoRoot  string
	Primary   string
	RepoEthos string
}

type principle struct {
	ID        string
	Title     string
	Summary   string
	Directive string
	QuickRef  []string
	Sections  []principleSection
	Order     int
}

type principleSection struct {
	Title string
	Body  string
}

type skill struct {
	ID               string
	Title            string
	Description      string
	PrincipleIDs     []string
	TriggerTerms     []string
	ShortHint        string
	Focus            string
	RemediationSteps []string
}

type bundle struct {
	RepoName   string
	Principles map[string]principle
	Skills     []skill
}
