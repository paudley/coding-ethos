// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestHookCLIBlocksBashBypass(t *testing.T) {
	t.Parallel()

	result, status, stderr := runHookCLI(
		t,
		hookJSON(t, "PreToolUse", "Bash", map[string]any{
			"command": "git commit --no-verify -m test",
		}),
	)
	if status != 2 {
		t.Fatalf("status mismatch: got %d", status)
	}

	if !hookOutputDenies(result) {
		t.Fatalf("result status mismatch: %#v", result)
	}
	if stderr == "" {
		t.Fatalf("--json agent hook output must include a compact blocking reason on stderr")
	}
	trimmedStderr := strings.TrimSpace(stderr)
	if strings.Contains(trimmedStderr, "format: toon") ||
		strings.Contains(trimmedStderr, "\n") {
		t.Fatalf("--json agent hook stderr must be compact provider advice:\n%s", stderr)
	}
}

func TestHookCLIAdvisesMultiEditPythonPolicy(t *testing.T) {
	t.Parallel()

	result, status, _ := runHookCLI(
		t,
		hookJSON(t, "PreToolUse", "MultiEdit", map[string]any{
			"file_path":  "src/app.py",
			"new_string": "try:\n    import missing\nexcept ImportError:\n    missing = None\n",
		}),
	)
	if status != 0 {
		t.Fatalf("status mismatch: got %d", status)
	}

	if hookOutputDenies(result) {
		t.Fatalf("result should not deny: %#v", result)
	}
}

func TestHookCLIAllowsUnknownEventAndTool(t *testing.T) {
	t.Parallel()

	result, status, _ := runHookCLI(
		t,
		hookJSON(t, "SessionStart", "Unknown", map[string]any{
			"command": "git status",
		}),
	)
	if status != 0 {
		t.Fatalf("status mismatch: got %d", status)
	}

	if hookOutputDenies(result) {
		t.Fatalf("result should not deny: %#v", result)
	}
}

func runHookCLI(t *testing.T, stdin string) (map[string]any, int, string) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "coding-ethos-hook")
	build := exec.CommandContext(
		context.Background(),
		"go",
		"build",
		"-buildvcs=false",
		"-o",
		bin,
		".",
	)

	buildOutput, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("build hook CLI: %v\n%s", err, buildOutput)
	}

	bundlePath := writeCLITestBundle(t)
	cmd := exec.CommandContext(context.Background(), bin, "--bundle", bundlePath, "--json")
	cmd.Stdin = bytes.NewBufferString(stdin)

	var (
		stdout bytes.Buffer
		stderr bytes.Buffer
	)

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	status := 0

	if err != nil {
		exitErr := &exec.ExitError{}

		ok := errors.As(err, &exitErr)
		if !ok {
			t.Fatalf("run hook CLI: %v\n%s", err, stderr.String())
		}

		status = exitErr.ExitCode()
	}

	var result map[string]any

	err = json.Unmarshal(stdout.Bytes(), &result)
	if err != nil {
		t.Fatalf(
			"decode hook result: %v\nstdout:\n%s\nstderr:\n%s",
			err,
			stdout.String(),
			stderr.String(),
		)
	}

	return result, status, stderr.String()
}

func hookOutputDenies(result map[string]any) bool {
	if result["status"] == "blocked" {
		return true
	}

	hookSpecific, ok := result["hookSpecificOutput"].(map[string]any)
	if !ok {
		return false
	}

	return hookSpecific["permissionDecision"] == "deny"
}

func hookJSON(
	t *testing.T,
	eventName string,
	toolName string,
	toolInput map[string]any,
) string {
	t.Helper()

	payload, err := json.Marshal(map[string]any{
		"hook_event_name": eventName,
		"provider":        "codex",
		"tool_name":       toolName,
		"tool_input":      toolInput,
	})
	if err != nil {
		t.Fatalf("marshal hook payload: %v", err)
	}

	return string(payload)
}

func writeCLITestBundle(t *testing.T) string {
	t.Helper()

	bundle := policy.ExampleBundle()
	writeDispatch := bundle.Dispatch.Hooks["PreToolUse"]["Write"]
	bundle.Dispatch.Hooks["PreToolUse"]["Edit"] = writeDispatch
	bundle.Dispatch.Hooks["PreToolUse"]["MultiEdit"] = writeDispatch
	path := filepath.Join(t.TempDir(), "policy-bundle.json")

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create bundle: %v", err)
	}
	defer file.Close()

	err = policy.EncodeBundle(file, bundle)
	if err != nil {
		t.Fatalf("encode bundle: %v", err)
	}

	return path
}
