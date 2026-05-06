// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import (
	"encoding/json"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestAmbientPostToolReminderRejectsEmptyConfig(t *testing.T) {
	t.Parallel()

	reminder, ok := ambientPostToolReminder(policy.ReminderConfig{}, Event{
		HookEventName: "PostToolUse",
		ToolName:      "Bash",
		ToolInput: map[string]any{
			"command": "ruff check .",
		},
	})
	if ok || reminder.PrincipleID != "" {
		t.Fatalf("ambientPostToolReminder() = %#v, %v; want empty false", reminder, ok)
	}
}

func TestRenderPrincipleReminderEscapesMCPArguments(t *testing.T) {
	t.Parallel()

	reminder := renderPrincipleReminder(policy.EthosReminder{
		PrincipleID: `principle"with\chars`,
		Axiom:       "Axiom.",
		Action:      "Act.",
	}, reminderKindAmbient)

	var payload map[string]any

	err := json.Unmarshal([]byte(reminder.MCPArguments), &payload)
	if err != nil {
		t.Fatalf("MCP arguments are not valid JSON: %q: %v", reminder.MCPArguments, err)
	}

	intent, ok := payload["intent"].(string)
	if !ok || !strings.Contains(intent, `principle"with\chars`) {
		t.Fatalf("MCP intent was not preserved: %#v", payload)
	}
}

func TestCommandMentionsTokenMatchesAbsoluteCommandWithoutArgs(t *testing.T) {
	t.Parallel()

	if !commandMentionsToken("/usr/bin/ruff", "ruff") {
		t.Fatal("expected absolute tool path without args to match")
	}
}
