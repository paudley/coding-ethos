// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import (
	"strings"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

func ethosReminderFor(
	result Result,
	decisions []policy.Decision,
) (policy.EthosReminder, bool) {
	candidates := ethosReminderCandidates(result.Advice.Reminders, decisions)
	if len(candidates) == 0 {
		return policy.EthosReminder{}, false
	}

	return candidates[stableReminderIndex(result, decisions, len(candidates))], true
}

func ethosReminderCandidates(
	config policy.ReminderConfig,
	decisions []policy.Decision,
) []policy.EthosReminder {
	if len(config.Items) == 0 {
		config = fallbackReminderConfig()
	}

	candidates := []policy.EthosReminder{}
	seen := map[string]bool{}

	for _, decision := range decisions {
		for _, principleID := range decision.PrincipleIDs {
			reminder, ok := ethosReminderForPrinciple(config, principleID)
			if !ok || seen[reminder.PrincipleID] {
				continue
			}

			candidates = append(candidates, reminder)
			seen[reminder.PrincipleID] = true
		}
	}

	return candidates
}

func fallbackReminderConfig() policy.ReminderConfig {
	return policy.ExampleBundle().Advice.Reminders
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

func ethosReminderForPrinciple(
	config policy.ReminderConfig,
	principleID string,
) (policy.EthosReminder, bool) {
	for _, reminder := range config.Items {
		if reminder.PrincipleID == principleID {
			return reminder, true
		}
	}

	return policy.EthosReminder{}, false
}
