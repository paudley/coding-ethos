// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

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

	exitCode := runPolicyToolGroupEntries(paths, group, false)
	if exitCode != 0 {
		requestRuntimeExit(exitCode)
	}

	return nil
}

func runPolicyToolGroupByName(paths runtimePaths, name string, codeIntel bool) int {
	group, found := policyToolGroup(name)
	if !found {
		return 1
	}

	return runPolicyToolGroupEntries(paths, group, codeIntel)
}

func runPolicyToolGroupEntries(
	paths runtimePaths,
	group []policyToolGroupEntry,
	codeIntel bool,
) int {
	exitCode := 0

	for _, entry := range group {
		args := policyToolLintArgs(paths, entry.Tool, entry.Args)
		if codeIntel {
			args = append([]string{"--code-intel"}, args...)
		}

		code := runtimeRunLint(paths, args...)

		if code != 0 && exitCode == 0 {
			exitCode = code
		}
	}

	return exitCode
}

func policyToolGroup(name string) ([]policyToolGroupEntry, bool) {
	switch name {
	case "linters":
		return []policyToolGroupEntry{
			{Tool: "ruff", Args: []string{"check", "coding_ethos", "tests"}},
			{Tool: "golangci-lint"},
		}, true
	case "formatters":
		return []policyToolGroupEntry{
			{Tool: "ruff-format", Args: []string{"format", "coding_ethos", "tests"}},
			{Tool: "golangci-lint-format"},
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
			{Tool: "golangci-lint-autofix"},
		}, true
	default:
		return nil, false
	}
}
