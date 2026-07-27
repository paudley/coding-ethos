// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agenthooks

import (
	"slices"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"blackcat.ca/coding-ethos/go/internal/toolaliases"
)

type kimiHook struct {
	Event          string `json:"event"             toml:"event"`
	Matcher        string `json:"matcher,omitempty" toml:"matcher,omitempty"`
	Command        string `json:"command"           toml:"command"`
	TimeoutSeconds int    `json:"timeout"           toml:"timeout"`
}

type kimiSettings struct {
	Hooks []kimiHook `json:"hooks"`
}

func buildKimiSettings(
	specs []HookSpec,
	hookCommand string,
	timeoutSeconds int,
) kimiSettings {
	command := strings.TrimSpace(hookCommand) + " --provider kimi"
	settings := kimiSettings{
		Hooks: make([]kimiHook, 0, len(specs)+kimiObservationEventCount),
	}

	for _, spec := range specs {
		if spec.Event == "PostToolBatch" {
			continue
		}

		settings.Hooks = append(settings.Hooks, kimiHook{
			Event:          spec.Event,
			Matcher:        spec.Tool,
			Command:        command,
			TimeoutSeconds: timeoutSeconds,
		})
	}

	for _, alias := range toolaliases.ProviderAliases(
		toolaliases.ProviderKimi,
		toolaliases.CanonicalNoop,
	) {
		settings.Hooks = append(
			settings.Hooks,
			kimiHook{
				Event:          eventPreToolUse,
				Matcher:        providerMatcher(alias),
				Command:        command,
				TimeoutSeconds: timeoutSeconds,
			},
			kimiHook{
				Event:          eventPostToolUse,
				Matcher:        providerMatcher(alias),
				Command:        command,
				TimeoutSeconds: timeoutSeconds,
			},
		)
	}

	for _, event := range kimiObservationEvents() {
		settings.Hooks = append(settings.Hooks, kimiHook{
			Event:          event,
			Command:        command,
			TimeoutSeconds: timeoutSeconds,
		})
	}

	return settings
}

const kimiObservationEventCount = 7

func kimiObservationEvents() []string {
	return []string{
		"PostToolUseFailure",
		eventPermissionRequest,
		"PermissionResult",
		"StopFailure",
		"Interrupt",
		"PostCompact",
		"Notification",
	}
}

const (
	kimiManagedHooksStart = "# BEGIN coding-ethos managed Kimi hooks"
	kimiManagedHooksEnd   = "# END coding-ethos managed Kimi hooks"
)

func ensureKimiConfig(content string, settings kimiSettings) string {
	base := removeKimiManagedHooks(content)

	var builder strings.Builder

	builder.WriteString(strings.TrimRight(base, "\n"))

	if builder.Len() > 0 {
		builder.WriteString("\n\n")
	}

	builder.WriteString(kimiManagedHooksStart)
	builder.WriteByte('\n')

	for _, hook := range settings.Hooks {
		builder.WriteString("[[hooks]]\n")
		builder.WriteString("event = " + tomlString(hook.Event) + "\n")

		if hook.Matcher != "" {
			builder.WriteString("matcher = " + tomlString(hook.Matcher) + "\n")
		}

		builder.WriteString("command = " + tomlString(hook.Command) + "\n")
		builder.WriteString("timeout = ")
		builder.WriteString(strconv.Itoa(hook.TimeoutSeconds))
		builder.WriteString("\n\n")
	}

	builder.WriteString(kimiManagedHooksEnd)
	builder.WriteByte('\n')

	return builder.String()
}

func removeKimiManagedHooks(content string) string {
	lines := strings.Split(content, "\n")
	output := make([]string, 0, len(lines))
	inManagedBlock := false

	for _, line := range lines {
		switch strings.TrimSpace(line) {
		case kimiManagedHooksStart:
			inManagedBlock = true
		case kimiManagedHooksEnd:
			inManagedBlock = false
		default:
			if !inManagedBlock {
				output = append(output, line)
			}
		}
	}

	return strings.TrimRight(strings.Join(output, "\n"), "\n")
}

func kimiConfigContainsExpectedHooks(
	content string,
	expected kimiSettings,
) bool {
	var actual kimiSettings

	err := toml.Unmarshal([]byte(content), &actual)
	if err != nil {
		return false
	}

	for _, expectedHook := range expected.Hooks {
		if !slices.Contains(actual.Hooks, expectedHook) {
			return false
		}
	}

	return true
}
