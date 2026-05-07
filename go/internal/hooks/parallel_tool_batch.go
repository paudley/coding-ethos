// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

const (
	parallelToolBatchPolicyID = "hook.parallel_tool_batch_unsupported"
	parallelToolBatchMarker   = "__coding_ethos_parallel_batch"
)

func parallelToolBatchRouteFor(event Event) InspectionRoute {
	if event.HookEventName != eventPreToolUse || event.ToolInput == nil {
		return InspectionRoute{}
	}

	marked, ok := event.ToolInput[parallelToolBatchMarker].(bool)
	if !ok {
		return InspectionRoute{}
	}

	if !marked || len(anySlice(event.ToolInput["tool_uses"])) <= 1 {
		return InspectionRoute{}
	}

	return InspectionRoute{
		BlockPolicyID: parallelToolBatchPolicyID,
		Reason: "Parallel tool batches with multiple actions are blocked until " +
			"each action can be evaluated independently.",
		Block: true,
	}
}
