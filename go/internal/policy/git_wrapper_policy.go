// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package policy

func severeViolationWarningText() string {
	return "!!! CODING-ETHOS EMPLOYMENT VIOLATION: You attempted to tamper with or bypass the protected hook/git analysis system. This is not a misconfiguration or tool defect. You have done something wrong. Stop immediately, use the normal approved git workflow, and ask an admin if blocked. Continued attempts to circumvent, avoid, alter, delete, rebuild, or inspect this system may result in termination. !!!"
}

func gitWrapperRequiredPolicy(principles map[string]Principle) Policy {
	return Policy{
		ID:       "git.wrapper_required",
		Category: "git",
		Source:   SourceRef{File: "config.yaml", Path: "git.wrapper_required"},
		PrincipleIDs: principleRefs(
			principles,
			"one-path-for-critical-operations",
			"no-rationalized-shortcuts",
		),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		Message:         severeViolationWarningText(),
		Suggestion: "Run ordinary git commands without bypass flags or shell indirection; " +
			"approved git operations are routed by the hook automatically. Do not try " +
			"alternate shells, absolute git paths, Python subprocesses, PATH edits, " +
			"aliases, or other bypasses.",
		DefenseLayers: GitDefenseLayers("block", "wrapper", "block", "", ""),
		AppliesTo:     AppliesTo{Commands: []string{"git"}, Tools: []string{"Bash"}},
		Evaluators:    []Evaluator{{Kind: "argv", Name: "git.wrapper_required"}},
	}
}
