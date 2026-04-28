// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import "strings"

const (
	providerClaude = "claude"
	providerCodex  = "codex"
	providerGemini = "gemini"
)

type Event struct {
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
	source := strings.ToLower(strings.TrimSpace(event.Source))
	switch {
	case strings.Contains(source, providerGemini):
		return providerGemini
	case strings.Contains(source, providerCodex):
		return providerCodex
	case strings.Contains(source, providerClaude):
		return providerClaude
	default:
		return providerClaude
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

	return 0
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
