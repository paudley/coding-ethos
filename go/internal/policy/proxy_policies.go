// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package policy

const ProxySearchReplaceEditPolicyID = "proxy.search_replace_edit"

func ProxySearchReplaceEditPolicy() Policy {
	return Policy{
		ID:       ProxySearchReplaceEditPolicyID,
		Category: "proxy",
		Source: SourceRef{
			File: "docs/AGENT_PROXY.md",
			Path: "Search/Replace Edit Enforcement",
		},
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record", "advise"},
		Message: "File edits must use exact SEARCH/REPLACE blocks that match " +
			"the current file content exactly once.",
		Suggestion: "Use Edit or MultiEdit with a non-empty old_string that " +
			"matches exactly one current file span; do not rewrite existing files " +
			"with Write.",
		PrincipleIDs:  []string{"one-path-for-critical-operations", "security-by-design"},
		DefenseLayers: CodeDefenseLayers(),
		AppliesTo: AppliesTo{
			Tools: []string{"Write", "Edit", "MultiEdit"},
		},
		Evaluators: []Evaluator{{
			Kind: "text",
			Name: ProxySearchReplaceEditPolicyID,
		}},
	}
}
