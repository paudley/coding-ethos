// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hookoutput_test

import (
	"encoding/json"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/diagnostics"
	. "blackcat.ca/coding-ethos/go/internal/hookoutput"
	"blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const (
	sha256HexLength    = 64
	sarifRepoURI       = "."
	sarifSchema        = "https://json.schemastore.org/sarif-2.1.0.json"
	sarifVersion       = "2.1.0"
	toonFindingsHeader = "findings[1]{tool,file,line,column,severity,code," +
		"policy_id,skill_id,message,advice,detail}:"
)

func TestFormatLintResultTOONUsesDiagnostics(t *testing.T) {
	t.Parallel()

	result := lint.Result{
		Scope:  lint.ScopeStaged,
		Status: "blocked",
		Diagnostics: []diagnostics.Diagnostic{{
			Tool:     "pii",
			File:     ".codex/config.toml",
			Line:     8,
			Severity: "block",
			PolicyID: "repo.pii_scrubber",
			Message:  "local machine detail detected",
			Advice:   "Replace local paths with generic placeholders.",
			Detail:   "matched /" + "home/example/project",
		}},
	}

	output, err := FormatLintResult(result, FormatTOON)
	if err != nil {
		t.Fatalf("format lint result: %v", err)
	}

	for _, want := range []string{
		"tool: policy-lint",
		"scope: staged",
		toonFindingsHeader,
		"pii,.codex/config.toml,8,0,block,,repo.pii_scrubber,," +
			"local machine detail detected,Replace local paths with generic placeholders.," +
			"matched /" + "home/example/project",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("TOON output missing %q:\n%s", want, output)
		}
	}

	if strings.Contains(output, `"decisions"`) || strings.Contains(output, "{\n") {
		t.Fatalf("TOON output looks like raw JSON:\n%s", output)
	}
}

func TestFormatLintResultTOONSuppressesRecordOnlyPolicyNoise(t *testing.T) {
	t.Parallel()

	result := lint.Result{
		Scope:  lint.ScopeFiles,
		Status: "resolved",
		Diagnostics: []diagnostics.Diagnostic{{
			Tool:     "policy",
			Severity: "record",
			PolicyID: "python.conditional_imports",
			Message:  "Required dependencies should fail immediately.",
		}},
		Findings: []lint.Finding{{
			CheckID:  "python.conditional_imports",
			Files:    []string{"src/app.py"},
			Message:  "Required dependencies should fail immediately.",
			PolicyID: "python.conditional_imports",
			Severity: "record",
			Status:   "pass",
		}},
	}

	output, err := FormatLintResult(result, FormatTOON)
	if err != nil {
		t.Fatalf("format lint result: %v", err)
	}

	if !strings.Contains(output, "findings[0]") {
		t.Fatalf("TOON output should report no findings:\n%s", output)
	}

	for _, forbidden := range []string{
		"policy,,0,0,record",
		"python.conditional_imports",
		"src/app.py,0,0,record",
	} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("TOON output included record-only noise %q:\n%s", forbidden, output)
		}
	}
}

func TestFormatLintResultTOONPreservesRuffCodeAndFullMessage(t *testing.T) {
	t.Parallel()

	result := lint.Result{
		Scope:  "tool:ruff",
		Status: "blocked",
		Diagnostics: []diagnostics.Diagnostic{{
			Tool:     "ruff",
			File:     "pkg/app.py",
			Line:     4,
			Column:   8,
			Severity: "error",
			Code:     "PLC0415",
			PolicyID: "python.conditional_imports",
			Message:  "import should be at the top-level of a file",
			Advice:   "Move required imports to module scope.",
		}},
	}

	output, err := FormatLintResult(result, FormatTOON)
	if err != nil {
		t.Fatalf("format lint result: %v", err)
	}

	want := "ruff,pkg/app.py,4,8,error,PLC0415,python.conditional_imports,," +
		"import should be at the top-level of a file," +
		"Move required imports to module scope.,"
	if !strings.Contains(output, want) {
		t.Fatalf("TOON output missing full Ruff diagnostic:\n%s", output)
	}
}

func TestFormatLintResultTOONIncludesExistingTraceID(t *testing.T) {
	t.Parallel()

	result := lint.Result{
		TraceID: "20260504T000000.000000000Z-123-staged.json",
		Scope:   lint.ScopeStaged,
		Status:  "blocked",
		Diagnostics: []diagnostics.Diagnostic{{
			Tool:     "policy",
			File:     "pkg/app.py",
			Line:     1,
			Severity: "block",
			PolicyID: "filesystem.line_limits",
			Message:  "Large source files must not keep growing.",
		}},
	}

	output, err := FormatLintResult(result, FormatTOON)
	if err != nil {
		t.Fatalf("format lint result: %v", err)
	}

	if !strings.Contains(
		output,
		"trace_id: 20260504T000000.000000000Z-123-staged.json",
	) {
		t.Fatalf("TOON output missing trace_id:\n%s", output)
	}
}

func TestFormatLintResultTOONCreatesTraceIDForBlockedResult(t *testing.T) {
	t.Parallel()

	result := lint.Result{
		Scope:  lint.ScopeStaged,
		Status: "blocked",
		Diagnostics: []diagnostics.Diagnostic{{
			Tool:     "policy",
			File:     "pkg/app.py",
			Line:     1,
			Severity: "block",
			PolicyID: "filesystem.line_limits",
			Message:  "Large source files must not keep growing.",
		}},
	}

	output, err := FormatLintResult(result, FormatTOON)
	if err != nil {
		t.Fatalf("format lint result: %v", err)
	}

	if !strings.Contains(output, "trace_id: ") ||
		!strings.Contains(output, "-staged.json") {
		t.Fatalf("TOON output missing generated trace_id:\n%s", output)
	}
}

func TestFormatLintResultSARIFIncludesRuleMetadata(t *testing.T) {
	t.Parallel()

	result := lint.Result{
		Scope:  "tool:ruff",
		Status: "blocked",
		Decisions: []policy.Decision{{
			Decision:     "block",
			PolicyID:     "python.unused_imports",
			Severity:     "block",
			PrincipleIDs: []string{"static-analysis-is-the-first-line-of-defense"},
		}},
		Diagnostics: []diagnostics.Diagnostic{{
			Tool:     "ruff",
			File:     "pkg/app.py",
			Line:     4,
			Column:   8,
			Severity: "error",
			Code:     "F401",
			PolicyID: "python.unused_imports",
			SkillID:  "lint-remediation",
			Message:  "unused import",
			Advice:   "Remove unused imports instead of suppressing Ruff.",
			Detail:   "imported but unused",
			Metadata: map[string]any{
				"implementation":       "cel",
				"ast_change_source":    "staged",
				"ast_language":         "python",
				"ast_end_byte":         int64(128),
				"ast_end_column":       int64(19),
				"ast_end_line":         int64(4),
				"ast_node_kind":        "function_definition",
				"ast_start_byte":       int64(72),
				"ast_symbol_kind":      "function",
				"ast_symbol_name":      "load_config",
				"ast_symbol_path":      "load_config",
				"input_schema_version": int64(1),
				"policy_source":        "coding_ethos.yml:principles.4",
				"proxy_direction":      "outbound",
				"proxy_event_id":       "proxy-event-1",
				"proxy_event_kind":     "provider_call",
				"proxy_payload_bytes":  int64(4096),
				"proxy_payload_kind":   "prompt",
				"proxy_provider":       "codex",
				"proxy_session_id":     "proxy-session-1",
				"proxy_token_total":    int64(512),
				"proxy_trace_id":       "trace-1",
				"proxy_tracking_id":    "track-1",
				"proxy_transform":      "dlp-inspection",
				"snippet":              "def load_config():",
				"when":                 "diagnostic.code == 'F401'",
			},
			PrincipleIDs: []string{"static-analysis-is-the-first-line-of-defense"},
			Tags:         []string{"linting", "quality"},
		}},
	}

	output, err := FormatLintResult(result, FormatSARIF)
	if err != nil {
		t.Fatalf("format SARIF: %v", err)
	}

	var payload map[string]any

	inlineErr0 := json.Unmarshal([]byte(output), &payload)
	if inlineErr0 != nil {
		t.Fatalf("decode SARIF: %v\n%s", inlineErr0, output)
	}

	assertSARIFRunMetadata(t, payload)
	assertSARIFRuleMetadata(t, payload)
	assertSARIFResultLocation(t, payload)
	assertSARIFResultProperties(t, payload)
	assertSARIFCoverageMetadata(t, payload)
}

func assertSARIFRunMetadata(t *testing.T, payload map[string]any) {
	t.Helper()

	assertJSONPath(t, payload, "$schema", sarifSchema)
	assertJSONPath(t, payload, "version", sarifVersion)
	assertJSONPath(t, payload, "runs.0.automationDetails.id", "coding-ethos/tool/ruff")
	assertJSONPath(
		t,
		payload,
		"runs.0.invocations.0.workingDirectory.uri",
		sarifRepoURI,
	)
	assertJSONPath(t, payload, "runs.0.invocations.0.executionSuccessful", false)
	assertJSONPath(t, payload, "runs.0.tool.driver.name", "coding-ethos")
	assertJSONPath(t, payload, "runs.0.tool.driver.rules.0.id", "python.unused_imports")
	assertJSONPath(
		t,
		payload,
		"runs.0.tool.driver.rules.0.properties.precision",
		"high",
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.tool.driver.rules.0.properties.policy_id",
		"python.unused_imports",
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.tool.driver.rules.0.properties.skill_id",
		"lint-remediation",
	)
}

func assertSARIFRuleMetadata(t *testing.T, payload map[string]any) {
	t.Helper()

	assertJSONPath(
		t,
		payload,
		"runs.0.tool.driver.rules.0.properties.implementation",
		"cel",
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.tool.driver.rules.0.properties.input_schema_version",
		float64(1),
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.tool.driver.rules.0.properties.policy_source",
		"coding_ethos.yml:principles.4",
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.tool.driver.rules.0.properties.cel_expression",
		"diagnostic.code == 'F401'",
	)
	assertJSONPath(t, payload, "runs.0.results.0.ruleId", "python.unused_imports")
	assertJSONPath(t, payload, "runs.0.results.0.ruleIndex", float64(0))
	assertJSONPath(t, payload, "runs.0.results.0.level", "error")
	assertJSONPath(t, payload, "runs.0.results.0.message.text", "unused import")
}

func assertSARIFResultLocation(t *testing.T, payload map[string]any) {
	t.Helper()

	assertJSONPath(
		t,
		payload,
		"runs.0.results.0.locations.0.physicalLocation.artifactLocation.uri",
		"pkg/app.py",
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.results.0.locations.0.physicalLocation.region.startLine",
		float64(4),
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.results.0.locations.0.physicalLocation.region.startColumn",
		float64(8),
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.results.0.locations.0.physicalLocation.region.endLine",
		float64(4),
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.results.0.locations.0.physicalLocation.region.endColumn",
		float64(19),
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.results.0.locations.0.physicalLocation.region.byteOffset",
		float64(72),
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.results.0.locations.0.physicalLocation.region.byteLength",
		float64(56),
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.results.0.locations.0.physicalLocation.region.snippet.text",
		"def load_config():",
	)
	assertJSONPathPrefix(
		t,
		payload,
		"runs.0.results.0.partialFingerprints.coding-ethos/v1",
	)
	assertJSONPathPrefix(
		t,
		payload,
		"runs.0.results.0.partialFingerprints.coding-ethos/stable/v1",
	)
	assertJSONPathPrefix(
		t,
		payload,
		"runs.0.results.0.partialFingerprints.coding-ethos/ast/v1",
	)
}

func assertSARIFResultProperties(t *testing.T, payload map[string]any) {
	t.Helper()

	assertJSONPath(
		t,
		payload,
		"runs.0.results.0.properties.ast_change_source",
		"staged",
	)
	assertJSONPath(t, payload, "runs.0.results.0.properties.ast_language", "python")
	assertJSONPath(
		t,
		payload,
		"runs.0.results.0.properties.ast_symbol_name",
		"load_config",
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.results.0.properties.ast_symbol_path",
		"load_config",
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.results.0.properties.policy_id",
		"python.unused_imports",
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.results.0.properties.skill_id",
		"lint-remediation",
	)
	assertJSONPath(t, payload, "runs.0.results.0.properties.implementation", "cel")
	assertJSONPath(
		t,
		payload,
		"runs.0.results.0.properties.input_schema_version",
		float64(1),
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.results.0.properties.policy_source",
		"coding_ethos.yml:principles.4",
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.results.0.properties.proxy_event_id",
		"proxy-event-1",
	)
	assertJSONPath(t, payload, "runs.0.results.0.properties.proxy_direction", "outbound")
	assertJSONPath(t, payload, "runs.0.results.0.properties.proxy_payload_kind", "prompt")
	assertJSONPath(
		t,
		payload,
		"runs.0.results.0.properties.proxy_token_total",
		float64(512),
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.results.0.properties.cel_expression",
		"diagnostic.code == 'F401'",
	)
	assertJSONPathPrefix(
		t,
		payload,
		"runs.0.results.0.properties.coding_ethos_group_id",
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.results.0.properties.coding_ethos_group_key",
		"python.unused_imports|lint-remediation|pkg/app.py|4",
	)
	assertJSONPath(t, payload, "runs.0.results.0.properties.coding_ethos", true)
}

func assertSARIFCoverageMetadata(t *testing.T, payload map[string]any) {
	t.Helper()

	assertJSONPathPrefix(t, payload, "runs.0.properties.finding_groups.0.id")
	assertJSONPath(
		t,
		payload,
		"runs.0.properties.finding_groups.0.key",
		"python.unused_imports|lint-remediation|pkg/app.py|4",
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.properties.finding_groups.0.result_count",
		float64(1),
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.properties.policy_coverage.policies.0",
		"python.unused_imports",
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.properties.policy_coverage.ethos_ids.0",
		"static-analysis-is-the-first-line-of-defense",
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.properties.policy_coverage.skills.0",
		"lint-remediation",
	)
	assertJSONPath(t, payload, "runs.0.properties.policy_coverage.tools.0", "ruff")
	assertJSONPath(
		t,
		payload,
		"runs.0.properties.policy_coverage.policy_count",
		float64(1),
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.properties.policy_coverage.result_count",
		float64(1),
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.properties.policy_coverage.decision_count",
		float64(1),
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.properties.policy_coverage.diagnostic_count",
		float64(1),
	)
}

func TestFormatLintResultSARIFGroupsCrossToolFindings(t *testing.T) {
	t.Parallel()

	output, err := FormatLintResult(crossToolFindingResult(), FormatSARIF)
	if err != nil {
		t.Fatalf("format SARIF: %v", err)
	}

	var payload map[string]any

	inlineErr1 := json.Unmarshal([]byte(output), &payload)
	if inlineErr1 != nil {
		t.Fatalf("decode SARIF: %v\n%s", inlineErr1, output)
	}

	assertSARIFCrossToolFindingGroups(t, payload)
}

func crossToolFindingResult() lint.Result {
	return lint.Result{
		Scope:  lint.ScopeFiles,
		Status: "blocked",
		Diagnostics: []diagnostics.Diagnostic{
			{
				Tool:     "ruff",
				File:     "pkg/app.py",
				Line:     20,
				Severity: "error",
				Code:     "PLC0415",
				PolicyID: "python.conditional_imports",
				SkillID:  "conditional-imports",
				Message:  "import should be at the top-level of a file",
			},
			{
				Tool:     "pyright",
				File:     "./pkg/app.py",
				Line:     20,
				Severity: "error",
				Code:     "reportMissingImports",
				PolicyID: "python.conditional_imports",
				SkillID:  "conditional-imports",
				Message:  "Import cannot be resolved",
			},
		},
	}
}

func assertSARIFCrossToolFindingGroups(t *testing.T, payload map[string]any) {
	t.Helper()

	assertJSONPath(
		t,
		payload,
		"runs.0.results.0.properties.coding_ethos_group_key",
		"python.conditional_imports|conditional-imports|pkg/app.py|20",
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.results.1.properties.coding_ethos_group_key",
		"python.conditional_imports|conditional-imports|pkg/app.py|20",
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.properties.finding_groups.0.key",
		"python.conditional_imports|conditional-imports|pkg/app.py|20",
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.properties.finding_groups.0.result_count",
		float64(2),
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.properties.finding_groups.0.source_tools.0",
		"pyright",
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.properties.finding_groups.0.source_tools.1",
		"ruff",
	)
	assertJSONPathPrefix(
		t,
		payload,
		"runs.0.properties.finding_groups.0.stable_fingerprints.0",
	)
	assertJSONPathPrefix(
		t,
		payload,
		"runs.0.properties.finding_groups.0.stable_fingerprints.1",
	)
}

func TestFormatLintResultSARIFUsesExplicitCategory(t *testing.T) {
	t.Parallel()

	result := lint.Result{
		Scope:  lint.ScopeFiles,
		Status: "resolved",
		Diagnostics: []diagnostics.Diagnostic{{
			Tool:     "policy-lint",
			File:     "pkg/app.py",
			Line:     1,
			Severity: "warning",
			PolicyID: "repo.example",
			Message:  "example",
		}},
	}

	output, err := FormatLintResultSARIFWithOptions(
		result,
		SARIFOptions{Category: "policy"},
	)
	if err != nil {
		t.Fatalf("format SARIF: %v", err)
	}

	var payload map[string]any

	inlineErr2 := json.Unmarshal([]byte(output), &payload)
	if inlineErr2 != nil {
		t.Fatalf("decode SARIF: %v\n%s", inlineErr2, output)
	}

	assertJSONPath(t, payload, "runs.0.automationDetails.id", "policy/")
}

func TestFormatLintResultSARIFOmitPathlessPolicyFindings(t *testing.T) {
	t.Parallel()

	result := lint.Result{
		Scope:  lint.ScopeStaged,
		Status: "blocked",
		Decisions: []policy.Decision{{
			Decision: "block",
			Severity: "block",
			PolicyID: "repo.pii_scrubber",
			Message:  "Local-machine PII must not be committed.",
		}},
	}

	output, err := FormatLintResult(result, FormatSARIF)
	if err != nil {
		t.Fatalf("format SARIF: %v", err)
	}

	var payload map[string]any

	inlineErr3 := json.Unmarshal([]byte(output), &payload)
	if inlineErr3 != nil {
		t.Fatalf("decode SARIF: %v\n%s", inlineErr3, output)
	}

	results := jsonPathSlice(t, payload, "runs.0.results")
	if len(results) != 0 {
		t.Fatalf("pathless policy SARIF results = %#v, want none", results)
	}
}

func TestFormatLintResultSARIFIncludesRecordOnlyPolicyContext(t *testing.T) {
	t.Parallel()

	result := lint.Result{
		Scope:  lint.ScopeStaged,
		Status: "resolved",
		Decisions: []policy.Decision{{
			Decision: "record",
			Severity: "record",
			PolicyID: "repo.pii_scrubber",
			Message:  "Local-machine PII must not be committed.",
		}},
	}

	output, err := FormatLintResult(result, FormatSARIF)
	if err != nil {
		t.Fatalf("format SARIF: %v", err)
	}

	var payload map[string]any

	inlineErr4 := json.Unmarshal([]byte(output), &payload)
	if inlineErr4 != nil {
		t.Fatalf("decode SARIF: %v\n%s", inlineErr4, output)
	}

	results := jsonPathSlice(t, payload, "runs.0.results")
	if len(results) != 0 {
		t.Fatalf("record-only policy SARIF results = %#v, want none", results)
	}

	assertJSONPath(
		t,
		payload,
		"runs.0.properties.policy_coverage.policies.0",
		"repo.pii_scrubber",
	)
}

func TestFormatLintResultSARIFIncludesCapturePayloads(t *testing.T) {
	t.Parallel()

	result := lint.Result{
		Scope:  "tool:go-test",
		Status: "blocked",
		Capture: &lint.ToolCapture{
			Tool:        "go-test",
			Parser:      "go-test",
			ParseStatus: "parse_error",
			Stdout:      "coverage: 79.6% of statements\n",
			Stderr:      "panic: hidden failure\n",
			ExitCode:    1,
		},
		Findings: []lint.Finding{{
			RawOutcome: map[string]any{
				"stdout":    "coverage: 79.6% of statements\n",
				"stderr":    "panic: hidden failure\n",
				"exit_code": 1,
			},
			CheckID:    "tool.go-test",
			Message:    "go-test exited with status 1 without parseable diagnostics",
			Severity:   "fatal",
			SourceTool: "go-test",
			Status:     "fail",
			Blocking:   true,
		}},
	}

	output, err := FormatLintResult(result, FormatSARIF)
	if err != nil {
		t.Fatalf("format SARIF: %v", err)
	}

	var payload map[string]any

	decodeErr := json.Unmarshal([]byte(output), &payload)
	if decodeErr != nil {
		t.Fatalf("decode SARIF: %v\n%s", decodeErr, output)
	}

	assertJSONPath(
		t,
		payload,
		"runs.0.properties.capture.stdout",
		"coverage: 79.6% of statements\n",
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.properties.capture.stderr",
		"panic: hidden failure\n",
	)

	results := jsonPathSlice(t, payload, "runs.0.results")
	if len(results) != 0 {
		t.Fatalf("pathless capture SARIF results = %#v, want none", results)
	}
}

func TestFormatLintResultSARIFMarksSecurityRulesForCodeScanning(t *testing.T) {
	t.Parallel()

	result := lint.Result{
		Scope:  "tool:bandit",
		Status: "blocked",
		Diagnostics: []diagnostics.Diagnostic{{
			Tool:     "bandit",
			File:     "./pkg/db.py",
			Line:     42,
			Severity: "warning",
			Code:     "S608",
			PolicyID: "python.sql_safety",
			Message:  "Possible SQL injection vector",
			Tags:     []string{"python"},
		}},
	}

	output, err := FormatLintResult(result, FormatSARIF)
	if err != nil {
		t.Fatalf("format SARIF: %v", err)
	}

	var payload map[string]any

	inlineErr5 := json.Unmarshal([]byte(output), &payload)
	if inlineErr5 != nil {
		t.Fatalf("decode SARIF: %v\n%s", inlineErr5, output)
	}

	assertJSONPath(
		t,
		payload,
		"runs.0.tool.driver.rules.0.properties.security-severity",
		"5.0",
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.tool.driver.rules.0.properties.tags.1",
		"security",
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.results.0.locations.0.physicalLocation.artifactLocation.uri",
		"pkg/db.py",
	)
}

func TestFormatLintResultSARIFIncludesSandboxEvidence(t *testing.T) {
	t.Parallel()

	result := lint.Result{
		Scope:  "tool:ruff",
		Status: "blocked",
		Capture: &lint.ToolCapture{
			Tool: "ruff",
			Sandbox: &lint.SandboxEvidence{
				Mode:            "required",
				Backend:         "native",
				Profile:         "lint-offline",
				Tool:            "ruff",
				Tags:            []string{"no-network", "no-git"},
				TimeoutSeconds:  300,
				MemoryMB:        2048,
				CPUQuotaPercent: 100,
				SeccompProfile:  "deny-privilege",
				NetworkIsolated: true,
				ProcessIsolated: true,
				TimeoutEnforced: true,
				CgroupRequested: true,
				SeccompEnabled:  true,
				Enabled:         true,
			},
		},
		Diagnostics: []diagnostics.Diagnostic{{
			Tool:     "ruff",
			File:     "pkg/app.py",
			Line:     1,
			Column:   1,
			Severity: "error",
			PolicyID: "python.unused_imports",
			Message:  "unused import",
		}},
	}

	output, err := FormatLintResult(result, FormatSARIF)
	if err != nil {
		t.Fatalf("format SARIF: %v", err)
	}

	var payload map[string]any

	inlineErr6 := json.Unmarshal([]byte(output), &payload)
	if inlineErr6 != nil {
		t.Fatalf("decode SARIF: %v\n%s", inlineErr6, output)
	}

	assertJSONPath(t, payload, "runs.0.properties.sandbox.profile", "lint-offline")
	assertJSONPath(
		t,
		payload,
		"runs.0.properties.sandbox.timeout_seconds",
		float64(300),
	)
	assertJSONPath(t, payload, "runs.0.properties.sandbox.memory_mb", float64(2048))
	assertJSONPath(
		t,
		payload,
		"runs.0.properties.sandbox.seccomp_profile",
		"deny-privilege",
	)
	assertJSONPath(t, payload, "runs.0.properties.sandbox.network_isolated", true)
}

func TestFormatLintResultTOONDedupesDiagnostics(t *testing.T) {
	t.Parallel()

	result := lint.Result{
		Scope:  "tool:mypy",
		Status: "blocked",
		Diagnostics: []diagnostics.Diagnostic{
			{
				Tool:     "mypy",
				File:     "pkg/app.py",
				Line:     12,
				Column:   4,
				Code:     "union-attr",
				PolicyID: "python.optional_required_types",
				Message:  "Item None has no attribute run",
				Advice:   "Make the required contract explicit.",
			},
			{
				Tool:     "pyright",
				File:     "pkg/app.py",
				Line:     12,
				Column:   4,
				Code:     "reportOptionalMemberAccess",
				PolicyID: "python.optional_required_types",
				Message:  "Object of type None has no run",
			},
		},
	}

	output, err := FormatLintResult(result, FormatTOON)
	if err != nil {
		t.Fatalf("format lint result: %v", err)
	}

	for _, want := range []string{
		toonFindingsHeader,
		"also reported by pyright:reportOptionalMemberAccess",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("TOON output missing %q:\n%s", want, output)
		}
	}
}

func assertJSONPath(t *testing.T, payload map[string]any, path string, want any) {
	t.Helper()

	var current any = payload
	for segment := range strings.SplitSeq(path, ".") {
		switch value := current.(type) {
		case map[string]any:
			var ok bool

			current, ok = value[segment]
			if !ok {
				t.Fatalf("JSON path %q missing segment %q in %#v", path, segment, value)
			}
		case []any:
			index := int(segment[0] - '0')
			if len(segment) != 1 || index < 0 || index >= len(value) {
				t.Fatalf("JSON path %q invalid index %q in %#v", path, segment, value)
			}

			current = value[index]
		default:
			t.Fatalf("JSON path %q cannot descend into %#v", path, value)
		}
	}

	if current != want {
		t.Fatalf("JSON path %q = %#v, want %#v", path, current, want)
	}
}

func jsonPathSlice(t *testing.T, payload map[string]any, path string) []any {
	t.Helper()

	current := jsonPathValue(t, payload, path)

	values, ok := current.([]any)
	if !ok {
		t.Fatalf("JSON path %q = %#v, want array", path, current)
	}

	return values
}

func jsonPathValue(t *testing.T, payload map[string]any, path string) any {
	t.Helper()

	var current any = payload
	for segment := range strings.SplitSeq(path, ".") {
		switch value := current.(type) {
		case map[string]any:
			var ok bool

			current, ok = value[segment]
			if !ok {
				t.Fatalf("JSON path %q missing segment %q in %#v", path, segment, value)
			}
		case []any:
			index := int(segment[0] - '0')
			if len(segment) != 1 || index < 0 || index >= len(value) {
				t.Fatalf("JSON path %q invalid index %q in %#v", path, segment, value)
			}

			current = value[index]
		default:
			t.Fatalf("JSON path %q cannot descend into %#v", path, value)
		}
	}

	return current
}

func assertJSONPathPrefix(
	t *testing.T,
	payload map[string]any,
	path string,
) {
	t.Helper()

	var current any = payload
	for segment := range strings.SplitSeq(path, ".") {
		switch value := current.(type) {
		case map[string]any:
			var ok bool

			current, ok = value[segment]
			if !ok {
				t.Fatalf("JSON path %q missing segment %q in %#v", path, segment, value)
			}
		case []any:
			index := int(segment[0] - '0')
			if len(segment) != 1 || index < 0 || index >= len(value) {
				t.Fatalf("JSON path %q invalid index %q in %#v", path, segment, value)
			}

			current = value[index]
		default:
			t.Fatalf("JSON path %q cannot descend into %#v", path, value)
		}
	}

	text, ok := current.(string)
	if !ok || len(text) != sha256HexLength {
		t.Fatalf(
			"JSON path %q = %#v, want string length %d",
			path,
			current,
			sha256HexLength,
		)
	}
}

func TestFormatLintResultTOONIncludesSkillAdvice(t *testing.T) {
	t.Parallel()

	result := lint.Result{
		Scope:  "tool:ruff",
		Status: "blocked",
		Diagnostics: []diagnostics.Diagnostic{{
			Tool:     "ruff",
			File:     "pkg/app.py",
			Line:     12,
			Severity: "error",
			Code:     "PLC" + "0415",
			PolicyID: "python.conditional_imports",
			SkillID:  "conditional-imports",
			Message:  "import outside top-level",
		}},
		SkillHints: []lint.SkillHint{{
			PrincipleID: "no-conditional-imports",
			SkillID:     "conditional-imports",
			Message:     "Conditional imports are banned; use protocols.",
			Next:        "Load the conditional-imports skill for the remediation playbook.",
		}},
	}

	output, err := FormatLintResult(result, FormatTOON)
	if err != nil {
		t.Fatalf("format lint result: %v", err)
	}

	for _, want := range []string{
		"advice[1]{skill_id,message}:",
		"conditional-imports,Conditional imports are banned; use protocols.",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("TOON output missing %q:\n%s", want, output)
		}
	}
}

func TestFormatLintResultTOONKeepsLongSkillAdviceLineStable(t *testing.T) {
	t.Parallel()

	result := lint.Result{
		Scope:  "tool:ruff",
		Status: "blocked",
		Diagnostics: []diagnostics.Diagnostic{{
			Tool:     "ruff",
			File:     "pkg/app.py",
			Line:     12,
			Severity: "error",
			Code:     "S608",
			PolicyID: "python.sql_safety",
			SkillID:  "lint-remediation",
			Message:  "Possible SQL injection vector through string-based query construction",
		}},
		SkillHints: []lint.SkillHint{{
			PrincipleID: "static-analysis-is-the-first-line-of-defense",
			SkillID:     "lint-remediation",
			Message: "Linters are enforced code reviewers. Fix findings structurally, " +
				"keep managed config intact, and suppress only when technically necessary " +
				"and fully documented.",
			Next: "Load the lint-remediation skill for the remediation playbook.",
		}},
	}

	output, err := FormatLintResult(result, FormatTOON)
	if err != nil {
		t.Fatalf("format lint result: %v", err)
	}

	for _, want := range []string{
		"advice[1]{skill_id,message}:",
		"lint-remediation,Linters are enforced code reviewers.",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("TOON output missing %q:\n%s", want, output)
		}
	}

	for line := range strings.SplitSeq(output, "\n") {
		if strings.HasPrefix(line, "  lint-remediation,") && len(line) > 96 {
			t.Fatalf("TOON skill advice line too long: %d\n%s", len(line), line)
		}
	}

	if strings.Contains(output, "Load the lint-remediation skill") {
		t.Fatalf("TOON output should not inline verbose next-step text:\n%s", output)
	}
}

func TestFormatLintResultTOONPrefersBlockingDecisions(t *testing.T) {
	t.Parallel()

	result := lint.Result{
		Scope:  lint.ScopeStaged,
		Status: "blocked",
		Decisions: []policy.Decision{
			{
				Decision:   "record",
				Severity:   "record",
				PolicyID:   "shell.forbidden_strings",
				Message:    "Commands must not inspect protected internals.",
				Suggestion: "Use documented commands.",
			},
			{
				Decision:   "block",
				Severity:   "block",
				PolicyID:   "git.staged_admin_files",
				Message:    "Administrative staged files require explicit handling.",
				Suggestion: "Confirm the policy change is intentional.",
				Evidence: map[string]any{
					"files": []string{"coding-ethos"},
				},
			},
		},
	}

	output, err := FormatLintResult(result, FormatTOON)
	if err != nil {
		t.Fatalf("format lint result: %v", err)
	}

	for _, want := range []string{
		toonFindingsHeader,
		"policy,coding-ethos,0,0,block,,git.staged_admin_files,," +
			"Administrative staged files require explicit handling.," +
			"Confirm the policy change is intentional.,",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("TOON output missing %q:\n%s", want, output)
		}
	}

	if strings.Contains(output, "shell.forbidden_strings") {
		t.Fatalf("TOON output included non-blocking record finding:\n%s", output)
	}
}

func TestFormatLintResultTOONUsesCapturedFindings(t *testing.T) {
	t.Parallel()

	result := lint.Result{
		Scope:  "tool:mypy",
		Status: "blocked",
		Findings: []lint.Finding{{
			RawOutcome: map[string]any{
				"category":  "configuration_error",
				"exit_code": 2,
				"output":    "mypy: error: cannot read file 'lbox/parsing/analyzer_base.py'",
			},
			CheckID:    "tool.mypy",
			Message:    "mypy configuration or usage failed with status 2",
			Severity:   "error",
			SourceTool: "mypy",
			Status:     "fail",
			Blocking:   true,
		}},
	}

	output, err := FormatLintResult(result, FormatTOON)
	if err != nil {
		t.Fatalf("format lint result: %v", err)
	}

	for _, want := range []string{
		toonFindingsHeader,
		"mypy,,0,0,error,,tool.mypy,," +
			"mypy configuration or usage failed with status 2,," +
			"category=configuration_error; exit_code=2; output=mypy: error: " +
			"cannot read file 'lbox/parsing/analyzer_base.py'",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("TOON output missing %q:\n%s", want, output)
		}
	}
}

func TestFormatLintResultTOONPrefersWarningFindingsOverRecordDiagnostics(
	t *testing.T,
) {
	t.Parallel()

	result := lint.Result{
		Scope:  "tool:go-test",
		Status: "warning",
		Diagnostics: []diagnostics.Diagnostic{{
			Tool:     "go-test",
			Severity: "record",
			Code:     "coverage-package",
			Message:  "Go test coverage for pkg/noisy is 65.10%.",
		}},
		Findings: []lint.Finding{{
			Advice:     "Add meaningful tests before committing.",
			CheckID:    "testing.go_coverage_goal",
			Message:    "Go test coverage is below the 90% project goal.",
			PolicyID:   "testing.go_coverage_goal",
			Severity:   "warn",
			SkillID:    "lint-remediation",
			SourceTool: "go-test",
			Status:     "warn",
		}},
	}

	output, err := FormatLintResult(result, FormatTOON)
	if err != nil {
		t.Fatalf("format lint result: %v", err)
	}

	for _, want := range []string{
		"status: WARN",
		toonFindingsHeader,
		"go-test,,0,0,warn,,testing.go_coverage_goal,lint-remediation," +
			"Go test coverage is below the 90% project goal.," +
			"Add meaningful tests before committing.,",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("TOON output missing %q:\n%s", want, output)
		}
	}

	if strings.Contains(output, "coverage-package") ||
		strings.Contains(output, "pkg/noisy") {
		t.Fatalf("TOON output included record-only coverage noise:\n%s", output)
	}
}

func TestFormatLintResultTOONSuppressesRecordDiagnosticsWhenActionableExists(
	t *testing.T,
) {
	t.Parallel()

	result := lint.Result{
		Scope:  "tool:go-test",
		Status: "warning",
		Diagnostics: []diagnostics.Diagnostic{
			{
				Tool:     "go-test",
				Severity: "record",
				Code:     "coverage-package",
				Message:  "Go test coverage for pkg/noisy is 65.10%.",
			},
			{
				Tool:     "policy",
				Severity: "warn",
				PolicyID: "testing.go_coverage_goal",
				SkillID:  "lint-remediation",
				Message:  "Go test coverage is below the 90% project goal.",
				Advice:   "Add meaningful tests before committing.",
			},
		},
	}

	output, err := FormatLintResult(result, FormatTOON)
	if err != nil {
		t.Fatalf("format lint result: %v", err)
	}

	for _, want := range []string{
		"findings[1]{tool,file,line,column,severity,code," +
			"policy_id,skill_id,message,advice,detail}:",
		"policy,,0,0,warn,,testing.go_coverage_goal,lint-remediation," +
			"Go test coverage is below the 90% project goal.," +
			"Add meaningful tests before committing.,",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("TOON output missing %q:\n%s", want, output)
		}
	}

	if strings.Contains(output, "coverage-package") ||
		strings.Contains(output, "pkg/noisy") {
		t.Fatalf("TOON output included record-only coverage noise:\n%s", output)
	}
}

func TestFormatLintResultHumanIncludesCapturedFailureDetail(t *testing.T) {
	t.Parallel()

	result := lint.Result{
		Scope:  "tool:actionlint",
		Status: "blocked",
		Findings: []lint.Finding{{
			RawOutcome: map[string]any{
				"category":  "tool_error",
				"exit_code": 127,
				"output":    "fork/exec actionlint: permission denied",
			},
			CheckID:    "tool.actionlint",
			Message:    "actionlint exited with status 127 without parseable diagnostics",
			Severity:   "error",
			SourceTool: "actionlint",
			Status:     "fail",
			Blocking:   true,
		}},
	}

	output, err := FormatLintResult(result, FormatHuman)
	if err != nil {
		t.Fatalf("format lint result: %v", err)
	}

	for _, want := range []string{
		"actionlint exited with status 127 without parseable diagnostics",
		"detail: category=tool_error; exit_code=127; " +
			"output=fork/exec actionlint: permission denied",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("human output missing %q:\n%s", want, output)
		}
	}
}

func TestFormatLintResultTOONBlockedOutputQuality(t *testing.T) {
	t.Parallel()

	result := lint.Result{
		Scope:  "tool:pyright",
		Status: "blocked",
		Findings: []lint.Finding{{
			RawOutcome: map[string]any{
				"category":  "tool_config_error",
				"exit_code": 2,
				"output":    "pyright: config failure in <repo>/pyrightconfig.json",
			},
			CheckID:    "tool.pyright",
			Message:    "pyright configuration or usage failed with status 2",
			Severity:   "error",
			SourceTool: "pyright",
			Status:     "fail",
			Blocking:   true,
		}},
	}

	output, err := FormatLintResult(result, FormatTOON)
	if err != nil {
		t.Fatalf("format lint result: %v", err)
	}

	for _, forbidden := range []string{
		"findings[0]",
		"/home/",
		"duration_ms",
		"groups[",
		"commands[",
	} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("TOON output contains forbidden %q:\n%s", forbidden, output)
		}
	}

	for _, want := range []string{
		toonFindingsHeader,
		"tool.pyright",
		"pyright configuration or usage failed with status 2",
		"output=pyright: config failure in <repo>/pyrightconfig.json",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("TOON output missing %q:\n%s", want, output)
		}
	}
}

func TestFormatLintResultTOONTruncatesPathologicalFindingCells(t *testing.T) {
	t.Parallel()

	longMessage := strings.Repeat("schema-value,", 80)
	result := lint.Result{
		Scope:  "tool:tombi",
		Status: "blocked",
		Diagnostics: []diagnostics.Diagnostic{{
			Tool:     "tombi",
			File:     "pyproject.toml",
			Line:     104,
			Column:   5,
			Severity: "error",
			Message:  longMessage,
		}},
	}

	output, err := FormatLintResult(result, FormatTOON)
	if err != nil {
		t.Fatalf("format lint result: %v", err)
	}

	if !strings.Contains(output, "...[truncated]") {
		t.Fatalf("TOON output did not mark truncation:\n%s", output)
	}

	if strings.Contains(output, longMessage) {
		t.Fatalf("TOON output included full pathological message:\n%s", output)
	}

	if len(output) > 900 {
		t.Fatalf(
			"TOON output too large after truncation: %d bytes\n%s",
			len(output),
			output,
		)
	}
}

func TestFormatLintResultCELBackedGoldenSurfaces(t *testing.T) {
	t.Parallel()

	for _, testCase := range celBackedGoldenCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assertCELBackedGoldenFormats(t, testCase.result, testCase.want)
		})
	}
}

type celBackedGoldenCase struct {
	name   string
	want   string
	result lint.Result
}

func celBackedGoldenCases() []celBackedGoldenCase {
	return []celBackedGoldenCase{
		celCommandGoldenCase(),
		celFileGoldenCase(),
		celDiagnosticGoldenCase(),
		celFindingGoldenCase(),
	}
}

func celCommandGoldenCase() celBackedGoldenCase {
	return celBackedGoldenCase{
		name: "command policy",
		result: lint.Result{
			Scope:      "files",
			Status:     "blocked",
			SkillHints: celSkillHints(),
			Decisions: []policy.Decision{celDecision(
				"custom.no_subprocess_git",
				"",
				"Git subprocesses are forbidden.",
			)},
		},
		want: "custom.no_subprocess_git",
	}
}

func celFileGoldenCase() celBackedGoldenCase {
	return celBackedGoldenCase{
		name: "file policy",
		result: lint.Result{
			Scope:      "files",
			Status:     "blocked",
			SkillHints: celSkillHints(),
			Decisions: []policy.Decision{celDecision(
				"custom.generated_python",
				"generated/model.py",
				"Generated Python must not be edited directly.",
			)},
		},
		want: "generated/model.py",
	}
}

func celDiagnosticGoldenCase() celBackedGoldenCase {
	return celBackedGoldenCase{
		name: "diagnostic policy",
		result: lint.Result{
			Scope:      "tool:ruff",
			Status:     "blocked",
			SkillHints: celSkillHints(),
			Diagnostics: []diagnostics.Diagnostic{{
				Tool:     "policy",
				File:     "pkg/app.py",
				Line:     10,
				Severity: "block",
				PolicyID: "custom.ruff_policy",
				SkillID:  "lint-remediation",
				Message:  "Ruff diagnostic maps to ETHOS policy.",
				Advice:   "Fix the diagnostic structurally.",
			}},
		},
		want: "custom.ruff_policy",
	}
}

func celFindingGoldenCase() celBackedGoldenCase {
	return celBackedGoldenCase{
		name: "lint finding policy",
		result: lint.Result{
			Scope:      "tool:mypy",
			Status:     "blocked",
			SkillHints: celSkillHints(),
			Findings: []lint.Finding{{
				CheckID:    "custom.mypy_policy",
				PolicyID:   "custom.mypy_policy",
				SkillID:    "lint-remediation",
				Message:    "Mypy finding maps to ETHOS policy.",
				Severity:   "error",
				SourceTool: "mypy",
				Status:     "fail",
				Blocking:   true,
			}},
		},
		want: "custom.mypy_policy",
	}
}

func assertCELBackedGoldenFormats(
	t *testing.T,
	result lint.Result,
	want string,
) {
	t.Helper()

	for _, format := range []string{FormatTOON, FormatJSON, FormatHuman} {
		output, err := FormatLintResult(result, format)
		if err != nil {
			t.Fatalf("format %s lint result: %v", format, err)
		}

		assertCELBackedGoldenOutput(t, format, output, want)
	}
}

func assertCELBackedGoldenOutput(
	t *testing.T,
	format string,
	output string,
	want string,
) {
	t.Helper()

	for _, expected := range []string{want, "lint-remediation"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("%s output missing %q:\n%s", format, expected, output)
		}
	}

	if format != FormatJSON &&
		!strings.Contains(output, "Fix the reported diagnostics before continuing.") {
		t.Fatalf("%s output missing guidance:\n%s", format, output)
	}
}

func TestSelectedFormatAutoDetectsAgent(t *testing.T) {
	t.Parallel()

	getenv := func(name string) string {
		switch name {
		case FormatEnv:
			return FormatAuto
		case "CODEX_THREAD_ID":
			return "thread"
		default:
			return ""
		}
	}

	if got := SelectedFormatWithEnv(getenv); got != FormatTOON {
		t.Fatalf("SelectedFormatWithEnv() = %q, want toon", got)
	}
}

func TestTOONCellEscapesCommasAndNewlines(t *testing.T) {
	t.Parallel()

	got := TOONCell("a,b\nc")
	if got != `a\,b\nc` {
		t.Fatalf("TOONCell() = %q", got)
	}
}

func celDecision(policyID, file, message string) policy.Decision {
	decision := policy.Decision{
		Decision:   "block",
		Severity:   "block",
		PolicyID:   policyID,
		Message:    message,
		Suggestion: "Load the lint remediation skill.",
		Diagnostics: []diagnostics.Diagnostic{{
			Tool:     "policy",
			File:     file,
			Severity: "block",
			PolicyID: policyID,
			SkillID:  "lint-remediation",
			Message:  message,
			Advice:   "Load the lint remediation skill.",
		}},
	}

	return decision
}

func celSkillHints() []lint.SkillHint {
	return []lint.SkillHint{{
		PrincipleID: "linting-as-code-quality-enforcement",
		SkillID:     "lint-remediation",
		Message:     "Resolve CEL-backed lint findings structurally.",
		Next:        "Load the lint-remediation skill for the remediation playbook.",
	}}
}
