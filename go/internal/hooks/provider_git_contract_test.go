// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/hooks"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestGitRoutingIsProviderNeutral(t *testing.T) {
	t.Parallel()

	assertRouteProducerProviderNeutral(t, "git_wrapper_enforcement.go")
}

func TestRewriteRouteProducersAreProviderNeutral(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"git_wrapper_enforcement.go",
		"lint_tool_capture.go",
		"python_runtime_route.go",
	} {
		path := path
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			assertRouteProducerProviderNeutral(t, path)
		})
	}
}

func assertRouteProducerProviderNeutral(t *testing.T, path string) {
	t.Helper()

	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read route source %s: %v", path, err)
	}
	source := string(payload)
	for _, forbidden := range []string{
		"providerClaude",
		"providerCodex",
		"providerGemini",
		"Provider()",
		"ProviderHint",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("%s must not branch on provider; found %q", path, forbidden)
		}
	}
}

func runProviderGitCommand(t *testing.T, provider string, command string) Result {
	t.Helper()

	return runProviderGitCommandInCWD(t, provider, command, t.TempDir())
}

func runProviderGitCommandInCWD(t *testing.T, provider string, command string, cwd string) Result {
	t.Helper()

	payload := providerGitPayload(provider, cwd, command)
	event, err := DecodeEvent(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("decode %s payload: %v", provider, err)
	}
	result, err := Run(policy.ExampleBundle(), Options{Event: event})
	if err != nil {
		t.Fatalf("run %s hook: %v", provider, err)
	}

	return result
}

func providerGitPayload(provider string, cwd string, command string) string {
	switch provider {
	case "claude":
		return fmt.Sprintf(`{
			"provider": "claude",
			"hook_event_name": "PreToolUse",
			"cwd": %q,
			"tool_name": "Bash",
			"tool_input": {"command": %q}
		}`, cwd, command)
	case "codex":
		return fmt.Sprintf(`{
			"provider": "codex",
			"event": "PreToolUse",
			"cwd": %q,
			"tool": "exec_command",
			"input": {"command": %q}
		}`, cwd, command)
	case "gemini-cli":
		return fmt.Sprintf(`{
			"provider": "gemini-cli",
			"hookEventName": "BeforeTool",
			"cwd": %q,
			"toolName": "run_shell_command",
			"toolInput": {"command": %q}
		}`, cwd, command)
	default:
		panic("unsupported provider " + provider)
	}
}

func hookTamperProbeCommand() string {
	return "rm /repo/.git/coding-ethos-" +
		"hooks/coding-ethos-git-hook"
}
