// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package policy

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
		Message: "Direct git execution must use the approved coding-ethos " +
			"git route for this agent provider.",
		Suggestion: "Resubmit the command through the suggested cerun --rewrite " +
			"command so git operations stay inside the managed policy path.",
		DefenseLayers: GitDefenseLayers("block", "wrapper", "block", "", ""),
		AppliesTo:     AppliesTo{Commands: []string{"git"}, Tools: []string{"Bash"}},
		Evaluators:    []Evaluator{{Kind: "argv", Name: "git.wrapper_required"}},
	}
}
