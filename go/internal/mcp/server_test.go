// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package mcp_test

import (
	"bytes"
	"encoding/json"
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
	if len(tools) != 5 {
		t.Fatalf("tool count = %d, want 5: %#v", len(tools), tools)
	}
	if !strings.Contains(output, "policy_check_command") ||
		!strings.Contains(output, "skill_lookup") {
		t.Fatalf("missing expected tools:\n%s", output)
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
		"id":3,
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

func runServer(t *testing.T, input string) string {
	t.Helper()

	var output bytes.Buffer
	server := mcp.NewServer(policy.ExampleBundle())
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
