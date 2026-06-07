// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package e2e_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/e2e"
	"blackcat.ca/coding-ethos/go/internal/sandbox"
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
		"--",
		"check",
		"pkg/clean.py",
	)
	if requiredModeSandboxDenied(t, result) {
		trace := repo.SingleTrace(t)
		assertRequiredModeSandboxDenialTrace(t, trace)

		return
	}

	result.RequireExit(t, 0)
	if strings.TrimSpace(result.Combined) != "" {
		t.Fatalf(
			"clean required-mode sandboxed lint should stay silent:\n%s",
			result.Combined,
		)
	}
	trace := repo.SingleTrace(t)
	assertSandboxTraceEvidence(t, trace, sandbox.ModeRequired)
	for _, want := range []string{
		`"scope": "tool:ruff"`,
		`"parse_status": "empty"`,
		`"exit_code": 0`,
		`"--output-format=json"`,
		`"backend_path": "`,
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
		"--",
		"check",
		"pkg/unused_import.py",
	)
	if requiredModeSandboxDenied(t, result) {
		assertRequiredModeSandboxDenialSARIF(t, result)

		return
	}

	result.RequireExit(t, 1)
	for _, want := range []string{
		`"$schema": "https://json.schemastore.org/sarif-2.1.0.json"`,
		`"ruleId": "ruff:F401"`,
		`"ruleId": "tool.ruff"`,
		`"uri": "pkg/unused_import.py"`,
		`"source_tool": "ruff"`,
		`"code": "F401"`,
		`"sandbox": {`,
		`"backend": "native"`,
		`"profile": "lint-offline"`,
		`"network_isolated": true`,
	} {
		result.RequireContains(t, want)
	}
}

func requiredModeSandboxDenied(t *testing.T, result e2e.CommandResult) bool {
	t.Helper()

	if result.Code != 2 {
		return false
	}

	for _, want := range []string{
		`"mode": "required"`,
		`"denied": true`,
		`Managed tool sandbox execution was denied.`,
	} {
		result.RequireContains(t, want)
	}

	return true
}

func assertRequiredModeSandboxDenialTrace(t *testing.T, trace string) {
	t.Helper()

	for _, want := range []string{
		`"policy_id": "runtime.sandbox_denial"`,
		`"tool": "coding-ethos-sandbox"`,
		`"mode": "required"`,
		`"cgroup_requested": true`,
		`"denied": true`,
	} {
		if !strings.Contains(trace, want) {
			t.Fatalf("required-mode sandbox denial trace missing %q:\n%s", want, trace)
		}
	}
}

func assertRequiredModeSandboxDenialSARIF(
	t *testing.T,
	result e2e.CommandResult,
) {
	t.Helper()

	for _, want := range []string{
		`"$schema": "https://json.schemastore.org/sarif-2.1.0.json"`,
		`"sandbox": {`,
		`"mode": "required"`,
		`"cgroup_requested": true`,
		`"denied": true`,
		`"policies": [`,
		`"runtime.sandbox_denial"`,
		`"diagnostic_count": 1`,
	} {
		result.RequireContains(t, want)
	}
}

func TestSandboxedManagedRuffCaptureRequiresNativeWrapper(t *testing.T) {
	repo := preparedManagedLintRepo(t)
	missingWrapper := filepath.Join(repo.EthosRoot, "bin", "coding-ethos-sandbox")
	err := os.Rename(missingWrapper, missingWrapper+".disabled")
	if err != nil {
		t.Fatalf("disable sandbox wrapper fixture: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Rename(missingWrapper+".disabled", missingWrapper)
	})

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
		"--",
		"check",
		"pkg/clean.py",
	)
	result.RequireExit(t, 2)

	for _, want := range []string{
		`"policy_id": "runtime.sandbox_denial"`,
		`"tool": "coding-ethos-sandbox"`,
		`"code": "SANDBOX_DENIED"`,
		`"denied": true`,
		`"mode": "required"`,
		`"reason": "sandbox wrapper is required`,
		`no such file or directory`,
	} {
		result.RequireContains(t, want)
	}

	trace := repo.SingleTrace(t)
	for _, want := range []string{
		`"policy_id": "runtime.sandbox_denial"`,
		`"tool": "coding-ethos-sandbox"`,
		`"denied": true`,
		`"mode": "required"`,
		`"reason": "sandbox wrapper is required`,
		`no such file or directory`,
	} {
		if !strings.Contains(trace, want) {
			t.Fatalf("missing native wrapper trace missing %q:\n%s", want, trace)
		}
	}
}

func TestSandboxWriteScopeAllowsDeclaredPathAndBlocksRepoWrite(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux namespace sandboxing is required for write-scope e2e")
	}

	managedRepo := preparedManagedLintRepo(t)
	repo := managedRepo.Root
	mustMkdirE2E(t, filepath.Join(repo, ".coding-ethos", "cache"))
	wrapper := filepath.Join(managedRepo.EthosRoot, "bin", "coding-ethos-sandbox")

	plan, err := sandbox.BuildPlan(sandbox.Request{
		Tool:        "write-scope",
		Executable:  "/bin/sh",
		WrapperPath: wrapper,
		Cwd:         repo,
		RepoRoot:    repo,
		Args:        []string{"-c", sandboxWriteScopeScript()},
		Capabilities: sandbox.Capabilities{
			SandboxProfile:  "agent-network",
			WritePaths:      []string{".coding-ethos/cache"},
			RequiresNetwork: true,
		},
	})
	if err != nil {
		t.Fatalf("build write-scope sandbox plan: %v", err)
	}
	defer func() { _ = plan.Close() }()

	command := exec.Command(plan.Executable, plan.Args...)
	command.Dir = repo
	command.SysProcAttr = sandbox.SysProcAttr(nil, plan.Evidence)

	output, err := command.CombinedOutput()
	if err != nil {
		text := strings.ToLower(strings.TrimSpace(string(output) + "\n" + err.Error()))
		if strings.Contains(text, "operation not permitted") ||
			strings.Contains(text, "permission denied") {
			assertSandboxWriteDidNotEscape(t, repo)

			return
		}

		t.Fatalf("write-scope sandbox command failed: %v\n%s", err, output)
	}

	allowed := filepath.Join(repo, ".coding-ethos", "cache", "allowed.txt")
	content, err := os.ReadFile(allowed)
	if err != nil {
		t.Fatalf("declared sandbox write was not persisted: %v", err)
	}
	if strings.TrimSpace(string(content)) != "allowed" {
		t.Fatalf("declared sandbox write content = %q", content)
	}

	blocked := filepath.Join(repo, "blocked.txt")
	if _, err := os.Stat(blocked); err == nil {
		t.Fatalf("undeclared sandbox write escaped to repo: %s", blocked)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat undeclared sandbox write: %v", err)
	}
}

func assertSandboxWriteDidNotEscape(t *testing.T, repo string) {
	t.Helper()

	for _, path := range []string{
		filepath.Join(repo, ".coding-ethos", "cache", "allowed.txt"),
		filepath.Join(repo, "blocked.txt"),
	} {
		_, err := os.Stat(path)
		if err == nil {
			t.Fatalf("sandbox failure left host write behind: %s", path)
		}
		if !os.IsNotExist(err) {
			t.Fatalf("stat sandbox write path %s: %v", path, err)
		}
	}
}

func sandboxWorkflowEnv() map[string]string {
	return sandboxWorkflowEnvWith(nil)
}

func sandboxWorkflowEnvWith(overrides map[string]string) map[string]string {
	env := map[string]string{"CODE_ETHOS_HOOK_LOGGING_ACTIVE": "1"}
	for key, value := range overrides {
		env[key] = value
	}

	return env
}

func sandboxWriteScopeScript() string {
	return strings.Join([]string{
		"set -eu",
		"echo allowed > .coding-ethos/cache/allowed.txt",
		"if /bin/sh -c 'echo denied > blocked.txt'; then exit 17; fi",
		"test ! -e blocked.txt",
	}, "; ")
}

func mustMkdirE2E(t *testing.T, path string) {
	t.Helper()

	err := os.MkdirAll(path, 0o700)
	if err != nil {
		t.Fatalf("create directory %s: %v", path, err)
	}
}

func assertSandboxTraceEvidence(t *testing.T, trace, mode string) {
	t.Helper()

	for _, want := range []string{
		`"sandbox": {`,
		`"backend": "native"`,
		`"profile": "lint-offline"`,
		`"tool": "ruff"`,
		`"mode": "` + mode + `"`,
		`"enabled": true`,
		`"git_read_only": true`,
		`"repo_read_only": true`,
		`"network_isolated": true`,
		`"process_isolated": true`,
		`".coding-ethos/cache/"`,
		`".ruff_cache/"`,
		`"no-network"`,
		`"no-git"`,
	} {
		if !strings.Contains(trace, want) {
			t.Fatalf("sandbox trace missing %q:\n%s", want, trace)
		}
	}
}
