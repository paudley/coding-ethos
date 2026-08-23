// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks

import (
	"strings"
	"testing"
)

func TestLifecycleContextGatesGuidanceByCodingRelevance(t *testing.T) {
	t.Setenv(lifecycleStateRootEnv, t.TempDir())

	irrelevant := lifecycleContext(Event{
		HookEventName: eventUserPromptSubmit,
		SessionID:     "irrelevant-session",
		ToolInput: map[string]any{
			"prompt": "Tell me a short joke about penguins.",
		},
	})
	if irrelevant != "" {
		t.Fatalf("irrelevant prompt received lifecycle guidance: %q", irrelevant)
	}

	relevant := lifecycleContext(Event{
		HookEventName: eventUserPromptSubmit,
		SessionID:     "relevant-session",
		ToolInput: map[string]any{
			"prompt": "Implement the parser fix in internal/parser.go.",
		},
	})
	if !strings.Contains(relevant, "todo list") {
		t.Fatalf("coding prompt missing lifecycle guidance: %q", relevant)
	}

	becameIrrelevant := lifecycleContext(Event{
		HookEventName: eventUserPromptSubmit,
		SessionID:     "relevant-session",
		ToolInput: map[string]any{
			"prompt": "Tell me a short joke about penguins.",
		},
	})
	if becameIrrelevant != "" {
		t.Fatalf(
			"irrelevant prompt inherited prior guidance: %q",
			becameIrrelevant,
		)
	}
}

func TestPostToolBatchGuidanceRequiresUnreviewedRelevantToolResult(t *testing.T) {
	t.Setenv(lifecycleStateRootEnv, t.TempDir())

	sessionID := "post-tool-batch-session"
	if context := lifecycleContext(Event{
		HookEventName: "PostToolBatch",
		SessionID:     sessionID,
	}); context != "" {
		t.Fatalf("empty batch received lifecycle guidance: %q", context)
	}

	lifecycleContext(Event{
		HookEventName: eventPostToolUse,
		SessionID:     sessionID,
		ToolName:      "Edit",
		ToolInput: map[string]any{
			"file_path": "internal/parser.go",
		},
	})

	first := lifecycleContext(Event{
		HookEventName: "PostToolBatch",
		SessionID:     sessionID,
	})
	if !strings.Contains(first, "Review tool results") {
		t.Fatalf("relevant batch missing lifecycle guidance: %q", first)
	}

	if repeated := lifecycleContext(Event{
		HookEventName: "PostToolBatch",
		SessionID:     sessionID,
	}); repeated != "" {
		t.Fatalf("unchanged batch repeated lifecycle guidance: %q", repeated)
	}
}

func TestPostToolBatchGuidanceRejectsUnsupportedLanguageEdit(t *testing.T) {
	t.Setenv(lifecycleStateRootEnv, t.TempDir())

	sessionID := "unsupported-language-session"
	lifecycleContext(Event{
		HookEventName: eventPostToolUse,
		SessionID:     sessionID,
		ToolName:      "Edit",
		ToolInput: map[string]any{
			"file_path": "notes.custom-source",
		},
	})

	if context := lifecycleContext(Event{
		HookEventName: "PostToolBatch",
		SessionID:     sessionID,
	}); context != "" {
		t.Fatalf("unsupported-language batch received guidance: %q", context)
	}
}

func TestSubagentStopGuidanceConsumesBalancedStartInSameSession(t *testing.T) {
	t.Setenv(lifecycleStateRootEnv, t.TempDir())

	sessionID := "balanced-subagent-session"
	lifecycleContext(Event{
		HookEventName: eventUserPromptSubmit,
		SessionID:     sessionID,
		ToolInput: map[string]any{
			"prompt": "Refactor the Go hook runner.",
		},
	})

	started := lifecycleContext(Event{
		HookEventName: "SubagentStart",
		SessionID:     sessionID,
	})
	if !strings.Contains(started, "delegated work") {
		t.Fatalf("balanced subagent start missing guidance: %q", started)
	}

	stopped := lifecycleContext(Event{
		HookEventName: "SubagentStop",
		SessionID:     sessionID,
	})
	if !strings.Contains(stopped, "accepting subagent work") {
		t.Fatalf("balanced subagent stop missing guidance: %q", stopped)
	}

	if duplicate := lifecycleContext(Event{
		HookEventName: "SubagentStop",
		SessionID:     sessionID,
	}); duplicate != "" {
		t.Fatalf("unbalanced duplicate stop received guidance: %q", duplicate)
	}

	if anotherSession := lifecycleContext(Event{
		HookEventName: "SubagentStop",
		SessionID:     "different-session",
	}); anotherSession != "" {
		t.Fatalf("different session consumed prior start: %q", anotherSession)
	}
}
