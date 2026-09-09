// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package e2e_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"blackcat.ca/coding-ethos/go/internal/e2e"
)

// The client spans the complete multi-request workflow; individual command
// deadlines remain independently bounded by the shared e2e command timeout.
const mcpClientTimeout = 2 * time.Minute

type MCPClient struct {
	cancel context.CancelFunc
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	t      *testing.T
	nextID int
}

func StartMCPClient(t *testing.T, ethosRoot, repoRoot string) *MCPClient {
	t.Helper()

	bin := filepath.Join(ethosRoot, "bin", "coding-ethos-run")

	ctx, cancel := context.WithTimeout(context.Background(), mcpClientTimeout)
	cmd := exec.CommandContext(ctx, bin, "mcp")
	cmd.Dir = repoRoot
	cmd.Env = e2e.CommandEnvironment(t, nil)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		t.Fatalf("mcp stdin: %v", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatalf("mcp stdout: %v", err)
	}

	// Capture stderr for debugging
	cmd.Stderr = testWriter{t: t, prefix: "mcp stderr"}

	err = cmd.Start()
	if err != nil {
		cancel()
		t.Fatalf("mcp start: %v", err)
	}

	t.Cleanup(func() {
		_ = stdin.Close()

		err := cmd.Wait()

		cancel()

		if err != nil {
			t.Logf("mcp exit: %v", err)
		}
	})

	return &MCPClient{
		cancel: cancel,
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
		t:      t,
		nextID: 1,
	}
}

type testWriter struct {
	t      *testing.T
	prefix string
}

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Logf("%s: %s", w.prefix, string(p))

	return len(p), nil
}

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      int             `json:"id"`
}

type mcpResponse struct {
	Error   *mcpError       `json:"error,omitempty"`
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	ID      int             `json:"id"`
}

type mcpError struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

func (c *MCPClient) Send(method string, params any) mcpResponse {
	c.t.Helper()

	resp := c.SendAllowError(method, params)
	if resp.Error != nil {
		c.t.Fatalf("mcp error: %s (code %d)", resp.Error.Message, resp.Error.Code)
	}

	return resp
}

func (c *MCPClient) SendAllowError(method string, params any) mcpResponse {
	c.t.Helper()

	var rawParams json.RawMessage

	if params != nil {
		payload, err := json.Marshal(params)
		if err != nil {
			c.t.Fatalf("marshal params: %v", err)
		}

		rawParams = payload
	}

	requestID := c.nextID
	c.nextID++

	req := mcpRequest{
		ID:      requestID,
		JSONRPC: "2.0",
		Method:  method,
		Params:  rawParams,
	}

	payload, err := json.Marshal(req)
	if err != nil {
		c.t.Fatalf("marshal request: %v", err)
	}

	_, err = fmt.Fprintf(c.stdin, "Content-Length: %d\r\n\r\n%s", len(payload), payload)
	if err != nil {
		c.t.Fatalf("write request: %v", err)
	}

	payload = c.readResponsePayload()

	var resp mcpResponse

	err = json.Unmarshal(payload, &resp)
	if err != nil {
		c.t.Fatalf("unmarshal response: %v\n%s", err, string(payload))
	}

	return resp
}

func (c *MCPClient) readResponsePayload() []byte {
	c.t.Helper()

	contentLength := -1

	for {
		line, err := c.stdout.ReadString('\n')
		if err != nil {
			c.t.Fatalf("read response header: %v", err)
		}

		header := strings.TrimRight(line, "\r\n")
		if header == "" {
			break
		}

		name, value, found := strings.Cut(header, ":")
		if !found {
			c.t.Fatalf("invalid response header: %q", header)
		}

		if !strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			continue
		}

		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || parsed < 0 {
			c.t.Fatalf("invalid response content length: %q", value)
		}

		contentLength = parsed
	}

	if contentLength < 0 {
		c.t.Fatal("missing response Content-Length header")
	}

	payload := make([]byte, contentLength)

	_, err := io.ReadFull(c.stdout, payload)
	if err != nil {
		c.t.Fatalf("read response payload: %v", err)
	}

	return payload
}

func TestMCPWorkflow(t *testing.T) {
	t.Parallel()

	ethosRoot, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatalf("abs ethos root: %v", err)
	}

	e2e.RequireRuntime(t, ethosRoot)
	runtimeRoot := e2e.InstrumentedEthosRoot(t, ethosRoot)

	repo := e2e.FromReference(t, ethosRoot, "policy-lint-basic")
	repo.EthosRoot = runtimeRoot
	repo.SyncHookPolicyBundle(t)
	client := StartMCPClient(t, runtimeRoot, repo.Root)

	testMCPInitialize(t, client, repo.Root)
	testMCPToolsList(t, client)
	testMCPToolCapabilities(t, client)
	testMCPPolicyCheckCommand(t, client)
	testMCPPolicyExplain(t, client)
	testMCPLintAdvice(t, client)
	testMCPCodeIntel(t, client)
	testMCPRepoMapResource(t, client)
	testMCPPolicyCheckEdit(t, client)
	testMCPErrorResponse(t, client)
}

func testMCPInitialize(t *testing.T, client *MCPClient, repoRoot string) {
	t.Helper()

	resp := client.Send("initialize", map[string]any{
		"clientInfo":            map[string]string{"name": "test-client", "version": "1.0"},
		"protocolVersion":       "2024-11-05",
		"capabilities":          map[string]any{},
		"serverCapabilities":    map[string]any{},
		"implementation":        map[string]string{"name": "test-client", "version": "1.0"},
		"rootUri":               "file://" + repoRoot,
		"workspaceFolders":      []any{},
		"initializationOptions": map[string]any{},
	})

	if len(resp.Result) == 0 {
		t.Fatalf("expected initialize result")
	}
}

func testMCPToolsList(t *testing.T, client *MCPClient) {
	t.Helper()

	resp := client.Send("tools/list", nil)

	var toolsResult struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}

	err := json.Unmarshal(resp.Result, &toolsResult)
	if err != nil {
		t.Fatalf("unmarshal tools/list: %v", err)
	}

	if len(toolsResult.Tools) == 0 {
		t.Fatalf("expected tools in tools/list response")
	}

	hasTool := func(name string) bool {
		for _, tool := range toolsResult.Tools {
			if tool.Name == name {
				return true
			}
		}

		return false
	}

	for _, name := range []string{
		"policy_check_command",
		"policy_check_edit",
		"policy_explain",
		"lint_advice",
		"tool_capabilities",
		"code_intel_index_status",
		"code_intel_index_code",
		"code_intel_search",
		"code_intel_repo_map",
	} {
		if !hasTool(name) {
			t.Errorf("missing tool: %s", name)
		}
	}
}

func testMCPToolCapabilities(t *testing.T, client *MCPClient) {
	t.Helper()

	resp := client.Send("tools/call", map[string]any{
		"name":      "tool_capabilities",
		"arguments": map[string]any{},
	})

	var capabilities struct {
		Kind    string `json:"kind"`
		Sandbox struct {
			DefaultBackend string `json:"default_backend"`
			RequiredMode   string `json:"required_mode"`
		} `json:"sandbox"`
		Tools []mcpCapabilityInfo `json:"tools"`
	}

	err := json.Unmarshal([]byte(mcpToolCallText(t, resp)), &capabilities)
	if err != nil {
		t.Fatalf("unmarshal tool capabilities text: %v", err)
	}

	if capabilities.Kind != "tool_capabilities" ||
		capabilities.Sandbox.DefaultBackend != "native" ||
		capabilities.Sandbox.RequiredMode != "fail_closed" {
		t.Fatalf("tool capability metadata mismatch: %#v", capabilities)
	}

	ruff, found := mcpCapabilityTool(capabilities.Tools, "ruff")
	if !found {
		t.Fatalf("tool capabilities missing ruff: %#v", capabilities.Tools)
	}

	if len(ruff.Command) == 0 || !slices.Contains(ruff.Tags, "no-network") {
		t.Fatalf("ruff capability missing command/no-network tag: %#v", ruff)
	}
}

func testMCPPolicyCheckCommand(t *testing.T, client *MCPClient) {
	t.Helper()

	args := map[string]any{
		"command": "git commit --no-verify -m 'test: bypass hooks'",
	}

	resp := client.Send("tools/call", map[string]any{
		"name":      "policy_check_command",
		"arguments": args,
	})

	var callResult struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"` //nolint: tagliatelle
	}

	err := json.Unmarshal(resp.Result, &callResult)
	if err != nil {
		t.Fatalf("unmarshal tools/call: %v", err)
	}

	if len(callResult.Content) == 0 {
		t.Fatalf("expected content in tools/call response")
	}

	var checkResult struct {
		Decisions []mcpDecision `json:"decisions"`
		Blocked   bool          `json:"blocked"`
	}

	err = json.Unmarshal([]byte(callResult.Content[0].Text), &checkResult)
	if err != nil {
		t.Fatalf("unmarshal policy_check_command text: %v", err)
	}

	if !checkResult.Blocked ||
		!mcpDecisionIncludes(checkResult.Decisions, "git.hook_bypass") {
		t.Fatalf("expected git.hook_bypass block: %s", callResult.Content[0].Text)
	}
}

func testMCPPolicyExplain(t *testing.T, client *MCPClient) {
	t.Helper()

	resp := client.Send("tools/call", map[string]any{
		"name": "policy_explain",
		"arguments": map[string]any{
			"policy_id": "git.hook_bypass",
		},
	})

	var callResult mcpToolCallResult

	err := json.Unmarshal(resp.Result, &callResult)
	if err != nil {
		t.Fatalf("unmarshal policy_explain: %v", err)
	}

	var explanation struct {
		Explanation string `json:"explanation"`
		PolicyID    string `json:"policy_id"`
	}

	err = json.Unmarshal([]byte(callResult.Content[0].Text), &explanation)
	if err != nil {
		t.Fatalf("unmarshal policy explanation text: %v", err)
	}

	if explanation.PolicyID != "git.hook_bypass" ||
		!strings.Contains(explanation.Explanation, "git.hook_bypass") {
		t.Fatalf("policy explanation mismatch: %#v", explanation)
	}
}

func testMCPLintAdvice(t *testing.T, client *MCPClient) {
	t.Helper()

	resp := client.Send("tools/call", map[string]any{
		"name": "lint_advice",
		"arguments": map[string]any{
			"tool":    "ruff",
			"code":    "PLC0415",
			"file":    "pkg/clean.py",
			"line":    1,
			"message": "import should be at the top-level of a file",
		},
	})

	var callResult mcpToolCallResult

	err := json.Unmarshal(resp.Result, &callResult)
	if err != nil {
		t.Fatalf("unmarshal lint_advice: %v", err)
	}

	var advice struct {
		Diagnostic struct {
			PolicyID string `json:"policy_id"`
			SkillID  string `json:"skill_id"`
		} `json:"diagnostic"`
		SkillHints []mcpSkillHint `json:"skill_hints"`
	}

	err = json.Unmarshal([]byte(callResult.Content[0].Text), &advice)
	if err != nil {
		t.Fatalf("unmarshal lint advice text: %v", err)
	}

	if advice.Diagnostic.PolicyID != "python.conditional_imports" ||
		advice.Diagnostic.SkillID != "conditional-imports" ||
		!mcpSkillHintsInclude(advice.SkillHints, "conditional-imports") {
		t.Fatalf("lint advice did not include expected policy and skill: %#v", advice)
	}
}

func testMCPCodeIntel(t *testing.T, client *MCPClient) {
	t.Helper()

	// 4. Run code intelligence (code_intel_index_code)
	resp := client.Send("tools/call", map[string]any{
		"name": "code_intel_index_code",
		"arguments": map[string]any{
			"paths": []string{"."},
		},
	})

	var callResult mcpToolCallResult

	err := json.Unmarshal(resp.Result, &callResult)
	if err != nil {
		t.Fatalf("unmarshal code_intel_index_code: %v", err)
	}

	if callResult.IsError {
		t.Fatalf("code_intel_index_code failed: %v", callResult.Content)
	}

	// 5. Query code intelligence (code_intel_search)
	resp = client.Send("tools/call", map[string]any{
		"name": "code_intel_search",
		"arguments": map[string]any{
			"text": "greet",
		},
	})

	err = json.Unmarshal(resp.Result, &callResult)
	if err != nil {
		t.Fatalf("unmarshal code_intel_search: %v", err)
	}

	var searchOutput struct {
		Results []any `json:"results"`
	}

	err = json.Unmarshal([]byte(callResult.Content[0].Text), &searchOutput)
	if err != nil {
		t.Fatalf("unmarshal search output text: %v", err)
	}

	if len(searchOutput.Results) == 0 {
		t.Fatalf("expected search results in JSON output: %s", callResult.Content[0].Text)
	}

	t.Logf("code_intel_search result: %s", callResult.Content[0].Text)
}

func testMCPRepoMapResource(t *testing.T, client *MCPClient) {
	t.Helper()

	listResp := client.Send("resources/list", nil)

	var listResult struct {
		Resources []mcpResourceInfo `json:"resources"`
	}

	err := json.Unmarshal(listResp.Result, &listResult)
	if err != nil {
		t.Fatalf("unmarshal resources/list: %v", err)
	}

	if !mcpResourcesInclude(
		listResult.Resources,
		"coding-ethos://code-intel/repo-map",
	) {
		t.Fatalf("resources/list missing repo-map resource: %#v", listResult.Resources)
	}

	readResp := client.Send("resources/read", map[string]any{
		"uri": "coding-ethos://code-intel/repo-map",
	})

	var readResult struct {
		Contents []struct {
			URI      string `json:"uri"`
			MimeType string `json:"mimeType"` //nolint: tagliatelle
			Text     string `json:"text"`
		} `json:"contents"`
	}

	err = json.Unmarshal(readResp.Result, &readResult)
	if err != nil {
		t.Fatalf("unmarshal resources/read: %v", err)
	}

	if len(readResult.Contents) != 1 {
		t.Fatalf("resources/read contents = %#v, want one entry", readResult.Contents)
	}

	content := readResult.Contents[0]
	if content.URI != "coding-ethos://code-intel/repo-map" ||
		content.MimeType != "text/vnd.coding-ethos.toon" ||
		!strings.Contains(content.Text, "pkg/clean.py") ||
		!strings.Contains(content.Text, "greet") {
		t.Fatalf("repo-map resource content mismatch: %#v", content)
	}
}

func testMCPPolicyCheckEdit(t *testing.T, client *MCPClient) {
	t.Helper()

	// 6. Run an edit check (policy_check_edit)
	// We simulate an edit that introduces branded attribution
	// (blocked by no-self-promotion policy on .md files)
	resp := client.Send("tools/call", map[string]any{
		"name": "policy_check_edit",
		"arguments": map[string]any{
			"path":   "README.md",
			"before": "Installation",
			"after":  "# Installation\n\nGenerated with Gemini",
		},
	})

	var callResult mcpToolCallResult

	err := json.Unmarshal(resp.Result, &callResult)
	if err != nil {
		t.Fatalf("unmarshal policy_check_edit: %v", err)
	}

	var editResult struct {
		Decisions []any `json:"decisions"`
		Blocked   bool  `json:"blocked"`
	}

	err = json.Unmarshal([]byte(callResult.Content[0].Text), &editResult)
	if err != nil {
		t.Fatalf("unmarshal policy_check_edit text: %v", err)
	}

	if !editResult.Blocked {
		t.Fatalf("expected edit to be blocked: %s", callResult.Content[0].Text)
	}

	t.Logf("policy_check_edit result: %s", callResult.Content[0].Text)
}

func testMCPErrorResponse(t *testing.T, client *MCPClient) {
	t.Helper()

	resp := client.SendAllowError("tools/call", map[string]any{
		"name":      "policy_explain",
		"arguments": map[string]any{},
	})

	if resp.Error == nil {
		t.Fatalf("expected MCP error response for invalid policy_explain call")
	}

	if resp.Error.Code != -32602 ||
		!strings.Contains(resp.Error.Message, "policy_id is required") {
		t.Fatalf("unexpected MCP error response: %#v", resp.Error)
	}
}

type mcpToolCallResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError"` //nolint: tagliatelle
}

type mcpDecision struct {
	PolicyID string `json:"policy_id"`
	SkillID  string `json:"skill_id"`
}

type mcpSkillHint struct {
	SkillID string `json:"skill_id"`
}

type mcpCapabilityInfo struct {
	Name    string   `json:"name"`
	Command []string `json:"command"`
	Tags    []string `json:"tags"`
}

type mcpResourceInfo struct {
	URI      string `json:"uri"`
	Name     string `json:"name"`
	MimeType string `json:"mimeType"` //nolint: tagliatelle
}

func mcpToolCallText(t *testing.T, resp mcpResponse) string {
	t.Helper()

	var callResult mcpToolCallResult

	err := json.Unmarshal(resp.Result, &callResult)
	if err != nil {
		t.Fatalf("unmarshal MCP tool call result: %v", err)
	}

	if callResult.IsError || len(callResult.Content) == 0 {
		t.Fatalf("MCP tool call returned no successful text content: %#v", callResult)
	}

	return callResult.Content[0].Text
}

func mcpDecisionIncludes(decisions []mcpDecision, policyID string) bool {
	return slices.ContainsFunc(decisions, func(decision mcpDecision) bool {
		return decision.PolicyID == policyID
	})
}

func mcpResourcesInclude(resources []mcpResourceInfo, uri string) bool {
	return slices.ContainsFunc(resources, func(resource mcpResourceInfo) bool {
		return resource.URI == uri
	})
}

func mcpCapabilityTool(
	tools []mcpCapabilityInfo,
	name string,
) (mcpCapabilityInfo, bool) {
	index := slices.IndexFunc(tools, func(tool mcpCapabilityInfo) bool {
		return tool.Name == name
	})
	if index < 0 {
		return mcpCapabilityInfo{}, false
	}

	return tools[index], true
}

func mcpSkillHintsInclude(hints []mcpSkillHint, skillID string) bool {
	return slices.ContainsFunc(hints, func(hint mcpSkillHint) bool {
		return hint.SkillID == skillID
	})
}
