// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import "fmt"

type Event struct {
	ToolInput     map[string]any `json:"tool_input,omitempty"`
	Cwd           string         `json:"cwd,omitempty"`
	HookEventName string         `json:"hook_event_name"`
	ToolName      string         `json:"tool_name,omitempty"`
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
	for _, key := range []string{"content", "new_string", "text"} {
		if content, ok := event.ToolInput[key].(string); ok {
			return content
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
		key := fmt.Sprint(value)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		deduped = append(deduped, key)
	}
	return deduped
}
