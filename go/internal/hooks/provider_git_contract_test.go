// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks_test

import (
	"fmt"
	"os"
	"strings"
	"testing"
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

func providerGitPayload(provider, cwd, command string) string {
	switch provider {
	case "claude":
		return fmt.Sprintf(`{
			"provider": "claude",
			"hook_event_name": "PreToolUse",
			"cwd": %q,
			"tool_name": "Bash",
			"tool_input": {"command": %q}
		}`, cwd, command)
	case providerCodex:
		return fmt.Sprintf(`{
			"provider": %q,
			"event": "PreToolUse",
			"cwd": %q,
			"tool": "exec_command",
			"input": {"command": %q}
		}`, providerCodex, cwd, command)
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
