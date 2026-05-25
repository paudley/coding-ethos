// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package policy

import (
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
)

type Decision struct {
	Evidence     map[string]any           `json:"evidence,omitempty"`
	Decision     string                   `json:"decision"`
	Message      string                   `json:"message"`
	PolicyID     string                   `json:"policy_id"`
	Severity     string                   `json:"severity"`
	Suggestion   string                   `json:"suggestion,omitempty"`
	Diagnostics  []diagnostics.Diagnostic `json:"diagnostics,omitempty"`
	PrincipleIDs []string                 `json:"principle_ids,omitempty"`
}

func NewDecision(decision string, policy Policy) Decision {
	return Decision{
		Decision:     decision,
		PolicyID:     policy.ID,
		Severity:     policy.DefaultSeverity,
		PrincipleIDs: append([]string(nil), policy.PrincipleIDs...),
		Message:      policy.Message,
		Suggestion:   policy.Suggestion,
	}
}

func (decision Decision) EvidenceFiles() []string {
	if files := evidenceStringList(decision.Evidence, "files"); len(files) > 0 {
		return files
	}

	if files := evidenceStringList(decision.Evidence, "staged_files"); len(files) > 0 {
		return files
	}

	return firstEvidenceStringList(
		decision.Evidence,
		"file",
		"path",
		"target_path",
	)
}

func (decision Decision) EvidenceCommands() []string {
	if commands := evidenceStringList(decision.Evidence, "commands"); len(commands) > 0 {
		return commands
	}

	if command := decision.EvidenceString("command"); command != "" {
		return []string{command}
	}

	if argv := evidenceStringList(decision.Evidence, "argv"); len(argv) > 0 {
		return []string{strings.Join(argv, " ")}
	}

	return shellCommandEvidence(decision.Evidence)
}

func (decision Decision) EvidenceCommand() string {
	commands := decision.EvidenceCommands()
	if len(commands) == 0 {
		return ""
	}

	return commands[0]
}

func (decision Decision) EvidenceString(key string) string {
	return evidenceString(decision.Evidence, key)
}

func (decision Decision) EvidenceStrings(key string) []string {
	return evidenceStringList(decision.Evidence, key)
}

func (decision Decision) EvidenceSkillID() string {
	return decision.EvidenceString("skill_id")
}

func (decision Decision) EvidenceTool() string {
	return decision.EvidenceString("tool")
}

func (decision Decision) EvidenceImplementation() string {
	return decision.EvidenceString("implementation")
}

func evidenceStringList(evidence map[string]any, key string) []string {
	if len(evidence) == 0 {
		return nil
	}

	value, found := evidence[key]
	if !found {
		return nil
	}

	switch typed := value.(type) {
	case []string:
		return normalizedEvidenceStrings(typed)
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if ok {
				values = append(values, text)
			}
		}

		return normalizedEvidenceStrings(values)
	default:
		return nil
	}
}

func evidenceString(evidence map[string]any, key string) string {
	if len(evidence) == 0 {
		return ""
	}

	value, found := evidence[key]
	if !found {
		return ""
	}

	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []string:
		return strings.Join(normalizedEvidenceStrings(typed), " ")
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if ok {
				values = append(values, text)
			}
		}

		return strings.Join(normalizedEvidenceStrings(values), " ")
	default:
		return ""
	}
}

func firstEvidenceStringList(evidence map[string]any, keys ...string) []string {
	for _, key := range keys {
		value := evidenceString(evidence, key)
		if value != "" {
			return []string{value}
		}
	}

	return nil
}

func shellCommandEvidence(evidence map[string]any) []string {
	value, found := evidence["shell_commands"]
	if !found {
		return nil
	}

	items, found := value.([]map[string]any)
	if found {
		return shellCommandEvidenceFromMaps(items)
	}

	rawItems, found := value.([]any)
	if !found {
		return nil
	}

	items = make([]map[string]any, 0, len(rawItems))
	for _, rawItem := range rawItems {
		item, ok := rawItem.(map[string]any)
		if ok {
			items = append(items, item)
		}
	}

	return shellCommandEvidenceFromMaps(items)
}

func shellCommandEvidenceFromMaps(items []map[string]any) []string {
	commands := make([]string, 0, len(items))
	for _, item := range items {
		if argv := evidenceStringList(item, "argv"); len(argv) > 0 {
			commands = append(commands, strings.Join(argv, " "))

			continue
		}

		if name := evidenceString(item, "name"); name != "" {
			commands = append(commands, name)
		}
	}

	return normalizedEvidenceStrings(commands)
}

func normalizedEvidenceStrings(values []string) []string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		text := strings.TrimSpace(value)
		if text != "" {
			normalized = append(normalized, text)
		}
	}

	return normalized
}
