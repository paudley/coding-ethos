// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/hooks"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestHookCLIBlocksBashBypass(t *testing.T) {
	result, status := runHookCLI(t, `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"git commit --no-verify -m test"}}`)
	if status != 2 {
		t.Fatalf("status mismatch: got %d", status)
	}
	if result.Status != "blocked" {
		t.Fatalf("result status mismatch: %#v", result)
	}
}

func TestHookCLIAdvisesMultiEditPythonPolicy(t *testing.T) {
	result, status := runHookCLI(t, `{"hook_event_name":"PreToolUse","tool_name":"MultiEdit","tool_input":{"file_path":"src/app.py","new_string":"try:\n    import missing\nexcept ImportError:\n    missing = None\n"}}`)
	if status != 0 {
		t.Fatalf("status mismatch: got %d", status)
	}
	if result.Status != "allowed" {
		t.Fatalf("result status mismatch: %#v", result)
	}
	if len(result.Decisions) != 1 {
		t.Fatalf("decision count mismatch: %#v", result.Decisions)
	}
	if result.Decisions[0].Decision != "advise" {
		t.Fatalf("decision mismatch: %#v", result.Decisions[0])
	}
}

func TestHookCLIAllowsUnknownEventAndTool(t *testing.T) {
	result, status := runHookCLI(t, `{"hook_event_name":"SessionStart","tool_name":"Unknown","tool_input":{"command":"git status"}}`)
	if status != 0 {
		t.Fatalf("status mismatch: got %d", status)
	}
	if result.Status != "allowed" {
		t.Fatalf("result status mismatch: %#v", result)
	}
	if len(result.Decisions) != 0 {
		t.Fatalf("expected no decisions, got %#v", result.Decisions)
	}
}

func runHookCLI(t *testing.T, stdin string) (hooks.Result, int) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "coding-ethos-hook")
	build := exec.Command("go", "build", "-buildvcs=false", "-o", bin, ".")
	buildOutput, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("build hook CLI: %v\n%s", err, buildOutput)
	}
	bundlePath := writeCLITestBundle(t)
	cmd := exec.Command(bin, "--bundle", bundlePath, "--json")
	cmd.Stdin = bytes.NewBufferString(stdin)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	status := 0
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run hook CLI: %v\n%s", err, stderr.String())
		}
		status = exitErr.ExitCode()
	}
	var result hooks.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode hook result: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	return result, status
}

func writeCLITestBundle(t *testing.T) string {
	t.Helper()
	bundle := policy.ExampleBundle()
	bundle.Dispatch.Hooks["PreToolUse"]["Edit"] = bundle.Dispatch.Hooks["PreToolUse"]["Write"]
	bundle.Dispatch.Hooks["PreToolUse"]["MultiEdit"] = bundle.Dispatch.Hooks["PreToolUse"]["Write"]
	path := filepath.Join(t.TempDir(), "policy-bundle.json")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create bundle: %v", err)
	}
	defer file.Close()
	if err := policy.EncodeBundle(file, bundle); err != nil {
		t.Fatalf("encode bundle: %v", err)
	}
	return path
}
