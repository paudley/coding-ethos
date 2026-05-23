// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks

import (
	"maps"

	"blackcat.ca/coding-ethos/go/internal/memories"
)

const memoryPolicyID = "memory.centralized"

func memoryRouteFor(event Event) InspectionRoute {
	if event.HookEventName != eventPreToolUse {
		return InspectionRoute{}
	}

	switch event.ToolName {
	case "Read", "Write", "Edit", "MultiEdit":
		return memoryFileToolRouteFor(event)
	default:
		return InspectionRoute{}
	}
}

func memoryFileToolRouteFor(event Event) InspectionRoute {
	filePath, key, ok := eventFilePath(event)
	if !ok {
		return InspectionRoute{}
	}

	classification := memories.Classify(event.Cwd, filePath, event.Provider())
	if !classification.Managed {
		return InspectionRoute{}
	}

	if !providerSupportsMemoryRewrite(event.Provider()) {
		return InspectionRoute{
			Block:         true,
			BlockPolicyID: memoryPolicyID,
			Reason:        memories.DeniedGuidance,
		}
	}

	err := memories.Ensure(event.Cwd)
	if err != nil {
		return InspectionRoute{
			Block:         true,
			BlockPolicyID: memoryPolicyID,
			Reason:        "Memory central store is unavailable: " + err.Error(),
		}
	}

	updated := map[string]any{}
	maps.Copy(updated, event.ToolInput)
	updated[key] = classification.CanonicalPath

	return InspectionRoute{
		UpdatedInput: updated,
		Reason: "Routed provider-specific memory access to " +
			memories.PrimaryFile + ".",
		Rewrite: true,
	}
}

func eventFilePath(event Event) (string, string, bool) {
	for _, key := range []string{"file_path", "path", "notebook_path"} {
		value, ok := event.ToolInput[key].(string)
		if ok && value != "" {
			return value, key, true
		}
	}

	return "", "", false
}

func providerSupportsMemoryRewrite(provider string) bool {
	switch provider {
	case providerClaude, providerGemini, providerCodingEthos:
		return true
	default:
		return false
	}
}
