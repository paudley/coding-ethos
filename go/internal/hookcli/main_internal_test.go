// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hookcli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/hooks"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/testlock"
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
		t.Fatalf(
			"--json agent hook output must include a compact blocking reason on stderr",
		)
	}

	trimmedStderr := strings.TrimSpace(stderr)
	if strings.Contains(trimmedStderr, "format: toon") ||
		strings.Contains(trimmedStderr, "\n") {
		t.Fatalf(
			"--json agent hook stderr must be compact provider advice:\n%s",
			stderr,
		)
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

func TestReadBundleAndPrintBlockedDirectly(t *testing.T) {
	t.Parallel()

	bundlePath := writeCLITestBundle(t)

	bundle, err := readBundle(bundlePath)
	if err != nil {
		t.Fatalf("readBundle() returned error: %v", err)
	}

	if bundle.BundleID == "" {
		t.Fatalf("bundle id should be populated: %#v", bundle)
	}

	output := captureHookStderr(t, func() {
		printBlocked(os.Stderr, hooks.Result{
			Status:   "blocked",
			Provider: "codex",
			Decisions: []policy.Decision{{
				PolicyID: "shell.forbidden_strings",
				Decision: "block",
				Message:  "blocked command",
			}},
		})
	})
	if !strings.Contains(output, "blocked command") {
		t.Fatalf("blocked output = %q", output)
	}
}

func TestRunWithIOBlocksBashBypass(t *testing.T) {
	t.Parallel()

	var (
		stdout bytes.Buffer
		stderr bytes.Buffer
	)

	status := runWithIO(
		[]string{"--bundle", writeCLITestBundle(t), "--json"},
		strings.NewReader(hookJSON(t, "PreToolUse", "Bash", map[string]any{
			"command": "git commit --no-verify -m test",
		})),
		&stdout,
		&stderr,
	)
	if status != blockedExitCode {
		t.Fatalf("status = %d, want %d", status, blockedExitCode)
	}

	result := map[string]any{}

	err := json.Unmarshal(stdout.Bytes(), &result)
	if err != nil {
		t.Fatalf("decode result: %v\n%s", err, stdout.String())
	}

	if !hookOutputDenies(result) {
		t.Fatalf("result should deny: %#v", result)
	}

	if !strings.Contains(stderr.String(), "blocked") &&
		!strings.Contains(stderr.String(), "bypass") {
		t.Fatalf("stderr should contain compact denial advice: %q", stderr.String())
	}
}

func TestRunWithIOReturnsErrorsWithoutExiting(t *testing.T) {
	t.Parallel()

	var (
		stdout bytes.Buffer
		stderr bytes.Buffer
	)

	status := runWithIO(nil, strings.NewReader("{}"), &stdout, &stderr)
	if status != 1 || !strings.Contains(stderr.String(), "--bundle is required") {
		t.Fatalf(
			"missing bundle status=%d stdout=%q stderr=%q",
			status,
			stdout.String(),
			stderr.String(),
		)
	}

	stdout.Reset()
	stderr.Reset()

	status = runWithIO(
		[]string{"--bundle", writeCLITestBundle(t), "--json"},
		strings.NewReader("{"),
		&stdout,
		&stderr,
	)
	if status != 1 || !strings.Contains(stderr.String(), "decode hook event") {
		t.Fatalf(
			"bad input status=%d stdout=%q stderr=%q",
			status,
			stdout.String(),
			stderr.String(),
		)
	}
}

func TestRunWithIOPersistsProxyOutputTransforms(t *testing.T) {
	t.Setenv("CODE_ETHOS_PROXY_OUTPUT_MAX_TOKENS", "30")
	t.Setenv("CODE_ETHOS_PROXY_OUTPUT_HEAD_TOKENS", "10")
	t.Setenv("CODE_ETHOS_PROXY_OUTPUT_TAIL_TOKENS", "10")

	repo := t.TempDir()
	err := os.Mkdir(filepath.Join(repo, ".git"), 0o700)
	if err != nil {
		t.Fatalf("create git marker: %v", err)
	}

	outputLines := []string{"ruff...Failed"}
	for index := 0; index < 40; index++ {
		outputLines = append(
			outputLines,
			"repeated diagnostic progress with package metadata",
		)
	}
	outputLines = append(outputLines, "ValueError: terminal failure remains visible")

	payload, err := json.Marshal(map[string]any{
		"hook_event_name": "PostToolUse",
		"provider":        "codex",
		"session_id":      "session-proxy-output",
		"cwd":             repo,
		"tool_name":       "Bash",
		"tool_input": map[string]any{
			"command": "uv run ruff check src/app.py",
		},
		"tool_response": map[string]any{
			"stdout":      strings.Join(outputLines, "\n"),
			"return_code": 1,
		},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	var (
		stdout bytes.Buffer
		stderr bytes.Buffer
	)

	status := runWithIO(
		[]string{"--bundle", writeCLITestBundle(t), "--json"},
		bytes.NewReader(payload),
		&stdout,
		&stderr,
	)
	if status != 0 {
		t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout.String(), stderr.String())
	}

	store, err := codeintel.Open(context.Background(), codeintel.DefaultDBPath(repo))
	if err != nil {
		t.Fatalf("open code-intel: %v", err)
	}
	defer store.Close()

	events, err := store.ProxyEvents(
		context.Background(),
		codeintel.ProxyEventQuery{SessionID: "session-proxy-output"},
	)
	if err != nil {
		t.Fatalf("query proxy events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("proxy events = %#v", events)
	}
	if events[0].Kind != "tool_output" ||
		events[0].Decision != "truncate" ||
		len(events[0].Transforms) != 3 {
		t.Fatalf("proxy output event = %#v", events[0])
	}
	if events[0].Transforms[2].PolicyID != "proxy.token_budget" ||
		events[0].Transforms[2].Decision != "truncate" ||
		events[0].Transforms[2].EvidencePath == "" {
		t.Fatalf("proxy transforms = %#v", events[0].Transforms)
	}
}

func runHookCLI(t *testing.T, stdin string) (map[string]any, int, string) {
	t.Helper()

	bundlePath := writeCLITestBundle(t)

	var (
		stdout bytes.Buffer
		stderr bytes.Buffer
	)

	status := runWithIO(
		[]string{"--bundle", bundlePath, "--json"},
		bytes.NewBufferString(stdin),
		&stdout,
		&stderr,
	)

	var result map[string]any

	err := json.Unmarshal(stdout.Bytes(), &result)
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

func captureHookStderr(t *testing.T, run func()) string {
	t.Helper()
	testlock.ProcessState(t, "coding-ethos-hook")

	original := os.Stderr

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stderr: %v", err)
	}

	os.Stderr = writer

	defer func() {
		os.Stderr = original
	}()

	run()

	inlineErr0 := writer.Close()
	if inlineErr0 != nil {
		t.Fatalf("close writer: %v", inlineErr0)
	}

	var buffer bytes.Buffer

	_, inlineErrA := buffer.ReadFrom(reader)
	if inlineErrA != nil {
		t.Fatalf("read stderr: %v", inlineErrA)
	}

	inlineErr1 := reader.Close()
	if inlineErr1 != nil {
		t.Fatalf("close reader: %v", inlineErr1)
	}

	return buffer.String()
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
