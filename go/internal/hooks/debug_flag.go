// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.uber.org/zap"

	"blackcat.ca/coding-ethos/go/internal/debuglog"
	"blackcat.ca/coding-ethos/go/internal/shellparse"
	"blackcat.ca/coding-ethos/go/internal/shellquote"
)

const debugEnvAssignment = debuglog.EnvName + "=1"

func activateDebugForEvent(event Event) {
	command := event.Command()
	if !commandRequestsDebug(command) {
		return
	}

	runDir := os.Getenv(debuglog.RunDirEnv)

	err := debuglog.Configure(true, runDir, os.Stderr)
	if err != nil {
		_, _ = os.Stderr.WriteString("WARN: configure debug logging: " + err.Error() + "\n")

		return
	}

	debuglog.Debug(
		"hook.debug.enabled",
		zap.String("run_dir", runDir),
		zap.String("command_shape", commandShapeHash(command)),
	)
}

func commandRequestsDebug(command string) bool {
	commands, err := shellparse.Commands(command)
	if err != nil {
		return false
	}

	return slices.ContainsFunc(commands, shellCommandRequestsDebug)
}

func shellCommandRequestsDebug(command shellparse.Command) bool {
	return slices.Contains(command.Argv, debuglog.Flag) ||
		slices.Contains(command.Assignments, debugEnvAssignment) ||
		(filepath.Base(command.Name) == "export" &&
			slices.Contains(command.Argv, debugEnvAssignment))
}

func eventWithoutDebugFlag(event Event) (Event, bool) {
	if event.ToolName != toolBash {
		return event, false
	}

	command := strings.TrimSpace(event.Command())
	if command == "" || !commandRequestsDebug(command) {
		return event, false
	}

	rewritten, stripped := stripDebugFlagFromShellCommand(command)
	if !stripped {
		return event, false
	}

	debuglog.Debug(
		"hook.debug.flag_stripped",
		zap.String("original_shape", commandShapeHash(command)),
		zap.String("rewritten_shape", commandShapeHash(rewritten)),
	)

	event.ToolInput = updatedBashInput(event.ToolInput, rewritten)

	return event, true
}

func routeWithDebugEnv(event Event, route InspectionRoute) InspectionRoute {
	command := routeCommand(event, route)
	rewritten := rewriteCommandWithDebugEnv(command)

	if route.Block {
		return route
	}

	if route.UpdatedInput == nil {
		route.UpdatedInput = updatedBashInput(event.ToolInput, rewritten)
	} else {
		route.UpdatedInput = updatedBashInput(route.UpdatedInput, rewritten)
	}

	if route.Reason == "" {
		route.Reason = "Captured coding-ethos debug flag."
	}

	route.Rewrite = true

	return route
}

func routeCommand(event Event, route InspectionRoute) string {
	if route.UpdatedInput != nil {
		command, ok := route.UpdatedInput["command"].(string)
		if ok {
			return command
		}
	}

	return event.Command()
}

func logMakeEventBoundary(event Event) {
	if event.ToolName != toolBash || !commandRequestsDebug(event.Command()) ||
		!shellCommandIncludesMake(event.Command()) {
		return
	}

	switch event.HookEventName {
	case eventPreToolUse:
		debuglog.Debug(
			"make.process.enter",
			zap.String("cwd", event.Cwd),
			zap.String("command_shape", commandShapeHash(event.Command())),
			zap.String("provider", event.Provider()),
			zap.String("source", event.Source),
		)
	case eventPostToolUse:
		debuglog.Debug(
			"make.process.exit",
			zap.String("cwd", event.Cwd),
			zap.String("command_shape", commandShapeHash(event.Command())),
			zap.String("provider", event.Provider()),
			zap.String("source", event.Source),
			zap.Int("exit_code", eventExitCode(event)),
		)
	}
}

func shellCommandIncludesMake(command string) bool {
	commands, err := shellparse.Commands(command)
	if err != nil {
		return strings.Contains(command, "make")
	}

	return slices.ContainsFunc(commands, func(parsed shellparse.Command) bool {
		return shellCommandTokenIncludesMake(parsed.Name) ||
			slices.ContainsFunc(parsed.Argv, shellCommandTokenIncludesMake)
	})
}

func shellCommandTokenIncludesMake(token string) bool {
	return filepath.Base(token) == "make"
}

func eventExitCode(event Event) int {
	if event.ToolResponse == nil {
		return -1
	}

	for _, key := range []string{"exit_code", "status"} {
		switch value := event.ToolResponse[key].(type) {
		case int:
			return value
		case float64:
			return int(value)
		}
	}

	return -1
}

func stripDebugFlagFromShellCommand(command string) (string, bool) {
	tokens, err := shellparse.ControlFields(command)
	if err != nil {
		return command, false
	}

	stripped := make([]string, 0, len(tokens))
	found := false

	for _, token := range tokens {
		if token == debuglog.Flag {
			found = true

			continue
		}

		stripped = append(stripped, token)
	}

	if !found {
		return command, false
	}

	return shellCommandFromTokens(stripped), true
}

func rewriteCommandWithDebugEnv(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return "export " + debugEnvAssignment
	}

	if commandRequestsDebug(command) {
		return command
	}

	return "export " + debugEnvAssignment + "; " + command
}

func shellCommandFromTokens(tokens []string) string {
	parts := make([]string, 0, len(tokens))

	for _, token := range tokens {
		if shellControlToken(token) || shellAssignmentToken(token) {
			parts = append(parts, token)

			continue
		}

		parts = append(parts, shellquote.Arg(token))
	}

	return strings.Join(parts, " ")
}

func shellControlToken(token string) bool {
	switch token {
	case ";", "&&", "||", "|", "|&", "&":
		return true
	default:
		return false
	}
}

func shellAssignmentToken(token string) bool {
	if strings.ContainsAny(token, "/ \t") {
		return false
	}

	name, _, found := strings.Cut(token, "=")
	if !found || name == "" {
		return false
	}

	return filepath.Base(name) == name
}
