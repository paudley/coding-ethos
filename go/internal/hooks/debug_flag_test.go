// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks

import (
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/debuglog"
)

func TestEventWithoutDebugFlagStripsExternalFlag(t *testing.T) {
	t.Parallel()

	event, debug := eventWithoutDebugFlag(Event{
		HookEventName: eventPreToolUse,
		ToolName:      toolBash,
		ToolInput: map[string]any{
			"command": "ruff check pkg --coding-ethos-debug --fix",
		},
	})

	if !debug {
		t.Fatal("debug flag was not captured")
	}

	command := event.Command()
	if strings.Contains(command, debuglog.Flag) {
		t.Fatalf("external debug flag leaked into command: %q", command)
	}

	for _, want := range []string{"ruff", "check", "pkg", "--fix"} {
		if !strings.Contains(command, want) {
			t.Fatalf("stripped command missing %q: %q", want, command)
		}
	}
}

func TestRouteWithDebugEnvWrapsSelectedRewrite(t *testing.T) {
	t.Parallel()

	route := routeWithDebugEnv(Event{
		HookEventName: eventPreToolUse,
		ToolName:      toolBash,
		ToolInput: map[string]any{
			"command": "ruff check pkg",
		},
	}, InspectionRoute{
		Rewrite: true,
		Reason:  "Routed lint tool through managed capture.",
		UpdatedInput: map[string]any{
			"command": "coding-ethos-run policy-tool ruff -- check pkg",
		},
	})

	if !route.Rewrite {
		t.Fatal("debug route did not preserve rewrite")
	}

	command, ok := route.UpdatedInput["command"].(string)
	if !ok {
		t.Fatalf("updated command missing: %#v", route.UpdatedInput)
	}

	if !strings.HasPrefix(command, "export "+debugEnvAssignment+"; ") {
		t.Fatalf("debug env was not prepended: %q", command)
	}

	if !strings.Contains(command, "policy-tool ruff") {
		t.Fatalf("selected route command was not preserved: %q", command)
	}
}

func TestRouteWithDebugEnvCreatesRewriteWhenNoRouteMatched(t *testing.T) {
	t.Parallel()

	route := routeWithDebugEnv(Event{
		HookEventName: eventPreToolUse,
		ToolName:      toolBash,
		ToolInput: map[string]any{
			"command": "make check",
		},
	}, InspectionRoute{})

	if !route.Rewrite {
		t.Fatal("debug-only command was not rewritten")
	}

	command, ok := route.UpdatedInput["command"].(string)
	if !ok || command != "export "+debugEnvAssignment+"; make check" {
		t.Fatalf("updated command = %#v", route.UpdatedInput["command"])
	}
}
