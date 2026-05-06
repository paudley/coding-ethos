// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/mcp"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestServerListsTools(t *testing.T) {
	t.Parallel()

	output := runServer(t, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	response := decodeResponse(t, output)

	result := response["result"].(map[string]any)

	tools := result["tools"].([]any)
	if len(tools) != 20 {
		t.Fatalf("tool count = %d, want 20: %#v", len(tools), tools)
	}

	for _, expected := range []string{
		"policy_check_command",
		"policy_check_edit",
		"lint_check",
		"lint_advice",
		"sarif_remediation_advice",
		"sarif_risk_summary",
		"sarif_trend_analysis",
		"sarif_policy_feedback",
		"tool_capabilities",
		"policy_explain",
		"skill_lookup",
		"remediation_explain",
		"code_intel_search",
		"code_intel_index_status",
		"code_intel_hook_usage",
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
		!strings.Contains(output, "executes_tools") {
		t.Fatalf("missing coding-ethos tool metadata:\n%s", output)
	}
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

	result := response["result"].(map[string]any)
	if result["protocolVersion"] != "2024-11-05" {
		t.Fatalf("protocolVersion = %#v, want client version", result["protocolVersion"])
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

	result := response["result"].(map[string]any)
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

	result := response["result"].(map[string]any)
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

	content := response["result"].(map[string]any)["structuredContent"].(map[string]any)
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

	root := t.TempDir()

	sourcePath := filepath.Join(root, "pkg", "app.py")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o700); err != nil {
		t.Fatalf("create source dir: %v", err)
	}

	if err := os.WriteFile(
		sourcePath,
		[]byte("def run():\n    return 'ok'\n"),
		0o600,
	); err != nil {
		t.Fatalf("write source: %v", err)
	}

	ctx := context.Background()

	store, err := codeintel.Open(ctx, codeintel.DefaultDBPath(root))
	if err != nil {
		t.Fatalf("open code-intel store: %v", err)
	}

	if _, err := codeintel.NewASTIndexer(store).
		IndexPaths(ctx, root, []string{"pkg"}); err != nil {
		t.Fatalf("index code: %v", err)
	}

	if err := store.RecordRemediationOutcome(ctx, codeintel.RemediationOutcome{
		RemediationID: "rem-1",
		FindingID:     "finding-1",
		PolicyID:      "python.conditional_imports",
		SkillID:       "conditional-imports",
		Path:          "pkg/app.py",
		Outcome:       "fixed",
		SearchText:    "Move import to module scope.",
	}); err != nil {
		t.Fatalf("record outcome: %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	runtime := mcp.Runtime{ConsumerRoot: root, InvocationCwd: root}

	requests := []struct {
		name string
		body string
		want string
	}{
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
			name: "embedding candidates",
			body: `{"record_kind":"remediation_outcome","policy_id":"python.conditional_imports"}`,
			want: "code_intel_embedding_candidates",
		},
		{
			name: "index status",
			body: `{"collection":"remediations","model_id":"test-model"}`,
			want: "ready_records",
		},
	}
	for index, request := range requests {
		output := runServerWithRuntime(t, compactJSON(t, fmt.Sprintf(`{
			"jsonrpc":"2.0",
			"id":%d,
			"method":"tools/call",
			"params":{"name":"code_intel_%s","arguments":%s}
		}`, 40+index, strings.ReplaceAll(request.name, " ", "_"), request.body)), runtime)
		if !strings.Contains(output, request.want) {
			t.Fatalf("%s output missing %q:\n%s", request.name, request.want, output)
		}
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
		"lint_check",
		"src/app.py",
		"cel_expression",
		"coding-ethos/stable/v1",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("missing %s in SARIF remediation output:\n%s", expected, output)
		}
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
		Status: "blocked",
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
		"lint_check",
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
		Status: "blocked",
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

	baseline := compactJSON(t, `{
		"version":"2.1.0",
		"runs":[{
			"tool":{"driver":{"rules":[{"id":"python.old","properties":{"policy_id":"python.old"}}]}},
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
			"tool":{"driver":{"rules":[{"id":"python.new","properties":{"policy_id":"python.new"}}]}},
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

	output := runServer(t, compactJSON(t, fmt.Sprintf(`{
		"jsonrpc":"2.0",
		"id":17,
		"method":"tools/call",
		"params":{
			"name":"sarif_trend_analysis",
			"arguments":{
				"baseline_sarif":%q,
				"current_sarif":%q
			}
		}
	}`, baseline, current)))

	for _, expected := range []string{
		"introduced",
		"fixed",
		"persisting",
		"python.new",
		"python.old",
		"python.persisting",
		"sarif_remediation_advice",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("missing %s in SARIF trend output:\n%s", expected, output)
		}
	}
}

func TestServerSARIFTrendAnalysisReportsReopenedAndWorsening(t *testing.T) {
	t.Parallel()

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

	output := runServer(t, compactJSON(t, fmt.Sprintf(`{
		"jsonrpc":"2.0",
		"id":19,
		"method":"tools/call",
		"params":{
			"name":"sarif_trend_analysis",
			"arguments":{
				"history_sarif":[%q],
				"baseline_sarif":%q,
				"current_sarif":%q
			}
		}
	}`, history, baseline, current)))

	for _, expected := range []string{
		"reopened",
		"worsening",
		"python.reopened",
		"python.worsening",
		"src/reopened.py",
		"src/risk.py",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("missing %s in SARIF trend output:\n%s", expected, output)
		}
	}
}

func TestServerSARIFPolicyFeedbackReportsAuthoringGaps(t *testing.T) {
	t.Parallel()

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

	sarif := compactJSON(t, fmt.Sprintf(`{
		"version":"2.1.0",
		"runs":[{
			"tool":{"driver":{"rules":[{"id":"python.noisy"}]}},
			"results":[{
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
			},%s]
		}]
	}`, strings.Join(noisyResults, ",")))

	output := runServer(t, compactJSON(t, fmt.Sprintf(`{
		"jsonrpc":"2.0",
		"id":18,
		"method":"tools/call",
		"params":{
			"name":"sarif_policy_feedback",
			"arguments":{"sarif":%q}
		}
	}`, sarif)))

	for _, expected := range []string{
		"unmapped_diagnostics",
		"missing_skill_ids",
		"weak_severities",
		"noisy_rules",
		"tool.unmapped",
		"python.security_note",
		"python.noisy",
		"skill_recommend",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("missing %s in SARIF policy feedback output:\n%s", expected, output)
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

func TestServerReportsCodeIntelHookUsage(t *testing.T) {
	t.Parallel()

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
		"decisions":[{"policy_id":"git.wrapper_required","decision":"block","severity":"block","skill_id":"safe-git-workflow"}]
	}`)
	if err := codeintel.NewTraceIngester(store).IngestHookTrace(ctx, payload); err != nil {
		t.Fatalf("ingest hook trace: %v", err)
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
	if !strings.Contains(indexOutput, `"files_indexed":1`) ||
		!strings.Contains(indexOutput, `"code_intel_index_code"`) {
		t.Fatalf("index output missing summary:\n%s", indexOutput)
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
		t.Fatalf("line code context output missing indexed symbol:\n%s", lineContextOutput)
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
	if _, err := fmt.Sscanf(lengthText, "%d", &length); err != nil {
		t.Fatalf("parse Content-Length: %v\n%s", err, output)
	}

	if len(body) != length {
		t.Fatalf("body length = %d, want %d\n%s", len(body), length, output)
	}

	return body
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

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()

	err := os.WriteFile(path, []byte(content), 0o700)
	if err != nil {
		t.Fatalf("write executable: %v", err)
	}
}
