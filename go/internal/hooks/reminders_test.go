// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks_test

import (
	"encoding/json"
	"strings"
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/hooks"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestAmbientPostToolReminderRejectsEmptyConfig(t *testing.T) {
	t.Parallel()

	reminder, found := AmbientPostToolReminder(policy.ReminderConfig{}, Event{
		HookEventName: "PostToolUse",
		ToolName:      "Bash",
		ToolInput: map[string]any{
			"command": "ruff check .",
		},
	})
	if found || reminder.PrincipleID != "" {
		t.Fatalf(
			"AmbientPostToolReminder() = %#v, %v; want empty false",
			reminder,
			found,
		)
	}
}

func TestRenderPrincipleReminderEscapesMCPArguments(t *testing.T) {
	t.Parallel()

	reminder := RenderAmbientPrincipleReminder(policy.EthosReminder{
		PrincipleID: `principle"with\chars`,
		Axiom:       "Axiom.",
		Action:      "Act.",
	})

	var payload map[string]any

	err := json.Unmarshal([]byte(reminder.MCPArguments), &payload)
	if err != nil {
		t.Fatalf("MCP arguments are not valid JSON: %q: %v", reminder.MCPArguments, err)
	}

	intent, found := payload["intent"].(string)
	if !found || !strings.Contains(intent, `principle"with\chars`) {
		t.Fatalf("MCP intent was not preserved: %#v", payload)
	}
}

func TestCommandMentionsTokenMatchesAbsoluteCommandWithoutArgs(t *testing.T) {
	t.Parallel()

	if !CommandMentionsToken("/usr/bin/ruff", "ruff") {
		t.Fatal("expected absolute tool path without args to match")
	}
}
