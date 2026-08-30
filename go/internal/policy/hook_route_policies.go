// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package policy

// Records for the blocks that the Go hook routes issue directly.
//
// These policies are enforced procedurally rather than by a CEL expression,
// which is why they were never compiled into the bundle. Enforcement did not
// need them; everything an agent does after being blocked did. A block names
// the policy that caused it, and with no record behind the name there is
// nothing to look up: policy_explain finds nothing, remediation_explain can
// only repeat "fix the violation", and the block cannot be attributed, tuned
// or even counted. One lane spent over half its refusals on
// shell.file_tool_emulation with no way to see what it was.
//
// Each is an "external" evaluator, which is the vocabulary for a decision the
// CEL engine does not make. Registering these does not move enforcement.

func hookRoutePolicies(principles map[string]Principle) map[string]Policy {
	policies := coreHookRoutePolicies(principles)
	policy := requiredGateExitStatusRoutePolicy(principles)
	policies[policy.ID] = policy

	return policies
}

func coreHookRoutePolicies(principles map[string]Principle) map[string]Policy {
	return map[string]Policy{
		"shell.file_tool_emulation": {
			ID:       "shell.file_tool_emulation",
			Category: "shell",
			Source:   SourceRef{File: "config.yaml", Path: "shell.file_tool_emulation"},
			PrincipleIDs: principleRefs(
				principles,
				"one-path-for-critical-operations",
			),
			DefaultSeverity: "block",
			SupportedModes:  []string{"block", "record"},
			DefenseLayers:   hookRouteDefenseLayers(),
			Message: "Shell file access must use provider file tools instead " +
				"of Bash file-tool emulation.",
			Suggestion: "Read and write files with the provider's own file " +
				"tools. This covers cat, sed, awk and tee reading a file " +
				"operand, echo or printf redirected into one, and any command " +
				"redirected from a file.",
			AppliesTo:  AppliesTo{Tools: []string{"Bash"}},
			Evaluators: []Evaluator{{Kind: "external", Name: "shell.file_tool_emulation"}},
		},
		"memory.centralized": {
			ID:       "memory.centralized",
			Category: "memory",
			Source:   SourceRef{File: "config.yaml", Path: "memory.centralized"},
			PrincipleIDs: principleRefs(
				principles,
				"one-path-for-critical-operations",
			),
			DefaultSeverity: "block",
			SupportedModes:  []string{"block", "record"},
			DefenseLayers:   hookRouteDefenseLayers(),
			Message:         "Memory access is routed to the central store.",
			Suggestion: "Use the central memory store rather than a " +
				"provider-specific location, so one set of memories is shared " +
				"rather than several that disagree.",
			Evaluators: []Evaluator{{Kind: "external", Name: "memory.centralized"}},
		},
		"hook.provider_required": {
			ID:       "hook.provider_required",
			Category: "hook",
			Source:   SourceRef{File: "config.yaml", Path: "hook.provider_required"},
			PrincipleIDs: principleRefs(
				principles,
				"one-path-for-critical-operations",
			),
			DefaultSeverity: "block",
			SupportedModes:  []string{"block", "record"},
			DefenseLayers:   hookRouteDefenseLayers(),
			Message:         "Hook input must identify the agent provider.",
			Suggestion: "Send the provider with the hook payload. Policy " +
				"depends on which provider is running, so an unnamed one " +
				"cannot be evaluated and is refused rather than guessed at.",
			Evaluators: []Evaluator{{Kind: "external", Name: "hook.provider_required"}},
		},
		"hook.parallel_tool_batch_unsupported": {
			ID:       "hook.parallel_tool_batch_unsupported",
			Category: "hook",
			Source: SourceRef{
				File: "config.yaml",
				Path: "hook.parallel_tool_batch_unsupported",
			},
			PrincipleIDs: principleRefs(
				principles,
				"one-path-for-critical-operations",
			),
			DefaultSeverity: "block",
			SupportedModes:  []string{"block", "record"},
			DefenseLayers:   hookRouteDefenseLayers(),
			Message: "Parallel tool batches with multiple actions are not " +
				"supported.",
			Suggestion: "Send the actions one at a time. A batch is evaluated " +
				"as a unit, so a batch that mixes permitted and refused " +
				"actions cannot be answered precisely.",
			Evaluators: []Evaluator{
				{Kind: "external", Name: "hook.parallel_tool_batch_unsupported"},
			},
		},
	}
}

func requiredGateExitStatusRoutePolicy(
	principles map[string]Principle,
) Policy {
	return Policy{
		ID:       "shell.required_gate_exit_status",
		Category: "shell",
		Source: SourceRef{
			File: "config.yaml",
			Path: "shell.required_gate_exit_status",
		},
		PrincipleIDs: principleRefs(
			principles,
			"validation-at-the-gate",
			"evidence-based-engineering-and-decision-quality",
		),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		DefenseLayers:   hookRouteDefenseLayers(),
		Message: "Required repository gates must return their own exact " +
			"terminal status.",
		Suggestion: "Run the gate directly. If output must be filtered or " +
			"logged, enable pipefail or capture the gate status immediately " +
			"and exit with that exact value.",
		AppliesTo: AppliesTo{Tools: []string{"Bash"}},
		Evaluators: []Evaluator{
			{Kind: "external", Name: "shell.required_gate_exit_status"},
		},
	}
}

// hookRouteDefenseLayers describes where these blocks actually happen.
//
// At the hook, before the tool runs, and nowhere else. The generic code layers
// would have claimed interception was advisory and enforcement came later at
// pre-commit, and neither is true here: nothing downstream re-checks these, so
// a reader deciding what still guards them would have been misled by the very
// record added to make them legible.
func hookRouteDefenseLayers() DefenseLayers {
	return DefenseLayers{
		Persuade:  true,
		Intercept: "block",
		Detect:    "block",
		Notify:    "on_block",
		Record:    true,
	}
}
