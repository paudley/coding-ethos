// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import (
	"os"
	"sort"
	"strings"
)

const (
	providerClaude = "claude"
	providerCodex  = "codex"
	providerGemini = "gemini"
)

const (
	eventPreToolUse       = "PreToolUse"
	eventPostToolUse      = "PostToolUse"
	eventSessionStart     = "SessionStart"
	eventUserPromptSubmit = "UserPromptSubmit"
	eventStop             = "Stop"
	toolBash              = "Bash"
)

type Event struct {
	ProviderHint   string         `json:"provider,omitempty"`
	ToolInput      map[string]any `json:"tool_input,omitempty"`
	ToolResponse   map[string]any `json:"tool_response,omitempty"`
	Cwd            string         `json:"cwd,omitempty"`
	HookEventName  string         `json:"hook_event_name"`
	Matcher        string         `json:"matcher,omitempty"`
	SessionID      string         `json:"session_id,omitempty"`
	Source         string         `json:"source,omitempty"`
	ToolName       string         `json:"tool_name,omitempty"`
	TranscriptPath string         `json:"transcript_path,omitempty"`
}

func (event Event) Provider() string {
	providerHint := strings.ToLower(strings.TrimSpace(event.ProviderHint))
	switch {
	case strings.Contains(providerHint, providerGemini):
		return providerGemini
	case strings.Contains(providerHint, providerCodex):
		return providerCodex
	case strings.Contains(providerHint, providerClaude):
		return providerClaude
	}

	source := strings.ToLower(strings.TrimSpace(event.Source))
	switch {
	case strings.Contains(source, providerGemini):
		return providerGemini
	case strings.Contains(source, providerCodex):
		return providerCodex
	case strings.Contains(source, providerClaude):
		return providerClaude
	default:
		return providerFromEnvironment()
	}
}

func providerFromEnvironment() string {
	switch {
	case strings.TrimSpace(os.Getenv("CODEX_THREAD_ID")) != "" ||
		strings.TrimSpace(os.Getenv("CODEX_CI")) != "" ||
		strings.TrimSpace(os.Getenv("CODEX_MANAGED_BY_NPM")) != "":
		return providerCodex
	case strings.TrimSpace(os.Getenv("GEMINI_CLI")) != "":
		return providerGemini
	case strings.TrimSpace(os.Getenv("CLAUDECODE")) != "" ||
		strings.TrimSpace(os.Getenv("CLAUDE_CODE_ENTRYPOINT")) != "":
		return providerClaude
	default:
		return ""
	}
}

func (event Event) Command() string {
	if event.ToolInput == nil {
		return ""
	}

	command, ok := event.ToolInput["command"].(string)
	if !ok {
		return ""
	}

	return command
}

func (event Event) Files() []string {
	if event.ToolInput == nil {
		return nil
	}

	files := []string{}

	for _, key := range []string{"file_path", "path", "notebook_path"} {
		if file, ok := event.ToolInput[key].(string); ok && file != "" {
			files = append(files, file)
		}
	}

	for _, key := range []string{"files", "paths"} {
		files = append(files, stringList(event.ToolInput[key])...)
	}

	return dedupeStrings(files)
}

func (event Event) Content() string {
	if event.ToolInput == nil {
		return ""
	}

	for _, key := range []string{"content", "new_string", "prompt", "text"} {
		if content, ok := event.ToolInput[key].(string); ok {
			return content
		}
	}

	return ""
}

func (event Event) OldContent() string {
	if event.ToolInput == nil {
		return ""
	}

	for _, key := range []string{"old_string", "old_content", "before"} {
		if content, ok := event.ToolInput[key].(string); ok {
			return content
		}
	}

	return ""
}

func (event Event) ToolOutput() string {
	if event.ToolResponse == nil {
		return ""
	}

	output := firstStringValue(
		event.ToolResponse,
		"stdout",
		"output",
		"result",
		"text",
		"content",
	)

	stderr := firstStringValue(event.ToolResponse, "stderr")
	if output != "" && stderr != "" {
		return output + "\n" + stderr
	}

	if output != "" {
		return output
	}

	return stderr
}

func (event Event) ReturnCode() int {
	if event.ToolResponse == nil {
		return 0
	}

	for _, key := range []string{
		"return_code",
		"returnCode",
		"exitCode",
		"exit_code",
		"code",
	} {
		value, ok := event.ToolResponse[key]
		if !ok {
			continue
		}

		switch typed := value.(type) {
		case int:
			return typed
		case float64:
			return int(typed)
		case string:
			if typed == "" || typed == "0" {
				return 0
			}

			return 1
		}
	}

	if responseStatusFailed(event.ToolResponse) {
		return 1
	}

	return 0
}

func (event Event) HasReturnCode() bool {
	if event.ToolResponse == nil {
		return false
	}

	for _, key := range []string{
		"return_code",
		"returnCode",
		"exitCode",
		"exit_code",
		"code",
	} {
		if _, ok := event.ToolResponse[key]; ok {
			return true
		}
	}

	return responseStatusFailed(event.ToolResponse)
}

func responseStatusFailed(response map[string]any) bool {
	for _, key := range []string{"status", "state", "outcome"} {
		value, ok := response[key].(string)
		if !ok {
			continue
		}

		switch strings.ToLower(strings.TrimSpace(value)) {
		case "blocked", "error", "failed", "failure":
			return true
		}
	}

	return false
}

func (event Event) ToolInputKeys() []string {
	return mapKeys(event.ToolInput)
}

func (event Event) ToolResponseKeys() []string {
	return mapKeys(event.ToolResponse)
}

func firstStringValue(values map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := values[key].(string)
		if ok && value != "" {
			return value
		}
	}

	return ""
}

func mapKeys(values map[string]any) []string {
	if values == nil {
		return nil
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		if key != "" {
			keys = append(keys, key)
		}
	}

	sort.Strings(keys)

	return keys
}

func stringList(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok && text != "" {
				items = append(items, text)
			}
		}

		return items
	case string:
		if typed == "" {
			return nil
		}

		return []string{typed}
	default:
		return nil
	}
}

func dedupeStrings(values []string) []string {
	seen := map[string]bool{}

	deduped := make([]string, 0, len(values))
	for _, value := range values {
		key := value
		if key == "" || seen[key] {
			continue
		}

		seen[key] = true
		deduped = append(deduped, key)
	}

	return deduped
}
