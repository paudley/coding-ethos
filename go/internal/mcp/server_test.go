// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package mcp_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/mcp"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestServerListsTools(t *testing.T) {
	t.Parallel()

	output := runServer(t, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	response := decodeResponse(t, output)

	result := response["result"].(map[string]any)
	tools := result["tools"].([]any)
	if len(tools) != 7 {
		t.Fatalf("tool count = %d, want 7: %#v", len(tools), tools)
	}
	for _, expected := range []string{
		"policy_check_command",
		"policy_check_edit",
		"lint_check",
		"lint_advice",
		"policy_explain",
		"skill_lookup",
		"skill_recommend",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("missing %s in tools:\n%s", expected, output)
		}
	}
	for _, removed := range []string{"policy_check_path", "repo_context"} {
		if strings.Contains(output, removed) {
			t.Fatalf("removed tool %s still listed:\n%s", removed, output)
		}
	}
	if !strings.Contains(output, "canonical lint path for agents") ||
		!strings.Contains(output, "executes_tools") {
		t.Fatalf("missing coding-ethos tool metadata:\n%s", output)
	}
}

func TestServerPolicyCheckEditUsesCompiledBundle(t *testing.T) {
	t.Parallel()

	output := runServer(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":3,
		"method":"tools/call",
		"params":{
			"name":"policy_check_edit",
			"arguments":{
				"path":"coding-ethos-hooks/coding-ethos-git-hook",
				"after":"replacement"
			}
		}
	}`))
	response := decodeResponse(t, output)

	content := response["result"].(map[string]any)["structuredContent"].(map[string]any)
	if content["blocked"] != true || content["status"] != "blocked" {
		t.Fatalf("content = %#v, want blocked", content)
	}
	if !strings.Contains(output, "filesystem.protected_path") {
		t.Fatalf("missing protected path policy in output:\n%s", output)
	}
}

func TestServerLintCheckRunsCompiledPolicies(t *testing.T) {
	t.Parallel()

	output := runServer(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":4,
		"method":"tools/call",
		"params":{
			"name":"lint_check",
			"arguments":{
				"scope":"staged",
				"command":"git commit --no-verify -m test"
			}
		}
	}`))
	response := decodeResponse(t, output)

	content := response["result"].(map[string]any)["structuredContent"].(map[string]any)
	if content["blocked"] != true || content["status"] != "blocked" {
		t.Fatalf("content = %#v, want blocked", content)
	}
	if content["engine"] != "compiled_policy_lint" {
		t.Fatalf("engine = %#v, want compiled_policy_lint", content["engine"])
	}
	if !strings.Contains(output, "git.hook_bypass") {
		t.Fatalf("missing hook bypass lint finding in output:\n%s", output)
	}
}

func TestServerLintCheckRunsManagedToolCapture(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	lintBinary := filepath.Join(tempDir, "coding-ethos-lint")
	writeExecutable(t, lintBinary, `#!/usr/bin/env bash
printf '%s\n' '{"scope":"managed","status":"blocked","findings":[{"check_id":"ruff:F401","source_tool":"ruff","code":"F401","message":"unused import","severity":"error","status":"fail","blocking":true}],"skill_hints":[{"skill_id":"lint-remediation","message":"fix lint structurally","next":"Load the lint-remediation skill."}],"capture":{"tool":"ruff","parser":"ruff","parse_status":"parsed","exit_code":1}}'
exit 1
`)

	output := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":8,
		"method":"tools/call",
		"params":{
			"name":"lint_check",
			"arguments":{
				"tool":"ruff",
				"files":["src/app.py"]
			}
		}
	}`), mcp.Runtime{
		BundlePath:    filepath.Join(tempDir, "policy-bundle.json"),
		EthosRoot:     tempDir,
		ConsumerRoot:  tempDir,
		InvocationCwd: tempDir,
		LintBinary:    lintBinary,
	})
	response := decodeResponse(t, output)

	content := response["result"].(map[string]any)["structuredContent"].(map[string]any)
	if content["engine"] != "managed_lint_capture" ||
		content["tool"] != "ruff" ||
		content["blocked"] != true {
		t.Fatalf("content = %#v, want managed ruff block", content)
	}
	if !strings.Contains(output, "lint-remediation") {
		t.Fatalf("missing managed lint skill hint:\n%s", output)
	}
}

func TestServerLintAdviceMapsDiagnosticToSkill(t *testing.T) {
	t.Parallel()

	output := runServer(t, compactJSON(t, fmt.Sprintf(`{
		"jsonrpc":"2.0",
		"id":5,
		"method":"tools/call",
		"params":{
			"name":"lint_advice",
			"arguments":{
				"tool":"ruff",
				"code":%q,
				"file":"src/app.py",
				"message":"import should be at the top-level of a file"
			}
		}
	}`, "PLC"+"0415")))

	if !strings.Contains(output, "conditional-imports") ||
		!strings.Contains(output, "python.conditional_imports") {
		t.Fatalf("missing lint advice:\n%s", output)
	}
}

func TestServerPolicyCheckCommandUsesCompiledBundle(t *testing.T) {
	t.Parallel()

	output := runServer(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":2,
		"method":"tools/call",
		"params":{
			"name":"policy_check_command",
			"arguments":{"command":"git commit --no-verify -m test"}
		}
	}`))
	response := decodeResponse(t, output)

	content := response["result"].(map[string]any)["structuredContent"].(map[string]any)
	if content["blocked"] != true || content["status"] != "blocked" {
		t.Fatalf("content = %#v, want blocked", content)
	}
	if !strings.Contains(output, "git.hook_bypass") {
		t.Fatalf("missing hook bypass policy in output:\n%s", output)
	}
}

func TestServerSkillLookupUsesBundleSkillData(t *testing.T) {
	t.Parallel()

	output := runServer(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":6,
		"method":"tools/call",
		"params":{
			"name":"skill_lookup",
			"arguments":{"skill_id":"safe-git-workflow"}
		}
	}`))

	if !strings.Contains(output, "safe-git-workflow") ||
		!strings.Contains(output, "Git is a protected critical operation") {
		t.Fatalf("missing skill data:\n%s", output)
	}
}

func TestServerSkillRecommendUsesDiagnosticAndTaskSignals(t *testing.T) {
	t.Parallel()

	output := runServer(t, compactJSON(t, fmt.Sprintf(`{
		"jsonrpc":"2.0",
		"id":7,
		"method":"tools/call",
		"params":{
			"name":"skill_recommend",
			"arguments":{
				"intent":"fix a ruff conditional import failure",
				"diagnostic":{
					"tool":"ruff",
					"code":%q,
					"message":"import should be at the top-level of a file"
				}
			}
		}
	}`, "PLC"+"0415")))

	if !strings.Contains(output, "conditional-imports") ||
		!strings.Contains(output, "recommendations") {
		t.Fatalf("missing skill recommendation:\n%s", output)
	}
}

func runServer(t *testing.T, input string) string {
	t.Helper()

	return runServerWithRuntime(t, input, mcp.Runtime{})
}

func runServerWithRuntime(t *testing.T, input string, runtime mcp.Runtime) string {
	t.Helper()

	var output bytes.Buffer
	server := mcp.NewServerWithRuntime(policy.ExampleBundle(), runtime)
	if err := server.Serve(strings.NewReader(input+"\n"), &output); err != nil {
		t.Fatalf("serve MCP: %v", err)
	}

	return output.String()
}

func decodeResponse(t *testing.T, output string) map[string]any {
	t.Helper()

	var response map[string]any
	if err := json.Unmarshal([]byte(output), &response); err != nil {
		t.Fatalf("decode response: %v\n%s", err, output)
	}

	return response
}

func compactJSON(t *testing.T, input string) string {
	t.Helper()

	var value any
	if err := json.Unmarshal([]byte(input), &value); err != nil {
		t.Fatalf("parse test JSON: %v", err)
	}

	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("compact test JSON: %v", err)
	}

	return string(payload)
}

func writeExecutable(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatalf("write executable: %v", err)
	}
}
