// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import "blackcat.ca/coding-ethos/go/internal/apperror"

type policyToolGroupEntry struct {
	Tool string
	Args []string
}

func runPolicyToolGroup(paths runtimePaths, rest []string) error {
	if len(rest) == 0 {
		return apperror.StaticError("policy-tool-group requires a group name")
	}

	group, found := policyToolGroup(rest[0])
	if !found {
		return apperror.Wrapf(
			apperror.StaticError("unknown policy-tool group"),
			"unknown policy-tool group %q",
			rest[0],
		)
	}

	requirePolicyBundle(paths)

	for _, entry := range group {
		runtimeRunLint(paths, policyToolLintArgs(paths, entry.Tool, entry.Args)...)
	}

	return nil
}

func policyToolGroup(name string) ([]policyToolGroupEntry, bool) {
	switch name {
	case "linters":
		return []policyToolGroupEntry{
			{Tool: "ruff", Args: []string{"check", "coding_ethos", "tests"}},
			{Tool: "golangci-lint", Args: []string{"go"}},
		}, true
	case "formatters":
		return []policyToolGroupEntry{
			{Tool: "ruff-format", Args: []string{"format", "coding_ethos", "tests"}},
			{Tool: "golangci-lint-format", Args: []string{"go"}},
		}, true
	case "autofixers":
		return []policyToolGroupEntry{
			{
				Tool: "ruff-autofix",
				Args: []string{
					"check",
					"--fix",
					"--quiet",
					"--ignore-noqa",
					"--output-format",
					"json",
					"coding_ethos",
					"tests",
				},
			},
			{Tool: "golangci-lint-autofix", Args: []string{"go"}},
		}, true
	default:
		return nil, false
	}
}
