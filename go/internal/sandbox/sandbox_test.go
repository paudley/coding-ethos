// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package sandbox_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"blackcat.ca/coding-ethos/go/internal/sandbox"
)

const (
	privateDirMode = 0o700
	toolRuffPath   = "/tools/ruff"
)

func TestBuildPlanWithoutProfileReturnsOriginalCommand(t *testing.T) {
	t.Parallel()

	plan, err := sandbox.BuildPlan(sandbox.Request{
		Tool:       "ruff",
		Executable: toolRuffPath,
		Args:       []string{"check", "pkg"},
	})
	if err != nil {
		t.Fatalf("sandbox.BuildPlan() error = %v", err)
	}

	if plan.Executable != toolRuffPath {
		t.Fatalf("executable = %q", plan.Executable)
	}
	if !slices.Equal(plan.Args, []string{"check", "pkg"}) {
		t.Fatalf("args = %#v", plan.Args)
	}
	if plan.Evidence.Enabled {
		t.Fatalf("sandbox should be disabled: %#v", plan.Evidence)
	}
}

func TestBuildPlanRequiredUsesNativeWrapper(t *testing.T) {
	requireLinuxSandbox(t)

	t.Setenv("CODING_ETHOS_SANDBOX_ACTIVE", "0")

	repo := t.TempDir()
	wrapper := filepath.Join(repo, "bin", "coding-ethos-sandbox")
	writeExecutable(t, wrapper)

	plan, err := sandbox.BuildPlan(sandbox.Request{
		Tool:        "ruff",
		Executable:  toolRuffPath,
		WrapperPath: wrapper,
		Cwd:         repo,
		RepoRoot:    repo,
		Args:        []string{"check", "pkg"},
		Capabilities: sandbox.Capabilities{
			SandboxProfile: "lint-offline",
			ReadPaths:      []string{"pkg"},
			WritePaths:     []string{".coding-ethos/cache", ".git/config"},
		},
	})
	if err != nil {
		t.Fatalf("sandbox.BuildPlan() error = %v", err)
	}

	if plan.Executable != wrapper || plan.Evidence.BackendPath != wrapper {
		t.Fatalf("plan did not route through wrapper %q: %#v", wrapper, plan)
	}
	for _, want := range []string{
		"--cwd", repo,
		"--repo-root", repo,
		"--write-path", ".coding-ethos/cache",
		"--", toolRuffPath,
		"check", "pkg",
	} {
		if !slices.Contains(plan.Args, want) {
			t.Fatalf("args missing %q: %#v", want, plan.Args)
		}
	}
	for _, want := range []string{"pkg", ".coding-ethos/cache", sandbox.SandboxTempWritePath} {
		if !slices.Contains(plan.Evidence.ReadPaths, want) {
			t.Fatalf("read paths missing %q: %#v", want, plan.Evidence.ReadPaths)
		}
	}
	if slices.Contains(plan.Args, ".git/config") {
		t.Fatalf(".git write path must not be passed to sandbox helper: %#v", plan.Args)
	}
	if slices.Contains(plan.Args, "--requires-network") {
		t.Fatalf("network-isolated tool must not request ssh rebuild: %#v", plan.Args)
	}
	assertNativeEvidence(t, plan.Evidence)
}

func TestBuildPlanPassesGitBindMountsForGitSandbox(t *testing.T) {
	requireLinuxSandbox(t)

	repo := t.TempDir()
	wrapper := filepath.Join(repo, "bin", "coding-ethos-sandbox")
	writeExecutable(t, wrapper)
	gitWrapper := filepath.Join(repo, "bin", "git")
	writeExecutable(t, gitWrapper)
	realGitBind := filepath.Join(repo, "real-git")
	writeExecutable(t, realGitBind)

	plan, err := sandbox.BuildPlan(sandbox.Request{
		Tool:        "agent-shell",
		Executable:  "/usr/bin/env",
		WrapperPath: wrapper,
		Cwd:         repo,
		RepoRoot:    repo,
		Args:        []string{"bash", "-lc", "git status"},
		Capabilities: sandbox.Capabilities{
			SandboxProfile:  "agent-shell",
			GitWrapperPath:  gitWrapper,
			RealGitPath:     "/usr/bin/git",
			RealGitBindPath: realGitBind,
			GitTargetPaths:  []string{"/usr/bin/git", "/usr/bin/git"},
			RequiresGit:     true,
		},
	})
	if err != nil {
		t.Fatalf("sandbox.BuildPlan() error = %v", err)
	}

	for _, want := range []string{
		"--git-wrapper", gitWrapper,
		"--real-git-path", "/usr/bin/git",
		"--real-git-bind", realGitBind,
		"--git-target", "/usr/bin/git",
	} {
		if !slices.Contains(plan.Args, want) {
			t.Fatalf("args missing %q: %#v", want, plan.Args)
		}
	}

	if count := countValues(plan.Args, "/usr/bin/git"); count != 2 {
		t.Fatalf("deduped git target count = %d in %#v", count, plan.Args)
	}
}

func TestBuildPlanAllowsExplicitGitWritesForManagedGitSandbox(t *testing.T) {
	requireLinuxSandbox(t)

	repo := t.TempDir()
	wrapper := filepath.Join(repo, "bin", "coding-ethos-sandbox")
	writeExecutable(t, wrapper)
	gitWrapper := filepath.Join(repo, "bin", "git")
	writeExecutable(t, gitWrapper)
	realGitBind := filepath.Join(repo, "real-git")
	writeExecutable(t, realGitBind)

	plan, err := sandbox.BuildPlan(sandbox.Request{
		Tool:        "agent-shell",
		Executable:  "/usr/bin/env",
		WrapperPath: wrapper,
		Cwd:         repo,
		RepoRoot:    repo,
		Args:        []string{"bash", "-lc", "git add file.txt"},
		Capabilities: sandbox.Capabilities{
			SandboxProfile:  "agent-shell",
			GitWrapperPath:  gitWrapper,
			RealGitPath:     "/usr/bin/git",
			RealGitBindPath: realGitBind,
			GitTargetPaths:  []string{"/usr/bin/git"},
			WritePaths:      []string{filepath.Join(repo, ".git")},
			AllowGitWrites:  true,
			RequiresGit:     true,
		},
	})
	if err != nil {
		t.Fatalf("sandbox.BuildPlan() error = %v", err)
	}

	if !slices.Contains(plan.Args, filepath.Join(repo, ".git")) {
		t.Fatalf("git metadata write path missing: %#v", plan.Args)
	}
	if plan.Evidence.GitReadOnly {
		t.Fatalf("managed git write sandbox reported read-only git: %#v", plan.Evidence)
	}
}

func TestBuildPlanAllowsGitPolicyWithoutNativeGitBinding(t *testing.T) {
	requireLinuxSandbox(t)

	repo := t.TempDir()
	wrapper := filepath.Join(repo, "bin", "coding-ethos-sandbox")
	writeExecutable(t, wrapper)

	plan, err := sandbox.BuildPlan(sandbox.Request{
		Tool:        "agent-shell",
		Executable:  "/usr/bin/env",
		WrapperPath: wrapper,
		Cwd:         repo,
		RepoRoot:    repo,
		Args:        []string{"bash", "-lc", "git status"},
		Capabilities: sandbox.Capabilities{
			SandboxProfile: "agent-shell",
			WritePaths:     []string{filepath.Join(repo, ".git")},
			AllowGitWrites: true,
			RequiresGit:    true,
		},
	})
	if err != nil {
		t.Fatalf("sandbox.BuildPlan() error = %v", err)
	}

	for _, unwanted := range []string{"--git-wrapper", "--real-git-path", "--git-target"} {
		if slices.Contains(plan.Args, unwanted) {
			t.Fatalf("git bind flag %q should not be present: %#v", unwanted, plan.Args)
		}
	}
	if !plan.Evidence.RequiresGit || plan.Evidence.GitReadOnly {
		t.Fatalf("git policy evidence not preserved: %#v", plan.Evidence)
	}
	if !slices.Contains(plan.Args, "--allow-git-writes") {
		t.Fatalf("git write policy flag missing: %#v", plan.Args)
	}
}

func TestBuildPlanIgnoresSpoofedActiveSandboxMarker(
	t *testing.T,
) {
	requireLinuxSandbox(t)

	t.Setenv("CODING_ETHOS_SANDBOX_ACTIVE", "1")

	repo := t.TempDir()
	wrapper := filepath.Join(repo, "bin", "coding-ethos-sandbox")
	writeExecutable(t, wrapper)

	plan, err := sandbox.BuildPlan(sandbox.Request{
		Tool:        "ruff",
		Executable:  toolRuffPath,
		WrapperPath: wrapper,
		Cwd:         repo,
		RepoRoot:    repo,
		Args:        []string{"check", "pkg"},
		Capabilities: sandbox.Capabilities{
			SandboxProfile: "lint-offline",
			TimeoutSeconds: 300,
			MemoryMB:       2048,
		},
	})
	if err != nil {
		t.Fatalf("sandbox.BuildPlan() error = %v", err)
	}
	if plan.Executable != wrapper || plan.Evidence.BackendPath != wrapper {
		t.Fatalf("spoofed active marker bypassed wrapper %q: %#v", wrapper, plan)
	}
	if !plan.Evidence.Enabled || !plan.Evidence.TimeoutEnforced {
		t.Fatalf("spoofed active marker disabled sandbox evidence: %#v", plan.Evidence)
	}
}

func TestBuildPlanRejectsForgeableAgentShellSandboxMarker(t *testing.T) {
	requireLinuxSandbox(t)

	repo := t.TempDir()
	wrapper := filepath.Join(repo, "bin", "coding-ethos-sandbox")
	writeExecutable(t, wrapper)
	realGitBind := filepath.Join(
		repo,
		".coding-ethos",
		"cache",
		"agent-shell",
		"run-1",
		"real-git",
	)
	if err := os.MkdirAll(filepath.Dir(realGitBind), 0o700); err != nil {
		t.Fatalf("create real git bind dir: %v", err)
	}
	writeExecutable(t, realGitBind)
	t.Setenv("CODING_ETHOS_AGENT_SHELL_SANDBOX", "1")
	t.Setenv("CODING_ETHOS_REAL_GIT", realGitBind)

	plan, err := sandbox.BuildPlan(sandbox.Request{
		Tool:        "golangci-lint-format",
		Executable:  "/usr/bin/env",
		WrapperPath: wrapper,
		Cwd:         repo,
		RepoRoot:    repo,
		Args:        []string{"true"},
		Capabilities: sandbox.Capabilities{
			SandboxProfile: "lint-offline",
			WritePaths:     []string{".coding-ethos/cache"},
		},
	})
	if err != nil {
		t.Fatalf("sandbox.BuildPlan() error = %v", err)
	}
	if plan.Executable != wrapper || plan.Evidence.BackendPath != wrapper {
		t.Fatalf("forgeable agent-shell marker bypassed wrapper %q: %#v", wrapper, plan)
	}
	if !plan.Evidence.Enabled || !plan.Evidence.NamespaceEnforced {
		t.Fatalf("forgeable agent-shell marker disabled sandbox evidence: %#v", plan.Evidence)
	}
}

func TestBuildPlanRejectsMissingAgentShellRealGitBind(t *testing.T) {
	requireLinuxSandbox(t)

	repo := t.TempDir()
	wrapper := filepath.Join(repo, "bin", "coding-ethos-sandbox")
	writeExecutable(t, wrapper)
	realGitBind := filepath.Join(
		repo,
		".coding-ethos",
		"cache",
		"agent-shell",
		"run-1",
		"real-git",
	)
	t.Setenv("CODING_ETHOS_AGENT_SHELL_SANDBOX", "1")
	t.Setenv("CODING_ETHOS_REAL_GIT", realGitBind)

	plan, err := sandbox.BuildPlan(sandbox.Request{
		Tool:        "golangci-lint-format",
		Executable:  "/usr/bin/env",
		WrapperPath: wrapper,
		Cwd:         repo,
		RepoRoot:    repo,
		Args:        []string{"true"},
		Capabilities: sandbox.Capabilities{
			SandboxProfile: "lint-offline",
			WritePaths:     []string{".coding-ethos/cache"},
		},
	})
	if err != nil {
		t.Fatalf("sandbox.BuildPlan() error = %v", err)
	}
	if plan.Executable != wrapper ||
		plan.Evidence.Reason == "reusing active agent-shell sandbox" {
		t.Fatalf("missing real-git bind reused sandbox: %#v", plan)
	}
}

func TestBuildPlanRejectsAgentShellRealGitBindOutsideRepo(t *testing.T) {
	requireLinuxSandbox(t)

	repo := t.TempDir()
	otherRepo := t.TempDir()
	wrapper := filepath.Join(repo, "bin", "coding-ethos-sandbox")
	writeExecutable(t, wrapper)
	realGitBind := filepath.Join(
		otherRepo,
		".coding-ethos",
		"cache",
		"agent-shell",
		"run-1",
		"real-git",
	)
	if err := os.MkdirAll(filepath.Dir(realGitBind), 0o700); err != nil {
		t.Fatalf("create real git bind dir: %v", err)
	}
	writeExecutable(t, realGitBind)
	t.Setenv("CODING_ETHOS_AGENT_SHELL_SANDBOX", "1")
	t.Setenv("CODING_ETHOS_REAL_GIT", realGitBind)

	plan, err := sandbox.BuildPlan(sandbox.Request{
		Tool:        "golangci-lint-format",
		Executable:  "/usr/bin/env",
		WrapperPath: wrapper,
		Cwd:         repo,
		RepoRoot:    repo,
		Args:        []string{"true"},
		Capabilities: sandbox.Capabilities{
			SandboxProfile: "lint-offline",
			WritePaths:     []string{".coding-ethos/cache"},
		},
	})
	if err != nil {
		t.Fatalf("sandbox.BuildPlan() error = %v", err)
	}
	if plan.Executable != wrapper ||
		plan.Evidence.Reason == "reusing active agent-shell sandbox" {
		t.Fatalf("outside real-git bind reused sandbox: %#v", plan)
	}
}

func TestBuildPlanReusesVerifiedActiveAgentShellSandbox(t *testing.T) {
	requireLinuxSandbox(t)

	root := t.TempDir()
	repo := filepath.Join(root, sandbox.SandboxTempWritePath, "consumer")
	wrapper := filepath.Join(root, "bin", "coding-ethos-sandbox")
	realGitBind := filepath.Join(
		root,
		".coding-ethos",
		"cache",
		"agent-shell",
		"run-1",
		"real-git",
	)
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatalf("create consumer repo: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(realGitBind), 0o700); err != nil {
		t.Fatalf("create real git bind dir: %v", err)
	}
	writeExecutable(t, wrapper)
	writeExecutable(t, realGitBind)

	t.Setenv("CODING_ETHOS_AGENT_SHELL_SANDBOX", "1")
	t.Setenv("CODING_ETHOS_SANDBOX_ACTIVE", "1")
	t.Setenv("CODING_ETHOS_SANDBOX_ROOT", root)
	t.Setenv("CODING_ETHOS_REAL_GIT", realGitBind)

	plan, err := sandbox.BuildPlan(sandbox.Request{
		Tool:        "ruff",
		Executable:  "/usr/bin/env",
		WrapperPath: wrapper,
		Cwd:         repo,
		RepoRoot:    repo,
		Args:        []string{"true"},
		Capabilities: sandbox.Capabilities{
			SandboxProfile:    "lint-offline",
			RequiresProcesses: true,
			WritePaths:        []string{".coding-ethos/cache"},
		},
	})
	if err != nil {
		t.Fatalf("sandbox.BuildPlan() error = %v", err)
	}
	if plan.Executable != "/usr/bin/env" ||
		plan.Evidence.Reason != "reusing active agent-shell sandbox" {
		t.Fatalf("active agent shell sandbox was not reused: %#v", plan)
	}
	if !plan.Evidence.Enabled || !plan.Evidence.RepoReadOnly {
		t.Fatalf("active agent shell reuse lost sandbox evidence: %#v", plan.Evidence)
	}
}

func TestBuildPlanFailsClosedWithoutWrapper(t *testing.T) {
	requireLinuxSandbox(t)

	t.Setenv("CODING_ETHOS_SANDBOX_ACTIVE", "0")

	plan, err := sandbox.BuildPlan(sandbox.Request{
		Tool:         "ruff",
		Executable:   "ruff",
		Args:         []string{"check"},
		Capabilities: sandbox.Capabilities{SandboxProfile: "lint-offline"},
	})
	if !errors.Is(err, sandbox.ErrBackendUnavailable) {
		t.Fatalf("sandbox.BuildPlan() error = %v, want unavailable", err)
	}
	if !plan.Evidence.Denied {
		t.Fatalf("sandbox failure must deny execution: %#v", plan.Evidence)
	}
}

func TestBuildPlanRejectsNativeSeccompProfile(t *testing.T) {
	requireLinuxSandbox(t)

	t.Setenv("CODING_ETHOS_SANDBOX_ACTIVE", "0")

	repo := t.TempDir()
	wrapper := filepath.Join(repo, "coding-ethos-sandbox")
	writeExecutable(t, wrapper)

	plan, err := sandbox.BuildPlan(sandbox.Request{
		Tool:        "ruff",
		Executable:  toolRuffPath,
		WrapperPath: wrapper,
		Cwd:         repo,
		RepoRoot:    repo,
		Capabilities: sandbox.Capabilities{
			SandboxProfile:     "lint-offline",
			SeccompProfile:     "deny-privilege",
			SeccompProfilePath: filepath.Join(repo, "seccomp.bpf"),
		},
	})
	if !errors.Is(err, sandbox.ErrBackendUnavailable) {
		t.Fatalf("sandbox.BuildPlan() error = %v, want unavailable", err)
	}
	if !plan.Evidence.Denied ||
		plan.Evidence.Reason != "native sandbox seccomp profiles are not implemented" {
		t.Fatalf("seccomp denial evidence mismatch: %#v", plan.Evidence)
	}
}

func TestBuildPlanPreservesNetworkWhenDeclared(t *testing.T) {
	requireLinuxSandbox(t)

	t.Setenv("CODING_ETHOS_SANDBOX_ACTIVE", "0")

	repo := t.TempDir()
	wrapper := filepath.Join(repo, "coding-ethos-sandbox")
	writeExecutable(t, wrapper)

	plan, err := sandbox.BuildPlan(sandbox.Request{
		Tool:        "gemini-check",
		Executable:  "/tools/gemini",
		WrapperPath: wrapper,
		Cwd:         repo,
		RepoRoot:    repo,
		Capabilities: sandbox.Capabilities{
			SandboxProfile:  "agent-network",
			RequiresNetwork: true,
		},
	})
	if err != nil {
		t.Fatalf("sandbox.BuildPlan() error = %v", err)
	}
	if slices.Contains(plan.Args, "--network") {
		t.Fatalf("network capability is enforced by outer namespace attrs: %#v", plan.Args)
	}
	if !slices.Contains(plan.Args, "--requires-network") {
		t.Fatalf("helper not told network stays reachable: %#v", plan.Args)
	}
	if !plan.Evidence.RequiresNetwork || plan.Evidence.NetworkIsolated {
		t.Fatalf("network capability not recorded: %#v", plan.Evidence)
	}
}

func TestBuildPlanRequiresProcessesKeepsFilesystemSandboxWithoutNamespaces(
	t *testing.T,
) {
	requireLinuxSandbox(t)

	t.Setenv("CODING_ETHOS_SANDBOX_ACTIVE", "0")

	repo := t.TempDir()
	wrapper := filepath.Join(repo, "coding-ethos-sandbox")
	writeExecutable(t, wrapper)

	plan, err := sandbox.BuildPlan(sandbox.Request{
		Tool:        "go-test",
		Executable:  "/tools/go",
		WrapperPath: wrapper,
		Cwd:         repo,
		RepoRoot:    repo,
		Capabilities: sandbox.Capabilities{
			SandboxProfile:    "lint-offline",
			RequiresProcesses: true,
			WritePaths:        []string{".coding-ethos/cache"},
		},
	})
	if err != nil {
		t.Fatalf("sandbox.BuildPlan() error = %v", err)
	}

	if plan.Evidence.BackendPath != wrapper ||
		plan.Executable != wrapper ||
		!plan.Evidence.Enabled ||
		slices.Contains(plan.Args, wrapper) {
		t.Fatalf("process-preserving tool lost sandbox wrapper: %#v", plan)
	}
	if plan.Evidence.NamespaceEnforced ||
		plan.Evidence.ProcessIsolated ||
		plan.Evidence.NetworkIsolated {
		t.Fatalf(
			"process-preserving tool must not claim namespace isolation: %#v",
			plan.Evidence,
		)
	}
	if !plan.Evidence.RepoReadOnly || !plan.Evidence.GitReadOnly {
		t.Fatalf("filesystem sandbox evidence missing: %#v", plan.Evidence)
	}
}

func TestValidateNativeRuntimeWithHelperIgnoresSpoofedActiveSandboxMarker(
	t *testing.T,
) {
	requireLinuxSandbox(t)

	t.Setenv("CODING_ETHOS_SANDBOX_ACTIVE", "1")

	evidence, err := sandbox.ValidateNativeRuntimeWithHelper("/missing")
	if err == nil {
		if evidence.Reason == "" || !evidence.Enabled {
			t.Fatalf("helper validation evidence mismatch: %#v", evidence)
		}

		return
	}

	if !errors.Is(err, sandbox.ErrBackendUnavailable) {
		t.Fatalf("ValidateNativeRuntimeWithHelper() error = %v, want unavailable", err)
	}
	if !evidence.Denied {
		t.Fatalf("spoofed active marker skipped helper validation: %#v", evidence)
	}
}

func TestExecuteReturnsPlanEvidenceAndRunsCommandCallback(t *testing.T) {
	requireLinuxSandbox(t)

	t.Setenv("CODING_ETHOS_SANDBOX_ACTIVE", "0")

	repo := t.TempDir()
	wrapper := filepath.Join(repo, "coding-ethos-sandbox")
	writeExecutable(t, wrapper)

	var gotExecutable string
	var gotArgs []string
	var gotEvidence sandbox.Evidence

	evidence, err := sandbox.Execute(
		context.Background(),
		sandbox.Request{
			Tool:        "ruff",
			Executable:  toolRuffPath,
			WrapperPath: wrapper,
			Cwd:         repo,
			RepoRoot:    repo,
			Args:        []string{"check", "."},
			Capabilities: sandbox.Capabilities{
				SandboxProfile: "lint-offline",
			},
		},
		func(executable string, args []string, evidence sandbox.Evidence) error {
			gotExecutable = executable
			gotArgs = append([]string(nil), args...)
			gotEvidence = evidence

			return nil
		},
	)
	if err != nil {
		t.Fatalf("sandbox.Execute() error = %v", err)
	}
	if evidence.Mode != gotEvidence.Mode ||
		evidence.BackendPath != gotEvidence.BackendPath ||
		evidence.Tool != gotEvidence.Tool ||
		gotExecutable != wrapper {
		t.Fatalf(
			"callback did not receive planned execution: executable=%q evidence=%#v got=%#v",
			gotExecutable,
			evidence,
			gotEvidence,
		)
	}
	for _, want := range []string{toolRuffPath, "check", "."} {
		if !slices.Contains(gotArgs, want) {
			t.Fatalf("callback args missing %q: %#v", want, gotArgs)
		}
	}
}

func TestExecuteSupportsDryPlanWithoutCallback(t *testing.T) {
	t.Parallel()

	evidence, err := sandbox.Execute(context.Background(), sandbox.Request{
		Tool:       "ruff",
		Executable: toolRuffPath,
		Args:       []string{"check"},
	}, nil)
	if err != nil {
		t.Fatalf("sandbox.Execute() dry-plan error = %v", err)
	}
	if evidence.Enabled || evidence.Tool != "" || len(evidence.Command) != 0 {
		t.Fatalf("dry-plan evidence mismatch: %#v", evidence)
	}
}

func TestCommandContextAppliesTimeout(t *testing.T) {
	t.Parallel()

	ctx, cancel := sandbox.CommandContext(context.Background(), 1)
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("sandbox.CommandContext() did not set deadline")
	}
	if deadline.Before(time.Now()) {
		t.Fatalf("deadline is in the past: %v", deadline)
	}
}

func TestValidateNativeRuntimeEvidence(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "linux" {
		evidence, _ := sandbox.ValidateNativeRuntime()
		if evidence.Enabled {
			t.Fatalf("non-Linux native runtime must not be enabled: %#v", evidence)
		}

		return
	}

	evidence := requireNativeSandboxRuntime(t)
	if evidence.Backend != sandbox.BackendNative || !evidence.Enabled {
		t.Fatalf("native runtime evidence mismatch: %#v", evidence)
	}
	if !evidence.NamespaceEnforced || !evidence.ProcessIsolated ||
		!evidence.NetworkIsolated {
		t.Fatalf("Linux runtime must enforce namespaces: %#v", evidence)
	}
}

func TestValidateNativeRuntimeWithHelperRejectsNoopWrapper(t *testing.T) {
	requireLinuxSandbox(t)

	t.Setenv("CODING_ETHOS_SANDBOX_ACTIVE", "0")

	evidence, err := sandbox.ValidateNativeRuntimeWithHelper("/bin/true")
	if err == nil {
		t.Fatalf("ValidateNativeRuntimeWithHelper() error = nil evidence=%#v", evidence)
	}
	if !evidence.Denied || evidence.Reason == "" {
		t.Fatalf("helper validation evidence mismatch: %#v", evidence)
	}
}

func assertNativeEvidence(t *testing.T, evidence sandbox.Evidence) {
	t.Helper()

	if !evidence.Enabled || evidence.Backend != sandbox.BackendNative {
		t.Fatalf("evidence mismatch: %#v", evidence)
	}
	if evidence.RepoReadOnly != (runtime.GOOS == "linux") ||
		evidence.GitReadOnly != true {
		t.Fatalf("filesystem evidence mismatch: %#v", evidence)
	}
	if runtime.GOOS == "linux" && evidence.Reason == "" &&
		(!evidence.NamespaceEnforced || !evidence.ProcessIsolated) {
		t.Fatalf("namespace evidence mismatch: %#v", evidence)
	}
	if evidence.Reason == "" && !evidence.NetworkIsolated {
		t.Fatalf("network isolation missing: %#v", evidence)
	}
	if slices.Contains(evidence.WritePaths, ".git/config") {
		t.Fatalf(".git write evidence must be filtered: %#v", evidence)
	}
}

func requireLinuxSandbox(t *testing.T) {
	t.Helper()

	if runtime.GOOS != "linux" {
		t.Skip("native sandbox planning is Linux-only")
	}
}

func requireNativeSandboxRuntime(t *testing.T) sandbox.Evidence {
	t.Helper()
	requireLinuxSandbox(t)

	evidence, err := sandbox.ValidateNativeRuntime()
	if err != nil {
		t.Skipf(
			"native sandbox runtime is unavailable in this test process: %v evidence=%#v",
			err,
			evidence,
		)
	}

	return evidence
}

func countValues(values []string, target string) int {
	count := 0
	for _, value := range values {
		if value == target {
			count++
		}
	}

	return count
}

func writeExecutable(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), privateDirMode); err != nil {
		t.Fatalf("create directory tree %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatalf("create directory tree %s: %v", path, err)
	}
}
