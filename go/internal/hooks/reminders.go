// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import (
	"strings"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

type ethosReminder struct {
	PrincipleID string `json:"principle_id"`
	Axiom       string `json:"axiom"`
	Action      string `json:"action"`
}

func ethosReminderFor(
	result Result,
	decisions []policy.Decision,
) (ethosReminder, bool) {
	candidates := ethosReminderCandidates(decisions)
	if len(candidates) == 0 {
		return ethosReminder{}, false
	}

	return candidates[stableReminderIndex(result, decisions, len(candidates))], true
}

func ethosReminderCandidates(decisions []policy.Decision) []ethosReminder {
	candidates := []ethosReminder{}
	seen := map[string]bool{}

	for _, decision := range decisions {
		for _, principleID := range decision.PrincipleIDs {
			reminder, ok := ethosReminderForPrinciple(principleID)
			if !ok || seen[reminder.PrincipleID] {
				continue
			}

			candidates = append(candidates, reminder)
			seen[reminder.PrincipleID] = true
		}
	}

	return candidates
}

func stableReminderIndex(
	result Result,
	decisions []policy.Decision,
	candidateCount int,
) int {
	parts := []string{result.Event, result.Tool, result.Status}
	for _, decision := range decisions {
		parts = append(parts, decision.PolicyID)
		parts = append(parts, decision.PrincipleIDs...)
	}

	index := 0
	for _, char := range []byte(strings.Join(parts, "\x00")) {
		index = (index + int(char)) % candidateCount
	}

	return index
}

func ethosReminderForPrinciple(principleID string) (ethosReminder, bool) {
	switch principleID {
	case "evidence-based-engineering-and-decision-quality":
		return ethosReminder{
			PrincipleID: principleID,
			Axiom:       "Todo lists prevent partial work from masquerading as completion.",
			Action: sentence(
				"Keep the task list current, mark progress as it happens,",
				"and do not report done while planned work remains.",
			),
		}, true
	case "no-rationalized-shortcuts":
		return ethosReminder{
			PrincipleID: principleID,
			Axiom:       "Laziness only moves the cost downstream.",
			Action: sentence(
				"Stop, use the documented path,",
				"and do not trade correctness for completion.",
			),
		}, true
	case "testing-as-specification":
		return ethosReminder{
			PrincipleID: principleID,
			Axiom:       "A green process is not the same as a correct result.",
			Action: sentence(
				"Define success by user-visible behavior",
				"and inspect representative output.",
			),
		}, true
	case "static-analysis-is-the-first-line-of-defense":
		return ethosReminder{
			PrincipleID: principleID,
			Axiom:       "Static analysis is a gate, not background noise.",
			Action:      "Treat the finding as a structural signal and fix the cause.",
		}, true
	case "linting-as-code-quality-enforcement":
		return ethosReminder{
			PrincipleID: principleID,
			Axiom:       "A linter warning is review feedback in executable form.",
			Action:      "Resolve it structurally instead of weakening the rule.",
		}, true
	case "forward-motion-only":
		return ethosReminder{
			PrincipleID: principleID,
			Axiom:       "History is context, not an excuse.",
			Action:      "Fix the current state with evidence and move forward.",
		}, true
	default:
		return ethosReminder{}, false
	}
}
