// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package e2e_test

import (
	"strings"
	"testing"
)

func TestSandboxedManagedRuffCaptureRecordsTraceEvidence(t *testing.T) {
	repo := preparedManagedLintRepo(t)

	result := repo.CodingEthosRunWithEnv(
		t,
		sandboxWorkflowEnv(),
		"policy-lint",
		"--json",
		"--managed-capture-tool",
		"ruff",
		"--ethos-root",
		repo.EthosRoot,
		"--consumer-root",
		repo.Root,
		"--invocation-cwd",
		repo.Root,
		"--sandbox-mode",
		"auto",
		"--",
		"check",
		"pkg/clean.py",
	)
	result.RequireExit(t, 0)

	if strings.TrimSpace(result.Combined) != "" {
		t.Fatalf("clean sandboxed lint should stay silent:\n%s", result.Combined)
	}

	trace := repo.SingleTrace(t)
	assertSandboxTraceEvidence(t, trace, "auto")
	for _, want := range []string{
		`"scope": "tool:ruff"`,
		`"parse_status": "empty"`,
		`"exit_code": 0`,
		`"--output-format=json"`,
		`"backend_path": "/usr/bin/bwrap"`,
	} {
		if !strings.Contains(trace, want) {
			t.Fatalf("sandbox trace missing %q:\n%s", want, trace)
		}
	}
}

func TestSandboxedManagedRuffCaptureProducesSARIFEvidence(t *testing.T) {
	repo := preparedManagedLintRepo(t)
	repo.Touch(t, "pkg/unused_import.py", unusedImportPython())

	result := repo.CodingEthosRunWithEnv(
		t,
		sandboxWorkflowEnv(),
		"policy-lint",
		"--sarif",
		"--managed-capture-tool",
		"ruff",
		"--ethos-root",
		repo.EthosRoot,
		"--consumer-root",
		repo.Root,
		"--invocation-cwd",
		repo.Root,
		"--sandbox-mode",
		"auto",
		"--",
		"check",
		"pkg/unused_import.py",
	)
	result.RequireExit(t, 1)

	for _, want := range []string{
		`"$schema": "https://json.schemastore.org/sarif-2.1.0.json"`,
		`"ruleId": "ruff:F401"`,
		`"ruleId": "tool.ruff"`,
		`"uri": "pkg/unused_import.py"`,
		`"source_tool": "ruff"`,
		`"code": "F401"`,
		`"sandbox": {`,
		`"backend": "bubblewrap"`,
		`"profile": "lint-offline"`,
		`"network_isolated": true`,
	} {
		result.RequireContains(t, want)
	}
}

func TestRequiredSandboxModeRecordsEnforcementOrStructuredDenial(t *testing.T) {
	repo := preparedManagedLintRepo(t)

	result := repo.CodingEthosRunWithEnv(
		t,
		sandboxWorkflowEnv(),
		"policy-lint",
		"--json",
		"--managed-capture-tool",
		"ruff",
		"--ethos-root",
		repo.EthosRoot,
		"--consumer-root",
		repo.Root,
		"--invocation-cwd",
		repo.Root,
		"--sandbox-mode",
		"required",
		"--",
		"check",
		"pkg/clean.py",
	)

	switch result.Code {
	case 0:
		trace := repo.SingleTrace(t)
		assertSandboxTraceEvidence(t, trace, "required")
		if strings.Contains(trace, `"denied": true`) {
			t.Fatalf("successful required sandbox trace recorded denial:\n%s", trace)
		}
	case 2:
		for _, want := range []string{
			`"policy_id": "runtime.sandbox_denial"`,
			`"tool": "coding-ethos-sandbox"`,
			`"code": "SANDBOX_DENIED"`,
			`"denied": true`,
			`"mode": "required"`,
		} {
			result.RequireContains(t, want)
		}

		trace := repo.SingleTrace(t)
		for _, want := range []string{
			`"policy_id": "runtime.sandbox_denial"`,
			`"tool": "coding-ethos-sandbox"`,
			`"denied": true`,
			`"mode": "required"`,
		} {
			if !strings.Contains(trace, want) {
				t.Fatalf("required sandbox denial trace missing %q:\n%s", want, trace)
			}
		}
	default:
		t.Fatalf(
			"exit code = %d, want sandbox success or structured denial\n%s",
			result.Code,
			result.Combined,
		)
	}
}

func sandboxWorkflowEnv() map[string]string {
	return map[string]string{"CODE_ETHOS_HOOK_LOGGING_ACTIVE": "1"}
}

func assertSandboxTraceEvidence(t *testing.T, trace, mode string) {
	t.Helper()

	for _, want := range []string{
		`"sandbox": {`,
		`"backend": "bubblewrap"`,
		`"profile": "lint-offline"`,
		`"tool": "ruff"`,
		`"mode": "` + mode + `"`,
		`"enabled": true`,
		`"git_read_only": true`,
		`"read_only_root": true`,
		`"network_isolated": true`,
		`"process_isolated": true`,
		`"hidden_credential_dirs": [`,
		`".coding-ethos/cache"`,
		`".ruff_cache/"`,
		`"no-network"`,
		`"no-git"`,
	} {
		if !strings.Contains(trace, want) {
			t.Fatalf("sandbox trace missing %q:\n%s", want, trace)
		}
	}
}
