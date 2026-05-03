// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import (
	"strings"
	"testing"
)

func FuzzDecodeEvent(f *testing.F) {
	for _, seed := range []string{
		`{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"git status"},"cwd":"/repo","provider":"codex"}`,
		`{"event":"BeforeTool","tool":{"name":"bash","input":{"command":"FILE=.claude/settings.json cat > ${FILE}"}}}`,
		`{"hookEventName":"PostToolUse","toolName":"Bash","toolResponse":{"stdout":"ok","exitCode":0}}`,
		`{"source":"claude","tool_name":"write_file","arguments":{"path":".claude/MEMORY.md","content":"note"}}`,
		`{"runtime":"gemini","tool_call":{"tool":"run_shell","args":{"command":"ruff check ."}}}`,
		`{`,
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		event, err := DecodeEvent(strings.NewReader(input))
		if err != nil {
			return
		}
		provider := event.Provider()
		switch provider {
		case "", providerClaude, providerCodex, providerGemini:
		default:
			t.Fatalf("unexpected provider %q from %#v", provider, event)
		}
		for _, file := range event.Files() {
			if strings.TrimSpace(file) == "" {
				t.Fatalf("empty file path from %#v", event.ToolInput)
			}
		}
		if event.ReturnCode() < 0 {
			t.Fatalf("negative return code from %#v", event.ToolResponse)
		}
	})
}
