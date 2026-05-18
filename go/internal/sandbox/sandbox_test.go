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

func TestBuildPlanOffReturnsOriginalCommand(t *testing.T) {
	t.Parallel()

	plan, err := sandbox.BuildPlan(sandbox.Request{
		Mode:         sandbox.ModeOff,
		Tool:         "ruff",
		Executable:   toolRuffPath,
		Args:         []string{"check", "pkg"},
		Capabilities: sandbox.Capabilities{SandboxProfile: "lint-offline"},
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
	t.Parallel()

	repo := t.TempDir()
	wrapper := filepath.Join(repo, "bin", "coding-ethos-sandbox")
	writeExecutable(t, wrapper)

	plan, err := sandbox.BuildPlan(sandbox.Request{
		Mode:        sandbox.ModeRequired,
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

	if plan.Executable != wrapper {
		t.Fatalf("executable = %q", plan.Executable)
	}
	for _, want := range []string{
		"--cwd", repo,
		"--repo-root", repo,
		"--read-path", "pkg",
		"--write-path", ".coding-ethos/cache",
		"--", toolRuffPath,
		"check", "pkg",
	} {
		if !slices.Contains(plan.Args, want) {
			t.Fatalf("args missing %q: %#v", want, plan.Args)
		}
	}
	if slices.Contains(plan.Args, ".git/config") {
		t.Fatalf(".git write path must not be passed to sandbox helper: %#v", plan.Args)
	}
	assertNativeEvidence(t, plan.Evidence)
}

func TestBuildPlanAutoStillFailsClosedWithoutWrapper(t *testing.T) {
	t.Parallel()

	plan, err := sandbox.BuildPlan(sandbox.Request{
		Mode:         sandbox.ModeAuto,
		Tool:         "ruff",
		Executable:   "ruff",
		Args:         []string{"check"},
		Capabilities: sandbox.Capabilities{SandboxProfile: "lint-offline"},
	})
	if !errors.Is(err, sandbox.ErrBackendUnavailable) {
		t.Fatalf("sandbox.BuildPlan() error = %v, want unavailable", err)
	}
	if !plan.Evidence.Denied {
		t.Fatalf("auto sandbox failure must deny execution: %#v", plan.Evidence)
	}
}

func TestBuildPlanRejectsNativeSeccompProfile(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	wrapper := filepath.Join(repo, "coding-ethos-sandbox")
	writeExecutable(t, wrapper)

	plan, err := sandbox.BuildPlan(sandbox.Request{
		Mode:        sandbox.ModeRequired,
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
	t.Parallel()

	repo := t.TempDir()
	wrapper := filepath.Join(repo, "coding-ethos-sandbox")
	writeExecutable(t, wrapper)

	plan, err := sandbox.BuildPlan(sandbox.Request{
		Mode:        sandbox.ModeRequired,
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
	if slices.Contains(plan.Args, "--network") != true {
		t.Fatalf("network-capable tool should pass --network: %#v", plan.Args)
	}
	if !plan.Evidence.RequiresNetwork || plan.Evidence.NetworkIsolated {
		t.Fatalf("network capability not recorded: %#v", plan.Evidence)
	}
}

func TestExecuteReturnsPlanEvidenceAndRunsCommandCallback(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	wrapper := filepath.Join(repo, "coding-ethos-sandbox")
	writeExecutable(t, wrapper)

	var gotExecutable string
	var gotArgs []string
	var gotEvidence sandbox.Evidence

	evidence, err := sandbox.Execute(
		context.Background(),
		sandbox.Request{
			Mode:        sandbox.ModeRequired,
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
		Mode:       sandbox.ModeOff,
		Tool:       "ruff",
		Executable: toolRuffPath,
		Args:       []string{"check"},
	}, nil)
	if err != nil {
		t.Fatalf("sandbox.Execute() dry-plan error = %v", err)
	}
	if evidence.Enabled || evidence.Tool != "ruff" ||
		!slices.Equal(evidence.Command, []string{toolRuffPath, "check"}) {
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

	evidence, err := sandbox.ValidateNativeRuntime()
	if runtime.GOOS == "linux" && err != nil {
		t.Fatalf("ValidateNativeRuntime() error = %v evidence=%#v", err, evidence)
	}
	if evidence.Backend != sandbox.BackendNative || !evidence.Enabled {
		t.Fatalf("native runtime evidence mismatch: %#v", evidence)
	}
	if runtime.GOOS == "linux" && !evidence.NamespaceEnforced {
		t.Fatalf("Linux runtime must enforce namespaces: %#v", evidence)
	}
}

func TestValidateNativeRuntimeWithHelperUsesWrapper(t *testing.T) {
	t.Parallel()

	evidence, err := sandbox.ValidateNativeRuntimeWithHelper("/bin/true")
	if err != nil {
		t.Fatalf("ValidateNativeRuntimeWithHelper() error = %v evidence=%#v", err, evidence)
	}
	if evidence.Backend != sandbox.BackendNative || !evidence.Enabled {
		t.Fatalf("helper validation evidence mismatch: %#v", evidence)
	}
}

func assertNativeEvidence(t *testing.T, evidence sandbox.Evidence) {
	t.Helper()

	if !evidence.Enabled || evidence.Backend != sandbox.BackendNative {
		t.Fatalf("evidence mismatch: %#v", evidence)
	}
	if evidence.ReadOnlyRoot != (runtime.GOOS == "linux") ||
		evidence.GitReadOnly != true {
		t.Fatalf("filesystem evidence mismatch: %#v", evidence)
	}
	if evidence.NamespaceEnforced != (runtime.GOOS == "linux") ||
		evidence.ProcessIsolated != (runtime.GOOS == "linux") {
		t.Fatalf("namespace evidence mismatch: %#v", evidence)
	}
	if !evidence.NetworkIsolated {
		t.Fatalf("network isolation missing: %#v", evidence)
	}
	if slices.Contains(evidence.WritePaths, ".git/config") {
		t.Fatalf(".git write evidence must be filtered: %#v", evidence)
	}
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
