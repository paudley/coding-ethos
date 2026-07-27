// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/mcp"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/sandbox"
	"blackcat.ca/coding-ethos/go/internal/toolconfigs"
	"blackcat.ca/coding-ethos/go/internal/webguidance"
)

const statusBlocked = "blocked"

var codeIntelMCPTestSlots = make(chan struct{}, 1)

func acquireCodeIntelMCPTestSlot(t *testing.T) {
	t.Helper()

	codeIntelMCPTestSlots <- struct{}{}
	t.Cleanup(func() {
		<-codeIntelMCPTestSlots
	})
}

func TestServerListsTools(t *testing.T) {
	t.Parallel()

	output := runServer(t, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	response := decodeResponse(t, output)

	result := mapValue(t, response["result"])

	tools := listValue(t, result["tools"])
	if len(tools) != 38 {
		t.Fatalf("tool count = %d, want 38: %#v", len(tools), tools)
	}

	for _, expected := range []string{
		"policy_check_command",
		"policy_check_edit",
		"cerun_check",
		"cerun_run",
		"managed_lint",
		"lint_advice",
		"sarif_remediation_advice",
		"sarif_risk_summary",
		"sarif_trend_analysis",
		"sarif_policy_feedback",
		"tool_capabilities",
		"policy_explain",
		"skill_lookup",
		"remediation_explain",
		"modern_web_guidance_search",
		"modern_web_guidance_retrieve",
		"modern_web_guidance_list",
		"code_intel_overview",
		"code_intel_workspace_status",
		"code_intel_search",
		"code_intel_answer",
		"semantic_search",
		"code_intel_index_status",
		"code_intel_hook_usage",
		"code_similarity_check",
		"code_intel_repo_map",
		"code_intel_context_card",
		"code_intel_change_risk",
		"code_intel_health",
		"code_intel_skill_health",
		"code_intel_why",
		"code_intel_proxy_denials",
		"code_intel_session_snapshot",
		"code_intel_index_code",
		"code_intel_embedding_candidates",
		"code_intel_code_chunks",
		"code_intel_code_context",
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
		!strings.Contains(output, "executes_tools") ||
		!strings.Contains(output, "mutating") ||
		!strings.Contains(output, "requires_network") {
		t.Fatalf("missing coding-ethos tool metadata:\n%s", output)
	}
}

func TestServerListsModernWebGuidanceToolsAsNetworkCapable(t *testing.T) {
	t.Parallel()

	output := runServer(t, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	response := decodeResponse(t, output)
	result := mapValue(t, response["result"])

	for _, toolValue := range listValue(t, result["tools"]) {
		tool := mapValue(t, toolValue)
		if tool["name"] != "modern_web_guidance_search" {
			continue
		}

		inputSchema := mapValue(t, tool["inputSchema"])
		properties := mapValue(t, inputSchema["properties"])
		for _, property := range []string{"query", "limit", "browser_policy", "refresh"} {
			if _, found := properties[property]; !found {
				t.Fatalf("modern web search schema missing %s: %#v", property, properties)
			}
		}

		meta := mapValue(t, mapValue(t, tool["_meta"])["coding_ethos"])
		if meta["advisory"] != true ||
			meta["executes_tools"] != true ||
			meta["requires_network"] != true ||
			meta["trace_persisted"] != true {
			t.Fatalf("modern web search metadata = %#v", meta)
		}

		return
	}

	t.Fatal("modern_web_guidance_search tool was not listed")
}

func TestServerCallsModernWebGuidanceSearchFromCache(t *testing.T) {
	t.Parallel()

	root := seedModernWebSearchCache(t)
	writeMCPFile(t, filepath.Join(root, "repo_config.toml"), `
[web_guidance.modern_web]
allow_network_refresh = false
`)

	output := runServerWithRuntime(t, `{
		"jsonrpc":"2.0",
		"id":12,
		"method":"tools/call",
		"params":{
			"name":"modern_web_guidance_search",
			"arguments":{"query":"navigation drawer"}
		}
	}`, mcp.Runtime{ConsumerRoot: root, InvocationCwd: root})

	content := structuredContent(t, decodeResponse(t, output))
	if content["kind"] != "modern_web_guidance" ||
		content["operation"] != "search" {
		t.Fatalf("modern web guidance content = %#v", content)
	}

	cache := mapValue(t, content["cache"])
	if cache["status"] != "hit" || cache["hit"] != true {
		t.Fatalf("modern web guidance cache = %#v", cache)
	}

	results := listValue(t, content["results"])
	if len(results) != 1 ||
		mapValue(t, results[0])["id"] != "navigation-drawer" {
		t.Fatalf("modern web guidance results = %#v", results)
	}
}

func TestServerListsCerunRunAsExplicitExecutionTool(t *testing.T) {
	t.Parallel()

	output := runServer(t, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	response := decodeResponse(t, output)
	result := mapValue(t, response["result"])

	for _, toolValue := range listValue(t, result["tools"]) {
		tool := mapValue(t, toolValue)
		if tool["name"] != "cerun_run" {
			continue
		}

		annotations := mapValue(t, tool["annotations"])
		if annotations["destructiveHint"] != true ||
			annotations["openWorldHint"] != true {
			t.Fatalf("cerun_run annotations = %#v", annotations)
		}

		meta := mapValue(t, mapValue(t, tool["_meta"])["coding_ethos"])
		if meta["executes_tools"] != true || meta["mutating"] != true {
			t.Fatalf("cerun_run coding_ethos metadata = %#v", meta)
		}

		inputSchema := mapValue(t, tool["inputSchema"])
		properties := mapValue(t, inputSchema["properties"])
		if _, found := mapValue(t, properties["command"])["description"]; !found {
			t.Fatalf("cerun_run command schema missing description: %#v", properties)
		}
		if _, found := mapValue(t, properties["provider"])["description"]; !found {
			t.Fatalf("cerun_run provider schema missing description: %#v", properties)
		}

		return
	}

	t.Fatal("cerun_run tool was not listed")
}

func TestServerListsManagedLintAsMutatingTool(t *testing.T) {
	t.Parallel()

	output := runServer(t, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	response := decodeResponse(t, output)
	result := mapValue(t, response["result"])

	for _, toolValue := range listValue(t, result["tools"]) {
		tool := mapValue(t, toolValue)
		if tool["name"] != "managed_lint" {
			continue
		}

		annotations := mapValue(t, tool["annotations"])
		if annotations["destructiveHint"] != true {
			t.Fatalf("managed_lint annotations = %#v", annotations)
		}

		meta := mapValue(t, mapValue(t, tool["_meta"])["coding_ethos"])
		if meta["mutating"] != true {
			t.Fatalf("managed_lint coding_ethos metadata = %#v", meta)
		}

		return
	}

	t.Fatal("managed_lint tool was not listed")
}

func TestServerSemanticSearchSchemaAllowsVectorOnlyInput(t *testing.T) {
	t.Parallel()

	output := runServer(t, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	response := decodeResponse(t, output)
	result := mapValue(t, response["result"])

	for _, toolValue := range listValue(t, result["tools"]) {
		tool := mapValue(t, toolValue)
		if tool["name"] != "semantic_search" {
			continue
		}

		inputSchema := mapValue(t, tool["inputSchema"])
		if _, found := inputSchema["required"]; found {
			t.Fatalf("semantic_search schema should not require query: %#v", inputSchema)
		}

		properties := mapValue(t, inputSchema["properties"])
		for _, property := range []string{"query", "text", "vector"} {
			if _, found := properties[property]; !found {
				t.Fatalf("semantic_search schema missing %s: %#v", property, properties)
			}
		}

		return
	}

	t.Fatal("semantic_search tool was not listed")
}

func TestServerReportsToolCapabilities(t *testing.T) {
	t.Parallel()

	output := runServer(t, `{
		"jsonrpc":"2.0",
		"id":12,
		"method":"tools/call",
		"params":{"name":"tool_capabilities","arguments":{}}
	}`)

	for _, want := range []string{
		`"structuredContent"`,
		`"tool_capabilities"`,
		`"no-network"`,
		`"no-git"`,
		`"gemini-check"`,
		`"network"`,
		`"seccomp_profile"`,
		`"timeout_seconds"`,
		`"memory_mb"`,
		`"cpu_quota_percent"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("tool capabilities output missing %q:\n%s", want, output)
		}
	}
}

func TestServerNegotiatesClientProtocolVersion(t *testing.T) {
	t.Parallel()

	output := runServer(t, `{
		"jsonrpc":"2.0",
		"id":10,
		"method":"initialize",
		"params":{"protocolVersion":"2024-11-05"}
	}`)
	response := decodeResponse(t, output)

	result := mapValue(t, response["result"])
	if result["protocolVersion"] != "2024-11-05" {
		t.Fatalf(
			"protocolVersion = %#v, want client version",
			result["protocolVersion"],
		)
	}
}

type modernWebFakeRunner struct{}

func (runner modernWebFakeRunner) Run(
	_ context.Context,
	_ string,
	args []string,
) (webguidance.CommandOutput, error) {
	joined := strings.Join(args, " ")
	switch {
	case strings.Contains(joined, " view "):
		return webguidance.CommandOutput{Stdout: `{
  "name": "modern-web-guidance",
  "version": "0.0.174",
  "dist-tags": {"latest": "0.0.174"},
  "bin": {"modern-web-guidance": "skills/modern-web-guidance/modern-web.mjs"},
  "repository": {"url": "git+https://github.com/GoogleChrome/modern-web-guidance-src.git"}
}`}, nil
	case strings.Contains(joined, " search "):
		return webguidance.CommandOutput{Stdout: `[
  {"id":"navigation-drawer","category":"user-experience","description":"Create a navigation drawer.","featuresUsed":["Popover"],"tokenCount":4317,"similarity":0.637}
]`}, nil
	default:
		return webguidance.CommandOutput{
				Stderr: "unexpected command",
			}, errors.New(
				"unexpected command",
			)
	}
}

func seedModernWebSearchCache(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	now := time.Now().UTC()
	_, err := webguidance.Adapter{
		Root:   root,
		Runner: modernWebFakeRunner{},
		Now:    func() time.Time { return now },
	}.Search(context.Background(), webguidance.SearchInput{Query: "navigation drawer"})
	if err != nil {
		t.Fatalf("seed modern web cache: %v", err)
	}

	return root
}

func writeMCPFile(t *testing.T, path, content string) {
	t.Helper()

	err := os.WriteFile(
		filepath.Clean(path),
		[]byte(strings.TrimSpace(content)+"\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestServerSupportsJSONLineStdioTransport(t *testing.T) {
	t.Parallel()

	request := compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":11,
		"method":"initialize",
		"params":{"protocolVersion":"2025-06-18"}
	}`)

	var output bytes.Buffer

	server := mcp.NewServer(policy.ExampleBundle())

	err := server.Serve(strings.NewReader(request+"\n"), &output)
	if err != nil {
		t.Fatalf("serve MCP JSON line: %v", err)
	}

	raw := output.String()
	if strings.Contains(raw, "Content-Length:") {
		t.Fatalf("JSON line transport returned framed response:\n%s", raw)
	}

	response := map[string]any{}

	err = json.Unmarshal([]byte(strings.TrimSpace(raw)), &response)
	if err != nil {
		t.Fatalf("decode JSON line response: %v\n%s", err, raw)
	}

	result := mapValue(t, response["result"])
	if result["protocolVersion"] != "2025-06-18" {
		t.Fatalf("protocolVersion = %#v", result["protocolVersion"])
	}
}

func TestServerHandlesPing(t *testing.T) {
	t.Parallel()

	output := runServer(t, `{"jsonrpc":"2.0","id":9,"method":"ping"}`)
	response := decodeResponse(t, output)

	if response["error"] != nil {
		t.Fatalf("ping returned error: %#v", response)
	}

	result := mapValue(t, response["result"])
	if len(result) != 0 {
		t.Fatalf("ping result = %#v, want empty object", result)
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

	content := structuredContent(t, response)
	if content["blocked"] != true || content["status"] != statusBlocked {
		t.Fatalf("content = %#v, want blocked", content)
	}

	if !strings.Contains(output, "filesystem.protected_path") {
		t.Fatalf("missing protected path policy in output:\n%s", output)
	}
}

func TestServerCerunCheckPreflightsWithoutExecuting(t *testing.T) {
	t.Parallel()

	output := runServer(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":13,
		"method":"tools/call",
		"params":{
			"name":"cerun_check",
			"arguments":{
				"command":"git commit --no-verify -m test",
				"intent":"verify git guard"
			}
		}
	}`))
	response := decodeResponse(t, output)

	content := structuredContent(t, response)
	if content["status"] != statusBlocked || content["blocked"] != true {
		t.Fatalf("content = %#v, want blocked cerun preflight", content)
	}

	if !strings.Contains(output, "git.hook_bypass") ||
		!strings.Contains(output, "cerun --check --rewrite --intent") ||
		!strings.Contains(output, "agent_remediation") {
		t.Fatalf("missing cerun_check guidance:\n%s", output)
	}
}

func TestServerCerunCheckRecommendsResolvedRuntime(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	cerunPath := filepath.Join(root, "coding-ethos", "bin", "cerun")
	err := os.MkdirAll(filepath.Dir(cerunPath), 0o700)
	if err != nil {
		t.Fatalf("create cerun dir: %v", err)
	}

	err = os.WriteFile(cerunPath, []byte("#!/usr/bin/env sh\nexit 0\n"), 0o700)
	if err != nil {
		t.Fatalf("write fake cerun: %v", err)
	}

	output := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":18,
		"method":"tools/call",
		"params":{
			"name":"cerun_check",
			"arguments":{
				"command":"printf ok",
				"provider":"codex"
			}
		}
	}`), mcp.Runtime{ConsumerRoot: root})
	response := decodeResponse(t, output)
	content := structuredContent(t, response)

	recommended, _ := content["recommended_command"].(string)
	if !strings.Contains(recommended, filepath.ToSlash(cerunPath)) {
		t.Fatalf("recommended command %q does not use %q", recommended, cerunPath)
	}
}

func TestServerCerunRunRequiresConfiguredRuntimeRoot(t *testing.T) {
	t.Parallel()

	output := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":19,
		"method":"tools/call",
		"params":{
			"name":"cerun_run",
			"arguments":{"command":"printf ok"}
		}
	}`), mcp.Runtime{})
	response := decodeResponse(t, output)

	if response["error"] == nil ||
		!strings.Contains(output, "repo-local cerun runtime is not configured") {
		t.Fatalf("expected missing runtime error, got:\n%s", output)
	}
}

func TestServerCerunRunExecutesResolvedRuntime(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	cwd := filepath.Join(root, "work")
	err := os.MkdirAll(cwd, 0o700)
	if err != nil {
		t.Fatalf("create cwd: %v", err)
	}

	cerunPath := filepath.Join(root, "coding-ethos", "bin", "cerun")
	err = os.MkdirAll(filepath.Dir(cerunPath), 0o700)
	if err != nil {
		t.Fatalf("create cerun dir: %v", err)
	}

	script := "#!/usr/bin/env sh\n" +
		"printf 'cwd=%s\\n' \"$PWD\"\n" +
		"printf 'args=%s\\n' \"$*\"\n" +
		"printf 'stderr-msg\\n' >&2\n" +
		"exit 7\n"
	err = os.WriteFile(cerunPath, []byte(script), 0o700)
	if err != nil {
		t.Fatalf("write fake cerun: %v", err)
	}

	output := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":14,
		"method":"tools/call",
		"params":{
			"name":"cerun_run",
			"arguments":{
				"command":"printf ok",
				"cwd":`+strconv.Quote(cwd)+`,
				"intent":"exercise mcp cerun",
				"timeout_seconds":5
			}
		}
	}`), mcp.Runtime{ConsumerRoot: root})
	response := decodeResponse(t, output)

	content := structuredContent(t, response)
	if content["exit_code"] != float64(7) || content["timed_out"] != false {
		t.Fatalf("content = %#v, want fake cerun exit", content)
	}

	stdout, _ := content["stdout"].(string)
	stderr, _ := content["stderr"].(string)
	if !strings.Contains(stdout, "cwd="+cwd) ||
		!strings.Contains(stdout, "--rewrite --intent exercise mcp cerun -- printf ok") ||
		!strings.Contains(stderr, "stderr-msg") {
		t.Fatalf("unexpected cerun output: stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestServerCerunRunDoesNotForwardInheritedExecStack(t *testing.T) {
	t.Setenv("CODING_ETHOS_EXEC_STACK", "cerun\ncoding-ethos-mcp")

	root := t.TempDir()
	cerunPath := filepath.Join(root, "bin", "cerun")
	err := os.MkdirAll(filepath.Dir(cerunPath), 0o700)
	if err != nil {
		t.Fatalf("create cerun dir: %v", err)
	}

	script := "#!/usr/bin/env sh\n" +
		"if [ -n \"$CODING_ETHOS_EXEC_STACK\" ]; then\n" +
		"  printf 'unexpected stack: %s\\n' \"$CODING_ETHOS_EXEC_STACK\" >&2\n" +
		"  exit 96\n" +
		"fi\n" +
		"printf 'stack-cleared\\n'\n"
	err = os.WriteFile(cerunPath, []byte(script), 0o700)
	if err != nil {
		t.Fatalf("write fake cerun: %v", err)
	}

	output := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":43,
		"method":"tools/call",
		"params":{
			"name":"cerun_run",
			"arguments":{"command":"git status","timeout_seconds":5}
		}
	}`), mcp.Runtime{ConsumerRoot: root})
	response := decodeResponse(t, output)
	content := structuredContent(t, response)

	stdout, _ := content["stdout"].(string)
	if content["exit_code"] != float64(0) ||
		!strings.Contains(stdout, "stack-cleared") {
		t.Fatalf("cerun_run forwarded recursive exec stack: %#v", content)
	}
}

func TestServerCerunRunCapsOutputAndOmitsSuccessFollowup(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	cerunPath := filepath.Join(root, "bin", "cerun")
	err := os.MkdirAll(filepath.Dir(cerunPath), 0o700)
	if err != nil {
		t.Fatalf("create cerun dir: %v", err)
	}

	script := "#!/usr/bin/env sh\n" +
		"head -c 65535 /dev/zero | tr '\\0' 'x'\n" +
		"printf '\\303\\251'\n"
	err = os.WriteFile(cerunPath, []byte(script), 0o700)
	if err != nil {
		t.Fatalf("write fake cerun: %v", err)
	}

	output := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":20,
		"method":"tools/call",
		"params":{
			"name":"cerun_run",
			"arguments":{"command":"printf ok","timeout_seconds":5}
		}
	}`), mcp.Runtime{ConsumerRoot: root})
	response := decodeResponse(t, output)
	content := structuredContent(t, response)

	stdout, _ := content["stdout"].(string)
	if content["exit_code"] != float64(0) ||
		content["stdout_truncated"] != true ||
		!utf8.ValidString(stdout) ||
		content["recommended_followup"] != nil {
		t.Fatalf("unexpected capped cerun response: %#v", content)
	}
}

func TestServerCerunRunReportsStartFailureAsNonZero(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	cerunPath := filepath.Join(root, "bin", "cerun")
	err := os.MkdirAll(filepath.Dir(cerunPath), 0o700)
	if err != nil {
		t.Fatalf("create cerun dir: %v", err)
	}

	err = os.WriteFile(cerunPath, []byte("#!/usr/bin/env sh\nexit 0\n"), 0o700)
	if err != nil {
		t.Fatalf("write fake cerun: %v", err)
	}

	missingCwd := filepath.Join(root, "missing")
	output := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":21,
		"method":"tools/call",
		"params":{
			"name":"cerun_run",
			"arguments":{
				"command":"printf ok",
				"cwd":`+strconv.Quote(missingCwd)+`,
				"timeout_seconds":5
			}
		}
	}`), mcp.Runtime{ConsumerRoot: root})
	response := decodeResponse(t, output)
	content := structuredContent(t, response)

	stderr, _ := content["stderr"].(string)
	if content["exit_code"] != float64(127) ||
		!strings.Contains(stderr, missingCwd) {
		t.Fatalf("unexpected start failure response: %#v", content)
	}
}

func TestServerLintCheckRunsCompiledPolicies(t *testing.T) {
	t.Parallel()

	output := runServer(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":4,
		"method":"tools/call",
		"params":{
			"name":"managed_lint",
			"arguments":{
				"scope":"staged",
				"command":"git commit --no-verify -m test"
			}
		}
	}`))
	response := decodeResponse(t, output)

	content := structuredContent(t, response)
	if content["blocked"] != true || content["status"] != statusBlocked {
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
	nativeSandboxAvailable := nativeSandboxRuntimeAvailable()

	tempDir := t.TempDir()
	writeManagedLintRuntimeFixture(t, tempDir)

	output := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":8,
		"method":"tools/call",
		"params":{
			"name":"managed_lint",
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
	})
	response := decodeResponse(t, output)

	content := structuredContent(t, response)
	if content["engine"] != "managed_lint_capture" ||
		content["tool"] != "ruff" ||
		content["blocked"] != true {
		t.Fatalf("content = %#v, want managed ruff block", content)
	}

	for _, want := range []string{
		`"engine":"managed_lint_capture"`,
		`"tool":"ruff"`,
		`"code":"F401"`,
		`"file":"src/app.py"`,
		`"check_id":"tool.ruff"`,
	} {
		if !strings.Contains(output, want) {
			if !nativeSandboxAvailable && strings.Contains(output, "runtime.sandbox_denial") {
				return
			}

			t.Fatalf("missing managed lint output %q:\n%s", want, output)
		}
	}
}

func writeManagedLintRuntimeFixture(t *testing.T, root string) {
	t.Helper()

	err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte("version: 1\n"), 0o600)
	if err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	_, err = toolconfigs.Sync(root, root, "")
	if err != nil {
		t.Fatalf("sync tool configs: %v", err)
	}

	ruffPath := filepath.Join(root, ".venv", "bin", "ruff")
	sandboxPath := filepath.Join(root, "bin", "coding-ethos-sandbox")

	err = os.MkdirAll(filepath.Dir(ruffPath), 0o700)
	if err != nil {
		t.Fatalf("create ruff fixture dir: %v", err)
	}
	err = os.MkdirAll(filepath.Dir(sandboxPath), 0o700)
	if err != nil {
		t.Fatalf("create sandbox fixture dir: %v", err)
	}

	writeExecutable(t, ruffPath, `#!/usr/bin/env sh
cat <<'JSON'
[
  {
    "filename": "src/app.py",
    "code": "F401",
    "message": "unused import",
    "location": {"row": 1, "column": 1}
  }
]
JSON
exit 1
`)
	buildSandboxHelper(t, sandboxPath)
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

func TestServerExplainsPolicyByID(t *testing.T) {
	t.Parallel()

	output := runServer(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":13,
		"method":"tools/call",
		"params":{
			"name":"policy_explain",
			"arguments":{
				"policy_id":"git.hook_bypass"
			}
		}
	}`))
	response := decodeResponse(t, output)

	content := structuredContent(t, response)
	if content["policy_id"] != "git.hook_bypass" {
		t.Fatalf("policy_id = %#v", content["policy_id"])
	}

	explanation, ok := content["explanation"].(string)
	if !ok || !strings.Contains(explanation, "git.hook_bypass") ||
		!strings.Contains(explanation, "CEL Expression") {
		t.Fatalf("policy explanation mismatch: %#v", content)
	}
}

func TestServerRejectsPolicyExplainWithoutID(t *testing.T) {
	t.Parallel()

	output := runServer(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":14,
		"method":"tools/call",
		"params":{"name":"policy_explain","arguments":{}}
	}`))

	response := decodeResponse(t, output)
	if response["error"] == nil || !strings.Contains(output, "policy_id is required") {
		t.Fatalf("expected policy explain error, got:\n%s", output)
	}
}

func TestServerCodeIntelToolsUseStoredCodeAndRemediationData(t *testing.T) {
	t.Parallel()
	acquireCodeIntelMCPTestSlot(t)

	root := t.TempDir()
	seedCodeIntelToolData(t, root)
	runtime := mcp.Runtime{ConsumerRoot: root, InvocationCwd: root}

	for index, request := range codeIntelToolRequests() {
		output := runServerWithRuntime(t, codeIntelToolRequest(t, index, request), runtime)
		if !strings.Contains(output, request.want) {
			t.Fatalf("%s output missing %q:\n%s", request.name, request.want, output)
		}
	}
}

func TestServerChangeRiskDoesNotRefreshMissingHealthSnapshot(t *testing.T) {
	t.Parallel()
	acquireCodeIntelMCPTestSlot(t)

	ctx := context.Background()
	root := t.TempDir()
	seedCodeIntelToolData(t, root)
	runtime := mcp.Runtime{ConsumerRoot: root, InvocationCwd: root}

	output := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":56,
		"method":"tools/call",
		"params":{
			"name":"code_intel_change_risk",
			"arguments":{"path":"pkg/app.py","limit":5}
		}
	}`), runtime)
	if !strings.Contains(output, `"code_intel_change_risk"`) ||
		!strings.Contains(output, `"health":[]`) {
		t.Fatalf("change risk output missing empty stored-health result:\n%s", output)
	}

	store, err := codeintel.Open(ctx, codeintel.DefaultDBPath(root))
	if err != nil {
		t.Fatalf("open code-intel store: %v", err)
	}
	defer store.Close()

	_, found, err := store.StoredCodeHealth(ctx, codeintel.CodeHealthQuery{
		Root:  root,
		Path:  "pkg/app.py",
		Limit: 1,
	})
	if err != nil {
		t.Fatalf("read stored health: %v", err)
	}
	if found {
		t.Fatalf("change risk unexpectedly refreshed health snapshot")
	}
}

func TestServerSemanticSearchReturnsCodeChunks(t *testing.T) {
	t.Parallel()
	acquireCodeIntelMCPTestSlot(t)

	root := t.TempDir()
	seedCodeIntelToolData(t, root)
	runtime := mcp.Runtime{ConsumerRoot: root, InvocationCwd: root}

	output := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":46,
		"method":"tools/call",
		"params":{
			"name":"semantic_search",
			"arguments":{"query":"run","path":"pkg/app.py","limit":5}
		}
	}`), runtime)

	for _, want := range []string{
		`"semantic_search"`,
		`"code_chunk"`,
		`"pkg/app.py"`,
		`def run():`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("semantic search output missing %q:\n%s", want, output)
		}
	}
}

func TestServerCodeIntelProxyDenialsExplainsStoredDenial(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ctx := context.Background()

	store, err := codeintel.Open(ctx, codeintel.DefaultDBPath(root))
	if err != nil {
		t.Fatalf("open code-intel store: %v", err)
	}

	inlineErr0 := store.RecordProxyEvent(ctx, agentproxy.ProviderEvent{
		ID:            "proxy-deny-1",
		SessionID:     "session-deny",
		Kind:          agentproxy.EventProviderResponse,
		Provider:      "openai",
		PolicyID:      "proxy.inbound_unsafe_tool_call",
		Decision:      "deny",
		Direction:     agentproxy.DirectionInbound,
		PayloadKind:   agentproxy.PayloadResponse,
		RecordedAtUTC: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Metadata: map[string]string{
			"proxy_event_id":        "proxy-deny-1",
			"proxy_session_id":      "session-deny",
			"proxy_direction":       "inbound",
			"proxy_payload_kind":    "response",
			"stream_denial_surface": "true",
		},
		Policy: agentproxy.PolicyEvidence{
			PolicyID:   "proxy.inbound_unsafe_tool_call",
			SkillID:    "security-by-design",
			Decision:   "deny",
			Reason:     "unsafe tool call requested",
			EvidenceID: "proxy-deny-1",
			PrincipleIDs: []string{
				"security-by-design",
				"one-path-for-critical-operations",
			},
		},
	})
	if inlineErr0 != nil {
		t.Fatalf("record proxy denial: %v", inlineErr0)
	}

	inlineErr1 := store.Close()
	if inlineErr1 != nil {
		t.Fatalf("close code-intel store: %v", inlineErr1)
	}

	output := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":61,
		"method":"tools/call",
		"params":{
			"name":"code_intel_proxy_denials",
			"arguments":{
				"session_id":"session-deny",
				"policy_id":"proxy.inbound_unsafe_tool_call"
			}
		}
	}`), mcp.Runtime{ConsumerRoot: root, InvocationCwd: root})

	for _, want := range []string{
		`"kind":"code_intel_proxy_denials"`,
		`"event_id":"proxy-deny-1"`,
		`"policy_id":"proxy.inbound_unsafe_tool_call"`,
		`"recommended_action"`,
		`"stream_denial_event":true`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("proxy denials output missing %q:\n%s", want, output)
		}
	}
}

func seedCodeIntelToolData(t *testing.T, root string) {
	t.Helper()

	sourcePath := filepath.Join(root, "pkg", "app.py")

	inlineErr0 := os.MkdirAll(filepath.Dir(sourcePath), 0o700)
	if inlineErr0 != nil {
		t.Fatalf("create source dir: %v", inlineErr0)
	}

	inlineErr1 := os.WriteFile(
		sourcePath,
		[]byte("def run():\n    return 'ok'\n"),
		0o600,
	)
	if inlineErr1 != nil {
		t.Fatalf("write source: %v", inlineErr1)
	}

	ctx := context.Background()

	store, err := codeintel.Open(ctx, codeintel.DefaultDBPath(root))
	if err != nil {
		t.Fatalf("open code-intel store: %v", err)
	}

	_, inlineErrA := codeintel.NewASTIndexer(store).
		IndexPaths(ctx, root, []string{"pkg"})
	if inlineErrA != nil {
		t.Fatalf("index code: %v", inlineErrA)
	}

	inlineErr2 := store.RecordRemediationOutcome(ctx, codeintel.RemediationOutcome{
		RemediationID: "rem-1",
		FindingID:     "finding-1",
		PolicyID:      "python.conditional_imports",
		SkillID:       "conditional-imports",
		Path:          "pkg/app.py",
		Outcome:       "fixed",
		SearchText:    "Move import to module scope.",
	})
	if inlineErr2 != nil {
		t.Fatalf("record outcome: %v", inlineErr2)
	}

	_, inlineErrDecision := store.RecordDecision(ctx, codeintel.DecisionRecord{
		Title:     "Use explicit startup flow",
		Rationale: "Explicit startup flow keeps the app entrypoint inspectable.",
		Status:    codeintel.DecisionStatusAccepted,
		Links: []codeintel.DecisionLink{{
			Path: "pkg/app.py",
			Kind: codeintel.DecisionLinkAffects,
		}},
	})
	if inlineErrDecision != nil {
		t.Fatalf("record decision: %v", inlineErrDecision)
	}

	inlineErr3 := store.Close()
	if inlineErr3 != nil {
		t.Fatalf("close store: %v", inlineErr3)
	}
}

type codeIntelToolRequestCase struct {
	name string
	body string
	want string
}

func codeIntelToolRequests() []codeIntelToolRequestCase {
	return []codeIntelToolRequestCase{
		{
			name: "search",
			body: `{"text":"Move import","policy_id":"python.conditional_imports"}`,
			want: "code_intel_search",
		},
		{
			name: "code chunks",
			body: `{"path":"pkg/app.py","symbol_name":"run"}`,
			want: "code_intel_code_chunks",
		},
		{
			name: "code context",
			body: `{"path":"pkg/app.py","symbol_path":"run"}`,
			want: "code_intel_code_context",
		},
		{
			name: "repo map",
			body: `{"limit":5,"symbols_per_file":2,"format":"toon"}`,
			want: "coding_ethos_repo_map",
		},
		{
			name: "embedding candidates",
			body: `{"record_kind":"remediation_outcome",` +
				`"policy_id":"python.conditional_imports"}`,
			want: "code_intel_embedding_candidates",
		},
		{
			name: "why",
			body: `{"path":"pkg/app.py","query":"startup","limit":5}`,
			want: "code_intel_why",
		},
		{
			name: "index status",
			body: `{"collection":"remediations","model_id":"test-model"}`,
			want: "ready_records",
		},
	}
}

func codeIntelToolRequest(
	t *testing.T,
	index int,
	request codeIntelToolRequestCase,
) string {
	t.Helper()

	return compactJSON(t, fmt.Sprintf(`{
			"jsonrpc":"2.0",
			"id":%d,
			"method":"tools/call",
			"params":{"name":"code_intel_%s","arguments":%s}
		}`, 40+index, strings.ReplaceAll(request.name, " ", "_"), request.body))
}

func TestServerCodeIntelRepoMapResource(t *testing.T) {
	t.Parallel()
	acquireCodeIntelMCPTestSlot(t)

	root := t.TempDir()
	seedCodeIntelToolData(t, root)
	runtime := mcp.Runtime{ConsumerRoot: root, InvocationCwd: root}

	listOutput := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":54,
		"method":"resources/list"
	}`), runtime)
	if !strings.Contains(listOutput, "coding-ethos://code-intel/repo-map") {
		t.Fatalf("resource list missing repo map:\n%s", listOutput)
	}

	readOutput := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":55,
		"method":"resources/read",
		"params":{"uri":"coding-ethos://code-intel/repo-map"}
	}`), runtime)
	if !strings.Contains(readOutput, "coding_ethos_repo_map") ||
		!strings.Contains(readOutput, "pkg/app.py") ||
		!strings.Contains(readOutput, "provenance") ||
		!strings.Contains(readOutput, "EXTRACTED") {
		t.Fatalf("resource read missing repo map:\n%s", readOutput)
	}
}

func TestServerCodeIntelRepoMapResourceDoesNotRefreshIndex(t *testing.T) {
	t.Parallel()
	acquireCodeIntelMCPTestSlot(t)

	root := t.TempDir()
	seedCodeIntelToolData(t, root)
	runtime := mcp.Runtime{ConsumerRoot: root, InvocationCwd: root}

	newSourcePath := filepath.Join(root, "pkg", "new_file.py")
	inlineErr0 := os.WriteFile(
		newSourcePath,
		[]byte("def newly_added():\n    return 'new'\n"),
		0o600,
	)
	if inlineErr0 != nil {
		t.Fatalf("write new source: %v", inlineErr0)
	}

	readOutput := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":56,
		"method":"resources/read",
		"params":{"uri":"coding-ethos://code-intel/repo-map"}
	}`), runtime)
	if strings.Contains(readOutput, "pkg/new_file.py") {
		t.Fatalf("resource read refreshed unindexed file:\n%s", readOutput)
	}

	assertRepoMapPathAbsent(t, root, "pkg/new_file.py")

	toolOutput := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":57,
		"method":"tools/call",
		"params":{
			"name":"code_intel_repo_map",
			"arguments":{"path":"pkg/new_file.py","format":"toon"}
		}
	}`), runtime)
	if !strings.Contains(toolOutput, "pkg/new_file.py") {
		t.Fatalf("repo-map tool did not refresh requested path:\n%s", toolOutput)
	}
}

func TestServerCodeIntelRepoMapRootPathDoesNotRefreshIndex(t *testing.T) {
	t.Parallel()
	acquireCodeIntelMCPTestSlot(t)

	root := t.TempDir()
	seedCodeIntelToolData(t, root)
	runtime := mcp.Runtime{ConsumerRoot: root, InvocationCwd: root}

	newSourcePath := filepath.Join(root, "pkg", "new_file.py")
	inlineErr0 := os.WriteFile(
		newSourcePath,
		[]byte("def newly_added():\n    return 'new'\n"),
		0o600,
	)
	if inlineErr0 != nil {
		t.Fatalf("write new source: %v", inlineErr0)
	}

	toolOutput := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":58,
		"method":"tools/call",
		"params":{
			"name":"code_intel_repo_map",
			"arguments":{"path":".","format":"toon"}
		}
	}`), runtime)
	if strings.Contains(toolOutput, "pkg/new_file.py") {
		t.Fatalf("root repo-map tool refreshed unindexed file:\n%s", toolOutput)
	}

	if !strings.Contains(toolOutput, "pkg/app.py") ||
		!strings.Contains(toolOutput, "coding_ethos_repo_map") ||
		!strings.Contains(toolOutput, "provenance") ||
		!strings.Contains(toolOutput, "EXTRACTED") {
		t.Fatalf("root repo-map tool did not return stored repo map:\n%s", toolOutput)
	}

	assertRepoMapPathAbsent(t, root, "pkg/new_file.py")
}

func TestServerCodeIntelRepoMapRejectsTraversalRefreshPath(t *testing.T) {
	t.Parallel()
	acquireCodeIntelMCPTestSlot(t)

	root := t.TempDir()
	seedCodeIntelToolData(t, root)
	runtime := mcp.Runtime{ConsumerRoot: root, InvocationCwd: root}

	outsideRoot := t.TempDir()
	outsidePath := filepath.Join(outsideRoot, "outside.py")
	inlineErr0 := os.WriteFile(
		outsidePath,
		[]byte("def outside():\n    return 'outside'\n"),
		0o600,
	)
	if inlineErr0 != nil {
		t.Fatalf("write outside source: %v", inlineErr0)
	}

	toolOutput := runServerWithRuntime(t, compactJSON(t, fmt.Sprintf(`{
		"jsonrpc":"2.0",
		"id":59,
		"method":"tools/call",
		"params":{
			"name":"code_intel_repo_map",
			"arguments":{"path":%q,"format":"toon"}
		}
	}`, outsidePath)), runtime)
	if strings.Contains(toolOutput, "outside.py") ||
		strings.Contains(toolOutput, "outside") {
		t.Fatalf("repo-map tool indexed absolute outside path:\n%s", toolOutput)
	}

	traversalOutput := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":60,
		"method":"tools/call",
		"params":{
			"name":"code_intel_repo_map",
			"arguments":{"path":"../outside.py","format":"toon"}
		}
	}`), runtime)
	if strings.Contains(traversalOutput, "outside.py") ||
		strings.Contains(traversalOutput, "outside") {
		t.Fatalf("repo-map tool indexed traversal outside path:\n%s", traversalOutput)
	}
}

func TestServerCodeIntelRepoMapReturnsDirectoryPath(t *testing.T) {
	t.Parallel()
	acquireCodeIntelMCPTestSlot(t)

	root := t.TempDir()
	goSourcePath := filepath.Join(root, "internal", "query", "postgres", "index.go")
	inlineErr0 := os.MkdirAll(filepath.Dir(goSourcePath), 0o700)
	if inlineErr0 != nil {
		t.Fatalf("create go source dir: %v", inlineErr0)
	}

	inlineErr1 := os.WriteFile(
		goSourcePath,
		[]byte(
			"package postgres\n\ntype Index struct{}\n\nfunc OpenIndex() *Index { return &Index{} }\n",
		),
		0o600,
	)
	if inlineErr1 != nil {
		t.Fatalf("write go source: %v", inlineErr1)
	}

	runtime := mcp.Runtime{ConsumerRoot: root, InvocationCwd: root}
	output := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":58,
		"method":"tools/call",
		"params":{
			"name":"code_intel_repo_map",
			"arguments":{
				"path":"internal/query/postgres",
				"language":"go",
				"limit":8,
				"symbols_per_file":8,
				"format":"compact"
			}
		}
	}`), runtime)

	if !strings.Contains(output, "internal/query/postgres/index.go") ||
		!strings.Contains(output, "OpenIndex") {
		t.Fatalf("repo-map tool did not return directory map:\n%s", output)
	}
}

func TestServerSARIFRemediationAdviceUsesSARIFPolicyMetadata(t *testing.T) {
	t.Parallel()

	sarif := compactJSON(t, fmt.Sprintf(`{
		"version":"2.1.0",
		"runs":[{
			"tool":{"driver":{"rules":[{
				"id":"python.conditional_imports",
				"name":"conditional imports",
				"shortDescription":{"text":"Move imports to module scope."},
				"help":{"text":"Fix conditional imports structurally."},
				"properties":{
					"policy_id":"python.conditional_imports",
					"skill_id":"conditional-imports",
					"source_tool":"ruff",
					"ethos_ids":["no-conditional-imports"]
				}
			}]}},
			"results":[{
				"ruleId":"python.conditional_imports",
				"ruleIndex":0,
				"level":"error",
				"message":{"text":"import should be at the top-level of a file"},
				"locations":[{
					"physicalLocation":{
						"artifactLocation":{"uri":"src/app.py"},
						"region":{"startLine":12,"startColumn":4}
					}
				}],
				"partialFingerprints":{"coding-ethos/stable/v1":"abc123"},
				"properties":{
					"policy_id":"python.conditional_imports",
					"skill_id":"conditional-imports",
					"source_tool":"ruff",
					"code":%q,
					"advice":"Move the import to module scope.",
					"ethos_ids":["no-conditional-imports"],
					"implementation":"cel",
					"policy_source":"coding_ethos.yml:principles.3",
					"input_schema_version":1,
					"cel_expression":"diagnostic.code == 'PLC0415'"
				}
			}]
		}]
	}`, "PLC"+"0415"))

	output := runServer(t, compactJSON(t, fmt.Sprintf(`{
		"jsonrpc":"2.0",
		"id":12,
		"method":"tools/call",
		"params":{
			"name":"sarif_remediation_advice",
			"arguments":{"sarif":%q}
		}
	}`, sarif)))

	for _, expected := range []string{
		"conditional-imports",
		"Move the import to module scope.",
		"managed_lint",
		"src/app.py",
		"cel_expression",
		"coding-ethos/stable/v1",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("missing %s in SARIF remediation output:\n%s", expected, output)
		}
	}
}

func assertRepoMapPathAbsent(t *testing.T, root, path string) {
	t.Helper()

	ctx := context.Background()
	store, err := codeintel.Open(ctx, codeintel.DefaultDBPath(root))
	if err != nil {
		t.Fatalf("open code-intel store: %v", err)
	}
	defer func() {
		inlineErr := store.Close()
		if inlineErr != nil {
			t.Fatalf("close code-intel store: %v", inlineErr)
		}
	}()

	repoMap, err := store.GlobalRepoMap(ctx, codeintel.RepoMapQuery{
		Root:  root,
		Path:  path,
		Limit: 5,
	})
	if err != nil {
		t.Fatalf("query repo map: %v", err)
	}

	if len(repoMap.Files) != 0 {
		t.Fatalf("repo map unexpectedly contains %s: %#v", path, repoMap.Files)
	}
}

func TestServerSARIFRiskSummaryUsesGroupsAndPolicyMetadata(t *testing.T) {
	t.Parallel()

	sarif := compactJSON(t, `{
		"version":"2.1.0",
		"runs":[{
			"properties":{
				"finding_groups":[{
					"id":"group-1",
					"key":"python.sql_safety|lint-remediation|src/db.py|8",
					"policy_id":"python.sql_safety",
					"skill_id":"lint-remediation",
					"file":"src/db.py",
					"line":8,
					"result_count":2
				}]
			},
			"tool":{"driver":{"rules":[{
				"id":"python.sql_safety",
				"shortDescription":{"text":"Possible SQL injection vector."},
				"properties":{
					"policy_id":"python.sql_safety",
					"skill_id":"lint-remediation",
					"source_tool":"bandit"
				}
			}]}},
			"results":[{
				"ruleId":"python.sql_safety",
				"ruleIndex":0,
				"level":"error",
				"message":{"text":"Possible SQL injection vector"},
				"locations":[{
					"physicalLocation":{
						"artifactLocation":{"uri":"src/db.py"},
						"region":{"startLine":8}
					}
				}],
				"properties":{
					"policy_id":"python.sql_safety",
					"skill_id":"lint-remediation",
					"source_tool":"bandit"
				}
			}]
		}]
	}`)

	output := runServer(t, compactJSON(t, fmt.Sprintf(`{
		"jsonrpc":"2.0",
		"id":13,
		"method":"tools/call",
		"params":{
			"name":"sarif_risk_summary",
			"arguments":{"sarif":%q}
		}
	}`, sarif)))

	for _, expected := range []string{
		"risk_score",
		"python.sql_safety",
		"lint-remediation",
		"src/db.py",
		"group-1",
		"sarif_remediation_advice",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("missing %s in SARIF risk summary output:\n%s", expected, output)
		}
	}
}

func TestServerSARIFRemediationAdviceCanReplayLintTraceID(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()

	tracePath, err := lint.LogResult(repo, lint.Result{
		Scope:  "tool:ruff",
		Status: statusBlocked,
		Diagnostics: []diagnostics.Diagnostic{{
			Tool:     "ruff",
			File:     "src/app.py",
			Line:     7,
			Severity: "error",
			Code:     "PLC0415",
			PolicyID: "python.conditional_imports",
			SkillID:  "conditional-imports",
			Message:  "import should be at the top-level of a file",
			Advice:   "Move the import to module scope.",
		}},
	})
	if err != nil {
		t.Fatalf("write lint trace: %v", err)
	}

	output := runServerWithRuntime(t, compactJSON(t, fmt.Sprintf(`{
		"jsonrpc":"2.0",
		"id":14,
		"method":"tools/call",
		"params":{
			"name":"sarif_remediation_advice",
			"arguments":{"trace_id":%q}
		}
	}`, filepath.Base(tracePath))), mcp.Runtime{ConsumerRoot: repo})

	for _, expected := range []string{
		"conditional-imports",
		"Move the import to module scope.",
		"src/app.py",
		"managed_lint",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("missing %s in trace remediation output:\n%s", expected, output)
		}
	}
}

func TestServerSARIFRiskSummaryCanReplayLintTraceID(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()

	tracePath, err := lint.LogResult(repo, lint.Result{
		Scope:  "tool:bandit",
		Status: statusBlocked,
		Diagnostics: []diagnostics.Diagnostic{{
			Tool:     "bandit",
			File:     "src/db.py",
			Line:     9,
			Severity: "error",
			Code:     "S608",
			PolicyID: "python.sql_safety",
			SkillID:  "lint-remediation",
			Message:  "Possible SQL injection vector",
		}},
	})
	if err != nil {
		t.Fatalf("write lint trace: %v", err)
	}

	output := runServerWithRuntime(t, compactJSON(t, fmt.Sprintf(`{
		"jsonrpc":"2.0",
		"id":15,
		"method":"tools/call",
		"params":{
			"name":"sarif_risk_summary",
			"arguments":{"trace_id":%q}
		}
	}`, filepath.Base(tracePath))), mcp.Runtime{ConsumerRoot: repo})

	for _, expected := range []string{
		"risk_score",
		"python.sql_safety",
		"lint-remediation",
		"src/db.py",
		"sarif_remediation_advice",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("missing %s in trace risk summary output:\n%s", expected, output)
		}
	}
}

func TestServerSARIFTrendAnalysisComparesRuns(t *testing.T) {
	t.Parallel()

	baseline, current := sarifTrendComparisonInputs(t)
	output := runServer(t, sarifTrendRequest(t, 17, map[string]string{
		"baseline_sarif": baseline,
		"current_sarif":  current,
	}))

	assertOutputContains(t, "SARIF trend", output, []string{
		"introduced",
		"fixed",
		"persisting",
		"python.new",
		"python.old",
		"python.persisting",
		"sarif_remediation_advice",
	})
}

func sarifTrendComparisonInputs(t *testing.T) (string, string) {
	t.Helper()

	baseline := compactJSON(t, `{
		"version":"2.1.0",
		"runs":[{
				"tool":{"driver":{"rules":[{
					"id":"python.old",
					"properties":{"policy_id":"python.old"}
				}]}},
			"results":[{
				"ruleId":"python.old",
				"ruleIndex":0,
				"level":"error",
				"message":{"text":"old issue"},
				"locations":[{
					"physicalLocation":{
						"artifactLocation":{"uri":"src/old.py"},
						"region":{"startLine":1}
					}
				}],
				"properties":{"coding_ethos_group_key":"python.old||src/old.py|1"}
			},{
				"ruleId":"python.persisting",
				"level":"error",
				"message":{"text":"persisting issue"},
				"locations":[{
					"physicalLocation":{
						"artifactLocation":{"uri":"src/app.py"},
						"region":{"startLine":3}
					}
				}],
				"properties":{"coding_ethos_group_key":"python.persisting||src/app.py|3"}
			}]
		}]
	}`)
	current := compactJSON(t, `{
		"version":"2.1.0",
		"runs":[{
				"tool":{"driver":{"rules":[{
					"id":"python.new",
					"properties":{"policy_id":"python.new"}
				}]}},
			"results":[{
				"ruleId":"python.persisting",
				"level":"error",
				"message":{"text":"persisting issue"},
				"locations":[{
					"physicalLocation":{
						"artifactLocation":{"uri":"src/app.py"},
						"region":{"startLine":3}
					}
				}],
				"properties":{"coding_ethos_group_key":"python.persisting||src/app.py|3"}
			},{
				"ruleId":"python.new",
				"ruleIndex":0,
				"level":"error",
				"message":{"text":"new issue"},
				"locations":[{
					"physicalLocation":{
						"artifactLocation":{"uri":"src/new.py"},
						"region":{"startLine":9}
					}
				}],
				"properties":{"coding_ethos_group_key":"python.new||src/new.py|9"}
			}]
		}]
	}`)

	return baseline, current
}

func TestServerSARIFTrendAnalysisReportsReopenedAndWorsening(t *testing.T) {
	t.Parallel()

	history, baseline, current := sarifTrendHistoryInputs(t)
	output := runServer(t, sarifTrendRequest(t, 19, map[string]string{
		"history_sarif":  "[" + strconv.Quote(history) + "]",
		"baseline_sarif": strconv.Quote(baseline),
		"current_sarif":  strconv.Quote(current),
	}))

	assertOutputContains(t, "SARIF trend", output, []string{
		"reopened",
		"worsening",
		"python.reopened",
		"python.worsening",
		"src/reopened.py",
		"src/risk.py",
	})
}

func sarifTrendHistoryInputs(t *testing.T) (string, string, string) {
	t.Helper()

	history := compactJSON(t, `{
		"version":"2.1.0",
		"runs":[{
			"results":[{
				"ruleId":"python.reopened",
				"level":"warning",
				"message":{"text":"old issue returns"},
				"locations":[{
					"physicalLocation":{
						"artifactLocation":{"uri":"src/reopened.py"},
						"region":{"startLine":5}
					}
				}],
				"properties":{"coding_ethos_group_key":"python.reopened||src/reopened.py|5"}
			}]
		}]
	}`)
	baseline := compactJSON(t, `{
		"version":"2.1.0",
		"runs":[{
			"results":[{
				"ruleId":"python.worsening",
				"level":"warning",
				"message":{"text":"severity increased"},
				"locations":[{
					"physicalLocation":{
						"artifactLocation":{"uri":"src/risk.py"},
						"region":{"startLine":7}
					}
				}],
				"properties":{"coding_ethos_group_key":"python.worsening||src/risk.py|7"}
			}]
		}]
	}`)
	current := compactJSON(t, `{
		"version":"2.1.0",
		"runs":[{
			"results":[{
				"ruleId":"python.reopened",
				"level":"warning",
				"message":{"text":"old issue returns"},
				"locations":[{
					"physicalLocation":{
						"artifactLocation":{"uri":"src/reopened.py"},
						"region":{"startLine":5}
					}
				}],
				"properties":{"coding_ethos_group_key":"python.reopened||src/reopened.py|5"}
			},{
				"ruleId":"python.worsening",
				"level":"error",
				"message":{"text":"severity increased"},
				"locations":[{
					"physicalLocation":{
						"artifactLocation":{"uri":"src/risk.py"},
						"region":{"startLine":7}
					}
				}],
				"properties":{"coding_ethos_group_key":"python.worsening||src/risk.py|7"}
			}]
		}]
	}`)

	return history, baseline, current
}

func sarifTrendRequest(
	t *testing.T,
	requestID int,
	arguments map[string]string,
) string {
	t.Helper()

	parts := make([]string, 0, len(arguments))
	for key, value := range arguments {
		if !strings.HasPrefix(value, `"`) && !strings.HasPrefix(value, "[") {
			value = strconv.Quote(value)
		}

		parts = append(parts, fmt.Sprintf("%q:%s", key, value))
	}

	sort.Strings(parts)

	return compactJSON(t, fmt.Sprintf(`{
		"jsonrpc":"2.0",
		"id":%d,
		"method":"tools/call",
		"params":{
			"name":"sarif_trend_analysis",
			"arguments":{%s}
		}
	}`, requestID, strings.Join(parts, ",")))
}

func TestServerSARIFPolicyFeedbackReportsAuthoringGaps(t *testing.T) {
	t.Parallel()

	sarif := sarifPolicyFeedbackFixture(t)
	output := runServer(t, compactJSON(t, fmt.Sprintf(`{
		"jsonrpc":"2.0",
		"id":18,
		"method":"tools/call",
		"params":{
			"name":"sarif_policy_feedback",
			"arguments":{"sarif":%q}
		}
	}`, sarif)))

	assertOutputContains(t, "SARIF policy feedback", output, []string{
		"unmapped_diagnostics",
		"missing_skill_ids",
		"weak_severities",
		"noisy_rules",
		"tool.unmapped",
		"python.security_note",
		"python.noisy",
		"skill_recommend",
	})
}

func sarifPolicyFeedbackFixture(t *testing.T) string {
	t.Helper()

	noisyResults := make([]string, 0, 5)
	for index := range 5 {
		noisyResults = append(noisyResults, fmt.Sprintf(`{
			"ruleId":"python.noisy",
			"level":"warning",
			"message":{"text":"Repeated noisy diagnostic %d"},
			"locations":[{
				"physicalLocation":{
					"artifactLocation":{"uri":"src/noisy.py"},
					"region":{"startLine":%d}
				}
			}],
			"properties":{"policy_id":"python.noisy"}
		}`, index, index+1))
	}

	return compactJSON(t, fmt.Sprintf(`{
		"version":"2.1.0",
		"runs":[{
			"tool":{"driver":{"rules":[{"id":"python.noisy"}]}},
			"results":[%s,%s]
		}]
	}`, sarifPolicyFeedbackBaseFindings(), strings.Join(noisyResults, ",")))
}

func sarifPolicyFeedbackBaseFindings() string {
	return `{
		"ruleId":"tool.unmapped",
		"level":"warning",
		"message":{"text":"Unmapped linter diagnostic"},
		"locations":[{
			"physicalLocation":{
				"artifactLocation":{"uri":"src/app.py"},
				"region":{"startLine":4}
			}
		}],
		"properties":{"source_tool":"ruff","code":"X999"}
	},{
		"ruleId":"python.security_note",
		"level":"note",
		"message":{"text":"Possible SQL injection vector"},
		"locations":[{
			"physicalLocation":{
				"artifactLocation":{"uri":"src/db.py"},
				"region":{"startLine":9}
			}
		}],
		"properties":{
			"policy_id":"python.sql_safety",
			"skill_id":"lint-remediation",
			"source_tool":"bandit",
			"code":"S608"
		}
	}`
}

func assertOutputContains(
	t *testing.T,
	label string,
	output string,
	expected []string,
) {
	t.Helper()

	for _, item := range expected {
		if !strings.Contains(output, item) {
			t.Fatalf("missing %s in %s output:\n%s", item, label, output)
		}
	}
}

func TestServerRejectsPathLikeTraceID(t *testing.T) {
	t.Parallel()

	output := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":16,
		"method":"tools/call",
		"params":{
			"name":"sarif_risk_summary",
			"arguments":{"trace_id":"../secret.json"}
		}
	}`), mcp.Runtime{ConsumerRoot: t.TempDir()})

	response := decodeResponse(t, output)
	if response["error"] == nil || !strings.Contains(output, "not a path") {
		t.Fatalf("expected path-like trace ID rejection:\n%s", output)
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

	content := structuredContent(t, response)
	if content["blocked"] != true || content["status"] != statusBlocked {
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

func TestServerRemediationExplainExpandsPayload(t *testing.T) {
	t.Parallel()

	output := runServer(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":17,
		"method":"tools/call",
		"params":{
			"name":"remediation_explain",
			"arguments":{
				"remediation":{
					"id":"rem-test",
					"policy_id":"git.hook_bypass",
					"skill_id":"safe-git-workflow",
					"message":"Use the coding-ethos git wrapper.",
					"failed_action":"Bash",
					"command":"git commit --no-verify -m test"
				}
			}
		}
	}`))

	for _, want := range []string{
		`"remediation_explain"`,
		`"rem-test"`,
		`"git.hook_bypass"`,
		`"safe-git-workflow"`,
		`"action_context"`,
		`"policy"`,
		`"skill"`,
		`"principles"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing %s in remediation explain output:\n%s", want, output)
		}
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

func TestServerSkillRecommendUsesBroadAgentWorkSignals(t *testing.T) {
	t.Parallel()

	output := runServer(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":8,
		"method":"tools/call",
		"params":{
			"name":"skill_recommend",
			"arguments":{
				"intent":"implement a refactor to simplify the parser and define success criteria"
			}
		}
	}`))

	if !strings.Contains(output, "agent-operating-discipline") ||
		!strings.Contains(output, "State assumptions") {
		t.Fatalf("missing agent operating discipline recommendation:\n%s", output)
	}
}

func TestServerSkillRecommendFallsBackForFixIntent(t *testing.T) {
	t.Parallel()

	output := runServer(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":8,
		"method":"tools/call",
		"params":{
			"name":"skill_recommend",
			"arguments":{
				"intent":"Fix PostgreSQL AGE connection-pool correctness, pgvector mixed-dimension filtering, and stale AGE node cleanup in internal/query/postgres/index.go",
				"path":"internal/query/postgres/index.go",
				"limit":5
			}
		}
	}`))

	if !strings.Contains(output, "agent-operating-discipline") ||
		!strings.Contains(output, "recommendations") {
		t.Fatalf("missing fallback skill recommendation:\n%s", output)
	}
}

func TestServerRecordsSkillToolUsageAndReportsSkillHealth(t *testing.T) {
	t.Parallel()
	acquireCodeIntelMCPTestSlot(t)

	root := t.TempDir()
	runtime := mcp.Runtime{ConsumerRoot: root}

	lookupOutput := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":19,
		"method":"tools/call",
		"params":{
			"name":"skill_lookup",
			"arguments":{"skill_id":"safe-git-workflow"}
		}
	}`), runtime)
	if !strings.Contains(lookupOutput, "safe-git-workflow") {
		t.Fatalf("skill lookup output missing skill:\n%s", lookupOutput)
	}

	recommendOutput := runServerWithRuntime(t, compactJSON(t, fmt.Sprintf(`{
		"jsonrpc":"2.0",
		"id":20,
		"method":"tools/call",
		"params":{
			"name":"skill_recommend",
			"arguments":{
				"intent":"fix a local import diagnostic",
				"diagnostic":{
					"tool":"ruff",
					"code":%q,
					"message":"import should be at the top-level of a file"
				}
			}
		}
	}`, "PLC"+"0415")), runtime)
	if !strings.Contains(recommendOutput, "conditional-imports") {
		t.Fatalf("skill recommend output missing skill:\n%s", recommendOutput)
	}

	store, err := codeintel.Open(context.Background(), codeintel.DefaultDBPath(root))
	if err != nil {
		t.Fatalf("open code-intel store: %v", err)
	}
	defer store.Close()

	outcomes, err := store.RemediationOutcomes(
		context.Background(),
		codeintel.RemediationOutcomeQuery{
			SkillID: "safe-git-workflow",
			Limit:   10,
		},
	)
	if err != nil {
		t.Fatalf("query skill lookup observation: %v", err)
	}
	if len(outcomes) != 1 || outcomes[0].Tool != "skill_lookup" ||
		outcomes[0].Outcome != "unknown" {
		t.Fatalf("lookup outcomes = %#v", outcomes)
	}

	healthOutput := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":21,
		"method":"tools/call",
		"params":{
			"name":"code_intel_skill_health",
			"arguments":{"format":"toon","limit":20}
		}
	}`), runtime)
	if !strings.Contains(healthOutput, "code_intel.skill_health.v1") ||
		!strings.Contains(healthOutput, "safe-git-workflow") ||
		!strings.Contains(healthOutput, "conditional-imports") {
		t.Fatalf("skill health output missing observations:\n%s", healthOutput)
	}
}

func TestServerReportsCodeIntelHookUsage(t *testing.T) {
	t.Parallel()
	acquireCodeIntelMCPTestSlot(t)

	root := t.TempDir()
	ctx := context.Background()

	store, err := codeintel.Open(ctx, codeintel.DefaultDBPath(root))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	payload := []byte(`{
		"trace_id":"hook-usage-a",
		"tracking_id":"deny-usage-a",
		"recorded_at_utc":"2026-01-01T00:00:00Z",
		"provider":"codex",
		"event":"PreToolUse",
		"tool":"Bash",
		"status":"blocked",
		"operation_kind":"git_status",
		"target_kind":"repo_state",
		"risk_category":"bypass",
		"output_shape":{"blocked":true},
			"decisions":[{
				"policy_id":"git.wrapper_required",
				"decision":"block",
				"severity":"block",
				"skill_id":"safe-git-workflow"
			}]
	}`)

	inlineErr4 := codeintel.NewTraceIngester(store).IngestHookTrace(ctx, payload)
	if inlineErr4 != nil {
		t.Fatalf("ingest hook trace: %v", inlineErr4)
	}

	output := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":34,
		"method":"tools/call",
		"params":{
			"name":"code_intel_hook_usage",
			"arguments":{"risk_category":"bypass"}
		}
	}`), mcp.Runtime{ConsumerRoot: root})
	if !strings.Contains(output, `"code_intel_hook_usage"`) ||
		!strings.Contains(output, `"git.wrapper_required"`) ||
		!strings.Contains(output, `"deny-usage-a"`) {
		t.Fatalf("hook usage output missing expected summary:\n%s", output)
	}
}

func TestServerIndexesAndReturnsCodeChunks(t *testing.T) {
	t.Parallel()
	acquireCodeIntelMCPTestSlot(t)

	root := t.TempDir()

	sourcePath := filepath.Join(root, "pkg", "app.py")

	err := os.MkdirAll(filepath.Dir(sourcePath), 0o700)
	if err != nil {
		t.Fatalf("create source dir: %v", err)
	}

	err = os.WriteFile(sourcePath, []byte(`def build_message(name):
    return f"hello {name}"
`), 0o600)
	if err != nil {
		t.Fatalf("write source: %v", err)
	}

	helperPath := filepath.Join(root, "pkg", "helper.py")

	err = os.WriteFile(helperPath, []byte(`def helper_message(name):
    return name.upper()
`), 0o600)
	if err != nil {
		t.Fatalf("write helper source: %v", err)
	}

	runtime := mcp.Runtime{ConsumerRoot: root}

	indexOutput := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":31,
		"method":"tools/call",
		"params":{
			"name":"code_intel_index_code",
			"arguments":{"paths":["pkg"]}
		}
	}`), runtime)
	if !strings.Contains(indexOutput, `"files_indexed":2`) ||
		!strings.Contains(indexOutput, `"code_intel_index_code"`) {
		t.Fatalf("index output missing summary:\n%s", indexOutput)
	}

	store, err := codeintel.Open(context.Background(), codeintel.DefaultDBPath(root))
	if err != nil {
		t.Fatalf("open code-intel store: %v", err)
	}
	_, err = store.RecordDecision(context.Background(), codeintel.DecisionRecord{
		Title:     "Use explicit startup flow",
		Rationale: "Explicit startup flow keeps the app entrypoint inspectable.",
		Status:    codeintel.DecisionStatusAccepted,
		Links: []codeintel.DecisionLink{{
			Path: "pkg/app.py",
			Kind: codeintel.DecisionLinkAffects,
		}},
	})
	if err != nil {
		t.Fatalf("record decision: %v", err)
	}
	err = store.Close()
	if err != nil {
		t.Fatalf("close code-intel store: %v", err)
	}

	chunksOutput := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":32,
		"method":"tools/call",
		"params":{
			"name":"code_intel_code_chunks",
			"arguments":{"path":"pkg/app.py","symbol_name":"build_message"}
		}
	}`), runtime)
	if !strings.Contains(chunksOutput, `"code_intel_code_chunks"`) ||
		!strings.Contains(chunksOutput, `"build_message"`) {
		t.Fatalf("code chunks output missing indexed symbol:\n%s", chunksOutput)
	}

	contextOutput := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":33,
		"method":"tools/call",
		"params":{
			"name":"code_intel_code_context",
			"arguments":{"path":"pkg/app.py","symbol_path":"build_message"}
		}
	}`), runtime)
	if !strings.Contains(contextOutput, `"code_intel_code_context"`) ||
		!strings.Contains(contextOutput, `"build_message"`) {
		t.Fatalf("code context output missing indexed symbol:\n%s", contextOutput)
	}

	lineContextOutput := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":34,
		"method":"tools/call",
		"params":{
			"name":"code_intel_code_context",
			"arguments":{"path":"pkg/app.py","line":2}
		}
	}`), runtime)
	if !strings.Contains(lineContextOutput, `"code_intel_code_context"`) ||
		!strings.Contains(lineContextOutput, `"build_message"`) {
		t.Fatalf(
			"line code context output missing indexed symbol:\n%s",
			lineContextOutput,
		)
	}

	overviewOutput := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":39,
		"method":"tools/call",
		"params":{
			"name":"code_intel_overview",
			"arguments":{"path":"pkg","limit":5}
		}
	}`), runtime)
	if !strings.Contains(overviewOutput, `"code_intel_overview"`) ||
		!strings.Contains(overviewOutput, `"next_mcp_calls"`) ||
		!strings.Contains(overviewOutput, `"_meta"`) {
		t.Fatalf("overview output missing task-shaped fields:\n%s", overviewOutput)
	}

	contextCardOutput := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":40,
		"method":"tools/call",
		"params":{
			"name":"code_intel_context_card",
			"arguments":{"path":"pkg/app.py","symbol_path":"build_message"}
		}
	}`), runtime)
	if !strings.Contains(contextCardOutput, `"code_intel_context_card"`) ||
		!strings.Contains(contextCardOutput, `"build_message"`) ||
		!strings.Contains(contextCardOutput, `"index_fresh":true`) ||
		!strings.Contains(contextCardOutput, `"decisions"`) ||
		!strings.Contains(contextCardOutput, `"Use explicit startup flow"`) {
		t.Fatalf("context card output missing indexed symbol:\n%s", contextCardOutput)
	}

	orderedContextOutput := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":43,
		"method":"tools/call",
		"params":{
			"name":"code_intel_context_card",
			"arguments":{"paths":["pkg/helper.py","pkg/app.py"],"limit":5}
		}
	}`), runtime)
	orderedContext := structuredContent(t, decodeResponse(t, orderedContextOutput))
	orderedTargets := listValue(t, orderedContext["targets"])
	if len(orderedTargets) != 2 {
		t.Fatalf("ordered context card target count = %d, want 2", len(orderedTargets))
	}
	if got := mapValue(t, orderedTargets[0])["path"]; got != "pkg/helper.py" {
		t.Fatalf("first context card target path = %#v, want pkg/helper.py", got)
	}
	if got := len(listValue(t, mapValue(t, orderedTargets[0])["chunks"])); got == 0 {
		t.Fatalf("first context card target chunks = %d, want at least 1", got)
	}
	if got := mapValue(t, orderedTargets[1])["path"]; got != "pkg/app.py" {
		t.Fatalf("second context card target path = %#v, want pkg/app.py", got)
	}
	if got := len(listValue(t, mapValue(t, orderedTargets[1])["chunks"])); got == 0 {
		t.Fatalf("second context card target chunks = %d, want at least 1", got)
	}

	answerOutput := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":41,
		"method":"tools/call",
		"params":{
			"name":"code_intel_answer",
			"arguments":{"question":"Where is build_message implemented?","limit":5}
		}
	}`), runtime)
	if !strings.Contains(answerOutput, `"code_intel_answer"`) ||
		!strings.Contains(answerOutput, `"retrieval_quality"`) ||
		!strings.Contains(answerOutput, `"citations"`) {
		t.Fatalf("answer output missing retrieval contract:\n%s", answerOutput)
	}

	limitedAnswerOutput := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":44,
		"method":"tools/call",
		"params":{
			"name":"code_intel_answer",
			"arguments":{
				"question":"Where are message functions implemented?",
				"paths":["pkg/helper.py","pkg/app.py"],
				"limit":1
			}
		}
	}`), runtime)
	limitedAnswer := structuredContent(t, decodeResponse(t, limitedAnswerOutput))
	limitedCitations := listValue(t, limitedAnswer["citations"])
	if len(limitedCitations) > 1 {
		t.Fatalf("limited answer citations = %d, want at most 1", len(limitedCitations))
	}

	riskOutput := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":42,
		"method":"tools/call",
		"params":{
			"name":"code_intel_change_risk",
			"arguments":{"path":"pkg/app.py","limit":5}
		}
	}`), runtime)
	if !strings.Contains(riskOutput, `"code_intel_change_risk"`) ||
		!strings.Contains(riskOutput, `"risk_level"`) ||
		!strings.Contains(riskOutput, `"git_signal_freshness"`) ||
		!strings.Contains(riskOutput, `"decision_health"`) ||
		!strings.Contains(riskOutput, `"Use explicit startup flow"`) ||
		!strings.Contains(riskOutput, `"health"`) ||
		!strings.Contains(riskOutput, `"recommended_checks"`) {
		t.Fatalf("change risk output missing task-shaped fields:\n%s", riskOutput)
	}

	healthOutput := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":45,
		"method":"tools/call",
		"params":{
			"name":"code_intel_health",
			"arguments":{"path":"pkg/app.py","refresh":true,"limit":5}
		}
	}`), runtime)
	if !strings.Contains(healthOutput, `"code_intel_health"`) ||
		!strings.Contains(healthOutput, `"total_health_score"`) ||
		!strings.Contains(healthOutput, `"trend"`) {
		t.Fatalf("health output missing snapshot fields:\n%s", healthOutput)
	}

	sessionOutput := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":46,
		"method":"tools/call",
		"params":{
			"name":"code_intel_session_snapshot",
			"arguments":{"format":"toon","limit":5}
		}
	}`), runtime)
	if !strings.Contains(sessionOutput, `"coding_ethos.session.v1"`) ||
		!strings.Contains(sessionOutput, `"snapshot"`) ||
		!strings.Contains(sessionOutput, `"content"`) ||
		!strings.Contains(sessionOutput, "session_source:") {
		t.Fatalf("session snapshot output missing MCP contract fields:\n%s", sessionOutput)
	}
}

func TestServerIndexesRepositoryIntoPrivateStateRoot(t *testing.T) {
	t.Parallel()
	acquireCodeIntelMCPTestSlot(t)

	repoRoot := t.TempDir()
	stateRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "pkg", "app.py")

	err := os.MkdirAll(filepath.Dir(sourcePath), 0o700)
	if err != nil {
		t.Fatalf("create source dir: %v", err)
	}

	err = os.WriteFile(
		sourcePath,
		[]byte("def private_index():\n    return True\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write source: %v", err)
	}

	output := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":47,
		"method":"tools/call",
		"params":{
			"name":"code_intel_index_code",
			"arguments":{"paths":["pkg/app.py"]}
		}
	}`), mcp.Runtime{
		ConsumerRoot: repoRoot,
		StateRoot:    stateRoot,
	})
	if !strings.Contains(output, `"files_indexed":1`) {
		t.Fatalf("private-state index output missing summary:\n%s", output)
	}

	if _, statErr := os.Stat(codeintel.DefaultDBPath(stateRoot)); statErr != nil {
		t.Fatalf("private state database missing: %v", statErr)
	}
	if _, statErr := os.Stat(codeintel.DefaultDBPath(repoRoot)); !os.IsNotExist(statErr) {
		t.Fatalf("repository-local state database exists: %v", statErr)
	}
}

func TestServerChecksCodeSimilarity(t *testing.T) {
	t.Parallel()
	acquireCodeIntelMCPTestSlot(t)

	root := t.TempDir()
	sourcePath := filepath.Join(root, "pkg", "existing.py")

	err := os.MkdirAll(filepath.Dir(sourcePath), 0o700)
	if err != nil {
		t.Fatalf("create source dir: %v", err)
	}

	err = os.WriteFile(sourcePath, []byte(`def build_message(name):
    prefix = "hello"
    cleaned = name.strip()
    output = f"{prefix} {cleaned}"
    return output
`), 0o600)
	if err != nil {
		t.Fatalf("write source: %v", err)
	}

	runtime := mcp.Runtime{ConsumerRoot: root}

	indexOutput := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":36,
		"method":"tools/call",
		"params":{
			"name":"code_intel_index_code",
			"arguments":{"paths":["pkg"]}
		}
	}`), runtime)
	if !strings.Contains(indexOutput, `"files_indexed":1`) {
		t.Fatalf("index output missing summary:\n%s", indexOutput)
	}

	arguments, err := json.Marshal(map[string]any{
		"code": `def make_label(value):
    text = "bye"
    cleaned = value.strip()
    output = f"{text} {cleaned}"
    return output
`,
		"language":  "python",
		"path":      "pkg/new.py",
		"threshold": 0.7,
	})
	if err != nil {
		t.Fatalf("encode arguments: %v", err)
	}

	output := runServerWithRuntime(t, compactJSON(t, fmt.Sprintf(`{
		"jsonrpc":"2.0",
		"id":37,
		"method":"tools/call",
		"params":{
			"name":"code_similarity_check",
			"arguments":%s
		}
	}`, arguments)), runtime)
	if !strings.Contains(output, `"code_similarity_check"`) ||
		!strings.Contains(output, `"build_message"`) ||
		!strings.Contains(output, `"exact_normalized":true`) {
		t.Fatalf("similarity output missing expected match:\n%s", output)
	}
}

func TestServerRejectsInvalidSimilarityThreshold(t *testing.T) {
	t.Parallel()

	output := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":38,
		"method":"tools/call",
		"params":{
			"name":"code_similarity_check",
			"arguments":{
				"code":"def hello():\n    return 'hello'\n",
				"language":"python",
				"threshold":1.1
			}
		}
	}`), mcp.Runtime{ConsumerRoot: t.TempDir()})
	if !strings.Contains(output, "thresholds must be") {
		t.Fatalf("missing invalid threshold error:\n%s", output)
	}
}

func TestServerRejectsUnderspecifiedCodeContext(t *testing.T) {
	t.Parallel()

	output := runServerWithRuntime(t, compactJSON(t, `{
		"jsonrpc":"2.0",
		"id":35,
		"method":"tools/call",
		"params":{
			"name":"code_intel_code_context",
			"arguments":{}
		}
	}`), mcp.Runtime{ConsumerRoot: t.TempDir()})
	if !strings.Contains(
		output,
		"chunk_id, both path and symbol_path, or path and line are required",
	) {
		t.Fatalf("missing underspecified context error:\n%s", output)
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

	err := server.Serve(strings.NewReader(frameMessage(input)), &output)
	if err != nil {
		t.Fatalf("serve MCP: %v", err)
	}

	return output.String()
}

func decodeResponse(t *testing.T, output string) map[string]any {
	t.Helper()

	var response map[string]any

	body := unframeMessage(t, output)

	err := json.Unmarshal([]byte(body), &response)
	if err != nil {
		t.Fatalf("decode response: %v\n%s", err, output)
	}

	return response
}

func structuredContent(t *testing.T, response map[string]any) map[string]any {
	t.Helper()

	return mapValue(t, mapValue(t, response["result"])["structuredContent"])
}

func mapValue(t *testing.T, value any) map[string]any {
	t.Helper()

	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("value = %#v, want object", value)
	}

	return result
}

func listValue(t *testing.T, value any) []any {
	t.Helper()

	result, ok := value.([]any)
	if !ok {
		t.Fatalf("value = %#v, want list", value)
	}

	return result
}

func frameMessage(payload string) string {
	return fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(payload), payload)
}

func unframeMessage(t *testing.T, output string) string {
	t.Helper()

	header, body, ok := strings.Cut(output, "\r\n\r\n")
	if !ok {
		t.Fatalf("missing MCP frame separator:\n%s", output)
	}

	prefix := "Content-Length: "
	if !strings.HasPrefix(header, prefix) {
		t.Fatalf("missing Content-Length header:\n%s", output)
	}

	lengthText := strings.TrimSpace(strings.TrimPrefix(header, prefix))

	var length int

	_, inlineErrB := fmt.Sscanf(lengthText, "%d", &length)
	if inlineErrB != nil {
		t.Fatalf("parse Content-Length: %v\n%s", inlineErrB, output)
	}

	if len(body) != length {
		t.Fatalf("body length = %d, want %d\n%s", len(body), length, output)
	}

	return body
}

func compactJSON(t *testing.T, input string) string {
	t.Helper()

	var value any

	inlineErr5 := json.Unmarshal([]byte(input), &value)
	if inlineErr5 != nil {
		t.Fatalf("parse test JSON: %v", inlineErr5)
	}

	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("compact test JSON: %v", err)
	}

	return string(payload)
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()

	err := os.WriteFile(path, []byte(content), 0o600)
	if err != nil {
		t.Fatalf("write executable: %v", err)
	}

	err = os.Chmod(path, 0o700)
	if err != nil {
		t.Fatalf("chmod executable: %v", err)
	}
}

func buildSandboxHelper(t *testing.T, output string) {
	t.Helper()

	command := exec.Command(
		filepath.Join(runtime.GOROOT(), "bin", "go"),
		"build",
		"-buildvcs=false",
		"-o",
		output,
		"./cmd/coding-ethos-sandbox",
	)
	command.Dir = mcpGoModuleRoot(t)

	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build sandbox helper: %v\n%s", err, output)
	}
}

func nativeSandboxRuntimeAvailable() bool {
	_, err := sandbox.ValidateNativeRuntime()

	return err == nil
}

func mcpGoModuleRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
