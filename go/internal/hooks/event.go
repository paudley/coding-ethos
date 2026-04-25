// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

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
